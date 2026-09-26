package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// 非流式请求返回 11101 时，插件会折叠成流式并成功作答。
//
// 这条路径必须【在失败上报之前】判定：11101 描述的是端点支持什么，不是凭据的
// 状态，而下面的折叠会把它变成一个成功的响应。先上报就把一次成功记成了失败，
// 三次之后账号被停用，之后所有请求都在拿这个已停的账号重试，客户端看到的是
// 那个陈旧的第一条错误 —— 正是 "no auth available" 的来源。
func TestNonStreamUnsupportedIsNotCountedAsFailure(t *testing.T) {
	resetState()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		// 先按非流式请求返回 11101；被强制转流式后正常作答。
		if !containsRawModel(body, `"stream":true`) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"code":11101,"msg":"Non-stream chat request is currently not supported"}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"id\":\"c1\",\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()
	defer pointWorkBuddyAt(server.URL)()

	uid := "u-11101"
	resp, errCall := executorExecute(buildNonStreamExecutorRequestForUID(t, uid))
	if errCall != nil {
		t.Fatal(errCall)
	}

	// 折叠成功：信封 ok，且带回内容。
	var env struct {
		OK     bool `json:"ok"`
		Result struct {
			// Payload 是 []byte，编码为 base64；解码后应是完整的 chat.completion。
			Payload []byte `json:"Payload"`
			Meta    struct {
				Aggregated bool `json:"aggregated"`
			} `json:"Metadata"`
		} `json:"result"`
	}
	mustDecode(t, resp, &env)
	if !env.OK {
		t.Fatalf("11101 应被折叠成成功响应；resp=%s", resp)
	}
	if !env.Result.Meta.Aggregated {
		t.Error("应标记 aggregated=true（说明走了折叠路径）")
	}
	if !strings.Contains(string(env.Result.Payload), `"content":"ok"`) {
		t.Errorf("折叠后的响应应含正文；payload=%s", env.Result.Payload)
	}

	// 账号不应因此被记账。
	lane := state.pool.lanes[laneKey("codebuddy", uid)]
	if lane != nil {
		if lane.ConsecutiveErrors != 0 {
			t.Errorf("ConsecutiveErrors = %d，11101 是端点能力问题，不该累积失败",
				lane.ConsecutiveErrors)
		}
		if !lane.CooldownUntil.IsZero() && time.Now().Before(lane.CooldownUntil) {
			t.Errorf("账号被冷却了（until=%v）", lane.CooldownUntil)
		}
		if len(lane.ModelCooldowns) != 0 {
			t.Errorf("模型被冷却了：%v", lane.ModelCooldowns)
		}
	}
}

// 11101 的文案也要被 isRequestContentFailure 认出来（双保险：即使顺序再被改动，
// 也不会把它算成账号故障）。
func TestNonStreamUnsupportedIsRequestContentFailure(t *testing.T) {
	body := []byte(`{"code":11101,"msg":"Non-stream chat request is currently not supported"}`)
	upErr := classifyUpstream(http.StatusBadRequest, body)
	if !isRequestContentFailure(upErr.Message) {
		t.Errorf("11101 应被识别为请求内容问题；Message=%q", upErr.Message)
	}
}

// containsRawModel 判断 body 里是否含某个字面量（用于区分流式/非流式请求）。
func containsRawModel(body []byte, want string) bool {
	return strings.Contains(string(body), want)
}

// buildNonStreamExecutorRequestForUID 构造非流式的 executor 调用体。
func buildNonStreamExecutorRequestForUID(t *testing.T, uid string) []byte {
	t.Helper()
	storage, _ := json.Marshal(map[string]any{
		"accessToken": "[REDACTED]",
		"domain":      "cn",
		"uid":         uid,
	})
	payload, _ := json.Marshal(map[string]any{
		"OriginalRequest": []byte(`{"model":"deepseek-v4.1-flash","stream":false,"messages":[]}`),
		"StorageJSON":     storage,
		"Stream":          false,
	})
	return payload
}
