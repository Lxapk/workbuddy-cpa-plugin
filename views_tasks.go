package main

// The tasks page's blocks: the check-in card, and the per-account task table.
//
// The check-in card used to live on a page of its own and the account table on
// another, which meant the operator could not see what was scheduled next to what had
// run. Both are on the tasks page now, each in its own card with its own controls in
// the header.

import (
	"fmt"
	"html"
	"strings"
)

// renderGrowthScheduleBox draws the scheduled run controls.
//
// Same shape as the check-in card next to it: a switch, a time of day, and an optional
// catch-up pass at startup. The two read alike because they are the same kind of
// thing — something that runs unattended at a chosen hour.
func renderGrowthScheduleBox() string {
	snap := growthScheduleSnapshot()
	enabled, _ := snap["enabled"].(bool)
	hour, _ := snap["hour"].(int)
	minute, _ := snap["minute"].(int)
	onStart, _ := snap["on_start"].(bool)
	ranToday, _ := snap["ran_today"].(bool)
	running, _ := snap["running"].(bool)
	lastSummary, _ := snap["last_summary"].(string)

	var b strings.Builder
	b.WriteString(`<div class="box">`)
	b.WriteString(`<header><h3>定时执行 <span class="hint">每天自动完成全部任务</span></h3><span class="grow"></span>`)
	if running {
		b.WriteString(`<span class="pill warn">执行中</span>`)
	} else if enabled && ranToday {
		b.WriteString(`<span class="pill ok">今日已完成</span>`)
	}
	b.WriteString(`<span class="note" id="growthScheduleMsg"></span>`)
	b.WriteString(`<button type="button" class="xs primary" data-call="saveGrowthSchedule">保存</button>`)
	b.WriteString(`</header>`)
	b.WriteString(`<div class="pad">`)
	b.WriteString(`<div class="row tight"><label class="field"><input type="checkbox" id="gsEnabled"`)
	if enabled {
		b.WriteString(` checked`)
	}
	b.WriteString(`> 启用每天定时执行</label></div>`)
	b.WriteString(`<div class="row tight"><label class="field">每天 <input type="number" id="gsHour" min="0" max="23" value="` +
		fmt.Sprint(clampHour(hour)) + `"> 时 <input type="number" id="gsMinute" min="0" max="59" value="` +
		fmt.Sprint(clampMinute(minute)) + `"> 分执行</label></div>`)
	b.WriteString(`<div class="row tight"><label class="field"><input type="checkbox" id="gsOnStart"`)
	if onStart {
		b.WriteString(` checked`)
	}
	b.WriteString(`> 启动时补跑（当天尚未执行时）</label></div>`)
	if lastSummary != "" {
		b.WriteString(`<div class="note" style="margin-top:9px">上次：` + html.EscapeString(lastSummary) + `</div>`)
	}
	b.WriteString(`<div class="note" style="margin-top:9px">定时执行与「全部执行」做同样的事：` +
		`成长任务、签到与猫猫旅行。它按本机时区判断日期，同一天只跑一次。</div>`)
	b.WriteString(`</div></div>`)
	return b.String()
}

// renderCheckinBox draws the daily sign-in: its schedule and the last run's summary.
func renderCheckinBox() string {
	settings := state.settings.get()
	history := state.checkin.snapshot(1)

	var b strings.Builder
	b.WriteString(`<div class="box">`)
	b.WriteString(`<header><h3>每日签到</h3><span class="grow"></span>`)
	b.WriteString(`<span class="note" id="runMsg"></span>`)
	b.WriteString(`<button type="button" class="xs" data-call="runCheckin">立即签到</button>`)
	b.WriteString(`<button type="button" class="xs primary" data-call="saveCheckinSettings">保存</button>`)
	b.WriteString(`</header>`)
	b.WriteString(`<div class="pad">`)
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
	b.WriteString(`</div>`)

	if len(history) == 0 {
		b.WriteString(`<div class="empty">还没有签到记录。</div>`)
	} else {
		b.WriteString(renderRun(history[0]))
	}
	b.WriteString(`</div>`)
	return b.String()
}

// renderTaskAccountsBox draws which accounts take part in the task runs, and the
// per-account switches.
//
// The controls live in this card's header rather than on a page toolbar: they act on
// this table, and a toolbar several blocks away left the operator guessing which
// button belonged where.
func renderTaskAccountsBox(accounts []workBuddyAccount) string {
	var b strings.Builder
	b.WriteString(`<div class="box">`)
	b.WriteString(`<header><h3>参与账号 <span class="hint">哪些账号执行成长任务</span></h3><span class="grow"></span>`)
	b.WriteString(`<button type="button" class="xs" data-call="selectAllTaskAccounts">全部启用</button>`)
	b.WriteString(`<button type="button" class="xs" data-call="clearAllTaskAccounts">全部停用</button>`)
	b.WriteString(`<button type="button" class="xs primary" data-call="loadTaskDetail">查看任务明细</button>`)
	b.WriteString(`</header>`)

	if len(accounts) == 0 {
		b.WriteString(`<div class="empty">还没有账号。</div>`)
		b.WriteString(`</div>`)
		return b.String()
	}

	b.WriteString(`<div class="tbl-wrap"><table class="data tasks"><thead><tr>`)
	b.WriteString(`<th>账号</th><th>参与</th><th>最近任务</th><th>执行时间</th>`)
	b.WriteString(`</tr></thead><tbody>`)

	for _, a := range accounts {
		label := firstNonEmpty(a.Label, a.UID, a.AuthIndex)
		uid := firstNonEmpty(a.UID, a.AuthIndex)
		enabled, running := taskAccountState(uid)

		// The toggle reads as a switch with an explicit state word, not a pill whose
		// colour is the only difference. "已启用 / 未启用" is unambiguous at a glance.
		stateCls, stateText := "idle", "未启用"
		if enabled {
			stateCls, stateText = "ok", "已启用"
		}
		action := "enable"
		if enabled {
			action = "disable"
		}

		rowClass := "bar"
		if running {
			rowClass += " warn"
		}
		b.WriteString(`<tr><td class="` + rowClass + `" data-label="账号"><strong>` + html.EscapeString(label) + `</strong>`)
		if running {
			b.WriteString(` <span class="pill warn">执行中</span>`)
		}
		b.WriteString(`</td>`)
		b.WriteString(`<td data-label="参与"><button type="button" class="xs ` + stateCls + `-btn"` +
			` data-task-toggle="1" data-uid="` + html.EscapeString(uid) + `" data-action="` + action + `">` +
			stateText + `</button></td>`)
		b.WriteString(`<td class="note" data-label="最近任务">` + html.EscapeString(taskLastLabel(uid)) + `</td>`)
		b.WriteString(`<td class="note mono" data-label="执行时间">` + html.EscapeString(taskLastRunTime(uid)) + `</td></tr>`)
	}

	b.WriteString(`</tbody></table></div>`)
	b.WriteString(`</div>`)
	return b.String()
}

