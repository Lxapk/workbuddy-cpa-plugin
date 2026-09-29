package main

// Credit-sweep feedback for the account card.
//
// The per-account balances are columns on the account table, so a second table of the
// same numbers was redundancy. What the table cannot show is an account whose balance
// could not be read at all — the cell keeps whatever it had, and nothing says the last
// attempt failed. So this only surfaces the failures, and stays silent when the sweep
// was clean.

import (
	"fmt"
	"html"
	"strings"
)

// renderQuotaSweepNotes lists the accounts whose last credit read failed.
func renderQuotaSweepNotes() string {
	state.quota.mu.Lock()
	lastRun := append([]quotaRefreshResult(nil), state.quota.lastRun...)
	ranAt := state.quota.lastRunAt
	state.quota.mu.Unlock()

	if len(lastRun) == 0 {
		return `<div class="foot">正在查询各账号的积分…</div>`
	}

	var failed []quotaRefreshResult
	for _, r := range lastRun {
		if r.Error != "" {
			failed = append(failed, r)
		}
	}

	var b strings.Builder
	if len(failed) == 0 {
		b.WriteString(`<div class="foot">全部 ` + fmt.Sprint(len(lastRun)) + ` 个账号的积分已更新`)
		if !ranAt.IsZero() {
			b.WriteString(` · ` + ranAt.In(panelLocation).Format("15:04"))
		}
		b.WriteString(`</div>`)
		return b.String()
	}

	b.WriteString(`<div class="foot">` + fmt.Sprint(len(failed)) + ` 个账号的积分查询失败：`)
	parts := make([]string, 0, len(failed))
	for _, r := range failed {
		parts = append(parts, html.EscapeString(firstNonEmpty(r.Label, r.AuthID, r.UID))+
			`（`+html.EscapeString(r.Error)+`）`)
	}
	b.WriteString(strings.Join(parts, "、"))
	b.WriteString(`</div>`)
	return b.String()
}
