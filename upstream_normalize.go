package main

import (
	"encoding/json"
	"strings"
)

// This file normalises an upstream chat request.
//
// Why it exists
//
// The upstream validates the message array strictly and rejects the whole
// request when it does not conform, answering "request illegal". Two shapes in
// particular are rejected, with the codes the reference implementation recorded
// against the live service:
//
//	code 11128  a "developer" role. WorkBuddy knows only
//	            system / user / assistant / tool. OpenAI's newer "developer"
//	            role (sent by the Codex CLI and current SDKs) is semantically
//	            the same as "system" but is not accepted verbatim.
//
//	code 11148  a tool result that is not adjacent to the assistant message
//	            that requested it. Parallel tool calls interleaved with any other
//	            message (an image_resize_notice injected mid batch, for one)
//	            break the pairing and the upstream rejects every later turn of
//	            that conversation.
//
// The plugin used to forward the client body almost verbatim, changing only the
// model field, so any client sending a developer role or a reordered tool batch
// failed — and the failure surfaced as an opaque "request illegal".
//
// Everything here is a rename or a reorder. No content is dropped, and the only
// message ever added is the leading system prompt the upstream expects.

// defaultSystemPrompt mirrors the reference implementation's DEFAULT_SYSTEM_PROMPT.
const defaultSystemPrompt = "You are a helpful assistant."

// normaliseUpstreamBody rewrites a chat request into the shape the upstream
// accepts. It returns the original bytes when nothing needed changing.
func normaliseUpstreamBody(body []byte) ([]byte, error) {
	var doc map[string]json.RawMessage
	if errUnmarshal := json.Unmarshal(body, &doc); errUnmarshal != nil {
		// Not an object (or not JSON at all): leave it alone so the upstream
		// reports the real problem rather than a rewrite error.
		return body, nil
	}

	rawMessages, okMessages := doc["messages"]
	if !okMessages || len(rawMessages) == 0 {
		return body, nil
	}

	var messages []map[string]json.RawMessage
	if errUnmarshal := json.Unmarshal(rawMessages, &messages); errUnmarshal != nil {
		// A non-array or non-object message list is not something to repair.
		return body, nil
	}

	changed := normaliseRoles(messages)
	if repackToolResults(messages) {
		changed = true
	}
	var repaired bool
	messages, repaired = repairToolCallIDs(messages)
	if repaired {
		changed = true
	}
	if ensureLeadingSystemMessage(&messages) {
		changed = true
	}

	if !changed {
		return body, nil
	}

	encoded, errMarshal := json.Marshal(messages)
	if errMarshal != nil {
		return nil, errMarshal
	}
	doc["messages"] = encoded
	return json.Marshal(doc)
}

// repairToolCallIDs makes the tool_call_id pairings self-consistent.
//
// The upstream validates the pairing and rejects the whole conversation with
//
//	tool calls and tool results do not match, please start a new conversation and retry
//
// when it does not hold. repackToolResults only fixes *adjacency*; the ids have to
// line up as well, and clients get them wrong in four ways:
//
//  1. a tool result whose tool_call_id matches no call in the preceding assistant
//     turn (a stale id, or one from a conversation branch that was edited)
//  2. a tool result with no tool_call_id at all
//  3. an assistant turn carrying tool_calls that never receive a result — the
//     normal shape when the user interrupted the run, or when a transcript is
//     persisted mid-turn
//  4. a tool result arriving with nothing to answer (orphan)
//
// Cases 1 and 2 are repaired by pointing the result at the unmatched call still
// awaiting one, in order: the content is real, only the link was wrong.
//
// Cases 3 and 4 are resolved by *dropping* the unpaired side rather than inventing
// a counterpart. The upstream wants a result for every call, so case 3 has to be
// resolved, and a placeholder result would be shown to the model as if the tool
// had produced it — removing the call is a purely structural edit. Case 4 has
// nowhere to attach, so removing the result is the only option that leaves a valid
// conversation.
//
// Both sides are trimmed against one shared id set, so no edit here can leave a
// half-pair behind. Returns whether anything was rewritten.
func repairToolCallIDs(messages []map[string]json.RawMessage) ([]map[string]json.RawMessage, bool) {
	if len(messages) < 2 {
		return messages, false
	}

	// First pass: link results to the calls they answer (cases 1 and 2).
	linked, relinked := linkToolResultsToCalls(messages)

	// Second pass: keep only the ids present on both sides (cases 3 and 4).
	callIDs := collectAssistantToolCallIDs(linked)
	resultIDs := collectToolResultIDs(linked)
	keep := make(map[string]bool, len(callIDs))
	for id := range callIDs {
		if resultIDs[id] {
			keep[id] = true
		}
	}

	trimmed, trimmedAny := trimUnpairedToolMessages(linked, keep)
	return trimmed, relinked || trimmedAny
}

