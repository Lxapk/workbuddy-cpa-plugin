package main

import (
	"encoding/json"
	"testing"
)

// 多轮工具调用：第二批 assistant(tool_calls) 夹在第一批结果之后，是合法的。
//
// 上游用 "tool calls and tool results do not match" 拒绝请求。规范化里有一趟会把
// 「夹在 tool_calls 与结果之间的消息」移到结果之后；多轮工具调用恰好有这样的
// 形状——第一批结果之后紧跟着第二批的 tool_calls——一旦被移动，每条结果就直接
// 贴在错误的 tool_calls 后面，上游于是认为配对不上。
//
// 这里锁定：一个已经合法的多轮工具会话必须原样通过规范化。
func TestNormaliseKeepsMultiRoundToolConversation(t *testing.T) {
	body := []byte(`{
		"model": "deepseek-v4.1-flash",
		"messages": [
			{"role": "user", "content": "start"},
			{"role": "assistant", "content": "", "tool_calls": [
				{"id": "c1", "type": "function", "function": {"name": "a", "arguments": "{}"}}
			]},
			{"role": "tool", "tool_call_id": "c1", "content": "r1"},
			{"role": "assistant", "content": "", "tool_calls": [
				{"id": "c2", "type": "function", "function": {"name": "b", "arguments": "{}"}}
			]},
			{"role": "tool", "tool_call_id": "c2", "content": "r2"},
			{"role": "assistant", "content": "done"}
		]
	}`)

	out, errNormalise := normaliseUpstreamBody(body)
	if errNormalise != nil {
		t.Fatalf("规范化不应报错: %v", errNormalise)
	}

	var doc struct {
		Messages []map[string]json.RawMessage `json:"messages"`
	}
	if errUnmarshal := json.Unmarshal(out, &doc); errUnmarshal != nil {
		t.Fatalf("输出不是合法 JSON: %v", errUnmarshal)
	}

	// 顺序必须保持：每条 tool 结果都紧跟在它对应的 tool_calls 之后。
	// 用出现次序而不是位置索引来断言，这样即使插入了 system 也不会误报。
	var order []string
	for _, message := range doc.Messages {
		switch string(message["role"]) {
		case `"user"`:
			order = append(order, "user")
		case `"assistant"`:
			if calls := assistantToolCallIDList(message); len(calls) > 0 {
				order = append(order, "call:"+calls[0])
			} else {
				order = append(order, "assistant")
			}
		case `"tool"`:
			var id string
			_ = json.Unmarshal(message["tool_call_id"], &id)
			order = append(order, "result:"+id)
		}
	}

	want := []string{"user", "call:c1", "result:c1", "call:c2", "result:c2", "assistant"}
	if len(order) != len(want) {
		t.Fatalf("消息序列被改变：%v, want %v\n%s", order, want, out)
	}
	for index := range want {
		if order[index] != want[index] {
			t.Fatalf("顺序被改坏（第 %d 项）：%v\nwant %v\n%s", index, order, want, out)
		}
	}
}

// assistantToolCallIDList reads the tool_call ids from an assistant message.
func assistantToolCallIDList(message map[string]json.RawMessage) []string {
	raw, okCalls := message["tool_calls"]
	if !okCalls {
		return nil
	}
	var calls []struct {
		ID string `json:"id"`
	}
	if errUnmarshal := json.Unmarshal(raw, &calls); errUnmarshal != nil {
		return nil
	}
	out := make([]string, 0, len(calls))
	for _, call := range calls {
		if call.ID != "" {
			out = append(out, call.ID)
		}
	}
	return out
}
