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
	"sort"
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
// renderScheduleColumns draws the two automatic jobs as side-by-side columns.
//
// Returns only the columns, not a card: the tasks page puts them inside the same card as
// the manual triggers, because the two are one question ("run this") answered two ways.
func renderScheduleColumns() string {
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
	b.WriteString(`<div class="sched-pair">`)

	// ---- growth tasks ----
	b.WriteString(`<div class="sched-col">`)
	b.WriteString(`<label class="field sched-switch"><input type="checkbox" id="gsEnabled"`)
	if growthEnabled {
		b.WriteString(` checked`)
	}
	b.WriteString(`><span class="sched-name">成长任务</span></label>`)
	b.WriteString(`<span class="sched-time">每天 <input type="number" id="gsHour" min="0" max="23" value="` +
		fmt.Sprint(clampHour(growthHour)) + `"> 时 <input type="number" id="gsMinute" min="0" max="59" value="` +
		fmt.Sprint(clampMinute(growthMinute)) + `"> 分</span>`)
	b.WriteString(`<label class="field sched-start"><input type="checkbox" id="gsOnStart"`)
	if growthOnStart {
		b.WriteString(` checked`)
	}
	b.WriteString(`> 启动时补跑</label>`)
	if growthRunning {
		b.WriteString(`<span class="pill warn">执行中</span>`)
	} else if growthEnabled && growthRanToday {
		b.WriteString(`<span class="pill ok">今日已完成</span>`)
	}
	if growthSummary != "" {
		b.WriteString(`<span class="uid">` + html.EscapeString(growthSummary) + `</span>`)
	}
	b.WriteString(`</div>`)

	// ---- check-in ----
	b.WriteString(`<div class="sched-col">`)
	b.WriteString(`<label class="field sched-switch"><input type="checkbox" id="ckEnabled"`)
	if checkin.Enabled {
		b.WriteString(` checked`)
	}
	b.WriteString(`><span class="sched-name">每日签到</span></label>`)
	b.WriteString(`<span class="sched-time">每天 <input type="number" id="ckHour" min="0" max="23" value="` +
		fmt.Sprint(clampHour(checkin.Hour)) + `"> 时 <input type="number" id="ckMinute" min="0" max="59" value="` +
		fmt.Sprint(clampMinute(checkin.Minute)) + `"> 分</span>`)
	b.WriteString(`<label class="field sched-start"><input type="checkbox" id="ckOnStart"`)
	if checkin.OnStart {
		b.WriteString(` checked`)
	}
	b.WriteString(`> 启动时补跑</label>`)
	b.WriteString(`<span class="uid">国际版账号不参与签到，会被自动跳过。</span>`)
	b.WriteString(`</div>`)

	b.WriteString(`</div>`)
	return b.String()
}

