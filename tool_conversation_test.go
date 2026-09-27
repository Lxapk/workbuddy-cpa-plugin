package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// 一个已经正确配对的 tool 会话，规范化后必须原样保留。
//
// 上游用 "tool calls and tool results do not match" 拒绝请求，而这一版新增的
// 规范化会重写 tool_call 的 id、重新打包 tool 结果、并删掉它认为未配对的消息。
// 三者都作用于宿主已经正确构造的请求体：一旦判断有误，一个本来合法的会话就被
// 改坏，三个账号会同时报同一个错——这正是用户看到的现象，而引入这些改写的版本
// 之前是正常的。
//
// 所以这里锁定的是：规范化不得改动一个已经合法的请求。
func TestNormaliseLeavesAWellFormedToolConversationAlone(t *testing.T) {
	body := []byte(`{
		"model": "deepseek-v4.1-flash",
		"messages": [
			{"role": "system", "content": "you are helpful"},
			{"role": "user", "content": "read the file"},
			{"role": "assistant", "content": "", "tool_calls": [
				{"id": "call_abc", "type": "function", "function": {"name": "read", "arguments": "{\"p\":\"a\"}"}}
			]},
			{"role": "tool", "tool_call_id": "call_abc", "content": "file contents"},
			{"role": "user", "content": "thanks"}
		],
		"max_tokens": 32
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

	if len(doc.Messages) != 5 {
		// One extra leading system message is expected; anything else means
		// content added or removed.
		nonSystem := 0
		for _, message := range doc.Messages {
			if string(message["role"]) != `"system"` {
				nonSystem++
			}
		}
		if nonSystem != 5 {
			t.Fatalf("消息数被改变：%d 条非 system, want 5\n%s", nonSystem, out)
		}
	}

	// tool 结果必须仍然存在，并且仍然指向那条 tool_call。
	var sawCallID, sawResultID string
	for _, message := range doc.Messages {
		if string(message["role"]) == `"assistant"` {
			var calls []struct {
				ID string `json:"id"`
			}
			_ = json.Unmarshal(message["tool_calls"], &calls)
			if len(calls) > 0 {
				sawCallID = calls[0].ID
			}
		}
		if string(message["role"]) == `"tool"` {
			_ = json.Unmarshal(message["tool_call_id"], &sawResultID)
		}
	}
	if sawCallID == "" || sawResultID == "" {
		t.Fatalf("tool_call 或 tool 结果被删除；out=%s", out)
	}
	if sawCallID != sawResultID {
		t.Errorf("配对被改坏：call=%q result=%q\n%s", sawCallID, sawResultID, out)
	}
	if sawCallID != "call_abc" {
		t.Errorf("id 被改写：%q, want %q", sawCallID, "call_abc")
	}
}

// 多个 tool_call 与多个结果，顺序正确时不得被改动或删除。
func TestNormaliseKeepsParallelToolCallsIntact(t *testing.T) {
	body := []byte(`{
		"model": "m",
		"messages": [
			{"role": "user", "content": "go"},
			{"role": "assistant", "content": "", "tool_calls": [
				{"id": "c1", "type": "function", "function": {"name": "a", "arguments": "{}"}},
				{"id": "c2", "type": "function", "function": {"name": "b", "arguments": "{}"}}
			]},
			{"role": "tool", "tool_call_id": "c1", "content": "r1"},
			{"role": "tool", "tool_call_id": "c2", "content": "r2"}
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
	if len(doc.Messages) != 4 {
		// One extra leading system message is expected (the normaliser supplies a
		// default prompt); anything else means content was added or dropped.
		nonSystem := 0
		for _, message := range doc.Messages {
			if string(message["role"]) != `"system"` {
				nonSystem++
			}
		}
		if nonSystem != 4 {
			t.Fatalf("消息数被改变：%d 条非 system, want 4\n%s", nonSystem, out)
		}
	}

	ids := map[string]bool{}
	for _, message := range doc.Messages {
		if string(message["role"]) == `"tool"` {
			var id string
			_ = json.Unmarshal(message["tool_call_id"], &id)
			ids[id] = true
		}
	}
	if !ids["c1"] || !ids["c2"] {
		t.Fatalf("并行 tool 结果被改动：%v\n%s", ids, out)
	}
}

// 已经合法的纯文本会话不得被插入或删除消息。
func TestNormaliseLeavesPlainConversationAlone(t *testing.T) {
	body := []byte(`{
		"model": "m",
		"messages": [
			{"role": "system", "content": "s"},
			{"role": "user", "content": "hello"},
			{"role": "assistant", "content": "hi"},
			{"role": "user", "content": "again"}
		]
	}`)

	out, errNormalise := normaliseUpstreamBody(body)
	if errNormalise != nil {
		t.Fatal(errNormalise)
	}
	var doc struct {
		Messages []map[string]json.RawMessage `json:"messages"`
	}
	if errUnmarshal := json.Unmarshal(out, &doc); errUnmarshal != nil {
		t.Fatal(errUnmarshal)
	}
	if len(doc.Messages) != 4 {
		t.Fatalf("纯文本会话被改动：%d, want 4\n%s", len(doc.Messages), out)
	}
	if !strings.Contains(string(out), `"hello"`) || !strings.Contains(string(out), `"again"`) {
		t.Errorf("内容丢失：%s", out)
	}
}