// taskStatusSnapshot returns the task engine's view, initialised from the account
// list.
//
// initFromAccounts is called first so a freshly started plugin — where nothing has
// run yet and the engine has never seen an account — still shows the table rather
// than an empty page.
func taskStatusSnapshot() map[string]any {
	state.taskEngine.initFromAccounts(listWorkBuddyAccounts())
	return taskStatusJSON()
}

// taskQueueDepth returns how many task runs are in flight and how many are waiting.
func taskQueueDepth() (int, int) {
	status := taskStatusSnapshot()
	running, _ := status["running"].(int)
	queued, _ := status["queued"].(int)
	return running, queued
}

// taskAccountState reports whether an account takes part in task runs and whether one
// is in flight for it.
func taskAccountState(uid string) (bool, bool) {
	status := taskStatusSnapshot()
	accounts, _ := status["accounts"].([]map[string]any)
	for _, acct := range accounts {
		acctUID, _ := acct["uid"].(string)
		if acctUID != uid {
			continue
		}
		enabled, _ := acct["enabled"].(bool)
		queuedFlag, _ := acct["queued"].(bool)
		inflight, _ := acct["inflight"].(int)
		return enabled, queuedFlag || inflight > 0
	}
	return false, false
}

// taskLastRunTime returns the most recent task execution time for an account, or "—".
func taskLastRunTime(uid string) string {
	status := taskStatusSnapshot()
	accounts, _ := status["accounts"].([]map[string]any)
	for _, acct := range accounts {
		acctUID, _ := acct["uid"].(string)
		if acctUID != uid {
			continue
		}
		tasks, _ := acct["tasks"].([]map[string]any)
		if len(tasks) == 0 {
			return "—"
		}
		last, _ := tasks[0]["last_run"].(string)
		if len(last) > 16 {
			return last[11:16]
		}
		return "—"
	}
	return "—"
}

// taskLastLabel returns the name of the task an account touched most recently.
func taskLastLabel(uid string) string {
	status := taskStatusSnapshot()
	accounts, _ := status["accounts"].([]map[string]any)
	for _, acct := range accounts {
		acctUID, _ := acct["uid"].(string)
		if acctUID != uid {
			continue
		}
		tasks, _ := acct["tasks"].([]map[string]any)
		if len(tasks) == 0 {
			return "—"
		}
		label, _ := tasks[0]["label"].(string)
		return firstNonEmpty(label, "—")
	}
	return "—"
}

func renderCallTable(recent []callRecord) string {
	var b strings.Builder
	b.WriteString(`<div class="tbl-wrap"><table class="data calls"><thead><tr>`)
	b.WriteString(`<th>时间</th><th>账号</th><th>模型</th><th class="num">状态</th><th class="num">Tokens</th><th>结果</th>`)
	b.WriteString(`</tr></thead><tbody>`)

	for _, rec := range recent {
		cls := "ok"
		if rec.StatusCode >= 400 || rec.Error != "" {
			cls = "bad"
		}
		when := "—"
		if !rec.StartedAt.IsZero() {
			when = rec.StartedAt.Local().Format("15:04:05")
		}
		tokens := fmt.Sprint(rec.PromptTokens) + " / " + fmt.Sprint(rec.CompletionTokens)

		b.WriteString(`<tr><td class="note mono" data-label="时间">` + html.EscapeString(when) + `</td>`)
		b.WriteString(`<td data-label="账号">` + html.EscapeString(firstNonEmpty(rec.Label, rec.UID, "—")) + `</td>`)
		b.WriteString(`<td class="mono" data-label="模型">` + html.EscapeString(rec.Model) + `</td>`)
		b.WriteString(`<td class="num" data-label="状态"><span class="pill ` + cls + `">` +
			fmt.Sprint(rec.StatusCode) + `</span></td>`)
		b.WriteString(`<td class="num mono" data-label="Tokens">` + html.EscapeString(tokens) + `</td>`)
		b.WriteString(`<td class="note wrap" data-label="结果">` + html.EscapeString(rec.Error) + `</td></tr>`)
	}

	b.WriteString(`</tbody></table></div>`)
	return b.String()
}
