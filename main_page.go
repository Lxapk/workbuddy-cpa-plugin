package main

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

// renderTaskPage builds the task centre tab content.
// renderCheckinCards builds the daily sign-in card and its last-run summary.
//
// Split out so the tasks tab can include it without duplicating the markup: the
// panel used to be a tab of its own, and an operator looking at "what is
// scheduled" wants the daily sign-in next to the growth tasks.
func renderCheckinCards(settings gatewaySettings, history []checkinRun) string {
	var b strings.Builder

	b.WriteString(`<div class="card"><h2>每日签到 <span class="hint">自动签到与上次结果</span></h2>`)
	b.WriteString(`<div class="row tight"><label class="field"><input type="checkbox" id="ckEnabled"`)
	if settings.Checkin.Enabled {
		b.WriteString(` checked`)
	}
	b.WriteString(`> 启用每日自动签到</label></div>`)
	b.WriteString(`<div class="row tight"><label class="field">每天 <input type="number" id="ckHour" min="0" max="23" value="` +
		fmt.Sprint(clampHour(settings.Checkin.Hour)) + `"> 时 <input type="number" id="ckMinute" min="0" max="59" value="` +
		fmt.Sprint(clampMinute(settings.Checkin.Minute)) + `"> 分执行</label></div>`)
	b.WriteString(`<div class="row tight"><label class="field"><input type="checkbox" id="ckOnStart"`)
	if settings.Checkin.OnStart {
		b.WriteString(` checked`)
	}
	b.WriteString(`> 启动时补跑（当天尚未执行时）</label></div>`)
	b.WriteString(`<div class="row"><button type="button" data-call="saveCheckinSettings">保存</button>`)
	b.WriteString(`<button type="button" class="ghost" data-call="runCheckin">立即签到</button></div>`)
	b.WriteString(`</div>`)

	b.WriteString(`<div class="card"><h2>最近一次签到</h2>`)
	if len(history) == 0 {
		b.WriteString(`<div class="empty">还没有签到记录。</div>`)
	} else {
		b.WriteString(renderRun(history[0]))
	}
	b.WriteString(`</div>`)

	return b.String()
}

// renderUsageTrend draws the last few days of traffic as a bar chart.
//
// The chart is filled in by JavaScript rather than rendered here: the table below
// it refreshes on a timer, and a server-rendered chart would freeze at page load
// while the numbers next to it moved. Hand-drawn SVG rather than a charting
// library — the panel is embedded and stays dependency-free.
func renderUsageTrend() string {
	var b strings.Builder
	b.WriteString(`<div class="card"><h2>用量趋势 <span class="hint">最近 7 天，按天汇总</span></h2>`)
	b.WriteString(`<div id="usageTrend" class="trend"><div class="empty">正在加载…</div></div>`)
	b.WriteString(`<div class="muted small trend-legend">`)
	b.WriteString(`<span><i class="sw sw-ok"></i>成功</span>`)
	b.WriteString(`<span><i class="sw sw-bad"></i>失败</span>`)
	b.WriteString(`<span class="trend-note">仅统计经本插件转发的调用</span>`)
	b.WriteString(`</div></div>`)
	return b.String()
}

