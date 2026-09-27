package main

import (
	"encoding/json"
	"testing"
	"time"
)

// 线上路径的账号级冷却必须是空操作。
//
// 这条写入被证明有害：host 会用自己的内存状态回写 auth 文件，于是写入被撤销，
// 撤销与重写互相追赶（debug 日志里 disabled 在相邻文件事件之间 false->true->false
// 反复横跳），每次翻转都会重新注册凭据，账号在注册表里进进出出。更糟的是被禁用的
// 凭据会被 host 整体丢弃，为一个模型的限流停掉整个账号，连它还能服务的其它模型
// 一起停掉——比触发它的那个请求损失更大。
func TestApplyAccountParkIsANoOp(t *testing.T) {
	file := map[string]json.RawMessage{"disabled": json.RawMessage("false")}
	before := string(file["disabled"])

	applyAccountPark(file, time.Now().Add(time.Hour))

	if got := string(file["disabled"]); got != before {
		t.Errorf("线上路径不能改动 disabled：%s -> %s", before, got)
	}
	if _, has := file[accountParkField]; has {
		t.Error("线上路径不能写入恢复时间")
	}
}

// 恢复循环不得改写任何文件。
func TestStartParkRecoveryDoesNothing(t *testing.T) {
	// 空操作：调用不应 panic，也不应启动任何会写文件的东西。
	startParkRecovery()
	startParkRecovery()
}

// 保留的写入逻辑仍然正确，供将来 host 提供正式接口时复用。
func TestAccountParkLegacyDisablesUntilDeadline(t *testing.T) {
	file := map[string]json.RawMessage{}
	until := time.Now().Add(40 * time.Minute)

	accountParkLegacy(file, until)

	if got := string(file["disabled"]); got != "true" {
		t.Errorf("disabled = %s, want true", got)
	}
	deadline, okDeadline := parkedUntil(file)
	if !okDeadline {
		t.Fatal("应记录恢复时间")
	}
	if !deadline.Equal(until.UTC()) {
		t.Errorf("deadline = %v, want %v", deadline, until.UTC())
	}
}

// 关闭必须是临时的：到点后两处标记都要清掉。
func TestAccountParkLegacyClearsOnExpiredDeadline(t *testing.T) {
	file := map[string]json.RawMessage{}
	accountParkLegacy(file, time.Now().Add(10*time.Minute))
	accountParkLegacy(file, time.Now().Add(-time.Minute))

	if _, has := file[accountParkField]; has {
		t.Error("过期的恢复时间应被移除")
	}
	if got := string(file["disabled"]); got != "false" {
		t.Errorf("disabled = %s, want false", got)
	}
}

// 更早的报告不能把更长的一次关闭缩短。
func TestAccountParkLegacyNeverShortensAnExistingPark(t *testing.T) {
	file := map[string]json.RawMessage{}
	later := time.Now().Add(2 * time.Hour)
	accountParkLegacy(file, later)
	accountParkLegacy(file, time.Now().Add(5*time.Minute))

	deadline, okDeadline := parkedUntil(file)
	if !okDeadline {
		t.Fatal("应保留恢复时间")
	}
	if !deadline.Equal(later.UTC()) {
		t.Errorf("deadline = %v，不应被更早的报告缩短（want %v）", deadline, later.UTC())
	}
}

// 没有恢复时间的 disabled 不是本插件写的（人工操作），不得自动改回。
func TestClearExpiredParkIgnoresManualDisable(t *testing.T) {
	file := map[string]json.RawMessage{"disabled": json.RawMessage("true")}

	if clearExpiredPark(file, time.Now()) {
		t.Error("没有恢复时间的禁用不应被自动解除")
	}
	if got := string(file["disabled"]); got != "true" {
		t.Errorf("disabled = %s，人工设置不应被改动", got)
	}
}

