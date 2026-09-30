package main

// Routing strategy controls.
//
// These live on the settings page rather than on the account page. The strategy is a
// policy — how requests are spread over whoever happens to be in the pool — and the
// account page is about which accounts those are.
//
// The card is one row: a segmented control with the four strategies, and a single line
// under it saying what the highlighted one does and what it costs. It used to be four
// stacked option cards, each with two lines of prose, which made the settings page tall
// enough that the supplier card below it needed a scroll to reach — especially on a
// phone. The prose is still there, one strategy at a time, swapped in on click.

import (
	"fmt"
	"html"
	"strings"
)

// renderRoutingBox draws the request-distribution strategy.
func renderRoutingBox(routing map[string]any) string {
	var b strings.Builder
	b.WriteString(`<div class="box routing-card">`)
	b.WriteString(`<header><h3>路由策略 <span class="hint">请求如何在这些账号之间分配</span></h3>`)
	b.WriteString(`<span class="grow"></span>`)
	b.WriteString(`<span class="note" id="strategyMsg"></span>`)
	b.WriteString(`</header>`)

	options, _ := routing["options"].([]map[string]any)
	current, _ := routing["strategy"].(string)

	b.WriteString(`<div class="setting-group">`)
	b.WriteString(`<div class="setting-control setting-control-wide">`)
	b.WriteString(`<div class="seg seg-4" id="strategySeg">`)
	effect := ""
	var attrs strings.Builder
	for _, opt := range options {
		value, _ := opt["value"].(string)
		label, _ := opt["label"].(string)
		desc, _ := opt["description"].(string)
		tradeoff, _ := opt["tradeoff"].(string)
		line := strings.TrimSpace(desc + " " + tradeoff)
		if value == current {
			effect = line
		}
		// data-<value> on the effect line carries each strategy's text, the same
		// mechanism the supplier card uses, so a click can swap it without a request.
		attrs.WriteString(` data-` + html.EscapeString(value) + `="` + html.EscapeString(line) + `"`)
		b.WriteString(`<button type="button" class="` +
			map[bool]string{true: "on", false: ""}[value == current] + `"` +
			` data-value="` + html.EscapeString(value) + `"` +
			` data-call="pickStrategy" data-arg0="` + html.EscapeString(value) + `"` +
			` title="` + html.EscapeString(desc) + `">` + html.EscapeString(label) + `</button>`)
	}
	b.WriteString(`</div>`)
	b.WriteString(`<div class="setting-effect" id="strategySegEffect"` + attrs.String() + `>` +
		html.EscapeString(effect) + `</div>`)
	b.WriteString(`</div></div>`)

	b.WriteString(`<div class="foot">`)
	b.WriteString(`<span class="note">当前：<b id="strategyCurrent">` +
		html.EscapeString(fmt.Sprint(routing["strategy_label"])) + `</b> · <span id="rotationHint">` +
		html.EscapeString(nextRotationHint()) + `</span></span>`)
	// The strategy applies on click, like the supplier switch below it, so there is no
	// separate apply button; only the rotation reset remains.
	b.WriteString(`<button type="button" class="xs" data-call="resetRotation">重置轮巡位置</button>`)
	b.WriteString(`</div>`)
	b.WriteString(`</div>`)
	return b.String()
}
