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
		{"view-usage", "记录"},
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
	b.WriteString(`<header><h3>用量趋势</h3><span class="grow"></span>`)
	// Range picks the window the bars cover. The data arrives as two series (per hour,
	// per day) and the buttons choose which one to cut — no re-fetch, so switching is
	// instant.
	b.WriteString(`<div class="seg seg-sm" id="trendRange">`)
	for i, opt := range []struct{ v, label, title string }{
		{"day", "1 天", "最近 24 小时，按小时"},
		{"3day", "3 天", "最近 3 天，按天"},
		{"week", "7 天", "最近 7 天，按天"},
	} {
		cls := ""
		if i == 0 {
			cls = "on"
		}
		b.WriteString(`<button type="button" class="` + cls + `" data-trend-range="` + opt.v +
			`" title="` + opt.title + `">` + opt.label + `</button>`)
	}
	b.WriteString(`</div>`)
	b.WriteString(`<button type="button" class="xs" data-call="refreshUsage">刷新</button>`)
	b.WriteString(`</header>`)
	b.WriteString(`<div id="usageTrend" class="trend"><div class="empty">正在加载…</div></div>`)
	b.WriteString(`<div class="trend-legend">`)
	b.WriteString(`<span><i class="sw sw-ok"></i>成功</span>`)
	b.WriteString(`<span><i class="sw sw-bad"></i>失败</span>`)
	b.WriteString(`<span class="grow"></span>`)
	b.WriteString(`<span class="note" id="trendCaption"></span>`)
	b.WriteString(`</div>`)
	b.WriteString(`</div>`)

	// Two lists, one card. Calls and operational notes answer different questions —
	// "what did the models serve" versus "what did the plugin itself do" — and mixing
	// them made each harder to read, so they get a tab each. Calls come first because
	// that is what the page is usually opened for.
	calls := state.log.modelCallsOnly(logListLimit)
	notes := collectRequestLog(logListLimit)

	b.WriteString(`<div class="box">`)
	b.WriteString(`<header><h3>记录</h3>`)
	b.WriteString(`<div class="seg seg-sm" id="logTab">`)
	b.WriteString(`<button type="button" class="on" data-log-tab="calls">调用记录</button>`)
	b.WriteString(`<button type="button" class="" data-log-tab="notes">请求日志</button>`)
	b.WriteString(`</div>`)
	b.WriteString(`<span class="grow"></span>`)
	b.WriteString(`<span class="note" id="logMsg"></span>`)
	// Clearing only the notes. The call list is the accounting the rest of the page is
	// built from, so throwing it away would leave the totals describing records that are
	// no longer there.
	b.WriteString(`<button type="button" class="xs danger" data-call="clearRequestLog">清空日志</button>`)
	b.WriteString(`</header>`)

	b.WriteString(`<div class="log-pane" id="logPaneCalls">`)
	if len(calls) == 0 {
		b.WriteString(`<div class="empty">暂无调用记录。发起一次请求后这里会出现明细。</div>`)
	} else {
		b.WriteString(renderCallTable(calls))
	}
	b.WriteString(`</div>`)

	b.WriteString(`<div class="log-pane" id="logPaneNotes" hidden>`)
	if len(notes) == 0 {
		b.WriteString(`<div class="empty">暂无请求日志。签到、任务、限流与禁用等事件会记在这里。</div>`)
	} else {
		b.WriteString(renderNoteTable(notes))
	}
	b.WriteString(`</div>`)

	b.WriteString(`</div>`)

	b.WriteString(`</section>`)
	return b.String()
}