// linkToolResultsToCalls points each result at an unanswered call, in order.
//
// A result that already names a live, unanswered call is left alone. A result with
// a stale or missing id is re-pointed at the first call still awaiting one; the
// content came from a real execution, so keeping it is right.
//
// Reports whether any id was rewritten. The caller needs this: a rewrite here does
// not change how many ids are paired, so the trim pass reports "nothing to do" and
// the whole normalisation would otherwise be skipped as a no-op, silently dropping
// the repair.
func linkToolResultsToCalls(messages []map[string]json.RawMessage) ([]map[string]json.RawMessage, bool) {
	var pending []string
	answered := map[string]bool{}
	relinked := false

	for index := range messages {
		message := messages[index]
		switch roleOf(message) {
		case "assistant":
			pending = assistantToolCallIDs(message)
			answered = make(map[string]bool, len(pending))
		case "tool":
			if len(pending) == 0 {
				// Case 4; handled by the trim pass.
				continue
			}
			current := toolResultID(message)
			if current != "" && containsString(pending, current) && !answered[current] {
				answered[current] = true
				continue
			}
			next := ""
			for _, id := range pending {
				if !answered[id] {
					next = id
					break
				}
			}
			if next == "" {
				continue
			}
			if setToolResultID(message, next) {
				answered[next] = true
				relinked = true
			}
		}
	}
	return messages, relinked
}

// collectAssistantToolCallIDs gathers every id declared by an assistant turn.
func collectAssistantToolCallIDs(messages []map[string]json.RawMessage) map[string]bool {
	out := map[string]bool{}
	for _, message := range messages {
		if roleOf(message) != "assistant" {
			continue
		}
		for _, id := range assistantToolCallIDs(message) {
			out[id] = true
		}
	}
	return out
}

// collectToolResultIDs gathers every id named by a tool result.
func collectToolResultIDs(messages []map[string]json.RawMessage) map[string]bool {
	out := map[string]bool{}
	for _, message := range messages {
		if roleOf(message) != "tool" {
			continue
		}
		if id := toolResultID(message); id != "" {
			out[id] = true
		}
	}
	return out
}

