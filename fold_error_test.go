package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 折叠路径必须认出上游的错误帧，而不是把它折进回答里。
//
// 上游把限流写在 HTTP 200 的流里。折叠时若只收集 payload，错误文本会被当成
// 模型的回答，而请求本身【没有报告任何失败】——CPA 拿到一个「成功但内容是错误
// 信息」的响应，既无法分类也无法冷却，最终把一次模型级限流描述成账号不可用。
func TestFoldReportsStreamErrorFrame(t *testing.T) {
	resetState()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 第一次：非流式请求被拒（11101），触发折叠路径。
		if !requestWantsStream(r) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"code":11101,"msg":"Non-stream chat request is currently not supported"}`))
			return
		}
		// 第二次：流式请求返回限流错误帧。
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`data: {"code":11148,"msg":"您的使用量已超出频率限制，将在 2026-09-27 20:03:46 UTC+8 重置，您也可以切换其他模型继续使用。"}` + "\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()
	defer pointWorkBuddyAt(server.URL)()

	out, errExecute := executorExecute(buildNonStreamExecutorRequestForUID(t, "u-fold"))
	if errExecute != nil {
		t.Fatal(errExecute)
	}

	var env struct {
		OK    bool `json:"ok"`
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if errUnmarshal := json.Unmarshal(out, &env); errUnmarshal != nil {
		t.Fatalf("输出不是合法信封: %v\n%s", errUnmarshal, out)
	}
	if env.OK {
		t.Fatalf("上游的限流错误帧必须作为失败上报，而不是折成回答；out=%s", out)
	}
	if env.Error == nil || env.Error.Message == "" {
		t.Fatalf("错误内容不能为空；out=%s", out)
	}
}

// 折叠出的错误文案要能被分类成限流，从而让 CPA 走「模型冷却」而不是兜底。
//
// 状态码写死 502 会丢掉唯一可用的区分依据：429 才是 CPA 认的限流信号。
func TestFrameFailureStatusIsClassified(t *testing.T) {
	cases := []struct {
		name    string
		message string
		want    int
	}{
		{"中文限流", "您的使用量已超出频率限制，将在 2026-09-27 20:03:46 UTC+8 重置，您也可以切换其他模型继续使用。", http.StatusTooManyRequests},
		{"英文限流", "rate limit exceeded, please retry later", http.StatusTooManyRequests},
		{"额度耗尽", "积分不足，请充值", http.StatusPaymentRequired},
		{"请求内容问题", "tool calls and tool results do not match, please start a new conversation and retry", http.StatusBadGateway},
	}
	for _, testCase := range cases {
		if got := statusCodeForFrameFailure(testCase.message); got != testCase.want {
			t.Errorf("%s: statusCodeForFrameFailure(%q) = %d, want %d",
				testCase.name, testCase.message, got, testCase.want)
		}
	}
}

// 折叠时遇到裸 {"code":…,"msg":…} 错误帧也要识别。
func TestAggregateDetectsFlatErrorFrame(t *testing.T) {
	frames := [][]byte{
		[]byte(`{"code":11148,"msg":"tool calls and tool results do not match"}`),
	}
	out := aggregateStreamToCompletion(frames, "m")
	var doc struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if errUnmarshal := json.Unmarshal(out, &doc); errUnmarshal != nil {
		t.Fatalf("输出不是合法 JSON: %v\n%s", errUnmarshal, out)
	}
	if doc.Error == nil || doc.Error.Message == "" {
		t.Errorf("扁平错误帧应被折叠为错误响应；out=%s", out)
	}
}

// 正常内容帧不能被误判成错误。
func TestAggregateKeepsNormalFrames(t *testing.T) {
	frames := [][]byte{
		[]byte(`{"id":"c1","choices":[{"index":0,"delta":{"content":"hello"}}]}`),
		[]byte(`{"id":"c1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`),
	}
	out := aggregateStreamToCompletion(frames, "m")
	var doc struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error any `json:"error"`
	}
	if errUnmarshal := json.Unmarshal(out, &doc); errUnmarshal != nil {
		t.Fatal(errUnmarshal)
	}
	if doc.Error != nil {
		t.Errorf("正常帧被误判为错误：%s", out)
	}
	if len(doc.Choices) != 1 || doc.Choices[0].Message.Content != "hello" {
		t.Errorf("正常内容丢失：%s", out)
	}
}

// requestWantsStream reports whether the request body asks for streaming.
func requestWantsStream(r *http.Request) bool {
	buffer := make([]byte, r.ContentLength)
	_, _ = r.Body.Read(buffer)
	return containsRawModel(buffer, `"stream":true`)
}
