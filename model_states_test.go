package main

import (
	"encoding/json"
	"testing"
	"time"
)

// 限流要写进 model_states，带上游给出的重置时刻。
//
// CPA 依据这份状态把「该模型正在冷却」与「账号不可用」区分开；只写插件内部的
// ModelCooldowns 时 CPA 看不到，于是一次模型级限流会被报成 auth_unavailable，
// 客户端以为账号没了。
func TestApplyModelParkRecordsThrottle(t *testing.T) {
	states := map[string]modelStateEntry{}
	until := time.Now().Add(90 * time.Minute)
	reason := "您的使用量已超出频率限制，将在 2026-09-27 20:03:46 UTC+8 重置"

	applyModelPark(states, "deepseek-v4.1-flash", until, reason, 429, false)

	entry, okEntry := states["deepseek-v4.1-flash"]
	if !okEntry {
		t.Fatal("该模型应被写入 model_states")
	}
	if !entry.Unavailable {
		t.Error("Unavailable 应为 true")
	}
	if entry.Status != modelStateStatusError {
		t.Errorf("Status = %q, want %q", entry.Status, modelStateStatusError)
	}
	if entry.NextRetryAfter == nil || !entry.NextRetryAfter.Equal(until.UTC()) {
		t.Errorf("NextRetryAfter = %v, want %v", entry.NextRetryAfter, until.UTC())
	}
	if entry.StatusMessage != reason {
		t.Errorf("StatusMessage = %q, want %q", entry.StatusMessage, reason)
	}
	if entry.LastError == nil || entry.LastError.HTTPStatus != 429 {
		t.Errorf("LastError 应记录 429；got %+v", entry.LastError)
	}
	if entry.Quota == nil || !entry.Quota.Exceeded {
		t.Errorf("429 应记录额度超限；got %+v", entry.Quota)
	}
}

// 其它模型的状态不能被覆盖。
//
// host.auth.save 是整文件覆盖，而写入是从「读到的全量」改一个键再写回，所以
// 只有把既有条目原样带过去，CPA 自己记录的冷却和别的模型的限流才不会被抹掉。
func TestApplyModelParkKeepsOtherModels(t *testing.T) {
	otherDeadline := time.Now().Add(30 * time.Minute).UTC()
	states := map[string]modelStateEntry{
		"kimi-k2.5": {Status: modelStateStatusError, Unavailable: true, NextRetryAfter: &otherDeadline},
	}

	applyModelPark(states, "deepseek-v4.1-flash", time.Now().Add(time.Hour), "throttled", 429, false)

	if _, okOther := states["kimi-k2.5"]; !okOther {
		t.Error("其它模型的条目不能被冲掉")
	}
	if len(states) != 2 {
		t.Errorf("states 数量 = %d, want 2", len(states))
	}
}

// 冷却到期的模型要摘掉条目，而不是留一条 unavailable:false 的死数据。
//
// 否则文件会为账号历史上限流过的每个模型留一行，越积越多。
func TestApplyModelParkClearsExpiredModel(t *testing.T) {
	states := map[string]modelStateEntry{
		"deepseek-v4.1-flash": {Unavailable: true},
	}
	// 已过去 / 零值都表示「又好用了」。
	applyModelPark(states, "deepseek-v4.1-flash", time.Now().Add(-time.Minute), "old", 429, false)
	if _, okEntry := states["deepseek-v4.1-flash"]; okEntry {
		t.Error("过期的条目应被移除")
	}

	states["deepseek-v4.1-flash"] = modelStateEntry{Unavailable: true}
	applyModelPark(states, "deepseek-v4.1-flash", time.Time{}, "old", 429, false)
	if _, okEntry := states["deepseek-v4.1-flash"]; okEntry {
		t.Error("零值 deadline 应被移除")
	}
}

// 已存在的 JSON 里若有本插件未建模的字段，读取时要容忍而不是报错丢弃。
func TestDecodeModelStatesToleratesUnknownFields(t *testing.T) {
	raw := json.RawMessage(`{
		"deepseek-v4.1-flash": {
			"status": "error",
			"unavailable": true,
			"next_retry_after": "2026-09-27T12:03:46Z",
			"future_field": {"nested": true}
		}
	}`)
	states := decodeModelStates(raw)
	entry, okEntry := states["deepseek-v4.1-flash"]
	if !okEntry {
		t.Fatal("应解析出该模型")
	}
	if !entry.Unavailable {
		t.Error("Unavailable 应为 true")
	}
	if entry.NextRetryAfter == nil {
		t.Fatal("NextRetryAfter 不应为空")
	}
}

// model_states 为空 / 缺失时不应 panic，返回空表。
func TestDecodeModelStatesHandlesEmpty(t *testing.T) {
	for _, raw := range []json.RawMessage{nil, {}, []byte("null"), []byte("not json")} {
		states := decodeModelStates(raw)
		if states == nil {
			t.Errorf("raw=%s 应返回非 nil 的空表", raw)
		}
		if len(states) != 0 {
			t.Errorf("raw=%s 应为空表，got %v", raw, states)
		}
	}
}

// 非模型级失败（网络错误、语义不明的 5xx）不写 model_states。
func TestPublishModelFailureIgnoresUnrelatedKinds(t *testing.T) {
	for _, kind := range []failureKind{failureAuth, failureTransient, failureKind(99)} {
		_, okUntil := modelParkDeadline(time.Now(), upstreamError{Kind: kind, Message: "x"}, 502)
		if okUntil {
			t.Errorf("kind=%v 不应产生模型级 deadline", kind)
		}
	}
}
