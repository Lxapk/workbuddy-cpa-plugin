package main

import (
	"encoding/json"
	"testing"
)

// 工具结果之后紧跟一条普通 assistant 回复，是对话最常见的收尾。
//
// 上游以 "tool calls and tool results do not match" 拒绝请求，而规范化里有一趟会
// 重排「夹在 tool_calls 与结果之间」的消息。批次的边界判断若把紧随其后的普通
// assistant 也卷进批次，那条回复就会被移到结果之后，配对关系随之错位。
//
// 这里覆盖几种真实会话的收尾形态：结果后接普通回复、回复后用户继续追问、
// 以及结果直接结束会话。
func TestNormaliseKeepsConversationsThatEndAfterToolResults(t *testing.T) {
	cases := []struct {
		name     string
		messages string
		wantRole []string
	}{
		{
			name: "结果后接普通回复",
			messages: `[
				{"role":"user","content":"read it"},
				{"role":"assistant","content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"read","arguments":"{}"}}]},
				{"role":"tool","tool_call_id":"c1","content":"data"},
				{"role":"assistant","content":"the file says hi"}
			]`,
			wantRole: []string{"user", "assistant", "tool", "assistant"},
		},
		{
			name: "结果后用户继续追问",
			messages: `[
				{"role":"user","content":"read it"},
				{"role":"assistant","content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"read","arguments":"{}"}}]},
				{"role":"tool","tool_call_id":"c1","content":"data"},
				{"role":"user","content":"now what"}
			]`,
			wantRole: []string{"user", "assistant", "tool", "user"},
		},
		{
			name: "结果直接结束",
			messages: `[
				{"role":"user","content":"read it"},
				{"role":"assistant","content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"read","arguments":"{}"}}]},
				{"role":"tool","tool_call_id":"c1","content":"data"}
			]`,
			wantRole: []string{"user", "assistant", "tool"},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			body := []byte(`{"model":"m","messages":` + testCase.messages + `}`)
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

			// 忽略规范化补上的 system 消息，只比较对话本身。
			gotRole := make([]string, 0, len(doc.Messages))
			for _, message := range doc.Messages {
				role := string(message["role"])
				if role == `"system"` {
					continue
				}
				gotRole = append(gotRole, trimQuotes(role))
			}
			if len(gotRole) != len(testCase.wantRole) {
				t.Fatalf("消息序列被改变：%v, want %v\n%s", gotRole, testCase.wantRole, out)
			}
			for index := range testCase.wantRole {
				if gotRole[index] != testCase.wantRole[index] {
					t.Fatalf("顺序被改坏（第 %d 项）：%v\nwant %v\n%s",
						index, gotRole, testCase.wantRole, out)
				}
			}
		})
	}
}

func trimQuotes(value string) string {
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		return value[1 : len(value)-1]
	}
	return value
}
