package main

// The tasks page.
//
// One table, one row per account, and the account's tasks underneath it. The page used
// to spread the same information over three places — a stats strip with bare counts, a
// "participating accounts" table, and a results box above both — so reading it meant
// jumping between them and a run's outcome appeared somewhere other than the account it
// belonged to. Account and task are one subject; the layout now says so.

import (
	"encoding/json"
	"html"
	"strings"
)

// taskRow is one account's task state, assembled once and used by both the rendering
// and the filtering.
type taskRow struct {
	Label    string
	UID      string
	Variant  string
	Enabled  bool
	Running  bool
	Queued   bool
	Inflight int
	LastTask string
	LastRun  string
	// Growth reports whether this account is in scope for the growth tasks. An
	// international account is not — the growth centre is a China-mainland feature —
	// so offering it an "expand tasks" button only produces a failure.
	Growth bool
}

// collectTaskRows gathers the per-account task state in one pass.
//
// The page used to call taskStatusSnapshot from several helpers, each of which rebuilt
// the engine's view of every account — a lot of repeated work to answer questions about
// a handful of rows.
func collectTaskRows(accounts []workBuddyAccount) []taskRow {
	status := taskStatusSnapshot()
	byUID := map[string]map[string]any{}
	if list, ok := status["accounts"].([]map[string]any); ok {
		for _, acct := range list {
			if uid, _ := acct["uid"].(string); uid != "" {
				byUID[uid] = acct
			}
		}
	}

	rows := make([]taskRow, 0, len(accounts))
	for _, a := range accounts {
		uid := firstNonEmpty(a.UID, a.AuthIndex)
		row := taskRow{
			Label:   firstNonEmpty(a.Label, a.UID, a.AuthIndex),
			UID:     uid,
			Variant: a.Variant,
			LastRun: taskLastRunTime(uid),
			// The same predicate the growth runner uses to decide who it will serve,
			// so the button and the endpoint agree about who is eligible.
			Growth: growthEligibleVariant(a.Variant),
		}
		if acct, ok := byUID[uid]; ok {
			row.Enabled, _ = acct["enabled"].(bool)
			row.Queued, _ = acct["queued"].(bool)
			row.Inflight, _ = acct["inflight"].(int)
			row.Running = row.Queued || row.Inflight > 0
			row.LastTask, _ = acct["last_task"].(string)
		}
		rows = append(rows, row)
	}
	return rows
}

// renderTaskAccountsBox draws the account table: one row per account, its tasks beneath.
func renderTaskAccountsBox(accounts []workBuddyAccount) string {
	rows := collectTaskRows(accounts)

	var b strings.Builder
	b.WriteString(`<div class="box" id="taskAccountsBox">`)
	b.WriteString(`<header><h3>账号与任务</h3><span class="grow"></span>`)
	b.WriteString(`<button type="button" class="xs" data-call="selectAllTaskAccounts">全部启用</button>`)
	b.WriteString(`<button type="button" class="xs" data-call="clearAllTaskAccounts">全部停用</button>`)
	b.WriteString(`<button type="button" class="xs" data-call="expandAllTaskDetail">展开全部任务</button>`)
	b.WriteString(`</header>`)

	if len(rows) == 0 {
		b.WriteString(`<div class="empty">还没有账号。先在「账号」页添加，之后每个账号会在这里列出它的任务。</div>`)
		b.WriteString(taskNotes())
		b.WriteString(`</div>`)
		return b.String()
	}

	b.WriteString(`<div class="tbl-wrap"><table class="data tasks" id="taskTable"><thead><tr>`)
	b.WriteString(`<th>账号</th><th>参与</th><th>状态</th><th>最近任务</th><th>执行时间</th>`)
	b.WriteString(`<th class="actions">操作</th>`)
	b.WriteString(`</tr></thead><tbody>`)

	for _, row := range rows {
		b.WriteString(renderTaskAccountRow(row))
	}

	b.WriteString(`</tbody></table></div>`)
	b.WriteString(taskNotes())
	b.WriteString(`</div>`)
	return b.String()
}

// taskNotes explains the two columns whose meaning is not self-evident, and the tasks
// that cannot be automated. It is emitted whether or not the list is empty, so the
// explanation is present the moment the page is opened.
func taskNotes() string {
	var b strings.Builder
	b.WriteString(`<div class="pad" style="padding-top:0"><div class="note">`)
	b.WriteString(`「参与」控制该账号是否加入批量执行；展开任务可以看到每个成长任务的完成情况，`)
	b.WriteString(`未完成的那几项会标出来。`)
	b.WriteString(`需要真实桌面操作的任务（资料库、发现应用）无法代做，会给出深链。`)
	b.WriteString(`国际版账号不在成长任务中心范围内，会被自动跳过。`)
	b.WriteString(`</div></div>`)
	return b.String()
}

