package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

// 面板在有账号时必须渲染出筛选条、表格标记与趋势图容器。
//
// 这些节点是客户端脚本的抓手：筛选靠 data-account-table 找表格，趋势图靠
// usageTrend 找容器。少一个，对应的功能会静默失效——脚本里读不到元素就直接
// 返回，页面上看不出任何异常。
func TestRenderPanelWithAccounts(t *testing.T) {
	resetState()
	seedPanelAccounts(t)

	page := renderMainPage()
	if len(page) < 5000 {
		t.Fatalf("面板过短：%d 字节", len(page))
	}

	for _, want := range []string{
		`id="accountFilter"`,
		`id="accountStatusFilter"`,
		`data-account-table="1"`,
		`data-account-toggle="1"`,
		`data-status="`,
		`data-search="`,
		`id="usageTrend"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("面板缺少 %s", want)
		}
	}

	// 账号表里不能再有把数据拼进 inline onclick 的写法。
	if strings.Contains(page, `onclick="toggleAccount(`) ||
		strings.Contains(page, `onclick="toggleAccountTask(`) {
		t.Error("账号表仍在用内联 onclick 传递数据，存在注入面")
	}

	_ = os.WriteFile("/tmp/panel.html", []byte(page), 0o644)
	t.Logf("面板 %d 字节", len(page))
}

// 没有账号时不渲染筛选条：没有什么可筛的。
func TestRenderPanelWithoutAccountsOmitsFilter(t *testing.T) {
	resetState()
	page := renderMainPage()
	if strings.Contains(page, `id="accountFilter"`) {
		t.Error("无账号时不应渲染筛选条")
	}
	// 趋势图容器始终存在——它的空态自己会给指引。
	if !strings.Contains(page, `id="usageTrend"`) {
		t.Error("趋势图容器应始终渲染")
	}
}

// 脚本必须包含新增的关键函数，且转义实现只有一处。
func TestPanelScriptHasNewFunctions(t *testing.T) {
	script := mainPageScript()
	for _, want := range []string{
		"function toast(",
		"function renderUsageTrend(",
		"function applyAccountFilter(",
		"window.refreshAccountsAndQuota",
		"data-task-toggle",
		"refreshUsageTrend",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("脚本缺少 %s", want)
		}
	}
	// 逐账号的签到/积分按钮已移除，对应的处理函数不应残留。
	for _, gone := range []string{
		"window.runAccountCheckin", "window.runAccountQuota",
		"data-account-checkin", "data-account-quota",
	} {
		if strings.Contains(script, gone) {
			t.Errorf("脚本仍含已移除的 %s", gone)
		}
	}
	// escapeHTML 必须只是委托：两份实现曾经各自漂移，修了一份漏了另一份。
	if strings.Count(script, `replace(/[&<>"']/g`) != 1 {
		t.Error("转义实现应只有一处")
	}
	if !strings.Contains(script, "function escapeHTML(v)") {
		t.Error("escapeHTML 应保留为 esc 的别名")
	}
}

// seedPanelAccounts puts two accounts (one per realm) into the panel's cache.
//
// Written straight into the store's cache rather than through the host: the panel
// renders whatever the store returns, so this is the seam that exercises the real
// rendering path without needing a live CPA.
func seedPanelAccounts(t *testing.T) {
	t.Helper()
	now := time.Now()

	state.accounts.mu.Lock()
	state.accounts.cached = []workBuddyAccount{
		{
			Label: "国内一号", UID: "cn-uid-1", AuthIndex: "auth-cn-1", Variant: "cn",
			Usable: true, CreditsKnown: true, Credits: 1200,
			CreditsExpireAt: now.Add(72 * time.Hour).Unix(), CreditsExpireDays: 3,
		},
		{
			Label: "国际一号", UID: "ai-uid-2", AuthIndex: "auth-ai-2", Variant: "ai",
			Usable: false, CreditsKnown: true, Credits: 340,
			CooldownUntil: now.Add(time.Minute),
		},
	}
	state.accounts.fetchedAt = now
	state.accounts.lastErr = ""
	state.accounts.mu.Unlock()
}
