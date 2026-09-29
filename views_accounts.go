package main

// The account pool's table, its filter bar, and the routing box.
//
// Kept apart from main_page.go so the page frame stays readable: the frame says
// which blocks a page has, and this file says what the pool block contains.

import (
	"fmt"
	"html"
	"strings"
	"time"
)

// renderAccountFilterBar is the strip above the pool table.
//
// Filtering happens in the browser because the rows are already in the document: a
// round trip per keystroke would be slower and would steal the focus the operator is
// typing into.
func renderAccountFilterBar() string {
	var b strings.Builder
	b.WriteString(`<div class="filter-bar">`)
	b.WriteString(`<span class="filter-search">`)
	b.WriteString(`<svg class="filter-icon" viewBox="0 0 16 16" width="14" height="14" aria-hidden="true">` +
		`<circle cx="7" cy="7" r="4.4" fill="none" stroke="currentColor" stroke-width="1.7"/>` +
		`<path d="M10.4 10.4L14 14" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round"/></svg>`)
	b.WriteString(`<input type="search" id="accountFilter" placeholder="搜索账号或备注" autocomplete="off">`)
	b.WriteString(`<button type="button" class="filter-clear" id="accountFilterClear" title="清除" aria-label="清除搜索" hidden>` +
		`<svg viewBox="0 0 16 16" width="12" height="12" aria-hidden="true">` +
		`<path d="M4 4l8 8M12 4l-8 8" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round"/></svg>` +
		`</button>`)
	b.WriteString(`</span>`)
	b.WriteString(`<select id="accountStatusFilter" aria-label="按状态筛选">`)
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
	b.WriteString(`<span class="filter-count" id="accountFilterCount"></span>`)
	b.WriteString(`</div>`)
	return b.String()
}

// accountCallStats returns how many calls an account has served and how many failed.
//
// Counted from the call history rather than tracked separately: the history already
// records every request with its account and outcome, and a second counter would have
// to be kept in step with it. The window is the same as the usage page shows.
func accountCallStats(uid string) (success, failed int) {
	if uid == "" {
		return 0, 0
	}
	for _, rec := range state.log.recent(500) {
		if rec.UID != uid && rec.Label != uid {
			continue
		}
		if rec.StatusCode >= 400 || rec.Error != "" {
			failed++
		} else {
			success++
		}
	}
	return success, failed
}

// renderAccountTable draws the pool.
//
// Columns: the account, its state, the credit ratio with a bar, the call tally, and
// the row's own controls. The uid sits under the name in small type — it is an
// identifier, not a column the operator scans by.
//
// 在途 / 用量 / 最近成功 were dropped at the operator's request: they answered
// questions nobody was asking in this table, and each cost a column of width that the
// remaining ones can use.
func renderAccountTable(accounts []workBuddyAccount) string {
	var b strings.Builder
	b.WriteString(`<div class="tbl-wrap"><table class="accounts" data-account-table="1"><thead><tr>`)
	b.WriteString(`<th>账号</th><th>状态</th><th>积分</th><th class="num">成功 / 失败</th>`)
	b.WriteString(`<th class="actions">操作</th>`)
	b.WriteString(`</tr></thead><tbody>`)

	for _, a := range accounts {
		b.WriteString(renderAccountRow(a))
	}

	b.WriteString(`</tbody></table></div>`)
	return b.String()
}

