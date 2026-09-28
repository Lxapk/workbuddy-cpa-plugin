package main

// The panel's page frame and per-page renderers.
//
// Layout rationale (mirrors the reference implementation the operator asked for):
// a fixed side navigation with one page per functional area, and every card carrying
// its own actions in its header. Actions used to live in a toolbar at the top of a
// page, several blocks away from the table they acted on, which is what made the
// interface hard to follow.
//
// Each page is its own function so that the structure of the panel is visible from
// the list of names, instead of being buried in one long builder.

import (
	"fmt"
	"html"
	"strings"
	"time"
)

// renderMainPage builds the whole document.
//
// Layout, top to bottom: a title block, the tab strip, then the page frame holding
// whichever page is showing. The title block is separate from the tabs so the panel
// announces what it is before offering navigation.
func renderMainPage() string {
	var b strings.Builder
	b.WriteString(`<!doctype html><html lang="zh-CN"><head><meta charset="utf-8">`)
	b.WriteString(`<meta name="viewport" content="width=device-width,initial-scale=1">`)
	b.WriteString(`<title>WorkBuddy 控制台</title><style>` + uiCSS + `</style></head><body>`)

	b.WriteString(`<div class="shell">`)
	b.WriteString(renderHeader())
	b.WriteString(renderNav())
	b.WriteString(`<main class="main">`)

	// Every page is always in the document; only one is visible at a time.
	b.WriteString(renderAccountsView())
	b.WriteString(renderUsageView())
	b.WriteString(renderTasksView())
	b.WriteString(renderSettingsView())

	b.WriteString(`</main></div>`)
	b.WriteString(`<div id="toasts"></div>`)
	// mainPageScript returns the whole <script> element, wrapper included.
	b.WriteString(mainPageScript())
	b.WriteString(`</body></html>`)
	return b.String()
}

// renderHeader is the title block above the tabs.
func renderHeader() string {
	var b strings.Builder
	b.WriteString(`<header class="page-header">`)
	b.WriteString(`<h1>WorkBuddy 控制台</h1>`)
	b.WriteString(`<p class="desc">把 WorkBuddy 账号反代为 CPA 的 OpenAI 兼容接口：` +
		`账号轮换、模型调用、每日签到与积分管理。</p>`)
	b.WriteString(`</header>`)
	return b.String()
}

// renderNav builds the tab strip between the header and the page frame.
//
// Tabs rather than a side column: this matches the host's own section navigation, and
// the panel is usually viewed in a narrow webview where a 196px column costs more than
// it gives.
func renderNav() string {
	var b strings.Builder
	b.WriteString(`<nav class="tabbar">`)
	for _, item := range []struct{ view, label string }{
		{"view-accounts", "账号"},
		{"view-tasks", "任务"},
		{"view-usage", "用量"},
		{"view-settings", "设置"},
	} {
		b.WriteString(`<button type="button" class="tab" data-view="` + item.view + `">` +
			item.label + `</button>`)
	}
	b.WriteString(`</nav>`)
	return b.String()
}

// renderAccountsView is the account pool page.
//
// Its cards, in reading order: the summary strip, then the pool itself with its
// filter bar and per-row controls, then the routing strategy that decides how
// requests are spread across those accounts, then the auto-refresh reading and the
// outcome of the last credit sweep.
func renderAccountsView() string {
	accounts := listWorkBuddyAccounts()
	total, usable := accountCounts(accounts)

	var b strings.Builder
	b.WriteString(`<section class="view" id="view-accounts">`)

	// ---- summary ----
	b.WriteString(`<div class="stats">`)
	statCard(&b, "", "账号总数", total)
	statCard(&b, "good", "可用", usable)
	statCard(&b, "warn", "冷却中", accountCoolingCount(accounts))
	statCard(&b, "bad", "已禁用", accountDisabledCount(accounts))
	statCard(&b, "", "积分合计", accountCreditsTotal(accounts))
	b.WriteString(`</div>`)

	// ---- pool ----
	//
	// One card holds the whole account view: the filter bar, the rows (each with its
	// balance and its own controls), and the outcome of the last credit sweep. They
	// were two cards — the pool and the sweep result — which showed the same numbers
	// twice and made the reader check which one was current.
	b.WriteString(`<div class="box">`)
	b.WriteString(`<header><h3>账号 <span class="hint">登录后自动出现</span></h3><span class="grow"></span>`)
	b.WriteString(`<span class="note" id="accountMsg"></span>`)
	b.WriteString(`<span class="note" id="quotaMsg"></span>`)
	b.WriteString(`<button type="button" class="xs primary" data-call="refreshAccountsAndQuota">刷新账号与积分</button>`)
	b.WriteString(`</header>`)
	b.WriteString(`<input type="hidden" id="accountsSignature" value="` + html.EscapeString(accountsSignature(accounts)) + `">`)
	if len(accounts) == 0 {
		b.WriteString(`<div class="empty">还没有账号。在 CPA 里完成一次 WorkBuddy 授权后会自动出现。</div>`)
	} else {
		b.WriteString(renderAccountFilterBar())
		b.WriteString(renderAccountTable(accounts))
	}
	// Sweep failures are the one thing the per-row view cannot express: a row shows
	// the balance it has, not that the last attempt to read it failed.
	b.WriteString(renderQuotaSweepNotes())
	b.WriteString(`</div>`)

	b.WriteString(`</section>`)
	return b.String()
}

