package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 写 disabled 只改这一个键，其余字段（尤其是宿主刚刷新过的 token）保持磁盘上的现值。
func TestSetAuthFileDisabledKeepsOtherFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "codebuddy-u.json")
	if err := os.WriteFile(path, []byte(`{"accessToken":"fresh","uid":"u","extra":{"k":1}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, err := setAuthFileDisabled(path, true)
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	raw, _ := os.ReadFile(path)
	var doc map[string]any
	_ = json.Unmarshal(raw, &doc)
	if doc["accessToken"] != "fresh" || doc["disabled"] != true || doc["extra"] == nil {
		t.Fatalf("文件被改坏：%s", raw)
	}
	if changed, _ := setAuthFileDisabled(path, true); changed {
		t.Error("已是目标状态时不该重写")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("临时文件没有清理：%d 个文件", len(entries))
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("权限被改：%v", info.Mode().Perm())
	}
}

// 写入失败时不能留下待定值，否则面板会一直显示一个从未生效的状态。
func TestFailedWriteLeavesNoPending(t *testing.T) {
	resetState()
	missing := filepath.Join(t.TempDir(), "gone.json")
	installAuthList(t, []map[string]any{
		{"auth_index": "idx-x", "provider": workBuddyProviderKey, "path": missing},
	})
	syncAccountDisabledToHost("", "idx-x", true)
	if _, has := pendingFor([]string{"idx-x"}); has {
		t.Fatal("写入失败却记下了待定值")
	}
}

// 区域开关只撤销它自己加上的禁用；操作者手动禁用的账号切回自动后仍保持禁用。
func TestVariantScopeOnlyLiftsItsOwnHold(t *testing.T) {
	resetState()
	dir := t.TempDir()
	write := func(name, domain string, disabled bool) string {
		p := filepath.Join(dir, name)
		blob, _ := json.Marshal(map[string]any{"accessToken": "t", "uid": name, "domain": domain, "disabled": disabled})
		if err := os.WriteFile(p, blob, 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	cnA := write("codebuddy-cn-a.json", "www.codebuddy.cn", false)
	cnManual := write("codebuddy-cn-b.json", "www.codebuddy.cn", true) // 操作者手动禁用
	aiManual := write("codebuddy-ai-a.json", "www.workbuddy.ai", true) // 操作者手动禁用
	restore := stubHostCall(func(method string, _ any) (json.RawMessage, error) {
		if method == "host.auth.list" {
			return mustMarshal(t, map[string]any{"files": []map[string]any{
				{"auth_index": "a", "provider": workBuddyProviderKey, "path": cnA},
				{"auth_index": "b", "provider": workBuddyProviderKey, "path": cnManual},
				{"auth_index": "c", "provider": workBuddyProviderKey, "path": aiManual},
			}}), nil
		}
		return json.RawMessage(`{}`), nil
	})
	defer restore()
	on := func(p string) bool { d, _ := diskDisabled(p); return d }

	syncVariantScopeToHost("ai")
	if !on(cnA) || !on(cnManual) {
		t.Fatal("仅国际时国内凭据应被停用")
	}
	if !on(aiManual) {
		t.Fatal("仅国际不该启用操作者手动禁用的国际账号")
	}
	if regionHold.isHeld(cnManual) {
		t.Error("本来就禁用的文件不该记为区域暂停")
	}

	syncVariantScopeToHost("")
	if on(cnA) {
		t.Error("切回自动后，区域暂停的账号应恢复")
	}
	if !on(cnManual) || !on(aiManual) {
		t.Error("切回自动把操作者手动禁用的账号也恢复了")
	}
	if regionHold.isHeld(cnA) {
		t.Error("恢复后仍残留区域暂停记录")
	}
}

// 自动禁用不写入文件，所以文件每次都读作启用；这不能被当作「宿主重新启用」而清掉。
func TestAutoDisableSurvivesHostListing(t *testing.T) {
	resetState()
	state.pool.observe(workBuddyProviderKey, "u-1", "一号")
	state.pool.failureForModel(workBuddyProviderKey, "u-1", "", failureAuth, "凭据失效", state.settings.get(), true)
	state.pool.applyHostDisabledFlags([]workBuddyAccount{{UID: "u-1", Disabled: false}})
	lane, _ := state.pool.laneFor("u-1")
	if !lane.AutoDisabled {
		t.Fatal("一次列表读取就清掉了自动禁用")
	}
	// 宿主先禁用、再启用：这是操作者在 CPA 里重新打开，自动禁用随之解除。
	state.pool.applyHostDisabledFlags([]workBuddyAccount{{UID: "u-1", Disabled: true}})
	state.pool.applyHostDisabledFlags([]workBuddyAccount{{UID: "u-1", Disabled: false}})
	lane, _ = state.pool.laneFor("u-1")
	if lane.AutoDisabled || lane.Disabled {
		t.Fatalf("宿主重新启用后应解除：%+v", lane)
	}
}

// 重绘时启用一个冷却中的账号，不能显示为可用。
func TestPendingEnableKeepsCooldown(t *testing.T) {
	resetState()
	rememberDisabled(hostAuthEntry{AuthIndex: "idx-1"}, false)
	out := applyPendingDisabled([]workBuddyAccount{{AuthIndex: "idx-1", UID: "u", Disabled: true, CooldownUntil: time.Now().Add(time.Hour)}})
	if out[0].Disabled || out[0].Usable {
		t.Fatalf("冷却中的账号被标成可用：%+v", out[0])
	}
}