// renderAccountRow draws one account.
func renderAccountRow(a workBuddyAccount) string {
	pillClass, statusText := "ok", "可用"
	detail := ""
	switch {
	case a.AutoDisabled:
		// Distinguished from a manual disable so the operator knows the pool retired
		// it and can re-enable deliberately.
		pillClass, statusText = "bad", "自动禁用"
		detail = firstNonEmpty(a.DisabledReason, a.Reason)
	case a.DisabledByUser || a.Disabled:
		pillClass, statusText = "idle", "已停用"
		detail = firstNonEmpty(a.DisabledReason, a.Reason)
	case a.Expired:
		pillClass, statusText = "bad", "凭据异常"
		detail = a.Reason
	case !a.CooldownUntil.IsZero() && time.Now().Before(a.CooldownUntil):
		pillClass, statusText = "warn", "冷却中"
		detail = "至 " + a.CooldownUntil.Local().Format("15:04")
	}

	ident := firstNonEmpty(a.UID, a.AuthIndex)

	// Health tone for the strip on the first cell: green serving, amber parked,
	// red retired.
	bar := "ok"
	switch {
	case a.AutoDisabled || a.Disabled || a.DisabledByUser || a.Expired:
		bar = "bad"
	case !a.CooldownUntil.IsZero() && time.Now().Before(a.CooldownUntil):
		bar = "warn"
	}

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

	rowAction, rowActionLabel := "disable", "禁用"
	if a.DisabledByUser || a.Disabled || a.AutoDisabled {
		rowAction, rowActionLabel = "enable", "启用"
	}

	var b strings.Builder
	b.WriteString(`<tr data-status="` + filterStatus + `" data-search="` + html.EscapeString(searchText) + `">`)

	// Name with the uid abbreviated underneath.
	b.WriteString(`<td class="bar ` + bar + `" data-label="账号"><strong>` + html.EscapeString(a.Label) + `</strong>`)
	// The realm decides what an account can do — growth tasks and check-in exist only
	// for domestic accounts — so it belongs next to the name, on every list that names
	// one.
	b.WriteString(` ` + variantBadge(a.Variant))
	if ident != "" && ident != a.Label {
		b.WriteString(`<div class="uid mono" title="` + html.EscapeString(ident) + `">` +
			html.EscapeString(shortenUID(ident)) + `</div>`)
	}
	b.WriteString(`</td>`)

	b.WriteString(`<td data-label="状态"><span class="pill ` + pillClass + `">` + statusText + `</span>`)
	if detail != "" {
		b.WriteString(`<div class="uid">` + html.EscapeString(detail) + `</div>`)
	}
	b.WriteString(`</td>`)

	b.WriteString(renderCreditsCell(a, ident))

	success, failed := accountCallStats(ident)
	b.WriteString(`<td class="num mono" data-label="成功 / 失败">` +
		fmt.Sprint(success) + ` <span class="sep">/</span> ` +
		`<span class="` + map[bool]string{true: "bad-text", false: ""}[failed > 0] + `">` +
		fmt.Sprint(failed) + `</span></td>`)

	// Row controls: sign in, refresh this account's balance, run its tasks, disable.
	b.WriteString(`<td class="actions">`)
	b.WriteString(`<button type="button" class="xs" data-row-action="checkin" data-uid="` +
		html.EscapeString(ident) + `">签到</button>`)
	b.WriteString(`<button type="button" class="xs" data-row-action="quota" data-uid="` +
		html.EscapeString(ident) + `">余额</button>`)
	b.WriteString(`<button type="button" class="xs" data-row-action="tasks" data-uid="` +
		html.EscapeString(ident) + `">任务</button>`)
	b.WriteString(`<button type="button" class="xs ` +
		map[bool]string{true: "danger", false: ""}[rowAction == "disable"] + `"` +
		` data-account-toggle="1" data-uid="` + html.EscapeString(ident) + `"` +
		` data-action="` + rowAction + `" data-auth-index="` + html.EscapeString(a.AuthIndex) + `">` +
		rowActionLabel + `</button>`)
	b.WriteString(`</td>`)
	b.WriteString(`</tr>`)
	return b.String()
}

// variantBadge renders the realm an account belongs to.
//
// Shown next to the account name on every list that names an account, because the realm
// decides what the account can do: growth tasks and check-in only exist for domestic
// accounts. It used to appear only on the tasks page, and there it printed the raw
// value ("cn" / "ai") rather than a word.
func variantBadge(variant string) string {
	label, cls := variantBadgeText(variant)
	if label == "" {
		return ""
	}
	return `<span class="tag ` + cls + `">` + label + `</span>`
}