// 期限未到时不得恢复，也不得改动文件。
func TestClearExpiredParkLeavesLiveDeadline(t *testing.T) {
	future, _ := json.Marshal(time.Now().Add(time.Hour).UTC())
	file := map[string]json.RawMessage{
		"disabled":       json.RawMessage("true"),
		accountParkField: future,
	}

	if clearExpiredPark(file, time.Now()) {
		t.Error("期限未到不应恢复")
	}
	if got := string(file["disabled"]); got != "true" {
		t.Errorf("disabled = %s，不应被改动", got)
	}
}

// 恢复时要一并清掉 model_states：它们属于同一次事件。
func TestClearExpiredParkClearsModelStates(t *testing.T) {
	states := map[string]modelStateEntry{"m": {Unavailable: true}}
	encoded, _ := json.Marshal(states)
	past, _ := json.Marshal(time.Now().Add(-time.Second).UTC())
	file := map[string]json.RawMessage{
		"disabled":       json.RawMessage("true"),
		accountParkField: past,
		"model_states":   encoded,
		"accessToken":    json.RawMessage(`"token"`),
		"uid":            json.RawMessage(`"u1"`),
	}

	if !clearExpiredPark(file, time.Now()) {
		t.Fatal("期限已过，应执行恢复")
	}
	if _, has := file["model_states"]; has {
		t.Error("恢复时应清掉 model_states")
	}
	if _, has := file[accountParkField]; has {
		t.Error("恢复时应清掉恢复时间")
	}
	if got := string(file["disabled"]); got != "false" {
		t.Errorf("disabled = %s, want false", got)
	}
	// 其它字段必须原样保留：save 是整文件覆盖。
	if string(file["accessToken"]) != `"token"` || string(file["uid"]) != `"u1"` {
		t.Error("恢复时不能丢掉凭据字段")
	}
}

// 畸形的时间值不能把账号永久挡在外面。
func TestParkedUntilRejectsMalformedDeadline(t *testing.T) {
	file := map[string]json.RawMessage{accountParkField: json.RawMessage(`"not a time"`)}
	if _, okDeadline := parkedUntil(file); okDeadline {
		t.Error("无法解析的时间应视为不存在，账号必须能回来")
	}
}

// 线上路径的模型级写入同样是空操作。
func TestPublishModelParkIsANoOp(t *testing.T) {
	publishModelPark("auth-1", "m", time.Now().Add(time.Hour), "throttled", 429, false)
	// 无 host 上下文时能安全返回，且不做任何写入。
}

// 上游给了重置时刻就必须用它，而不是本地的默认冷却时长。
func TestModelParkDeadlinePrefersUpstreamResetTime(t *testing.T) {
	now := time.Now()
	upErr := upstreamError{
		Kind:    failureRate,
		Message: "您的使用量已超出频率限制，将在 2026-09-27 20:03:46 UTC+8 重置，您也可以切换其他模型继续使用。",
	}
	until, okUntil := modelParkDeadline(now, upErr, 502)
	if !okUntil {
		t.Fatal("应从文案里解析出重置时刻")
	}
	if until.Hour() != 12 {
		t.Errorf("20:03:46 UTC+8 应换算成 12:03:46 UTC，got %v", until)
	}
}

// 文案里没有重置时刻时退回本地冷却时长。
func TestModelParkDeadlineFallsBackToCooldown(t *testing.T) {
	resetState()
	state.settings.set(gatewaySettings{RateCooldownMillis: 60_000, QuotaCooldownMillis: 120_000})

	now := time.Now()
	until, okUntil := modelParkDeadline(now, upstreamError{Kind: failureRate, Message: "rate limited"}, 429)
	if !okUntil {
		t.Fatal("应回退到本地冷却时长")
	}
	if delta := until.Sub(now); delta < 55*time.Second || delta > 65*time.Second {
		t.Errorf("冷却时长 = %v, want ~60s", delta)
	}
}

// 非模型级失败不产生模型级期限。
func TestModelParkDeadlineIgnoresUnrelatedKinds(t *testing.T) {
	for _, kind := range []failureKind{failureAuth, failureTransient, failureKind(99)} {
		if _, okUntil := modelParkDeadline(time.Now(), upstreamError{Kind: kind, Message: "x"}, 502); okUntil {
			t.Errorf("kind=%v 不应产生模型级 deadline", kind)
		}
	}
}