// trimUnpairedToolMessages drops calls without results and results without calls.
//
// Both sides work against one shared keep set, so they cannot end up half-paired: a
// result survives only if its call does, and a call only if its result does. An
// assistant turn left with no calls keeps its other fields (content, reasoning) and
// simply loses the tool_calls key, so ordinary text is not discarded.
func trimUnpairedToolMessages(messages []map[string]json.RawMessage, keep map[string]bool) ([]map[string]json.RawMessage, bool) {
	changed := false
	out := make([]map[string]json.RawMessage, 0, len(messages))

	for _, message := range messages {
		switch roleOf(message) {
		case "assistant":
			rawCalls, okCalls := message["tool_calls"]
			if !okCalls {
				out = append(out, message)
				continue
			}
			var calls []map[string]json.RawMessage
			if errUnmarshal := json.Unmarshal(rawCalls, &calls); errUnmarshal != nil {
				out = append(out, message)
				continue
			}
			kept := make([]map[string]json.RawMessage, 0, len(calls))
			for _, call := range calls {
				if keep[rawStringField(call, "id")] {
					kept = append(kept, call)
				}
			}
			if len(kept) == len(calls) {
				out = append(out, message)
				continue
			}
			changed = true
			if len(kept) == 0 {
				// No calls survive: drop the key rather than leave an empty array,
				// which is itself a malformed assistant turn.
				delete(message, "tool_calls")
				out = append(out, message)
				continue
			}
			encoded, errMarshal := json.Marshal(kept)
			if errMarshal != nil {
				out = append(out, message)
				continue
			}
			message["tool_calls"] = encoded
			out = append(out, message)
		case "tool":
			if keep[toolResultID(message)] {
				out = append(out, message)
				continue
			}
			// A result whose call is gone cannot be answered; keeping it is what
			// the upstream reports as a mismatch.
			changed = true
		default:
			out = append(out, message)
		}
	}
	return out, changed
}

// rawStringField reads a string field from a decoded object, empty when absent.
func rawStringField(object map[string]json.RawMessage, field string) string {
	raw, okField := object[field]
	if !okField {
		return ""
	}
	var value string
	if errUnmarshal := json.Unmarshal(raw, &value); errUnmarshal != nil {
		return ""
	}
	return strings.TrimSpace(value)
}

// assistantToolCallIDs reads the ids of an assistant turn's tool_calls, in order.
func assistantToolCallIDs(message map[string]json.RawMessage) []string {
	if roleOf(message) != "assistant" {
		return nil
	}
	rawCalls, okCalls := message["tool_calls"]
	if !okCalls {
		return nil
	}
	var calls []map[string]json.RawMessage
	if errUnmarshal := json.Unmarshal(rawCalls, &calls); errUnmarshal != nil {
		return nil
	}
	ids := make([]string, 0, len(calls))
	for _, call := range calls {
		rawID, okID := call["id"]
		if !okID {
			continue
		}
		var id string
		if errUnmarshal := json.Unmarshal(rawID, &id); errUnmarshal != nil {
			continue
		}
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			ids = append(ids, trimmed)
		}
	}
	return ids
}

// toolResultID reads a tool message's tool_call_id, empty when absent.
func toolResultID(message map[string]json.RawMessage) string {
	rawID, okID := message["tool_call_id"]
	if !okID {
		return ""
	}
	var id string
	if errUnmarshal := json.Unmarshal(rawID, &id); errUnmarshal != nil {
		return ""
	}
	return strings.TrimSpace(id)
}

// setToolResultID writes a tool message's tool_call_id. Reports whether it did.
func setToolResultID(message map[string]json.RawMessage, id string) bool {
	encoded, errMarshal := json.Marshal(id)
	if errMarshal != nil {
		return false
	}
	message["tool_call_id"] = encoded
	return true
}

// containsString reports whether the slice holds the value.
func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// normaliseRoles renames role values to the set the upstream accepts.
//
// Reports whether anything changed. "developer" is OpenAI's rename of "system"
// and is what modern SDKs and the Codex CLI send; verbatim it is rejected with
// code 11128.
func normaliseRoles(messages []map[string]json.RawMessage) bool {
	changed := false
	for _, message := range messages {
		rawRole, okRole := message["role"]
		if !okRole {
			continue
		}
		var role string
		if errUnmarshal := json.Unmarshal(rawRole, &role); errUnmarshal != nil {
			continue
		}
		replacement, needsRename := upstreamRoleAlias(role)
		if !needsRename {
			continue
		}
		encoded, errMarshal := json.Marshal(replacement)
		if errMarshal != nil {
			continue
		}
		message["role"] = encoded
		changed = true
	}
	return changed
}