func renderTaskPage() string {
	// Populate the task engine with the current account list first, otherwise
	// a freshly started plugin shows an empty task table.
	state.taskEngine.initFromAccounts(listWorkBuddyAccounts())
	status := taskStatusJSON()
	accounts, _ := status["accounts"].([]map[string]any)
	running, _ := status["running"].(int)
	queued, _ := status["queued"].(int)

	var b strings.Builder
	b.WriteString(`<div id="tab-tasks" class="wb-panel">`)
	b.WriteString(`<div class="card"><h2>任务列表</h2><div class="grid stats">`)
	stat := func(k string, v any) {
		b.WriteString(`<div class="stat"><div class="v">` + fmt.Sprint(v) + `</div><div class="k">` + k + `</div></div>`)
	}
	stat("账号数", len(accounts))
	stat("正在运行", running)
	stat("排队中", queued)
	b.WriteString(`</div></div>`)

	// The daily sign-in sits between the task list and the run controls: it is
	// scheduled work like the rest of this page, and putting it above the buttons
	// means it reads as one more thing that runs rather than an afterthought
	// appended below the actions.
	b.WriteString(renderCheckinCards(state.settings.get(), state.checkin.snapshot(1)))

	b.WriteString(`<div class="card"><div class="row">`)
	b.WriteString(`<button type="button" id="btnRunAllTasks" data-call="runAllTasks">全部执行</button>`)
	b.WriteString(`<button type="button" class="ghost" id="btnRunGrowth" data-call="runGrowthTasks">完成成长任务</button>`)
	b.WriteString(`<button type="button" class="ghost" id="btnTravel" data-call="runTravel">猫猫旅行</button>`)
	b.WriteString(`<span class="muted small" id="taskMsg"></span>`)
	b.WriteString(`</div>`)
	b.WriteString(`<div class="note">「完成成长任务」会自动接取、点亮并领取每日成长任务奖励，顺带检查猫猫旅行。` +
		`需要真实桌面操作的任务（如资料库、发现应用）无法代做，会列出深链提示；国际版账号不在成长任务中心范围内，会自动跳过。</div>`)
	b.WriteString(`</div>`)
	b.WriteString(`<div id="taskResult"></div>`)

	b.WriteString(renderGrowthSection())

	b.WriteString(`<div class="card"><h2>账号任务状态</h2>`)
	if len(accounts) == 0 {
		b.WriteString(`<div class="empty">还没有账号。</div>`)
	} else {
		b.WriteString(`<div class="table-wrap"><table><thead><tr><th>账号</th><th>启用</th><th>任务</th><th>上次</th><th>结果</th></tr></thead><tbody>`)
		for _, acct := range accounts {
			uid, _ := acct["uid"].(string)
			label, _ := acct["label"].(string)
			enabled, _ := acct["enabled"].(bool)
			queuedFlag, _ := acct["queued"].(bool)
			inflight, _ := acct["inflight"].(int)
			enableCls := "ok"
			enableText := "启用"
			if !enabled {
				enableCls = "muted"
				enableText = "禁用"
			}
			b.WriteString(`<tr><td><strong>` + html.EscapeString(label) + `</strong></td>`)
			btnCls := "pill " + enableCls
			// The button offers the opposite of the current state, so an enabled
			// account gets "disable". The previous code derived the action from
			// the current state instead, which made every click a no-op.
			action := "enable"
			if enabled {
				action = "disable"
			}
			b.WriteString(`<td><span class="` + btnCls + `" style="cursor:pointer" ` +
				`data-task-toggle="1" data-uid="` + html.EscapeString(uid) + `" data-action="` + action + `">` +
				enableText + `</span></td>`)
			if queuedFlag || inflight > 0 {
				b.WriteString(`<td colspan="3"><span class="pill warn">` + map[bool]string{true: "排队中", false: "运行中"}[queuedFlag] + `</span></td>`)
				b.WriteString(`</tr>`)
				continue
			}
			b.WriteString(`<td>`)
			tasks, _ := acct["tasks"].([]map[string]any)
			for _, t := range tasks[:min(len(tasks), 1)] {
				label, _ := t["label"].(string)
				b.WriteString(html.EscapeString(label))
			}
			b.WriteString(`</td><td class="mono muted">`)
			for _, t := range tasks[:min(len(tasks), 1)] {
				lr, _ := t["last_run"].(string)
				if lr != "" && len(lr) > 16 {
					b.WriteString(html.EscapeString(lr[11:16]))
				} else {
					b.WriteString("—")
				}
			}
			b.WriteString(`</td><td>`)
			for _, t := range tasks[:min(len(tasks), 1)] {
				lr, _ := t["last_result"].(string)
				lok, _ := t["last_ok"].(bool)
				cls := map[bool]string{true: "ok", false: "bad"}[lok]
				b.WriteString(`<span class="pill ` + cls + `">` + html.EscapeString(firstNonEmpty(lr, "—")) + `</span>`)
			}
			b.WriteString(`</td></tr>`)
		}
		b.WriteString(`</tbody></table></div>`)
	}
	b.WriteString(`</div></div>`)
	return b.String()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// renderMainPage builds the plugin's single management page.
//
// Everything the operator needs lives here behind a tab bar — accounts,
// switching strategy, check-in, credits, usage and gateway settings. Before
// this, each feature had its own resource route and menu entry, which meant
// hopping between pages to do one job.
//
// The markup is intentionally plain: CPA renders these pages inside its own
// panel, so the layout must be responsive and must not assume it owns the
// viewport. Styling comes from uiCSS.
func renderMainPage() string {
	settings := state.settings.get()
	accounts := listWorkBuddyAccounts()
	total, usable, known, credits := accountSummary(accounts)
	totals := state.log.totals()

	state.quota.mu.Lock()
	quotaRunning := state.quota.running
	state.quota.mu.Unlock()
	state.checkin.mu.Lock()
	checkinRunning := state.checkin.running
	state.checkin.mu.Unlock()

	recentCalls := state.log.recent(12)

	var b strings.Builder
	b.WriteString(`<!doctype html><html lang="zh-CN"><head><meta charset="utf-8">`)
	b.WriteString(`<meta name="viewport" content="width=device-width,initial-scale=1">`)
	b.WriteString(`<title>WorkBuddy</title><style>` + uiCSS + `</style></head><body><div class="root">`)

	// ---------------- hero ----------------
	b.WriteString(`<div class="hero"><div>`)
	b.WriteString(`<h1>WorkBuddy</h1>`)
	b.WriteString(`<div class="sub">把 WorkBuddy 账号反代为 CPA 的 OpenAI 兼容接口：账号轮换、模型输出、签到与积分管理。</div>`)
	b.WriteString(`</div><div class="badge"><span class="dot"></span>`)
	if quotaRunning || checkinRunning {
		b.WriteString(`任务执行中…`)
	} else {
		b.WriteString(fmt.Sprintf(`%d 个账号可用`, usable))
	}
	b.WriteString(`</div></div>`)

	// ---------------- tabs ----------------
	//
	// The buttons carry a data attribute and are wired by a delegated listener
	// rather than an inline onclick. An inline handler is blocked by a Content
	// Security Policy that disallows inline script, and then nothing happens at
	// all when a tab is pressed — the page stays on whichever panel was already
	// marked active. Only the account panel is active in the markup, so the
	// symptom is precisely "the other tabs show nothing".
	b.WriteString(`<div class="tabs">`)
	tab := func(id, label string, first bool) {
		cls := ""
		if first {
			cls = ` active`
		}
		b.WriteString(`<button type="button" class="tab` + cls + `" data-tab="` + html.EscapeString(id) + `">` +
			html.EscapeString(label) + `</button>`)
	}
	tab("tab-accounts", "账号", true)
	tab("tab-switch", "账号切换", false)
	tab("tab-credits", "积分", false)
	tab("tab-usage", "统计", false)
	tab("tab-tasks", "任务", false)
	tab("tab-settings", "设置", false)
	b.WriteString(`</div>`)

	// ---------------- tab: accounts ----------------
	b.WriteString(`<div id="tab-accounts" class="wb-panel active">`)
	b.WriteString(`<div class="grid stats">`)
	stat := func(k string, v any) {
		b.WriteString(`<div class="stat"><div class="v">` +
			html.EscapeString(fmt.Sprint(v)) + `</div><div class="k">` +
			html.EscapeString(k) + `</div></div>`)
	}
	stat("账号总数", total)
	stat("可用账号", usable)
	creditsText := "—"
	if known > 0 {
		creditsText = fmt.Sprint(credits)
	}
	stat("积分合计", creditsText)
	stat("已查积分", fmt.Sprintf("%d / %d", known, total))
	b.WriteString(`</div>`)

	b.WriteString(`<div class="card"><h2>账号列表 <span class="hint">登录后自动出现</span></h2>`)
	b.WriteString(`<input type="hidden" id="accountsSignature" value="` + html.EscapeString(accountsSignature(accounts)) + `">`)
	b.WriteString(`<div id="accountMsg"></div>`)
	if len(accounts) > 0 {
		// Filter bar. Rendering is client-side because the list is already on the
		// page: a round trip per keystroke would be slower and would lose the
		// focus that the operator is typing into.
		//
		// The input takes the flexible width and the controls after it size to their
		// content, so the bar fills the card instead of leaving a gap on the right.
		b.WriteString(`<div class="filter-bar">`)
		b.WriteString(`<span class="filter-search">`)
		b.WriteString(`<svg class="filter-icon" viewBox="0 0 16 16" width="14" height="14" aria-hidden="true" focusable="false">` +
			`<circle cx="7" cy="7" r="4.4" fill="none" stroke="currentColor" stroke-width="1.7"/>` +
			`<path d="M10.4 10.4L14 14" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round"/></svg>`)
		b.WriteString(`<input type="search" id="accountFilter" placeholder="搜索账号或备注" autocomplete="off">`)
		b.WriteString(`<button type="button" class="filter-clear" id="accountFilterClear" title="清除" aria-label="清除搜索" hidden>` +
			`<svg viewBox="0 0 16 16" width="12" height="12" aria-hidden="true" focusable="false">` +
			`<path d="M4 4l8 8M12 4l-8 8" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round"/></svg>` +
			`</button>`)
		b.WriteString(`</span>`)
		b.WriteString(`<select id="accountStatusFilter" class="filter-select" aria-label="按状态筛选">`)
		for _, option := range []struct{ value, label string }{
			{"", "全部状态"},
			{"usable", "仅可用"},
			{"cooling", "仅冷却中"},
			{"disabled", "仅已停用"},
			{"expired", "仅凭据异常"},
		} {
			b.WriteString(`<option value="` + option.value + `">` + option.label + `</option>`)
		}
		b.WriteString(`</select>`)
		// Refresh moves next to the filter controls, where it acts on what is shown.
		// It fetches the credit readings too, so a single press answers "which
		// accounts do I have" and "what are they worth" at once.
		b.WriteString(`<button type="button" class="ghost" data-call="refreshAccountsAndQuota" ` +
			`title="重新读取账号列表并刷新积分">刷新</button>`)
		b.WriteString(`<span class="filter-count muted small" id="accountFilterCount"></span>`)
		b.WriteString(`</div>`)
	}
	if len(accounts) == 0 {
		// Distinguish "the host could not be read" from "there really is no
		// account". Reporting the latter while the former is true sends the
		// operator looking for a login problem that does not exist.
		if warn := state.accounts.lastError(); warn != "" {
			b.WriteString(`<div class="empty">账号列表读取失败，无法判断是否已有账号。请查看下方错误详情后重试。</div>`)
		} else {
			b.WriteString(`<div class="empty">还没有 WorkBuddy 账号。<br>` +
				`请在 CPA 管理面板的 <strong>认证 / Auth</strong> 页选择 <strong>WorkBuddy</strong> 登录；` +
				`登录完成后本列表会在数秒内自动出现该账号。</div>`)
		}
	} else {
		// Grouped by realm. A mixed pool is the normal case now, and the two
		// halves behave differently (no check-in internationally, no growth
		// centre either), so showing one flat list hid which accounts a given
		// feature would actually act on.
		cnAccounts, aiAccounts := splitAccountsByVariant(accounts)
		b.WriteString(`<div class="muted small" style="margin:.2rem 0 .6rem">共 ` +
			fmt.Sprint(len(accounts)) + ` 个账号：国内供应商 ` + fmt.Sprint(len(cnAccounts)) +
			` 个，国际供应商 ` + fmt.Sprint(len(aiAccounts)) + ` 个。` +
			`「供应商切换」为全部供应商时两组都会参与调用；选定某一侧时仅该组参与。</div>`)
		b.WriteString(renderAccountGroup("国内供应商账号", "cn", cnAccounts))
		b.WriteString(renderAccountGroup("国际供应商账号", "ai", aiAccounts))
	}
	if warn := state.accounts.lastError(); warn != "" {
		b.WriteString(`<div class="note bad">读取账号列表失败：` + html.EscapeString(warn) + `</div>`)
	}
	b.WriteString(`</div>`)

	b.WriteString(`<div class="card"><h2>一键操作</h2><div class="row">`)
	b.WriteString(`<button type="button" id="btnRun" data-call="runAll"`)
	if quotaRunning || checkinRunning {
		b.WriteString(` disabled`)
	}
	b.WriteString(`>签到 + 刷新积分</button>`)
	b.WriteString(`<button type="button" class="ghost" data-call="refreshAccounts">刷新列表</button>`)
	b.WriteString(`<span class="muted small" id="runMsg"></span></div>`)
	b.WriteString(`<div id="runResult"></div></div>`)
	b.WriteString(`</div>`)

	// ---------------- tab: switching strategy ----------------
	routing := routingStatusJSON()
	b.WriteString(`<div id="tab-switch" class="wb-panel">`)
	b.WriteString(`<div class="card"><h2>账号切换策略 <span class="hint">请求如何在这些账号之间分配</span></h2>`)
	options, _ := routing["options"].([]map[string]any)
	current, _ := routing["strategy"].(string)
	for _, opt := range options {
		value, _ := opt["value"].(string)
		label, _ := opt["label"].(string)
		desc, _ := opt["description"].(string)
		b.WriteString(`<label class="opt"><input type="radio" name="strategy" value="` +
			html.EscapeString(value) + `"`)
		if value == current {
			b.WriteString(` checked`)
		}
		b.WriteString(`><span><span class="name">` + html.EscapeString(label) + `</span><br>` +
			`<span class="desc">` + html.EscapeString(desc) + `</span></span></label>`)
	}
	b.WriteString(`<div class="row">`)
	b.WriteString(`<button type="button" data-call="saveStrategy">应用策略</button>`)
	b.WriteString(`<button type="button" class="ghost" data-call="resetRotation">重置轮巡位置</button>`)
	b.WriteString(`<span class="muted small" id="strategyMsg"></span></div>`)
	b.WriteString(`<div class="note">当前：<b>` + html.EscapeString(fmt.Sprint(routing["strategy_label"])) +
		`</b> · ` + html.EscapeString(nextRotationHint()) + `</div>`)
	b.WriteString(`</div>`)

	if rows, okRows := routing["order"].([]map[string]any); okRows && len(rows) > 0 {
		b.WriteString(`<div class="card"><h2>选择顺序预览</h2>`)
		b.WriteString(`<div class="table-wrap"><table><thead><tr><th class="num">#</th><th>账号</th><th class="num">积分</th><th class="num">已选中</th></tr></thead><tbody>`)
		for _, row := range rows {
			cv := "—"
			if knownValue, _ := row["known"].(bool); knownValue {
				cv = fmt.Sprint(row["credits"])
			}
			b.WriteString(`<tr><td class="num">` + fmt.Sprint(row["position"]) + `</td>`)
			b.WriteString(`<td>` + html.EscapeString(fmt.Sprint(row["label"])) + `</td>`)
			b.WriteString(`<td class="num">` + html.EscapeString(cv) + `</td>`)
			b.WriteString(`<td class="num">` + fmt.Sprint(row["picks"]) + `</td></tr>`)
		}
		// Closes the scroll wrapper, then the card. The card is opened above and the
		// wrapper inside it; leaving the card open here makes this panel one level
		// short of closed, and every later tab ends up nested inside it.
		b.WriteString(`</tbody></table></div>`)
		b.WriteString(`</div>`)
	} else {
		// The card closes on the same line: an empty state does not need a wrapper,
		// and adding one here would need a matching close further down that the
		// populated branch does not have.
		b.WriteString(`<div class="card"><div class="empty">暂无可用账号，无法预览顺序。</div></div>`)
	}
	// Closes the tab-switch panel itself.
	b.WriteString(`</div>`)

	// ---------------- check-in (lives inside the tasks tab) ----------------
	//
	// Check-in is a task, so it belongs on the tasks tab rather than in a tab of
	// its own: an operator looking at "what is scheduled" wants the daily sign-in
	// next to the growth tasks, not one tab away.
	//
	// The markup is emitted here but placed by renderIntoTaskTab below, which keeps
	// the two halves of the panel in one place while still producing a single
	// container in the output.
	// ---------------- tab: credits ----------------
	b.WriteString(`<div id="tab-credits" class="wb-panel">`)
	b.WriteString(`<div class="card"><h2>自动刷新积分</h2>`)
	b.WriteString(`<div class="row tight"><label class="field"><input type="checkbox" id="qEnabled"`)
	if settings.Quota.Enabled {
		b.WriteString(` checked`)
	}
	b.WriteString(`> 启用定时刷新积分</label></div>`)
	b.WriteString(`<div class="row tight"><label class="field">每 <input type="number" id="qInterval" min="5" max="1440" value="` +
		fmt.Sprint(clampIntervalMinutes(settings.Quota.IntervalMinutes)) + `"> 分钟刷新一次</label></div>`)
	b.WriteString(`<div class="row tight"><label class="field"><input type="checkbox" id="qOnStart"`)
	if settings.Quota.RefreshOnStart {
		b.WriteString(` checked`)
	}
	b.WriteString(`> 启动时刷新一次</label></div>`)
	b.WriteString(`<div class="row"><button type="button" data-call="saveQuotaSettings">保存</button>`)
	b.WriteString(`<button type="button" class="ghost" data-call="refreshQuota">立即刷新积分</button></div>`)
	b.WriteString(`<div class="note">积分决定账号选用顺序：源应用按剩余积分从多到少选用。</div>`)
	b.WriteString(`</div>`)

	b.WriteString(`<div class="card"><h2>账号积分</h2>`)
	state.quota.mu.Lock()
	lastRun := append([]quotaRefreshResult(nil), state.quota.lastRun...)
	state.quota.mu.Unlock()
	// Wrapped in a container the script can replace after a refresh, so the table
	// updates in place instead of the whole page reloading.
	b.WriteString(`<div id="quotaResults">`)
	if len(lastRun) == 0 {
		b.WriteString(`<div class="empty">点「立即刷新积分」查询各账号的剩余积分与到期时间。</div>`)
	} else {
		b.WriteString(renderQuotaResults(lastRun))
	}
	b.WriteString(`</div>`)
	b.WriteString(`<div class="muted small" id="quotaMsg"></div>`)
	b.WriteString(`</div></div>`)

	// ---------------- tab: usage ----------------
	b.WriteString(`<div id="tab-usage" class="wb-panel">`)
	b.WriteString(`<div class="grid stats">`)
	stat("总调用", totals.TotalCalls)
	stat("今日", totals.TodayCalls)
	stat("失败", totals.TotalFailed)
	stat("输入 Tokens", totals.TotalPrompt)
	stat("输出 Tokens", totals.TotalCompletion)
	b.WriteString(`</div>`)
	b.WriteString(renderUsageTrend())
	b.WriteString(`<div class="card"><h2>最近调用</h2>`)
	if len(recentCalls) == 0 {
		b.WriteString(`<div class="empty">暂无调用记录。</div>`)
	} else {
		b.WriteString(`<div class="table-wrap"><table><thead><tr><th>时间</th><th>供应商</th><th>账号</th><th>模型</th><th class="num">状态</th><th class="num">Tokens</th></tr></thead><tbody>`)
		for _, rec := range recentCalls {
			cls := "ok"
			if rec.StatusCode >= 400 || rec.Error != "" {
				cls = "bad"
			}
			b.WriteString(`<tr><td class="mono">` + rec.StartedAt.Local().Format("15:04:05") + `</td>`)
			// The realm, not the provider key: both realms share "codebuddy",
			// so that column alone could not tell them apart.
			pillClass := "idle"
			switch rec.Variant {
			case "ai":
				pillClass = "warn"
			case "cn":
				pillClass = "ok"
			}
			b.WriteString(`<td><span class="pill ` + pillClass + `">` + html.EscapeString(variantLabelOrDash(rec.Variant)) + `</span></td>`)
			b.WriteString(`<td class="mono small">` + html.EscapeString(firstNonEmpty(rec.Label, rec.UID, "—")) + `</td>`)
			b.WriteString(`<td><code>` + html.EscapeString(rec.Model) + `</code></td>`)
			b.WriteString(`<td class="num ` + cls + `">` + fmt.Sprint(rec.StatusCode) + `</td>`)
			b.WriteString(`<td class="num">` + fmt.Sprint(rec.PromptTokens) + " / " + fmt.Sprint(rec.CompletionTokens) + `</td></tr>`)
		}
		b.WriteString(`</tbody></table></div>`)
	}
	b.WriteString(`</div></div>`)

	// ---------------- tab: tasks ----------------
	b.WriteString(renderTaskPage())
	// ---------------- tab: settings ----------------
	b.WriteString(`<div id="tab-settings" class="wb-panel">`)
	b.WriteString(`<div class="card"><h2>管理密钥 <span class="hint">仅保存在本机浏览器</span></h2>`)
	b.WriteString(`<div class="row"><input type="password" id="mgmtKey" placeholder="CPA management key" style="flex:1 1 320px">`)
	b.WriteString(`<button type="button" data-call="saveKey">保存到浏览器</button>`)
	b.WriteString(`<button type="button" class="ghost" data-call="clearKey">清除</button></div>`)
	b.WriteString(`<div class="muted small" id="keyState"></div>`)
	b.WriteString(`<div class="note">密钥仅保存在本机浏览器（localStorage），不会上传到插件或服务器。</div>`)
	b.WriteString(`</div>`)

	curVariant := state.settings.get().VariantOverride
	curAuth := state.settings.get().AuthSupplier
	b.WriteString(`<div class="card"><h2>供应商切换 <span class="hint">仅影响模型调用</span></h2>`)
	b.WriteString(`<div class="seg" id="variantSeg">`)
	for _, opt := range []struct{ v, label, title string }{
		{"auto", "全部供应商", "两组账号都参与调用"},
		{"cn", "国内供应商", "仅国内账号参与调用"},
		{"ai", "国际供应商", "仅国际账号参与调用"},
	} {
		cls := ""
		if (opt.v == "auto" && curVariant == "") || opt.v == curVariant {
			cls = ` class="active"`
		}
		b.WriteString(`<button type="button" data-variant="` + opt.v + `"` + cls +
			` title="` + html.EscapeString(opt.title) + `"` +
			` data-call="setVariant" data-arg0="` + opt.v + `">` + opt.label + `</button>`)
	}
	b.WriteString(`</div>`)
	b.WriteString(`<div class="note">决定<strong>调用</strong>时使用哪些账号：<strong>全部供应商</strong>（默认）按每个账号自身归属自动选择——国内账号走国内接口、国际账号走国际接口，两组并存；` +
		`<strong>国内供应商</strong>只调用国内账号，<strong>国际供应商</strong>只调用国际账号。</div>`)
	b.WriteString(`<div class="note">不影响已登录账号的归属：每个账号始终调用签发它凭据的那一侧，另一侧只是被跳过，无需重新登录。</div>`)
	b.WriteString(`<div class="muted small" id="variantMsg"></div>`)

	// Authorisation is its own switch.
	//
	// It used to share the call-scope control, so changing which accounts a run
	// touched also changed which supplier the next login would authorise
	// against. They are independent decisions.
	b.WriteString(`<h3 style="margin:1rem 0 .35rem;font-size:.95rem">授权 <span class="hint">在 CPA 的 OAuth 登录中完成</span></h3>`)
	b.WriteString(`<div class="seg" id="authSeg">`)
	for _, opt := range []struct{ v, label, title string }{
		{"", "跟随调用设置", "不单独指定；按上面的供应商切换决定（默认国内）"},
		{"cn", "国内授权", "CPA 的授权入口打开 copilot.tencent.com"},
		{"ai", "国际授权", "CPA 的授权入口打开 www.workbuddy.ai"},
	} {
		cls := ""
		if opt.v == curAuth {
			cls = ` class="active"`
		}
		b.WriteString(`<button type="button" data-auth="` + opt.v + `"` + cls +
			` title="` + html.EscapeString(opt.title) + `"` +
			` data-call="setAuthSupplier" data-arg0="` + opt.v + `">` + opt.label + `</button>`)
	}
	b.WriteString(`</div>`)
	b.WriteString(`<div class="note">本开关<strong>只决定授权走哪一侧</strong>，与上面的调用设置互相独立。` +
		`选<strong>国内授权</strong>时，点 CPA 的授权入口会用 <code>copilot.tencent.com</code> 签发国内凭据；` +
		`选<strong>国际授权</strong>时用 <code>www.workbuddy.ai</code> 签发国际凭据。</div>`)
	b.WriteString(`<div class="note"><strong>要两个供应商的账号</strong>：这里选国内授权 → 到 CPA 完成授权；` +
		`再选国际授权 → 到 CPA 完成授权；之后把上面的调用设置保持为「全部供应商」，两组账号会一起参与调用。</div>`)
	b.WriteString(`<div class="muted small" id="authMsg"></div>`)
	b.WriteString(`</div>`)
	// Close the settings panel and the .root wrapper. Both are opened above —
	// missing one leaves the script tag nested inside .root, and the browser then
	// treats the rest of the document as element content, which is how every tab
	// after the account list ended up rendering as blank.
	b.WriteString(`</div>`)
	b.WriteString(`</div>` + mainPageScript() + `</body></html>`)
	return b.String()
}

// accountsSignature summarises an inventory for change detection.
//
// The browser polls /accounts and reloads only when this string changes, so the
// Go and JS implementations must produce identical output for identical input.
// Keep the two in sync: "total:usable:uid+state,..." with the entries in the
// order the panel renders them.
func accountsSignature(accounts []workBuddyAccount) string {
	var b strings.Builder
	usable := 0
	for i := range accounts {
		if accounts[i].Usable {
			usable++
		}
	}
	fmt.Fprintf(&b, "%d:%d:", len(accounts), usable)
	for i := range accounts {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(firstNonEmpty(accounts[i].UID, accounts[i].AuthIndex))
		if accounts[i].DisabledByUser {
			b.WriteByte('D')
		} else {
			b.WriteByte('E')
		}
	}
	return b.String()
}

// handleMainRequest serves the combined page and its JSON endpoints.
func handleMainRequest(req pluginapi.ManagementRequest) (managementResponse, bool) {
	path := normaliseManagementPath(req.Path)
	method := strings.ToUpper(strings.TrimSpace(req.Method))

	switch path {
	case "", "/", "/home", "/dashboard":
		return managementResponse{
			StatusCode: http.StatusOK,
			Headers:    htmlResponseHeaders(),
			Body:       []byte(renderMainPage()),
		}, true

	case "/accounts":
		accounts := listWorkBuddyAccounts()
		total, usable, known, credits := accountSummary(accounts)
		return managementResponse{
			StatusCode: http.StatusOK,
			Headers:    jsonResponseHeaders(),
			Body: mustJSON(map[string]any{
				"accounts":      accounts,
				"total":         total,
				"usable":        usable,
				"credits_known": known,
				"total_credits": credits,
				"fetched_at":    time.Now().Format(time.RFC3339),
				"warning":       state.accounts.lastError(),
			}),
		}, true

	case "/models":
		return handleModelsRequest(req)

	case "/routing/status":
		return managementResponse{
			StatusCode: http.StatusOK,
			Headers:    jsonResponseHeaders(),
			Body: mustJSON(map[string]any{
				"routing":   routingStatusJSON(),
				"scheduler": schedulerSnapshot(),
			}),
		}, true

	case "/variant":
		return handleVariantRequest(req)

	case "/account/toggle":
		return handleAccountToggleRequest(req)

	case "/growth/tasks":
		return handleGrowthTasksRequest(req), true

	case "/growth/summary":
		return handleGrowthTasksRequest(req), true
	}

	if method == http.MethodPost {
		switch path {
		case "/routing/config":
			var body struct {
				Strategy string `json:"strategy"`
				Routing  *struct {
					Strategy string `json:"strategy"`
				} `json:"routing"`
			}
			if len(req.Body) > 0 {
				if errUnmarshal := json.Unmarshal(req.Body, &body); errUnmarshal != nil {
					return managementResponse{
						StatusCode: http.StatusBadRequest,
						Headers:    jsonResponseHeaders(),
						Body:       mustJSON(map[string]any{"error": errUnmarshal.Error()}),
					}, true
				}
			}
			value := body.Strategy
			if value == "" && body.Routing != nil {
				value = body.Routing.Strategy
			}
			applied := applyRoutingConfig(routingSettings{Strategy: normalizeStrategy(value)})
			return managementResponse{
				StatusCode: http.StatusOK,
				Headers:    jsonResponseHeaders(),
				Body: mustJSON(map[string]any{
					"ok":      true,
					"routing": routingStatusJSON(),
					"applied": string(applied.Strategy),
				}),
			}, true

		case "/routing/reset":
			state.scheduler.resetCursor()
			return managementResponse{
				StatusCode: http.StatusOK,
				Headers:    jsonResponseHeaders(),
				Body:       mustJSON(map[string]any{"ok": true, "routing": routingStatusJSON()}),
			}, true

		case "/run":
			// Combined action: check in, then refresh credits.
			//
			// The growth pass is opt-in ("growth": true) because it is far slower
			// than the other two stages: a bare /run keeps the quick behaviour
			// the existing buttons rely on.
			var runBody struct {
				Growth bool `json:"growth"`
			}
			if len(req.Body) > 0 {
				_ = json.Unmarshal(req.Body, &runBody)
			}
			run := runFromManagement()
			results, errQuota := runQuotaRefresh("manual")
			refreshAccountsAfterLogin()
			payload := map[string]any{"checkin": run}
			if errQuota == nil {
				payload["quota"] = results
			} else {
				payload["quota_error"] = errQuota.Error()
			}
			if runBody.Growth {
				payload["growth"] = runGrowthForAll()
			}
			return managementResponse{
				StatusCode: http.StatusOK,
				Headers:    jsonResponseHeaders(),
				Body:       mustJSON(payload),
			}, true

		case "/growth/run":
			return handleGrowthRunRequest(req), true

		case "/growth/travel":
			return handleGrowthTravelRequest(req), true
		}
	}

	return managementResponse{}, false
}

// ---- variant endpoint --------------------------------------------------

// handleVariantRequest serves the two panel switches.
//
// GET  -> current values of both (call scope and authorisation supplier)
// POST -> update one or both; the body may carry "variant" and "auth_supplier"
//
// They are two settings on one endpoint because the panel renders them together
// and a single round trip keeps the two values consistent on screen.
func handleVariantRequest(req pluginapi.ManagementRequest) (managementResponse, bool) {
	method := strings.ToUpper(strings.TrimSpace(req.Method))

	// GET reports the current selections; the panel reads them on load.
	if method == http.MethodGet {
		current := state.settings.get()
		return managementResponse{
			StatusCode: http.StatusOK,
			Headers:    jsonResponseHeaders(),
			Body: mustJSON(map[string]any{
				"ok":             true,
				"variant":        current.VariantOverride,
				"label":          callScopeLabel(current.VariantOverride),
				"auth_supplier":  current.AuthSupplier,
				"auth_label":     authSupplierLabel(current),
				"auth_effective": string(current.authSupplierOrDefault()),
			}),
		}, true
	}

	if method != http.MethodPost {
		return managementResponse{StatusCode: http.StatusMethodNotAllowed}, true
	}

	var body struct {
		Variant      *string `json:"variant"`
		AuthSupplier *string `json:"auth_supplier"`
	}
	if len(req.Body) > 0 {
		if errUnmarshal := json.Unmarshal(req.Body, &body); errUnmarshal != nil {
			return managementResponse{
				StatusCode: http.StatusBadRequest,
				Headers:    jsonResponseHeaders(),
				Body:       mustJSON(map[string]any{"error": errUnmarshal.Error()}),
			}, true
		}
	}

	// A field the caller omitted keeps its current value: the panel sends only
	// the switch the operator touched.
	if body.Variant != nil {
		v := strings.TrimSpace(*body.Variant)
		if !validVariantChoice(v) {
			v = ""
		}
		state.settings.setVariantOverride(v)
	}
	if body.AuthSupplier != nil {
		v := strings.TrimSpace(*body.AuthSupplier)
		if !validVariantChoice(v) {
			v = ""
		}
		state.settings.setAuthSupplier(v)
	}

	current := state.settings.get()
	return managementResponse{
		StatusCode: http.StatusOK,
		Headers:    jsonResponseHeaders(),
		Body: mustJSON(map[string]any{
			"ok":             true,
			"variant":        current.VariantOverride,
			"label":          callScopeLabel(current.VariantOverride),
			"auth_supplier":  current.AuthSupplier,
			"auth_label":     authSupplierLabel(current),
			"auth_effective": string(current.authSupplierOrDefault()),
		}),
	}, true
}

// callScopeLabel renders the call-scope switch for the panel.
func callScopeLabel(v string) string {
	switch v {
	case "cn":
		return "仅国内供应商"
	case "ai":
		return "仅国际供应商"
	}
	return "全部供应商"
}

// authSupplierLabel renders the authorisation switch, noting when it inherits
// the call scope so the operator can tell a real value from a fallback.
func authSupplierLabel(g gatewaySettings) string {
	switch g.AuthSupplier {
	case "cn":
		return "国内授权"
	case "ai":
		return "国际授权"
	}
	switch g.VariantOverride {
	case "cn":
		return "跟随调用设置（国内授权）"
	case "ai":
		return "跟随调用设置（国际授权）"
	}
	return "跟随调用设置（默认国内授权）"
}

// ---- account toggle endpoint -------------------------------------------

func handleAccountToggleRequest(req pluginapi.ManagementRequest) (managementResponse, bool) {
	if method := strings.ToUpper(strings.TrimSpace(req.Method)); method != http.MethodPost {
		return managementResponse{StatusCode: http.StatusMethodNotAllowed}, true
	}
	var body struct {
		UID       string `json:"uid"`
		AuthIndex string `json:"auth_index"`
		Action    string `json:"action"`
		Disabled  bool   `json:"disabled"`
	}
	if len(req.Body) > 0 {
		if errUnmarshal := json.Unmarshal(req.Body, &body); errUnmarshal != nil {
			return managementResponse{
				StatusCode: http.StatusBadRequest,
				Headers:    jsonResponseHeaders(),
				Body:       mustJSON(map[string]any{"error": errUnmarshal.Error()}),
			}, true
		}
	}
	if body.UID == "" && body.AuthIndex == "" {
		return managementResponse{
			StatusCode: http.StatusBadRequest,
			Headers:    jsonResponseHeaders(),
			Body:       mustJSON(map[string]any{"error": "缺少 uid"}),
		}, true
	}
	switch body.Action {
	case "disable":
		// The panel only sends uid/auth_index/action — never "disabled" — so
		// the disable branch must force-disable. Passing body.Disabled through
		// would silently no-op (false default) and leave the account enabled:
		// the "账号禁用没解开" report.
		state.pool.disableAccountKeyed(body.UID, body.AuthIndex, true)
	case "enable":
		state.pool.disableAccountKeyed(body.UID, body.AuthIndex, false)
	case "toggle":
		lane, found := state.pool.findAccountKeyedCopy(body.UID, body.AuthIndex)
		if !found {
			state.pool.disableAccountKeyed(body.UID, body.AuthIndex, true)
		} else {
			state.pool.disableAccountKeyed(body.UID, body.AuthIndex, !lane.DisabledByUser)
		}
	default:
		return managementResponse{
			StatusCode: http.StatusBadRequest,
			Headers:    jsonResponseHeaders(),
			Body:       mustJSON(map[string]any{"error": "未知操作"}),
		}, true
	}
	return managementResponse{
		StatusCode: http.StatusOK,
		Headers:    jsonResponseHeaders(),
		Body:       mustJSON(map[string]any{"ok": true}),
	}, true
}

// renderAccountGroup draws one realm's accounts as a table.
//
// Each group states which features it serves, because they differ: check-in and
// the growth centre exist only domestically, so an operator looking at an
// international account should not expect those buttons to do anything.
func renderAccountGroup(title, variantKey string, accounts []workBuddyAccount) string {
	var b strings.Builder
	b.WriteString(`<h3 style="margin:.9rem 0 .35rem;font-size:.95rem">` + html.EscapeString(title) +
		` <span class="muted small">（` + fmt.Sprint(len(accounts)) + ` 个）</span></h3>`)

	if variantKey == "ai" {
		b.WriteString(`<div class="muted small" style="margin-bottom:.4rem">` +
			`调用主机 www.workbuddy.ai。国际供应商没有签到，也没有成长任务中心；额度查询与对外调用可用。</div>`)
	} else {
		b.WriteString(`<div class="muted small" style="margin-bottom:.4rem">` +
			`调用主机 copilot.tencent.com。签到、成长任务、猫猫旅行与额度查询均可用。</div>`)
	}

	if len(accounts) == 0 {
		b.WriteString(`<div class="empty">该版本暂无账号。</div>`)
		return b.String()
	}

	b.WriteString(`<div class="table-wrap"><table data-account-table="1"><thead><tr>`)
	b.WriteString(`<th>账号</th><th class="num">积分</th><th>到期</th><th>状态</th><th class="actions">操作</th></tr></thead><tbody>`)
	for _, a := range accounts {
		pillClass, statusText := "ok", "可用"
		detail := ""
		switch {
		case a.AutoDisabled:
			// Distinguished from a manual disable so the operator knows the
			// pool retired it and can re-enable deliberately.
			pillClass, statusText = "bad", "自动禁用"
			detail = firstNonEmpty(a.DisabledReason, a.Reason)
		case a.DisabledByUser || a.Disabled:
			pillClass, statusText = "bad", "已停用"
			detail = a.Reason
		case a.Expired:
			pillClass, statusText = "bad", "凭据过期"
			detail = "请重新登录"
		case a.CreditsExpired:
			pillClass, statusText = "bad", "积分过期"
		case !a.CooldownUntil.IsZero() && time.Now().Before(a.CooldownUntil):
			pillClass, statusText = "warn", "冷却中"
			detail = a.CooldownUntil.Local().Format("15:04:05")
		}
		cv := "—"
		if a.CreditsKnown {
			cv = fmt.Sprint(a.Credits)
		}

		// Status bucket for the filter select. Derived from the same cases the
		// pill uses, so the two can never disagree about what an account is.
		filterStatus := "usable"
		switch {
		case a.AutoDisabled || a.DisabledByUser || a.Disabled:
			filterStatus = "disabled"
		case a.Expired || a.CreditsExpired:
			filterStatus = "expired"
		case !a.CooldownUntil.IsZero() && time.Now().Before(a.CooldownUntil):
			filterStatus = "cooling"
		}
		searchText := strings.Join([]string{
			a.Label, a.UID, a.AuthIndex, a.Variant, a.DisabledReason, a.Reason, statusText,
		}, " ")

		expiry, expiryClass := "—", "muted"
		switch {
		case a.CreditsExpired:
			expiry, expiryClass = "已过期", "bad"
		case a.CreditsExpireAt > 0 && a.CreditsExpiringSoon:
			expiry, expiryClass = fmt.Sprintf("%d 天后", a.CreditsExpireDays), "warn"
		case a.CreditsExpireAt > 0:
			expiry, expiryClass = fmt.Sprintf("%d 天后", a.CreditsExpireDays), "muted"
		}
		// The label carries everything the operator needs to identify a row: the
		// upstream's display name, and the uid underneath in small type. A separate
		// uid column was removed — the two read almost the same, so a second column
		// spent width without adding information.
		ident := firstNonEmpty(a.UID, a.AuthIndex)
		b.WriteString(`<tr data-status="` + filterStatus + `" data-search="` + html.EscapeString(searchText) + `">` +
			`<td data-label="账号"><strong>` + html.EscapeString(a.Label) + `</strong>`)
		if ident != "" && ident != a.Label {
			b.WriteString(`<div class="muted small mono">` + html.EscapeString(ident) + `</div>`)
		}
		b.WriteString(`</td>`)
		b.WriteString(`<td class="num" data-label="积分" data-credits-for="` + html.EscapeString(ident) + `">` +
			html.EscapeString(cv) + `</td>`)
		b.WriteString(`<td class="` + expiryClass + `" data-label="到期">` + html.EscapeString(expiry) + `</td>`)
		b.WriteString(`<td data-label="状态"><span class="pill ` + pillClass + `">` + statusText + `</span>`)
		if detail != "" {
			b.WriteString(` <span class="muted small">` + html.EscapeString(detail) + `</span>`)
		}
		b.WriteString(`</td>`)

		uid := firstNonEmpty(a.UID, a.AuthIndex)
		// Only the enable/disable toggle lives on the row.
		//
		// Per-account check-in and credit refresh were removed at the operator's
		// request: the panel already has a button that refreshes every account's
		// credits at once, and sign-in runs on a schedule for everyone. A row-level
		// copy of both added clutter without adding a capability.
		rowAction := "disable"
		rowActionLabel := "禁用"
		if a.DisabledByUser || a.Disabled || a.AutoDisabled {
			rowAction = "enable"
			rowActionLabel = "启用"
		}
		b.WriteString(`<td class="actions">` +
			`<button type="button" class="ghost mini" data-account-toggle="1" data-uid="` + html.EscapeString(uid) +
			`" data-action="` + rowAction + `" data-auth-index="` + html.EscapeString(a.AuthIndex) + `">` +
			rowActionLabel + `</button>` +
			`</td>`)
		b.WriteString(`</tr>`)
	}
	b.WriteString(`</tbody></table></div>`)
	return b.String()
}
