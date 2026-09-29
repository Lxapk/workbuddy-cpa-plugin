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

// renderScheduleBox draws the two automatic daily jobs on one card.
//
// Growth tasks and check-in are the same kind of thing — something the plugin does once
// a day at a chosen hour — so they share a card and a save button. Two cards asked the
// operator to configure the same idea twice and press save twice, with no indication
// that either had taken effect until a reload.
//
// Each keeps its own switch and its own time: the two jobs hit different endpoints and
// there is no reason to tie one to the other's schedule.
// renderScheduleRows draws the two automatic jobs as rows.
//
// Returns only the rows, not a card: the tasks page puts them inside the same card as
// the manual triggers, because the two are one question ("run this") answered two ways.
func renderScheduleRows() string {
	growth := growthScheduleSnapshot()
	checkin := state.settings.get().Checkin

	growthEnabled, _ := growth["enabled"].(bool)
	growthHour, _ := growth["hour"].(int)
	growthMinute, _ := growth["minute"].(int)
	growthOnStart, _ := growth["on_start"].(bool)
	growthRunning, _ := growth["running"].(bool)
	growthRanToday, _ := growth["ran_today"].(bool)
	growthSummary, _ := growth["last_summary"].(string)

	var b strings.Builder

	// ---- growth tasks ----
	b.WriteString(`<div class="sched-row">`)
	b.WriteString(`<label class="field sched-switch"><input type="checkbox" id="gsEnabled"`)
	if growthEnabled {
		b.WriteString(` checked`)
	}
	b.WriteString(`><span class="sched-name">成长任务</span></label>`)
	b.WriteString(`<span class="sched-time">每天 <input type="number" id="gsHour" min="0" max="23" value="` +
		fmt.Sprint(clampHour(growthHour)) + `"> 时 <input type="number" id="gsMinute" min="0" max="59" value="` +
		fmt.Sprint(clampMinute(growthMinute)) + `"> 分</span>`)
	if growthEnabled && growthRanToday && !growthRunning {
		b.WriteString(`<span class="pill ok">今日已完成</span>`)
	}
	if growthRunning {
		b.WriteString(`<span class="pill warn">执行中</span>`)
	}
	b.WriteString(`<span class="grow"></span>`)
	b.WriteString(`<label class="field sched-start"><input type="checkbox" id="gsOnStart"`)
	if growthOnStart {
		b.WriteString(` checked`)
	}
	b.WriteString(`> 启动时补跑</label>`)
	b.WriteString(`</div>`)

	// ---- check-in ----
	b.WriteString(`<div class="sched-row">`)
	b.WriteString(`<label class="field sched-switch"><input type="checkbox" id="ckEnabled"`)
	if checkin.Enabled {
		b.WriteString(` checked`)
	}
	b.WriteString(`><span class="sched-name">每日签到</span></label>`)
	b.WriteString(`<span class="sched-time">每天 <input type="number" id="ckHour" min="0" max="23" value="` +
		fmt.Sprint(clampHour(checkin.Hour)) + `"> 时 <input type="number" id="ckMinute" min="0" max="59" value="` +
		fmt.Sprint(clampMinute(checkin.Minute)) + `"> 分</span>`)
	b.WriteString(`<span class="grow"></span>`)
	b.WriteString(`<label class="field sched-start"><input type="checkbox" id="ckOnStart"`)
	if checkin.OnStart {
		b.WriteString(` checked`)
	}
	b.WriteString(`> 启动时补跑</label>`)
	b.WriteString(`</div>`)

	if growthSummary != "" {
		b.WriteString(`<div class="note">上次任务：` + html.EscapeString(growthSummary) + `</div>`)
	}
	return b.String()
}

// renderScheduleBox draws the automatic jobs as a standalone card.
//
// Kept for callers that only want the schedule; the tasks page composes
// renderScheduleRows into a larger card instead.
func renderScheduleBox() string {
	var b strings.Builder
	b.WriteString(`<div class="box">`)
	b.WriteString(`<header><h3>每日自动执行 <span class="hint">按本机时区，每天各跑一次</span></h3>`)
	b.WriteString(`<span class="grow"></span>`)
	b.WriteString(`<span class="note" id="scheduleMsg"></span>`)
	b.WriteString(`<button type="button" class="xs primary" data-call="saveSchedule">保存</button>`)
	b.WriteString(`</header>`)
	b.WriteString(`<div class="pad">`)
	b.WriteString(renderScheduleRows())
	b.WriteString(`</div>`)
	b.WriteString(`<div class="foot">`)
	b.WriteString(`<span class="note" id="runMsg"></span>`)
	b.WriteString(`<span class="grow"></span>`)
	b.WriteString(`<button type="button" class="xs" data-call="runCheckin">立即签到</button>`)
	b.WriteString(`<button type="button" class="xs" data-call="runAllTasks">立即执行任务</button>`)
	b.WriteString(`</div>`)

	b.WriteString(`</div>`)

	// The last check-in run, when there is one.
	history := state.checkin.snapshot(1)
	if len(history) > 0 {
		b.WriteString(`<div class="box">`)
		b.WriteString(`<header><h3>最近一次签到</h3></header>`)
		b.WriteString(renderRun(history[0]))
		b.WriteString(`</div>`)
	}

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
