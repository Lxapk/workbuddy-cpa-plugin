package main

import (
	"encoding/json"
	"testing"
	"time"
)

// 限流要把账号也短暂移出轮换。
//
// CPA 自己决定用哪个凭据，而且从不问插件某个凭据还能服务哪些模型，所以只停模型
// 对它不可见——请求会一直落在被限流的凭据上，然后以「无可用凭据」被拒，即便还有
// 另一个健康的凭据本可以服务它。把凭据短暂移出轮换才能让 CPA 换一个。
func TestApplyAccountParkDisablesCredentialUntilDeadline(t *testing.T) {
	file := map[string]json.RawMessage{}
	until := time.Now().Add(40 * time.Minute)

	applyAccountPark(file, until)

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
//
// 否则账号会被一次写入永久关掉，且因为没有任何东西再去看它，这个过程是静默的。
func TestApplyAccountParkClearsOnExpiredDeadline(t *testing.T) {
	file := map[string]json.RawMessage{}
	applyAccountPark(file, time.Now().Add(10*time.Minute))
	applyAccountPark(file, time.Now().Add(-time.Minute))

	if _, has := file[accountParkField]; has {
		t.Error("过期的恢复时间应被移除")
	}
	if got := string(file["disabled"]); got != "false" {
		t.Errorf("disabled = %s, want false", got)
	}
}

// 更早的报告不能把更长的一次关闭缩短。
//
// 同一凭据上两个模型先后失败时，第二个（恢复更早）若覆盖了第一个的时间，账号就会
// 在前一个模型仍然不可用时被放回轮换。
func TestApplyAccountParkNeverShortensAnExistingPark(t *testing.T) {
	file := map[string]json.RawMessage{}
	later := time.Now().Add(2 * time.Hour)
	applyAccountPark(file, later)
	applyAccountPark(file, time.Now().Add(5*time.Minute))

	deadline, okDeadline := parkedUntil(file)
	if !okDeadline {
		t.Fatal("应保留恢复时间")
	}
	if !deadline.Equal(later.UTC()) {
		t.Errorf("deadline = %v，不应被更早的报告缩短（want %v）", deadline, later.UTC())
	}
}

// 没有恢复时间的 disabled 不是本插件写的（人工操作），不得自动改回。
func TestRestoreParkOnlyTouchesOurOwnDeadline(t *testing.T) {
	file := map[string]json.RawMessage{"disabled": json.RawMessage("true")}
	if _, okDeadline := parkedUntil(file); okDeadline {
		t.Fatal("没有恢复时间时不应被认为可恢复")
	}
}

// 畸形的时间值不能把账号永久挡在外面。
func TestParkedUntilRejectsMalformedDeadline(t *testing.T) {
	file := map[string]json.RawMessage{accountParkField: json.RawMessage(`"not a time"`)}
	if _, okDeadline := parkedUntil(file); okDeadline {
		t.Error("无法解析的时间应视为不存在，账号必须能回来")
	}
}

// 恢复时要一并清掉 model_states：它们属于同一次事件，凭据回来了却还留着模型冷却，
// 会让 CPA 继续避开上游其实已经放开的模型。
func TestRestoreClearsModelStatesAlongsideDisable(t *testing.T) {
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

// 没有恢复时间的 disabled 是人工设置的，不得自动改回。
func TestClearExpiredParkIgnoresManualDisable(t *testing.T) {
	file := map[string]json.RawMessage{"disabled": json.RawMessage("true")}

	if clearExpiredPark(file, time.Now()) {
		t.Error("没有恢复时间的禁用不应被自动解除")
	}
	if got := string(file["disabled"]); got != "true" {
		t.Errorf("disabled = %s，人工设置不应被改动", got)
	}
}