// variantBadgeText maps a realm to its display label and tone.
func variantBadgeText(variant string) (string, string) {
	switch strings.TrimSpace(variant) {
	case string(variantAi):
		return "国际", "tag-ai"
	case string(variantCn):
		return "国内", "tag-cn"
	default:
		// An account whose realm has not been recorded yet. Saying so is better than
		// guessing: the runner treats it as domestic, but that is a default, not a fact.
		return "未标注", "tag-unknown"
	}
}

// renderCreditsCell draws the balance as "remaining / total" over a progress bar.
//
// The ratio is what makes the number readable: 3735 alone says nothing, 3735 / 4600
// says four fifths of the cycle is still available. When the upstream reported no
// capacity there is nothing to divide by, so the bare remainder is shown instead.
func renderCreditsCell(a workBuddyAccount, ident string) string {
	var b strings.Builder
	b.WriteString(`<td data-label="积分" data-credits-for="` + html.EscapeString(ident) + `">`)

	if !a.CreditsKnown {
		b.WriteString(`<span class="uid">—</span></td>`)
		return b.String()
	}

	if a.CreditsTotal > 0 {
		pct := float64(a.Credits) / float64(a.CreditsTotal) * 100
		if pct < 0 {
			pct = 0
		}
		if pct > 100 {
			pct = 100
		}
		tone := "ok"
		switch {
		case pct <= 10:
			tone = "bad"
		case pct <= 30:
			tone = "warn"
		}
		b.WriteString(`<span class="credit-ratio mono"><span class="credit-remaining ` + tone + `">` +
			fmt.Sprint(a.Credits) + `</span><span class="credit-total uid"> / ` + fmt.Sprint(a.CreditsTotal) +
			`</span></span>`)
		b.WriteString(`<div class="credit-bar"><span class="` + tone + `" style="width:` +
			fmt.Sprintf("%.1f", pct) + `%"></span></div>`)
	} else {
		b.WriteString(`<span class="credit-ratio mono">` + fmt.Sprint(a.Credits) + `</span>`)
	}

	if a.CreditsExpired {
		b.WriteString(`<div class="uid bad-text">已过期</div>`)
	} else if a.CreditsExpiringSoon && a.CreditsExpireDays > 0 {
		b.WriteString(`<div class="uid warn-text">` + fmt.Sprint(a.CreditsExpireDays) + ` 天后过期</div>`)
	}
	b.WriteString(`</td>`)
	return b.String()
}

// shortenUID trims a long identifier for display, keeping both ends so it is still
// recognisable against the full value shown on hover.
func shortenUID(uid string) string {
	if len(uid) <= 22 {
		return uid
	}
	return uid[:12] + "…" + uid[len(uid)-6:]
}

