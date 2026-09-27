package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// 普通 assistant 回复不得清空待配对的调用列表。
//
// linkToolResultsToCalls 用 pending 记录「已声明但尚未收到结果」的 tool_call id。
// 它把每个 assistant 都当成新一批调用：只要遇到 assistant 就整体替换 pending、
// 清空 answered。但对话里存在不含 tool_calls 的普通 assistant 回复，多轮工具使用
// 时它出现在两批调用之间——

//   assistant(tool_calls: c1)   ← pending = [c1]
//   tool(c1)                    ← 已回答
//   assistant("好的，我继续")     ← 普通回复，没有 tool_calls
//   assistant(tool_calls: c2)   ← pending = [c2]

// 判断本身没有错，错在中间那条。它会让后续所有配对判断都基于一个错误的「当前批次」。
// 这里锁定：普通 assistant 不改变待配对集合。
func TestLinkKeepsPendingAcrossPlainAssistantReply(t *testing.T) {
	body := []byte(`{
		"model": "m",
		"messages": [
			{"role": "user", "content": "go"},
			{"role": "assistant", "content": "", "tool_calls": [
				{"id": "c1", "type": "function", "function": {"name": "a", "arguments": "{}"}}
			]},
			{"role": "tool", "tool_call_id": "c1", "content": "r1"},
			{"role": "assistant", "content": "继续"},
			{"role": "user", "content": "再来"},
			{"role": "assistant", "content": "", "tool_calls": [
				{"id": "c2", "type": "function", "function": {"name": "b", "arguments": "{}"}}
			]},
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

	// 每条 tool_call 都必须仍然被它的结果回应，且结果没有被删掉。
	calls := map[string]bool{}
	results := map[string]bool{}
	for _, message := range doc.Messages {
		switch string(message["role"]) {
		case `"assistant"`:
			for _, id := range assistantToolCallIDList(message) {
				calls[id] = true
			}
		case `"tool"`:
			var id string
			_ = json.Unmarshal(message["tool_call_id"], &id)
			results[id] = true
		}
	}
	if !calls["c1"] || !calls["c2"] {
		t.Fatalf("tool_call 被删除：calls=%v\n%s", calls, out)
	}
	if !results["c1"] || !results["c2"] {
		t.Fatalf("tool 结果被删除（配对失衡）：results=%v\n%s", results, out)
	}
	if !strings.Contains(string(out), `"继续"`) {
		t.Errorf("普通 assistant 回复被丢弃：%s", out)
	}
}