// renderTasksView is the tasks page.
//
// Two cards: what the plugin does on its own, and what each account has done. The
// automatic schedule and the manual triggers used to be two cards with two rows of
// buttons — six controls for one question ("run this"), so the operator had to work out
// which button belonged to which card before pressing anything. They are now one card:
// the schedule on top, the immediate actions beneath it.
func renderTasksView() string {
	accounts := listWorkBuddyAccounts()
	running, queued := taskQueueDepth()

	var b strings.Builder
	b.WriteString(`<section class="view" id="view-tasks" hidden>`)

	b.WriteString(`<div class="box">`)
	b.WriteString(`<header><h3>任务</h3><span class="grow"></span>`)
	b.WriteString(renderRunSummaryChips(len(accounts), running, queued))
	b.WriteString(`<span class="note" id="taskMsg"></span>`)
	b.WriteString(`</header>`)

	// ---- automatic ----
	// Description first, then the two jobs side by side, then the save action at the
	// card's bottom right. The label column is dropped: the heading already says what
	// this block is, and a paragraph beside it only squeezed the controls.
	b.WriteString(`<div class="card-block">`)
	b.WriteString(`<div class="block-head">`)
	b.WriteString(`<span class="name">每天自动执行</span>`)
	b.WriteString(`<span class="desc">按本机时区判断日期，同一天各跑一次。</span>`)
	b.WriteString(`</div>`)
	b.WriteString(renderScheduleColumns())
	b.WriteString(`<div class="sched-foot">`)
	b.WriteString(`<span class="note">「启动时补跑」指插件加载时若当天尚未执行则补上一次。</span>`)
	b.WriteString(`<button type="button" class="xs primary" data-call="saveSchedule">保存定时</button>`)
	b.WriteString(`</div>`)
	b.WriteString(`</div>`)

	// ---- manual ----
	b.WriteString(`<div class="card-block">`)
	b.WriteString(`<div class="block-head">`)
	b.WriteString(`<span class="name">立即执行</span>`)
	b.WriteString(`<span class="desc">不想等到设定时间时用这里的按钮。</span>`)
	b.WriteString(`</div>`)
	// Description on the left, actions pushed to the right edge on the same line.
	b.WriteString(`<div class="action-row">`)
	b.WriteString(`<span class="note">「全部执行」依次完成成长任务、签到与猫猫旅行。</span>`)
	b.WriteString(`<span class="grow"></span>`)
	b.WriteString(`<button type="button" class="primary" data-call="runAllTasks">全部执行</button>`)
	b.WriteString(`<button type="button" class="xs" data-call="runGrowthTasks">成长任务</button>`)
	b.WriteString(`<button type="button" class="xs" data-call="runTravel">猫猫旅行</button>`)
	b.WriteString(`<button type="button" class="xs" data-call="runCheckin">立即签到</button>`)
	b.WriteString(`</div>`)
	b.WriteString(`<div class="empty" id="taskResult" hidden></div>`)
	b.WriteString(`</div>`)

	b.WriteString(`</div>`)

	// Per-account state, with its tasks underneath each row.
	b.WriteString(renderTaskAccountsBox(accounts))

	b.WriteString(`</section>`)
	return b.String()
}

// renderRunSummaryChips renders the three counters as inline chips.
func renderRunSummaryChips(accounts, running, queued int) string {
	var b strings.Builder
	b.WriteString(runSummaryChip("账号", accounts, ""))
	b.WriteString(runSummaryChip("执行中", running, runTone(running)))
	b.WriteString(runSummaryChip("排队", queued, runTone(queued)))
	return b.String()
}

// runTone picks the chip colour for a run counter: a non-zero value is worth noticing.
func runTone(n int) string {
	if n > 0 {
		return "warn"
	}
	return ""
}

// runSummaryChip renders one figure with its label.
func runSummaryChip(label string, value int, tone string) string {
	cls := "run-chip"
	if tone != "" {
		cls += " " + tone
	}
	return `<span class="` + cls + `"><span class="v">` + fmt.Sprint(value) +
		`</span><span class="k">` + label + `</span></span>`
}

// renderSettingsView holds the management key and the authorisation switches.
func renderSettingsView() string {
	settings := state.settings.get()

	var b strings.Builder
	b.WriteString(`<section class="view" id="view-settings" hidden>`)

	// Three groups, in the order they matter: how calls are served, who may reach the
	// panel, and which supplier a new authorisation belongs to. The page used to be a
	// flat run of cards, so it was not obvious which settings were related.
	b.WriteString(`<div class="group-head"><h2>调用与路由</h2>` +
		`<span class="desc">决定一次模型调用如何挑选账号</span></div>`)
	b.WriteString(renderRoutingBox(routingStatusJSON()))
	b.WriteString(renderVariantBox(settings))

	b.WriteString(`<div class="group-head"><h2>面板访问</h2>` +
		`<span class="desc">浏览器如何向 CPA 证明自己的身份</span></div>`)
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