// renderUsageView is the traffic page.
func renderUsageView() string {
	totals := state.log.totals()
	recent := state.log.recent(60)

	var b strings.Builder
	b.WriteString(`<section class="view" id="view-usage" hidden>`)

	b.WriteString(`<div class="stats">`)
	statCard(&b, "", "总调用", totals.TotalCalls)
	statCard(&b, "", "今日", totals.TodayCalls)
	statCard(&b, "bad", "失败", totals.TotalFailed)
	statCard(&b, "", "输入 Tokens", totals.TotalPrompt)
	statCard(&b, "", "输出 Tokens", totals.TotalCompletion)
	b.WriteString(`</div>`)

	b.WriteString(`<div class="box">`)
	b.WriteString(`<header><h3>用量趋势 <span class="hint">最近 7 天</span></h3><span class="grow"></span>`)
	b.WriteString(`<button type="button" class="xs" data-call="refreshUsage">刷新</button></header>`)
	b.WriteString(`<div id="usageTrend" class="trend"><div class="empty">正在加载…</div></div>`)
	b.WriteString(`<div class="trend-legend">`)
	b.WriteString(`<span><i class="sw sw-ok"></i>成功</span>`)
	b.WriteString(`<span><i class="sw sw-bad"></i>失败</span>`)
	b.WriteString(`</div>`)
	b.WriteString(`</div>`)

	b.WriteString(`<div class="box">`)
	b.WriteString(`<header><h3>最近调用 <span class="hint">` + fmt.Sprint(len(recent)) + ` 条</span></h3></header>`)
	if len(recent) == 0 {
		b.WriteString(`<div class="empty">暂无调用记录。发起一次请求后这里会出现明细。</div>`)
	} else {
		b.WriteString(renderCallTable(recent))
	}
	b.WriteString(`</div>`)

	b.WriteString(`</section>`)
	return b.String()
}

// renderTasksView is the tasks page: growth tasks, check-in and the travel routine.
func renderTasksView() string {
	accounts := listWorkBuddyAccounts()
	running, queued := taskQueueDepth()

	var b strings.Builder
	b.WriteString(`<section class="view" id="view-tasks" hidden>`)

	// Runs and the controls that start them share one card: they answer one question
	// — what is scheduled, and how do I run it.
	b.WriteString(`<div class="box">`)
	b.WriteString(`<header><h3>任务执行</h3><span class="grow"></span>`)
	// Every action on this page reports through this element; without it the messages
	// had nowhere to go and the buttons looked like they did nothing.
	b.WriteString(`<span class="note" id="taskMsg"></span>`)
	b.WriteString(`<button type="button" class="xs primary" id="btnRunAllTasks" data-call="runAllTasks">全部执行</button>`)
	b.WriteString(`<button type="button" class="xs" id="btnRunGrowth" data-call="runGrowthTasks">成长任务</button>`)
	b.WriteString(`<button type="button" class="xs" id="btnTravel" data-call="runTravel">猫猫旅行</button>`)
	b.WriteString(`</header>`)
	b.WriteString(`<div class="stats" style="border:none;border-radius:0;box-shadow:none;margin:0">`)
	statCard(&b, "", "账号数", len(accounts))
	statCard(&b, "", "正在运行", running)
	statCard(&b, "", "排队中", queued)
	b.WriteString(`</div>`)
	b.WriteString(`<div class="pad"><div class="note">「全部执行」会依次完成成长任务、签到与猫猫旅行。` +
		`需要真实桌面操作的任务（如资料库、发现应用）无法代做，会列出深链提示；` +
		`国际版账号不在成长任务中心范围内，会自动跳过。</div></div>`)
	b.WriteString(`<div id="taskResult" style="padding:0 16px 16px"><div id="growthDetail"></div></div>`)
	b.WriteString(`<div class="pad" style="padding-top:0"><span class="note" id="growthMsg"></span></div>`)
	b.WriteString(`</div>`)

	// Scheduled runs first, then the manual check-in: both are "what happens on its
	// own", and the schedule is the thing an operator sets up once.
	b.WriteString(renderGrowthScheduleBox())

	// Check-in sits with the other scheduled work.
	b.WriteString(renderCheckinBox())

	// Per-account task state, with its own controls in its own header.
	b.WriteString(renderTaskAccountsBox(accounts))

	b.WriteString(`</section>`)
	return b.String()
}

