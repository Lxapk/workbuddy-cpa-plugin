package main

import (
	"encoding/json"
	"strings"
	"time"
)

// This file handles a constraint discovered during local end-to-end testing:
// the WorkBuddy upstream refuses non-streaming chat requests outright.
//
//	POST /v2/chat/completions  {"stream": false}
//	-> {"code":11101,"msg":"Non-stream chat request is currently not supported"}
//
// The source app never hit this because it always sends stream=true, so the APK
// gives no hint that the restriction exists.
//
// The plugin therefore satisfies a non-streaming client by issuing a streaming
// request upstream and folding the SSE frames back into a single
// chat.completion object — the shape the client asked for.

// upstreamCodeNonStreamUnsupported is the code the provider returns for a
// non-streaming chat request.
const upstreamCodeNonStreamUnsupported = 11101

// isNonStreamUnsupported reports whether an upstream error body is the
// "non-stream not supported" rejection.
func isNonStreamUnsupported(statusCode int, body []byte) bool {
	if len(body) == 0 {
		return false
	}
	var doc struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if errUnmarshal := json.Unmarshal(body, &doc); errUnmarshal != nil {
		return false
	}
	if doc.Code == upstreamCodeNonStreamUnsupported {
		return true
	}
	return strings.Contains(strings.ToLower(doc.Msg), "non-stream")
}

// forceStream sets "stream": true in a chat body.
func forceStream(body []byte) ([]byte, error) {
	var doc map[string]json.RawMessage
	if errUnmarshal := json.Unmarshal(body, &doc); errUnmarshal != nil {
		return nil, errUnmarshal
	}
	doc["stream"] = json.RawMessage("true")
	// stream_options carries usage accounting; the upstream accepts it with
	// stream=true and the aggregated response needs the usage block.
	if _, ok := doc["stream_options"]; !ok {
		doc["stream_options"] = json.RawMessage(`{"include_usage":true}`)
	}
	return json.Marshal(doc)
}

// aggregateStreamToCompletion folds a sequence of OpenAI SSE frames into one
// non-streaming chat.completion response.
//
// It mirrors what a client library would do: concatenate delta.content, keep the
// first frame's envelope fields, take the last finish_reason, and carry the usage
// block from whichever frame supplied it.
// toolCallAccumulator rebuilds tool calls that arrived split across frames.
//
// A streamed tool call is not sent whole. The first frame carries the id and the
// function name, and the arguments arrive in pieces:
//
//	{"index":0,"id":"call_x","function":{"name":"get_weather","arguments":""}}
//	{"index":0,"function":{"arguments":"{\"city\""}}
//	{"index":0,"function":{"arguments":":\"Hangzhou\"}"}}
//
// Keeping only the last frame (what a plain overwrite does) loses the id, the name
// and most of the arguments, so a folded response reaches the client with an
// unusable tool call. Everything is therefore merged per index, and the argument
// pieces are concatenated in arrival order.
type toolCallAccumulator struct {
	order []int
	calls map[int]map[string]any
}

// newToolCallAccumulator prepares an empty accumulator.
func newToolCallAccumulator() *toolCallAccumulator {
	return &toolCallAccumulator{calls: map[int]map[string]any{}}
}

// merge folds one frame's tool_calls array into the accumulator.
func (accumulator *toolCallAccumulator) merge(raw json.RawMessage) {
	var calls []struct {
		Index    *int            `json:"index"`
		ID       string          `json:"id"`
		Type     string          `json:"type"`
		Function json.RawMessage `json:"function"`
	}
	if errUnmarshal := json.Unmarshal(raw, &calls); errUnmarshal != nil {
		return
	}
	for position, call := range calls {
		// Frames that omit index still address distinct calls by their position in
		// the array, which is what the upstream means by sending them separately.
		index := position
		if call.Index != nil {
			index = *call.Index
		}
		target, okTarget := accumulator.calls[index]
		if !okTarget {
			target = map[string]any{"index": index, "function": map[string]any{}}
			accumulator.calls[index] = target
			accumulator.order = append(accumulator.order, index)
		}
		if call.ID != "" {
			target["id"] = call.ID
		}
		if call.Type != "" {
			target["type"] = call.Type
		}
		if len(call.Function) == 0 {
			continue
		}
		var function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}
		if errUnmarshal := json.Unmarshal(call.Function, &function); errUnmarshal != nil {
			continue
		}
		existing, _ := target["function"].(map[string]any)
		if existing == nil {
			existing = map[string]any{}
		}
		if function.Name != "" {
			existing["name"] = function.Name
		}
		if function.Arguments != "" {
			// Arguments are a JSON string that arrives in pieces; append, do not
			// replace, or every piece but the last is lost.
			previous, _ := existing["arguments"].(string)
			existing["arguments"] = previous + function.Arguments
		}
		target["function"] = existing
	}
}

