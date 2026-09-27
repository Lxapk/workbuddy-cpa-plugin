package main

import (
	"encoding/json"
	"testing"
)

// 规范化不得为一个调用造出两个结果，也不得把结果改指到别的调用上。
//
// linkToolResultsToCalls 原本在「结果带了一个不属于当前批次的 id」时，会把那个 id
// 改写成本批次里下一个未回答的调用。前一条结果本来是对的，改写之后却出现两个结果
// 指向同一个调用、另一个调用没有结果——上游照样以 "tool calls and tool results do
// not match" 拒掉请求，也就是这一趟修复本身制造的错误。
//
// 结果顺序与调用顺序不一致（重试导致的重复结果、乱序持久化的 transcript）都会走到
// 这条路径，所以这里把几种形态都锁住：不论输入如何，每条工具结果要么保持它自己的
// id，要么只补上确实缺失的 id。
func TestNormaliseNeverInventsASecondResultForOneCall(t *testing.T) {
	cases := []struct {
		name     string
		messages string
	}{
		{
			name: "重复结果",
			messages: `[
				{"role":"user","content":"go"},
				{"role":"assistant","content":"","tool_calls":[
					{"id":"c1","type":"function","function":{"name":"a","arguments":"{}"}},
					{"id":"c2","type":"function","function":{"name":"b","arguments":"{}"}}
				]},
				{"role":"tool","tool_call_id":"c1","content":"r1"},
				{"role":"tool","tool_call_id":"c1","content":"r1-again"},
				{"role":"tool","tool_call_id":"c2","content":"r2"}
			]`,
		},
		{
			name: "结果排在调用之前",
			messages: `[
				{"role":"user","content":"go"},
				{"role":"assistant","content":"","tool_calls":[
					{"id":"c1","type":"function","function":{"name":"a","arguments":"{}"}}
				]},
				{"role":"tool","tool_call_id":"c1","content":"r1"},
				{"role":"tool","tool_call_id":"c2","content":"r2"},
				{"role":"assistant","content":"","tool_calls":[
					{"id":"c2","type":"function","function":{"name":"b","arguments":"{}"}}
				]},
				{"role":"tool","tool_call_id":"c2","content":"r2"}
			]`,
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

			// 统计每个 id 收到几条结果，以及结果是否出现在它的调用之前。
			//
			// 这两种输入本身就不合法，插件不该假装能修好它：上游会拒绝并要用户
			// 重开会话。要守住的是插件**不会把情况变得更坏**——
			// 原本一对一的配对不能因为改写而变成一对多，结果也不能被改指到另一
			// 个调用上。所以这里比对的是「改写前后每个 id 的结果数」有没有变多。
			results := map[string]int{}
			declared := map[string]int{}
			resultBeforeCall := 0
			for _, message := range doc.Messages {
				switch string(message["role"]) {
				case `"assistant"`:
					for _, id := range assistantToolCallIDList(message) {
						declared[id]++
					}
				case `"tool"`:
					var id string
					_ = json.Unmarshal(message["tool_call_id"], &id)
					results[id]++
					if declared[id] == 0 {
						resultBeforeCall++
					}
				}
			}

			// 与输入相比，任何 id 都不该多出结果。
			wantResults := map[string]int{}
			var inputDoc struct {
				Messages []map[string]json.RawMessage `json:"messages"`
			}
			_ = json.Unmarshal([]byte(`{"messages":`+testCase.messages+`}`), &inputDoc)
			for _, message := range inputDoc.Messages {
				if string(message["role"]) != `"tool"` {
					continue
				}
				var id string
				_ = json.Unmarshal(message["tool_call_id"], &id)
				wantResults[id]++
			}
			for id, want := range wantResults {
				if want == 0 {
					continue
				}
				if results[id] > want {
					t.Errorf("调用 %s 的结果数从 %d 涨到 %d —— 改写制造了错配\n%s",
						id, want, results[id], out)
				}
			}

			// 结果排在其调用之前是输入自身的乱序，插件不得靠改写 id 来掩盖它。
			_ = resultBeforeCall
		})
	}
}

// 结果缺少 tool_call_id 时，补上本批次里未回答的调用是正确的。
func TestNormaliseFillsInAMissingToolResultID(t *testing.T) {
	body := []byte(`{
		"model": "m",
		"messages": [
			{"role": "user", "content": "go"},
			{"role": "assistant", "content": "", "tool_calls": [
				{"id": "c1", "type": "function", "function": {"name": "a", "arguments": "{}"}}
			]},
			{"role": "tool", "content": "r1"}
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
		t.Fatal(errUnmarshal)
	}

	found := false
	for _, message := range doc.Messages {
		if string(message["role"]) != `"tool"` {
			continue
		}
		var id string
		_ = json.Unmarshal(message["tool_call_id"], &id)
		if id == "c1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("缺失的 tool_call_id 应被补上\n%s", out)
	}
}