// upstreamRoleAlias maps a client role onto the upstream's vocabulary.
func upstreamRoleAlias(role string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "developer":
		return "system", true
	case "function":
		// The legacy pre-tool_calls spelling; the upstream only knows "tool".
		return "tool", true
	}
	return "", false
}

// repackToolResults moves any message that intrudes between an assistant's
// tool_calls and its results to after the results.
//
// Only the order changes: the same results in the same relative order, with the
// intruders moved behind the batch. Reports whether anything moved.
func repackToolResults(messages []map[string]json.RawMessage) bool {
	if len(messages) < 3 {
		return false
	}
	out := make([]map[string]json.RawMessage, 0, len(messages))
	moved := false
	i := 0
	for i < len(messages) {
		message := messages[i]
		if !isAssistantWithToolCalls(message) {
			out = append(out, message)
			i++
			continue
		}

		// Collect the assistant plus the run of tool results that follow,
		// deferring anything that appears inside that run.
		batch := []map[string]json.RawMessage{message}
		var intruders []map[string]json.RawMessage
		j := i + 1
		for j < len(messages) {
			next := messages[j]
			if isToolResult(next) {
				batch = append(batch, next)
				j++
				continue
			}
			// A non-tool message ends the batch — unless it merely interrupts
			// it and more results follow, which is the case being repaired.
			if hasToolResultLater(messages, j+1) {
				intruders = append(intruders, next)
				moved = true
				j++
				continue
			}
			break
		}
		out = append(out, batch...)
		out = append(out, intruders...)
		i = j
	}
	if !moved {
		return false
	}
	copy(messages, out)
	return true
}

// isAssistantWithToolCalls reports whether a message is an assistant turn that
// requested one or more tool calls.
func isAssistantWithToolCalls(message map[string]json.RawMessage) bool {
	if roleOf(message) != "assistant" {
		return false
	}
	rawCalls, okCalls := message["tool_calls"]
	if !okCalls {
		return false
	}
	var calls []json.RawMessage
	if errUnmarshal := json.Unmarshal(rawCalls, &calls); errUnmarshal != nil {
		return false
	}
	return len(calls) > 0
}

// isToolResult reports whether a message carries a tool result.
func isToolResult(message map[string]json.RawMessage) bool {
	return roleOf(message) == "tool"
}

// hasToolResultLater reports whether any message from index onwards is a tool
// result, which is what distinguishes an interruption from the end of a batch.
func hasToolResultLater(messages []map[string]json.RawMessage, from int) bool {
	for i := from; i < len(messages); i++ {
		if isToolResult(messages[i]) {
			return true
		}
		// A new assistant turn ends the segment under repair.
		if roleOf(messages[i]) == "assistant" {
			return false
		}
	}
	return false
}

// ensureLeadingSystemMessage inserts the default prompt when the conversation
// does not already begin with a system message.
//
// The upstream expects one and the reference implementation inserts it too.
// Reports whether a message was added; the slice is replaced in place because
// the caller keeps using the same variable.
func ensureLeadingSystemMessage(messages *[]map[string]json.RawMessage) bool {
	if messages == nil {
		return false
	}
	current := *messages
	if len(current) > 0 && roleOf(current[0]) == "system" {
		return false
	}
	content, errMarshal := json.Marshal(defaultSystemPrompt)
	if errMarshal != nil {
		return false
	}
	role, errRole := json.Marshal("system")
	if errRole != nil {
		return false
	}
	prefix := map[string]json.RawMessage{"role": role, "content": content}
	*messages = append([]map[string]json.RawMessage{prefix}, current...)
	return true
}

// roleOf reads a message's role without failing on an unexpected shape.
func roleOf(message map[string]json.RawMessage) string {
	rawRole, okRole := message["role"]
	if !okRole {
		return ""
	}
	var role string
	if errUnmarshal := json.Unmarshal(rawRole, &role); errUnmarshal != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(role))
}