// json returns the accumulated calls in the order they first appeared.
func (accumulator *toolCallAccumulator) json() (json.RawMessage, bool) {
	if len(accumulator.order) == 0 {
		return nil, false
	}
	out := make([]map[string]any, 0, len(accumulator.order))
	for _, index := range accumulator.order {
		call := accumulator.calls[index]
		if call == nil {
			continue
		}
		// A call with no id cannot be referenced by a later tool result; drop it
		// rather than emit something the client cannot answer.
		if id, _ := call["id"].(string); strings.TrimSpace(id) == "" {
			continue
		}
		if function, okFunction := call["function"].(map[string]any); okFunction {
			if _, okName := function["name"]; !okName {
				function["name"] = ""
			}
			if _, okArguments := function["arguments"]; !okArguments {
				function["arguments"] = ""
			}
		}
		out = append(out, call)
	}
	if len(out) == 0 {
		return nil, false
	}
	encoded, errMarshal := json.Marshal(out)
	if errMarshal != nil {
		return nil, false
	}
	return encoded, true
}

// aggregateStreamToCompletion folds a sequence of OpenAI SSE frames into one
// non-streaming chat.completion response.
func aggregateStreamToCompletion(frames [][]byte, model string) []byte {
	type delta struct {
		Role             string          `json:"role,omitempty"`
		Content          string          `json:"content,omitempty"`
		ReasoningContent string          `json:"reasoning_content,omitempty"`
		ToolCalls        json.RawMessage `json:"tool_calls,omitempty"`
		FunctionCall     json.RawMessage `json:"function_call,omitempty"`
	}

	var (
		id        string
		created   int64
		object    = "chat.completion"
		content   strings.Builder
		reasoning strings.Builder
		toolCalls = newToolCallAccumulator()
		finish    string
		usage     json.RawMessage
		sawFrame  bool
	)

	for _, frame := range frames {
		var doc struct {
			ID      string `json:"id"`
			Model   string `json:"model"`
			Object  string `json:"object"`
			Created int64  `json:"created"`
			Choices []struct {
				Delta        delta           `json:"delta"`
				FinishReason string          `json:"finish_reason"`
				Message      json.RawMessage `json:"message"`
			} `json:"choices"`
			Usage json.RawMessage `json:"usage"`
			Error *struct {
				Message string `json:"message"`
				Type    string `json:"type"`
				Code    string `json:"code"`
			} `json:"error"`
		}
		if errUnmarshal := json.Unmarshal(frame, &doc); errUnmarshal != nil {
			continue
		}
		// A mid-stream error frame must surface as an error response.
		if doc.Error != nil && doc.Error.Message != "" {
			return errorBody(doc.Error.Message, firstNonEmpty(doc.Error.Type, "upstream_error"),
				firstNonEmpty(doc.Error.Code, "upstream_error"))
		}
		if doc.ID != "" {
			id = doc.ID
		}
		if doc.Created != 0 {
			created = doc.Created
		}
		if doc.Object != "" && doc.Object != "chat.completion.chunk" {
			object = doc.Object
		}
		if len(doc.Usage) > 0 && string(doc.Usage) != "null" {
			usage = doc.Usage
		}
		for _, choice := range doc.Choices {
			sawFrame = true
			content.WriteString(choice.Delta.Content)
			reasoning.WriteString(choice.Delta.ReasoningContent)
			if len(choice.Delta.ToolCalls) > 0 && string(choice.Delta.ToolCalls) != "null" {
				toolCalls.merge(choice.Delta.ToolCalls)
			}
			if choice.FinishReason != "" {
				finish = choice.FinishReason
			}
		}
	}

	if !sawFrame {
		// Nothing usable arrived; report an error rather than an empty success.
		return errorBody("上游未返回任何内容", "upstream_error", "empty_upstream_stream")
	}

	message := map[string]any{
		"role":    "assistant",
		"content": content.String(),
	}
	if reasoning.Len() > 0 {
		message["reasoning_content"] = reasoning.String()
	}
	if accumulated, okCalls := toolCalls.json(); okCalls {
		message["tool_calls"] = accumulated
	}

	choices := []map[string]any{{
		"index":         0,
		"message":       message,
		"finish_reason": firstNonEmpty(finish, "stop"),
		"logprobs":      nil,
	}}

	out := map[string]any{
		"id":      firstNonEmpty(id, "chatcmpl-aggregated"),
		"object":  object,
		"created": firstNonZero(created, time.Now().Unix()),
		"model":   model,
		"choices": choices,
	}
	if len(usage) > 0 {
		out["usage"] = usage
	} else {
		out["usage"] = map[string]any{
			"prompt_tokens":     0,
			"completion_tokens": 0,
			"total_tokens":      0,
		}
	}
	raw, _ := json.Marshal(out)
	return raw
}

// errorBody renders an OpenAI-shaped error payload.
func errorBody(message, errType, code string) []byte {
	raw, _ := json.Marshal(map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    errType,
			"code":    code,
		},
	})
	return raw
}

func firstNonZero(v, fallback int64) int64 {
	if v != 0 {
		return v
	}
	return fallback
}