// renderVariantBox draws the two supplier settings.
//
// They are shown as two separate groups rather than two rows of identical segmented
// buttons. Both are "pick one of three", so without a structural break the eye reads
// them as one question asked twice — and the two really are different: one decides
// which credentials serve a call, the other which realm a new authorisation belongs to.
func renderVariantBox(settings gatewaySettings) string {
	var b strings.Builder
	b.WriteString(`<div class="box">`)
	b.WriteString(`<header><h3>供应商</h3><span class="grow"></span>`)
	b.WriteString(`<span class="note" id="variantMsg"></span>`)
	b.WriteString(`</header>`)

	// ---- what calls use ----
	b.WriteString(`<div class="setting-group">`)
	b.WriteString(`<div class="setting-label">`)
	b.WriteString(`<span class="name">调用时使用哪些账号</span>`)
	b.WriteString(`<span class="desc">决定一次模型调用会拿到哪一组凭据。已登录的账号不受影响——它们只是被排除在调用之外。</span>`)
	b.WriteString(`</div>`)
	b.WriteString(`<div class="setting-control"><div class="seg" id="variantSeg">`)
	for _, opt := range []struct{ v, label, title string }{
		{"auto", "全部", "国内与国际账号都参与调用"},
		{"cn", "仅国内", "只调用 codebuddy.cn 账号"},
		{"ai", "仅国际", "只调用 workbuddy.ai 账号"},
	} {
		b.WriteString(`<button type="button" class="` +
			map[bool]string{true: "on", false: ""}[opt.v == settings.VariantOverride] + `"` +
			` data-call="setVariant" data-arg0="` + opt.v + `" title="` + opt.title + `">` + opt.label + `</button>`)
	}
	b.WriteString(`</div>`)
	// Spell out what the current choice means, so the pill row does not have to be
	// decoded.
	b.WriteString(`<div class="setting-effect">` + variantEffect(settings.VariantOverride) + `</div>`)
	b.WriteString(`</div></div>`)

	// ---- where new authorisations go ----
	b.WriteString(`<div class="setting-group">`)
	b.WriteString(`<div class="setting-label">`)
	b.WriteString(`<span class="name">新增授权的归属</span>`)
	b.WriteString(`<span class="desc">在 CPA 的 OAuth 登录页完成授权时，这个账号算国内还是国际。只影响新授权，不改变已有账号。</span>`)
	b.WriteString(`</div>`)
	b.WriteString(`<div class="setting-control"><div class="seg">`)
	for _, opt := range []struct{ v, label, title string }{
		{"follow", "跟随上面", "与「调用时使用哪些账号」选同一侧"},
		{"cn", "国内", "新授权记为 codebuddy.cn 账号"},
		{"ai", "国际", "新授权记为 workbuddy.ai 账号"},
	} {
		b.WriteString(`<button type="button" class="` +
			map[bool]string{true: "on", false: ""}[opt.v == settings.AuthSupplier] + `"` +
			` data-call="setAuthSupplier" data-arg0="` + opt.v + `" title="` + opt.title + `">` + opt.label + `</button>`)
	}
	b.WriteString(`</div>`)
	b.WriteString(`<div class="setting-effect">` + authSupplierEffect(settings.AuthSupplier, settings.VariantOverride) + `</div>`)
	b.WriteString(`</div></div>`)

	b.WriteString(`<div class="foot">`)
	b.WriteString(`<span class="note">要同时使用两个供应商：先把「新增授权的归属」设为国内，去 CPA 完成一次授权；` +
		`再设为国际，完成第二次；最后把「调用时使用哪些账号」保持为「全部」。</span>`)
	b.WriteString(`</div>`)
	b.WriteString(`</div>`)
	return b.String()
}

// variantEffect spells out the current call-side choice.
func variantEffect(variant string) string {
	switch variant {
	case "cn":
		return `当前：只有国内账号会收到调用；国际账号即使已登录也不参与。成长任务同样只支持国内账号。`
	case "ai":
		return `当前：只有国际账号会收到调用；国内账号即使已登录也不参与。成长任务与签到需要国内账号，当前不可用。`
	default:
		return `当前：两组账号一起参与调用，按路由策略挑选。`
	}
}

// authSupplierEffect spells out the current authorisation-side choice.
func authSupplierEffect(supplier, variant string) string {
	switch supplier {
	case "cn":
		return `当前：新授权记为国内账号。`
	case "ai":
		return `当前：新授权记为国际账号。`
	default:
		switch variant {
		case "cn":
			return `当前：新授权记为国内账号（跟随「调用时使用哪些账号」）。`
		case "ai":
			return `当前：新授权记为国际账号（跟随「调用时使用哪些账号」）。`
		default:
			return `当前：因为「调用时使用哪些账号」是「全部」，新授权会记为国内账号。` +
				`如果这次要授权国际账号，请先把它改成「仅国际」。`
		}
	}
}
