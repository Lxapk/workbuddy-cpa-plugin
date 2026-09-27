package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// 上游的错误帧不止一种形状，全部都要认出来。
//
// 漏认的后果不是「少报一个错」，而是这帧被当成正常内容转发：客户端看到错误文本
// 被当作模型的回答，而真正的失败在稍后才以「上游未返回任何内容」的形式冒出来，
// 把线索指向完全错误的方向。
//
// 供应商自己的风格是 {"code":11148,"msg":…}（没有 error 包装），OpenAI 风格是
// {"error":{"message":…}}。两者都出现在同一套接口上。
func TestExtractStreamErrorRecognisesAllUpstreamShapes(t *testing.T) {
	cases := []struct {
		name  string
		frame string
		want  string
	}{
		{"OpenAI 风格", `data: {"error":{"message":"boom"}}`, "boom"},
		{"供应商扁平 code/msg", `data: {"code":11148,"msg":"boom"}`, "boom"},
		{"扁平 code/message", `data: {"code":11148,"message":"boom"}`, "boom"},
		{"error 内用 msg", `data: {"error":{"code":11148,"msg":"boom"}}`, "boom"},
		{"顶层 message", `data: {"message":"boom"}`, "boom"},
		{"顶层 error_msg", `data: {"error_msg":"boom"}`, "boom"},
		{"response 包裹", `data: {"response":{"error":{"message":"boom"}}}`, "boom"},
		{"裸 JSON（无 data: 前缀）", `{"error":{"message":"boom"}}`, "boom"},
		{"裸 JSON code/msg", `{"code":11148,"msg":"boom"}`, "boom"},
	}
	for _, testCase := range cases {
		if got := extractStreamError([]byte(testCase.frame)); got != testCase.want {
			t.Errorf("%s: extractStreamError(%s) = %q, want %q",
				testCase.name, testCase.frame, got, testCase.want)
		}
	}
}

// 正常的内容帧与结束哨兵绝不能被误判成错误。
//
// 误判会把一次成功的回答变成失败，比漏判更难排查。
func TestExtractStreamErrorIgnoresNormalFrames(t *testing.T) {
	frames := []string{
		`data: {"choices":[{"delta":{"content":"hello"}}]}`,
		`data: {"choices":[{"delta":{"reasoning_content":"thinking"}}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`,
		`data: {"id":"c1","usage":{"total_tokens":10}}`,
		`data: [DONE]`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_x"}]}}]}`,
		`: keep-alive`,
		``,
	}
	for _, frame := range frames {
		if got := extractStreamError([]byte(frame)); got != "" {
			t.Errorf("正常帧被误判为错误: %s -> %q", frame, got)
		}
	}
}

// 上游用 HTTP 200 + 错误帧报告「tool 对不匹配」时，必须在第一帧就识别出来。
//
// 这是导致账号被误停的路径之一：识别不出来 → 帧被当内容 → 稍后以
// 「上游未返回任何内容」收尾 → 错误信息把线索指向错误的方向。
func TestToolMismatchInsideStreamIsDetected(t *testing.T) {
	frame := []byte(`data: {"code":11148,"msg":"tool calls and tool results do not match, please start a new conversation and retry"}`)
	got := extractStreamError(frame)
	if got == "" {
		t.Fatal("帧内 tool 不匹配错误未被识别")
	}
	if !isRequestContentFailure(got) {
		t.Errorf("应被判定为请求内容问题（不该停号）；got=%q", got)
	}
}

// 上游用 HTTP 200 + {"code":11148,"msg":…} 报告 tool 不匹配。
//
// 修复前：extractStreamError 只认 error.message，这帧识别不到 → 被当内容转发
// → 以「上游未返回任何内容」收尾，线索指向错误的方向。
// 修复后：第一帧就识别出错误，并按请求内容问题处理（不停号）。
func TestStreamToolMismatchFromCodeMsgFrame(t *testing.T) {
	resetState()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`data: {"code":11148,"msg":"tool calls and tool results do not match, please start a new conversation and retry"}` + "\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()
	defer pointWorkBuddyAt(server.URL)()

	uid := "u-code11148"
	executorExecuteStream(buildStreamExecutorRequestForUID(t, "s11", uid))

	// 等异步收尾
	time.Sleep(1200 * time.Millisecond)

	lane := state.pool.lanes[laneKey("codebuddy", uid)]
	if lane == nil {
		return // 没有记录 = 没算作失败，可接受
	}
	if lane.ConsecutiveErrors != 0 {
		t.Errorf("ConsecutiveErrors = %d；tool 不匹配是请求内容问题，不该累积", lane.ConsecutiveErrors)
	}
	if len(lane.ModelCooldowns) != 0 {
		t.Errorf("模型被冷却了：%v", lane.ModelCooldowns)
	}
	if !lane.CooldownUntil.IsZero() && time.Now().Before(lane.CooldownUntil) {
		t.Errorf("账号被冷却了：%v", lane.CooldownUntil)
	}
}
