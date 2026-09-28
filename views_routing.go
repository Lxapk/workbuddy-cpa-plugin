package main

// Routing strategy controls.
//
// These live on the settings page rather than on the account page. The strategy is a
// policy — how requests are spread over whoever happens to be in the pool — and the
// account page is about which accounts those are. Mixing the two made the account
// page carry a control that said nothing about the list above it.
//
// The order preview that used to sit here is gone with it: it listed account names and
// their credit balances, which is exactly what the account page already shows, and it
// made a settings card depend on live pool state.

import (
	"fmt"
	"html"
	"strings"
)

// renderRoutingBox draws the request-distribution strategy.
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
		tradeoff, _ := opt["tradeoff"].(string)
		b.WriteString(`<label class="opt"><input type="radio" name="strategy" value="` + html.EscapeString(value) + `"`)
		if value == current {
			b.WriteString(` checked`)
		}
		b.WriteString(`><span><span class="name">` + html.EscapeString(label) + `</span>` +
			`<span class="desc">` + html.EscapeString(desc) + `</span>`)
		if tradeoff != "" {
			b.WriteString(`<span class="desc tradeoff">` + html.EscapeString(tradeoff) + `</span>`)
		}
		b.WriteString(`</span></label>`)
	}

	b.WriteString(`<div class="note" style="margin-top:10px">当前：<b>` +
		html.EscapeString(fmt.Sprint(routing["strategy_label"])) + `</b> · ` +
		html.EscapeString(nextRotationHint()) + `</div>`)
	b.WriteString(`</div>`)
	b.WriteString(`</div>`)
	return b.String()
}