// renderScheduleBox draws the automatic jobs as a standalone card.
//
// Kept for callers that only want the schedule; the tasks page composes
// renderScheduleColumns into a larger card instead.
func renderScheduleBox() string {
	var b strings.Builder
	b.WriteString(`<div class="box">`)
	b.WriteString(`<header><h3>每日自动执行 <span class="hint">按本机时区，每天各跑一次</span></h3>`)
	b.WriteString(`<span class="grow"></span>`)
	b.WriteString(`<span class="note" id="scheduleMsg"></span>`)
	b.WriteString(`<button type="button" class="xs primary" data-call="saveSchedule">保存</button>`)
	b.WriteString(`</header>`)
	b.WriteString(`<div class="pad">`)
	b.WriteString(renderScheduleColumns())
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
			when = rec.StartedAt.In(panelLocation).Format("15:04:05")
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

// renderNoteTable draws the operational events: sign-ins, task runs, throttling, an
// account being parked or disabled.
//
// Its shape differs from the call table on purpose. A note has no model, no tokens and no
// upstream status; what it has is a time, an account it concerns, and a sentence. Showing
// it in the call columns would leave most of every row empty and invite reading the
// sentence as an error.
func renderNoteTable(notes []callRecord) string {
	var b strings.Builder
	b.WriteString(`<div class="tbl-wrap"><table class="data notes"><thead><tr>`)
	b.WriteString(`<th>时间</th><th>账号</th><th>事件</th>`)
	b.WriteString(`</tr></thead><tbody>`)

	for _, rec := range notes {
		when := "—"
		if !rec.StartedAt.IsZero() {
			when = rec.StartedAt.In(panelLocation).Format("01-02 15:04:05")
		}
		account := firstNonEmpty(rec.Label, rec.UID)
		if account == "" {
			account = "—"
		}
		b.WriteString(`<tr>`)
		b.WriteString(`<td class="mono" data-label="时间">` + html.EscapeString(when) + `</td>`)
		b.WriteString(`<td data-label="账号">` + html.EscapeString(account) + `</td>`)
		b.WriteString(`<td class="note wrap" data-label="事件">` + html.EscapeString(rec.Error) + `</td>`)
		b.WriteString(`</tr>`)
	}

	b.WriteString(`</tbody></table></div>`)
	return b.String()
}

// collectRequestLog gathers the operational events the panel has to show, newest first.
//
// They come from three places, each kept by the subsystem that produces it rather than
// funnelled through one log at the moment they happen: the pool's retirement audit, the
// check-in runs, and the growth-task passes. Merging them at read time keeps each
// subsystem's own history authoritative — and keeps their locks out of each other's way,
// which writing to a shared log from inside the pool's mutex would not have done.
//
// Notices recorded directly (a scheduled pass finishing, a credential disagreement) are
// included as well, since those have no other home.
func collectRequestLog(limit int) []callRecord {
	if limit < 1 {
		limit = 1
	}

	out := make([]callRecord, 0, limit)

	// The pool's retirement audit: an account parked or disabled for good. Manual
	// actions share the queue but read differently — "已自动禁用" for something the
	// operator did by hand sends them looking for a fault that is not there.
	for _, ev := range state.pool.autoDisableHistory() {
		reason := firstNonEmpty(ev.Reason, "原因未记录")
		var text string
		switch {
		case ev.Manual && ev.Recovered:
			text = "已启用：" + reason
		case ev.Manual:
			text = "已禁用：" + reason
		case ev.Recovered:
			text = "账号已恢复：" + reason
		default:
			text = "账号已自动禁用：" + reason
		}
		out = append(out, callRecord{
			ProviderID: workBuddyProviderKey,
			UID:        ev.UID,
			Label:      ev.Label,
			Error:      text,
			StartedAt:  ev.At,
			Notice:     true,
		})
	}

	// Check-in runs. The state keeps them newest-first already.
	for _, run := range state.checkin.snapshot(20) {
		text := fmt.Sprintf("签到：成功 %d，失败 %d，跳过 %d（%s）",
			run.Succeeded, run.Failed, run.Skipped, run.Trigger)
		if run.Total == 0 {
			text = "签到：没有可签到的账号"
		}
		out = append(out, callRecord{
			ProviderID: workBuddyProviderKey,
			Error:      text,
			StartedAt:  run.StartedAt,
			Notice:     true,
		})
	}

	// Growth-task passes.
	for _, entry := range state.growth.recentHistory() {
		text := fmt.Sprintf("任务：%s，完成 %d，失败 %d，获得 %d 积分",
			accountLabelOrUID(entry.Label, entry.UID), entry.Claimed, entry.Failed, entry.Earned)
		out = append(out, callRecord{
			ProviderID: workBuddyProviderKey,
			UID:        entry.UID,
			Label:      entry.Label,
			Error:      text,
			StartedAt:  entry.FinishedAt,
			Notice:     true,
		})
	}

	// Notices recorded directly.
	out = append(out, state.log.noticeLog(limit)...)

	// Newest first, then trimmed. Each source is already ordered, so a sort is what
	// interleaves them correctly.
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].StartedAt.After(out[j].StartedAt)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// accountLabelOrUID prefers the display name and falls back to the identifier.
func accountLabelOrUID(label, uid string) string {
	if s := firstNonEmpty(label, uid); s != "" {
		return s
	}
	return "未知账号"
}