// renderSettingsView holds the management key and the authorisation switches.
func renderSettingsView() string {
	settings := state.settings.get()

	var b strings.Builder
	b.WriteString(`<section class="view" id="view-settings" hidden>`)

	// Routing is a policy, not a property of the account list, so it belongs with the
	// other settings.
	b.WriteString(renderRoutingBox(routingStatusJSON()))

	b.WriteString(`<div class="box">`)
	b.WriteString(`<header><h3>管理密钥 <span class="hint">仅保存在本机浏览器</span></h3></header>`)
	b.WriteString(`<div class="pad">`)
	b.WriteString(`<div class="row"><input type="password" id="mgmtKey" placeholder="CPA management key" style="flex:1 1 300px">`)
	b.WriteString(`<button type="button" class="primary" data-call="saveKey">保存到浏览器</button>`)
	b.WriteString(`<button type="button" class="ghost" data-call="clearKey">清除</button></div>`)
	b.WriteString(`<div class="note" id="keyState" style="margin-top:9px"></div>`)
	b.WriteString(`<div class="note" style="margin-top:9px">密钥仅存在本机 localStorage，随请求头发送，不经过插件。` +
		`与 CPA 面板使用同一个 management key。</div>`)
	b.WriteString(`</div></div>`)

	b.WriteString(renderVariantBox(settings))

	b.WriteString(`</section>`)
	return b.String()
}

// statCard appends one cell of the summary strip. tone is "", "good", "warn" or "bad".
func statCard(b *strings.Builder, tone, label string, value any) {
	cls := "stat"
	if tone != "" {
		cls += " " + tone
	}
	b.WriteString(`<div class="` + cls + `"><div class="v">` + html.EscapeString(fmt.Sprint(value)) +
		`</div><div class="k">` + html.EscapeString(label) + `</div></div>`)
}

// --- helpers shared with the page builders ---

// accountsSignature is a cheap fingerprint of the list, compared by the script to
// decide whether the rendered table is out of date.
//
// Format: "<total>:<usable>:<uid><flag>,<uid><flag>,…" where flag is "D" for an
// account taken out of rotation and "E" otherwise. It must stay byte-identical to the
// string the script builds in pollAccounts — a mismatch would make every poll think
// the inventory changed and reload the page on a timer.
func accountsSignature(accounts []workBuddyAccount) string {
	parts := make([]string, 0, len(accounts))
	usable := 0
	for _, a := range accounts {
		if a.Usable {
			usable++
		}
		flag := "E"
		if a.DisabledByUser {
			flag = "D"
		}
		ident := a.UID
		if ident == "" {
			ident = a.AuthIndex
		}
		parts = append(parts, ident+flag)
	}
	return fmt.Sprint(len(accounts)) + ":" + fmt.Sprint(usable) + ":" + strings.Join(parts, ",")
}

// accountCounts returns the total and the number currently usable.
func accountCounts(accounts []workBuddyAccount) (int, int) {
	usable := 0
	for _, a := range accounts {
		if a.Usable {
			usable++
		}
	}
	return len(accounts), usable
}

// accountCoolingCount counts accounts parked by a cooldown.
func accountCoolingCount(accounts []workBuddyAccount) int {
	n := 0
	now := time.Now()
	for _, a := range accounts {
		if !a.CooldownUntil.IsZero() && now.Before(a.CooldownUntil) {
			n++
		}
	}
	return n
}

// accountDisabledCount counts accounts taken out of rotation, by the operator or by
// the pool itself.
func accountDisabledCount(accounts []workBuddyAccount) int {
	n := 0
	for _, a := range accounts {
		if a.Disabled || a.DisabledByUser || a.AutoDisabled {
			n++
		}
	}
	return n
}

// accountCreditsTotal sums the known credit balances.
func accountCreditsTotal(accounts []workBuddyAccount) int64 {
	var total int64
	for _, a := range accounts {
		if a.CreditsKnown {
			total += a.Credits
		}
	}
	return total
}
