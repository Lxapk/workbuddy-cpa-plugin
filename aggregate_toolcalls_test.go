package main

import (
	"encoding/json"
	"testing"
)

// 流式 tool_calls 是分片到达的，折叠时必须按 index 累积、arguments 拼接。
//
// 真实分片形状：
//
//	{"index":0,"id":"call_x","function":{"name":"get_weather","arguments":""}}
//	{"index":0,"function":{"arguments":"{\"city\""}}
//	{"index":0,"function":{"arguments":":\"Hangzhou\"}"}}
//
// 直接覆盖（只留最后一帧）会丢掉 id、函数名和大部分 arguments，客户端拿到的
// tool_calls 无法使用。
func TestAggregateAccumulatesStreamedToolCallArguments(t *testing.T) {
	frames := [][]byte{
		[]byte(`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_x","type":"function","function":{"name":"get_weather","arguments":""}}]}}]}`),
		[]byte(`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"city\""}}]}}]}`),
		[]byte(`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":":\"Hangzhou\"}"}}]}}]}`),
		[]byte(`{"id":"c1","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`),
	}

	out := aggregateStreamToCompletion(frames, "m")

	var doc struct {
		Choices []struct {
			Message struct {
				ToolCalls []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if errUnmarshal := json.Unmarshal(out, &doc); errUnmarshal != nil {
		t.Fatalf("输出不是合法 JSON: %v\n%s", errUnmarshal, out)
	}
	if len(doc.Choices) != 1 {
		t.Fatalf("choices = %d, want 1", len(doc.Choices))
	}
	calls := doc.Choices[0].Message.ToolCalls
	if len(calls) != 1 {
		t.Fatalf("tool_calls = %d, want 1\n%s", len(calls), out)
	}
	if calls[0].ID != "call_x" {
		t.Errorf("id = %q, want call_x（首帧的 id 不能被后续帧覆盖掉）", calls[0].ID)
	}
	if calls[0].Function.Name != "get_weather" {
		t.Errorf("name = %q, want get_weather", calls[0].Function.Name)
	}
	want := `{"city":"Hangzhou"}`
	if calls[0].Function.Arguments != want {
		t.Errorf("arguments = %q, want %q（分片必须按顺序拼接）", calls[0].Function.Arguments, want)
	}
	if doc.Choices[0].FinishReason != "tool_calls" {
		t.Errorf("finish_reason = %q, want tool_calls", doc.Choices[0].FinishReason)
	}
}

// 多个并行调用按 index 各自累积，不能互相覆盖。
func TestAggregateKeepsParallelToolCallsSeparate(t *testing.T) {
	frames := [][]byte{
		[]byte(`{"id":"c1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"f","arguments":"{\"x\":1}"}}]}}]}`),
		[]byte(`{"id":"c1","choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"call_b","type":"function","function":{"name":"g","arguments":"{\"y\":2}"}}]}}]}`),
		[]byte(`{"id":"c1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":""}}]},"finish_reason":"tool_calls"}]}`),
	}

	out := aggregateStreamToCompletion(frames, "m")

	var doc struct {
		Choices []struct {
			Message struct {
				ToolCalls []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if errUnmarshal := json.Unmarshal(out, &doc); errUnmarshal != nil {
		t.Fatal(errUnmarshal)
	}
	calls := doc.Choices[0].Message.ToolCalls
	if len(calls) != 2 {
		t.Fatalf("tool_calls = %d, want 2\n%s", len(calls), out)
	}
	if calls[0].ID != "call_a" || calls[1].ID != "call_b" {
		t.Errorf("ids = [%s %s], want [call_a call_b]（按 index 各自成槽）", calls[0].ID, calls[1].ID)
	}
	if calls[0].Function.Arguments != `{"x":1}` {
		t.Errorf("call_a arguments = %q", calls[0].Function.Arguments)
	}
	if calls[1].Function.Arguments != `{"y":2}` {
		t.Errorf("call_b arguments = %q", calls[1].Function.Arguments)
	}
}

// 没有 id 的调用不应被输出：客户端无法为它回填 tool 结果。
func TestAggregateDropsToolCallWithoutID(t *testing.T) {
	frames := [][]byte{
		[]byte(`{"id":"c1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"type":"function","function":{"name":"f","arguments":"{}"}}]}}]}`),
		[]byte(`{"id":"c1","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`),
	}
	out := aggregateStreamToCompletion(frames, "m")
	var doc struct {
		Choices []struct {
			Message map[string]json.RawMessage `json:"message"`
		} `json:"choices"`
	}
	if errUnmarshal := json.Unmarshal(out, &doc); errUnmarshal != nil {
		t.Fatal(errUnmarshal)
	}
	if _, has := doc.Choices[0].Message["tool_calls"]; has {
		t.Errorf("无 id 的调用应被丢弃，而不是输出一个客户端无法回填的结果\n%s", out)
	}
}
