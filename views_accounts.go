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

// renderAccountTable draws the pool.
//
// Columns: the account (name with its uid underneath), credits, expiry, status, and
// the rows own controls. There is no separate uid column — it read almost the same as
// the name and spent a column's width saying so.
func renderAccountTable(accounts []workBuddyAccount) string {
	var b strings.Builder
	b.WriteString(`<div class="tbl-wrap"><table class="stack" data-account-table="1"><thead><tr>`)
	b.WriteString(`<th>账号</th><th class="num">积分</th><th>到期</th><th>状态</th><th class="actions">操作</th>`)
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

	cv := "—"
	if a.CreditsKnown {
		cv = fmt.Sprint(a.Credits)
	}

	// Health tone for the strip on the first cell: green serving, amber parked,
	// red retired.
	bar := "ok"
	switch {
	case a.AutoDisabled || a.Disabled || a.DisabledByUser || a.Expired:
		bar = "bad"
	case !a.CooldownUntil.IsZero() && time.Now().Before(a.CooldownUntil):
		bar = "warn"
	}

	expiry, expiryClass := "—", ""
	switch {
	case a.CreditsExpired:
		expiry, expiryClass = "已过期", "bad"
	case a.CreditsExpireAt > 0 && a.CreditsExpiringSoon:
		expiry, expiryClass = fmt.Sprintf("%d 天后", a.CreditsExpireDays), "warn"
	case a.CreditsExpireAt > 0:
		expiry = fmt.Sprintf("%d 天后", a.CreditsExpireDays)
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

	ident := firstNonEmpty(a.UID, a.AuthIndex)
	rowAction, rowActionLabel := "disable", "停用"
	if a.DisabledByUser || a.Disabled || a.AutoDisabled {
		rowAction, rowActionLabel = "enable", "启用"
	}

	var b strings.Builder
	b.WriteString(`<tr data-status="` + filterStatus + `" data-search="` + html.EscapeString(searchText) + `">`)

	// Name, with the uid underneath in small type when it says something the name
	// does not.
	b.WriteString(`<td class="bar ` + bar + `" data-label="账号"><strong>` + html.EscapeString(a.Label) + `</strong>`)
	if ident != "" && ident != a.Label {
		b.WriteString(`<div class="note mono">` + html.EscapeString(ident) + `</div>`)
	}
	b.WriteString(`</td>`)

	b.WriteString(`<td class="num" data-label="积分" data-credits-for="` + html.EscapeString(ident) + `">` +
		html.EscapeString(cv) + `</td>`)
	b.WriteString(`<td data-label="到期" class="` + expiryClass + `">` + html.EscapeString(expiry) + `</td>`)
	b.WriteString(`<td data-label="状态"><span class="pill ` + pillClass + `">` + statusText + `</span>`)
	if detail != "" {
		b.WriteString(` <span class="note">` + html.EscapeString(detail) + `</span>`)
	}
	b.WriteString(`</td>`)
	b.WriteString(`<td class="actions"><button type="button" class="xs ghost ` +
		map[bool]string{true: "", false: "danger"}[rowAction == "enable"] + `"` +
		` data-account-toggle="1" data-uid="` + html.EscapeString(ident) + `"` +
		` data-action="` + rowAction + `" data-auth-index="` + html.EscapeString(a.AuthIndex) + `">` +
		rowActionLabel + `</button></td>`)
	b.WriteString(`</tr>`)
	return b.String()
}

// renderRoutingBox draws the request-distribution strategy and a preview of the
// resulting order.
//
// The strategy and the pool are the same subject — how requests are spread over
// these accounts — so they share a page, and the preview sits inside the same card as
// the control that produces it.
func renderRoutingBox(routing map[string]any) string {
	var b strings.Builder
	b.WriteString(`<div class="box">`)
	b.WriteString(`<header><h3>路由策略 <span class="hint">请求如何在这些账号之间分配</span></h3>`)
	b.WriteString(`<span class="grow"></span>`)
	b.WriteString(`<span class="note" id="strategyMsg"></span>`)
	b.WriteString(`<button type="button" class="xs" data-call="resetRotation">重置轮巡位置</button>`)
	b.WriteString(`<button type="button" class="xs primary" data-call="saveStrategy">应用策略</button>`)
	b.WriteString(`</header>`)
	b.WriteString(`<div class="pad">`)

	options, _ := routing["options"].([]map[string]any)
	current, _ := routing["strategy"].(string)
	for _, opt := range options {
		value, _ := opt["value"].(string)
		label, _ := opt["label"].(string)
		desc, _ := opt["description"].(string)
		b.WriteString(`<label class="opt"><input type="radio" name="strategy" value="` + html.EscapeString(value) + `"`)
		if value == current {
			b.WriteString(` checked`)
		}
		b.WriteString(`><span><span class="name">` + html.EscapeString(label) + `</span><br>` +
			`<span class="desc">` + html.EscapeString(desc) + `</span></span></label>`)
	}

	b.WriteString(`<div class="note" style="margin-top:10px">当前：<b>` +
		html.EscapeString(fmt.Sprint(routing["strategy_label"])) + `</b> · ` +
		html.EscapeString(nextRotationHint()) + `</div>`)
	b.WriteString(`</div>`)

	if rows, okRows := routing["order"].([]map[string]any); okRows && len(rows) > 0 {
		b.WriteString(`<div class="tbl-wrap"><table><thead><tr>`)
		b.WriteString(`<th class="num">#</th><th>账号</th><th class="num">积分</th><th class="num">已选中</th>`)
		b.WriteString(`</tr></thead><tbody>`)
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
		b.WriteString(`</tbody></table></div>`)
	} else {
		b.WriteString(`<div class="empty">暂无可用账号，无法预览顺序。</div>`)
	}

	b.WriteString(`</div>`)
	return b.String()
}

// renderQuotaBox draws the outcome of the last credit sweep.
//
// The schedule controls used to live here too. They are gone: the panel fetches every
// account's balance on load, which is what the operator actually wanted — the
// per-schedule toggles only asked them to configure something the panel could decide.
func renderQuotaBox(settings gatewaySettings) string {
	state.quota.mu.Lock()
	lastRun := append([]quotaRefreshResult(nil), state.quota.lastRun...)
	state.quota.mu.Unlock()

	var b strings.Builder
	b.WriteString(`<div class="box">`)
	b.WriteString(`<header><h3>积分刷新结果 <span class="hint">进入面板时自动刷新</span></h3>`)
	b.WriteString(`<span class="grow"></span>`)
	b.WriteString(`<span class="note" id="quotaMsg"></span>`)
	b.WriteString(`</header>`)

	b.WriteString(`<div id="quotaResults">`)
	if len(lastRun) == 0 {
		b.WriteString(`<div class="empty">正在查询各账号的积分…若长时间没有结果，点上方「刷新账号与积分」。</div>`)
	} else {
		b.WriteString(renderQuotaResults(lastRun))
	}
	b.WriteString(`</div>`)
	b.WriteString(`</div>`)
	return b.String()
}

// renderVariantBox draws the provider selection.
func renderVariantBox(settings gatewaySettings) string {
	var b strings.Builder
	b.WriteString(`<div class="box">`)
	b.WriteString(`<header><h3>供应商 <span class="hint">仅影响模型调用</span></h3><span class="grow"></span>`)
	// The segmented controls apply on click, so no "apply" button is needed: one
	// would only add a step that changes nothing.
	b.WriteString(`</header>`)
	b.WriteString(`<div class="pad">`)
	b.WriteString(`<div class="note">调用设置 <span class="hint">决定<b>调用</b>时使用哪些账号，不影响已登录账号的归属</span></div>`)
	b.WriteString(`<div class="seg" id="variantSeg">`)
	for _, opt := range []struct{ v, label, title string }{
		{"auto", "全部供应商", "两组账号都参与调用"},
		{"cn", "国内供应商", "只调用 codebuddy.cn 账号"},
		{"ai", "国际供应商", "只调用 workbuddy.ai 账号"},
	} {
		b.WriteString(`<button type="button" class="` +
			map[bool]string{true: "on", false: ""}[opt.v == settings.VariantOverride] + `"` +
			` data-call="setVariant" data-arg0="` + opt.v + `" title="` + opt.title + `">` + opt.label + `</button>`)
	}
	b.WriteString(`</div>`)
	b.WriteString(`<div class="note" style="margin-top:12px">授权来源 <span class="hint">只决定授权走哪一侧；新授权在 CPA 的 OAuth 登录中完成</span></div>`)
	b.WriteString(`<div class="seg">`)
	for _, opt := range []struct{ v, label string }{
		{"follow", "跟随调用设置"},
		{"cn", "国内授权"},
		{"ai", "国际授权"},
	} {
		b.WriteString(`<button type="button" class="` +
			map[bool]string{true: "on", false: ""}[opt.v == settings.AuthSupplier] + `"` +
			` data-call="setAuthSupplier" data-arg0="` + opt.v + `">` + opt.label + `</button>`)
	}
	b.WriteString(`</div>`)
	b.WriteString(`<span class="note" id="variantMsg"></span>`)
	b.WriteString(`<div class="note" style="margin-top:12px"><strong>要两个供应商的账号</strong>：` +
		`这里选国内授权 → 到 CPA 完成授权；再选国际授权 → 到 CPA 完成授权；` +
		`之后把上面的调用设置保持为「全部供应商」，两组账号会一起参与调用。</div>`)
	b.WriteString(`<span class="note" id="authMsg"></span>`)
	b.WriteString(`</div></div>`)
	return b.String()
}