// renderTaskAccountRow draws one account and, directly beneath it, the slot its task
// detail will occupy.
//
// The detail row is always present but empty, and filled on demand. Inserting a row on
// expand would renumber nothing visible to the operator but would make the table jump;
// an empty row keeps every account's controls where they were.
func renderTaskAccountRow(row taskRow) string {
	stateCls, stateText := "idle", "未启用"
	if row.Enabled {
		stateCls, stateText = "ok", "已启用"
	}
	action := "enable"
	if row.Enabled {
		action = "disable"
	}

	// Run state reads as a separate column from participation: an account can be
	// enabled and idle, or disabled and still finishing a run it already started.
	runCls, runText := "idle", "空闲"
	switch {
	case row.Running && row.Inflight > 0:
		runCls, runText = "warn", "执行中"
	case row.Queued:
		runCls, runText = "warn", "排队中"
	}

	rowClass := "bar"
	switch {
	case row.Running:
		rowClass += " warn"
	case !row.Enabled:
		rowClass += " idle-bar"
	}

	var b strings.Builder
	b.WriteString(`<tr data-task-row="1" data-uid="` + html.EscapeString(row.UID) + `" data-enabled="` +
		map[bool]string{true: "1", false: "0"}[row.Enabled] + `">`)

	// Account: name on the first line, realm beneath. Same arrangement as the accounts
	// page, so a name reads the same wherever it appears.
	b.WriteString(`<td class="` + rowClass + `" data-label="账号">`)
	b.WriteString(`<div class="acct-name"><strong>` + html.EscapeString(row.Label) + `</strong>`)
	b.WriteString(`<div class="acct-tags">` + variantBadge(row.Variant) + `</div></div>`)
	b.WriteString(`</td>`)

	// Participation toggle: an explicit state word, not a pill whose colour is the only
	// difference between the two states.
	b.WriteString(`<td data-label="参与"><button type="button" class="xs ` + stateCls + `-btn"` +
		` data-task-toggle="1" data-uid="` + html.EscapeString(row.UID) + `" data-action="` + action + `">` +
		stateText + `</button></td>`)

	b.WriteString(`<td data-label="状态"><span class="pill ` + runCls + `">` + runText + `</span></td>`)
	b.WriteString(`<td data-label="最近任务" class="uid">` + html.EscapeString(firstNonEmpty(row.LastTask, "—")) + `</td>`)
	b.WriteString(`<td data-label="执行时间" class="uid">` + html.EscapeString(row.LastRun) + `</td>`)

	// Controls. "展开任务" is a disclosure, not a mode: it shows this account's own tasks
	// in the row below, which is where an operator looks after starting a run.
	b.WriteString(`<td class="actions">`)
	if row.Growth {
		b.WriteString(`<button type="button" class="xs" data-task-expand="1" data-uid="` +
			html.EscapeString(row.UID) + `">展开任务</button>`)
		b.WriteString(`<button type="button" class="xs" data-task-run="1" data-uid="` +
			html.EscapeString(row.UID) + `">执行</button>`)
	} else {
		// Say why there is nothing to expand rather than offering a button that fails.
		b.WriteString(`<span class="note" title="成长任务中心仅国内版可用">国际版不适用</span>`)
	}
	b.WriteString(`</td>`)
	b.WriteString(`</tr>`)

	// The detail slot. Hidden until expanded so an idle page stays short.
	b.WriteString(`<tr class="task-detail-row" hidden><td colspan="6">` +
		`<div class="task-detail" data-detail-for="` + html.EscapeString(row.UID) + `"></div></td></tr>`)

	return b.String()
}

// growthEligibleVariant reports whether a credential's realm is served by the growth
// task centre.
//
// The growth centre is a China-mainland feature. The runner already filters on this;
// exposing the same predicate to the page keeps the button and the endpoint from
// disagreeing about who is eligible — which is how "展开任务" came to fail on an
// international account.
func growthEligibleVariant(variant string) bool {
	// An account with no recorded variant is treated as domestic, matching the runner's
	// default when the variant is unknown.
	if strings.TrimSpace(variant) == "" {
		return true
	}
	return wbVariant(variant).hasGrowthCenter()
}

// numberFrom reads a number that may arrive as any JSON numeric type.
func numberFrom(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	case json.Number:
		i, errAtoi := n.Int64()
		if errAtoi != nil {
			return 0
		}
		return int(i)
	}
	return 0
}
