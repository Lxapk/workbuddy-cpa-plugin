package main

import (
	"strings"
	"testing"
	"time"
)

// 每个标签页都必须有内容，且整页的标记结构闭合正确。
//
// 曾经的回归：给表格包滚动容器时，给一个只输出 <tr> 的函数多补了一个 </div>。
// 多出的闭合标签提前关掉了外层容器，于是账号页之后的每个标签页都变成空白——
// 页面上看不出报错，只是什么都没有。
//
// 检查用真正的解析器而不是数标签：页面里存在 <div> 这类字面量（说明文字、
// data 属性），字符串计数会把它们算成标记，得出错误的结论。
func TestEveryTabRendersContent(t *testing.T) {
	resetState()
	seedPanelAccounts(t)
	page := renderMainPage()

	tabs := []string{
		"tab-tasks", "tab-accounts", "tab-switch",
		"tab-credits", "tab-usage", "tab-settings",
	}
	for _, tab := range tabs {
		section := sectionOf(page, tab)
		if section == "" {
			t.Errorf("缺少标签页 %s", tab)
			continue
		}
		if len(section) < 150 {
			t.Errorf("%s 内容过短（%d 字节），可能是空白页", tab, len(section))
		}
	}

	if problem := unbalancedMarkup(page); problem != "" {
		t.Errorf("标记未闭合：%s", problem)
	}
}

// unbalancedMarkup reports the first structural imbalance in the markup, or "" when
// it is well-formed.
//
// A small scanner rather than an HTML parser: pulling in golang.org/x/net for a
// single test would put it in go.mod as a production dependency, and the question
// here is narrow — are the tags closed, and in the right order.
//
// Nothing is treated as an error merely for being unusual; only genuinely unclosed
// tags and stray close tags are.
func unbalancedMarkup(page string) string {
	markup := stripScriptBlocks(page)
	var stack []string

	for i := 0; i < len(markup); {
		if markup[i] != '<' {
			i++
			continue
		}
		end := strings.IndexByte(markup[i:], '>')
		if end < 0 {
			break
		}
		token := markup[i+1 : i+end]
		i += end + 1

		switch {
		case strings.HasPrefix(token, "/"):
			tag := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(token, "/")))
			tag = strings.Fields(tag + " ")[0]
			if isVoidElement(tag) {
				continue
			}
			if len(stack) == 0 {
				return "多余的闭合标签 </" + tag + ">"
			}
			if top := stack[len(stack)-1]; top != tag {
				return "闭合不匹配：期望 </" + top + ">，得到 </" + tag + ">"
			}
			stack = stack[:len(stack)-1]
		case strings.HasPrefix(token, "!"), strings.HasPrefix(token, "?"):
			// Comment, doctype or processing instruction.
		default:
			tag := strings.ToLower(strings.TrimSpace(token))
			tag = strings.Fields(tag + " ")[0]
			if tag == "" || isVoidElement(tag) || strings.HasSuffix(token, "/") {
				continue
			}
			stack = append(stack, tag)
		}
	}

	if len(stack) > 0 {
		return "未闭合: <" + strings.Join(stack, "> <") + ">"
	}
	return ""
}

// isVoidElement reports whether a tag never has a closing counterpart.
func isVoidElement(tag string) bool {
	switch tag {
	case "area", "base", "br", "col", "embed", "hr", "img", "input",
		"link", "meta", "param", "source", "track", "wbr", "path", "circle":
		return true
	}
	return false
}

// stripScriptBlocks removes <script> and <style> blocks so tag counting sees only
// markup.
//
// The style block matters as much as the script one: selectors contain ">" (child
// combinators), and an inline SVG data URI inside it can carry stray angle brackets,
// either of which the scanner would read as markup.
func stripScriptBlocks(page string) string {
	page = stripBlock(page, "<script", "</script>")
	return stripBlock(page, "<style", "</style>")
}

// stripBlock removes every block delimited by open and close markers, including the
// markers themselves.
//
// Leaving the close marker behind is what broke this the first time: the scanner
// then met a </style> with no matching <style> on the stack and reported a mismatch
// against an unrelated open tag.
func stripBlock(page, open, close string) string {
	var out strings.Builder
	rest := page
	for {
		start := strings.Index(rest, open)
		if start < 0 {
			out.WriteString(rest)
			return out.String()
		}
		out.WriteString(rest[:start])
		rest = rest[start:]
		end := strings.Index(rest, close)
		if end < 0 {
			// Unterminated block: drop the remainder rather than leaving the
			// scanner to trip over its contents.
			return out.String()
		}
		rest = rest[end+len(close):]
	}
}

// sectionOf returns the slice of the page belonging to one tab container.
//
// Cuts at the next tab marker: the page is built by concatenation and each tab is a
// top-level panel, so the marker is an unambiguous boundary and finding the matching
// close tag would need real nesting analysis.
func sectionOf(page, tab string) string {
	marker := `<div id="` + tab + `" class="wb-panel`
	start := strings.Index(page, marker)
	if start < 0 {
		return ""
	}
	rest := page[start:]
	if next := strings.Index(rest[1:], `<div id="tab-`); next > 0 {
		rest = rest[:next+1]
	}
	return rest
}

// 表格的滚动容器必须自有开闭，且窄屏能滚动。
//
// 出错的形状是一个只渲染 <tr> 的函数被外部补上了 </table></div>——那个函数所在
// 的表格并没有开 table-wrap，多出来的 </div> 就落到了外层容器头上。
func TestTableWrappersAreBalanced(t *testing.T) {
	resetState()
	seedPanelAccounts(t)

	page := renderMainPage()
	if opens, closes := strings.Count(page, `<div class="table-wrap">`), strings.Count(page, `</table></div>`); opens != closes {
		t.Errorf("table-wrap 未配平：开 %d 闭 %d", opens, closes)
	}

	// 积分页在「已刷新过」的状态下应当有表格；未刷新时是引导文案，两种都合法。
	state.quota.mu.Lock()
	state.quota.lastRun = []quotaRefreshResult{
		{Label: "国内一号", AuthID: "auth-cn-1", Region: "cn", Credits: 100, Message: "ok"},
	}
	state.quota.mu.Unlock()

	quotaTab := sectionOf(renderMainPage(), "tab-credits")
	if !strings.Contains(quotaTab, `<div class="table-wrap">`) {
		t.Error("积分页的结果表没有包滚动容器")
	}

	// 结果表自带 table-wrap，开闭必须配平。
	results := renderQuotaResults([]quotaRefreshResult{
		{Label: "国内一号", AuthID: "auth-cn-1", Region: "cn", Credits: 100, Message: "ok"},
	})
	if problem := unbalancedMarkup(results); problem != "" {
		t.Errorf("结果表标记未闭合：%s", problem)
	}
}

// 操作列用带文字的紧凑按钮，不再用图标。
//
// 图标方案被回退：每个字形都要学一遍才知道是什么意思，而这里的文字都只有两个字，
// 三个按钮放得下。这个测试守着「改回文字」不再被推翻。
//
// 按钮文字取决于账号当前状态：可用时显示「禁用」，停用时显示「启用」。所以断言
// 两组里各取一个，而不是要求两组同时出现。
func TestActionButtonsUseShortLabels(t *testing.T) {
	resetState()
	seedPanelAccounts(t)
	page := renderMainPage()

	cell := extractActionCells(page)
	if cell == "" {
		t.Fatal("未找到操作列")
	}
	// 行内只保留启停开关。逐账号的签到与积分按钮已按需求移除：面板上已有一次性
	// 刷新全部积分、签到也由计划对所有人执行，行内再放一份只是增加杂乱。
	if !strings.Contains(cell, `data-account-toggle="1"`) {
		t.Error("操作列缺少启停按钮")
	}
	for _, removed := range []string{`data-account-checkin`, `data-account-quota`} {
		if strings.Contains(cell, removed) {
			t.Errorf("操作列仍含已移除的 %s", removed)
		}
	}
	// 启停按钮的文字随状态变化，但二者之一必须出现。
	if !strings.Contains(cell, "禁用") && !strings.Contains(cell, "启用") {
		t.Error("操作列缺少启停按钮")
	}
	// 只有启停按钮的文字，且随状态在「启用/禁用」之间切换。
	if !strings.Contains(cell, "启用") && !strings.Contains(cell, "禁用") {
		t.Error("操作列缺少启停按钮文字")
	}
	if !strings.Contains(cell, `class="ghost mini"`) {
		t.Error("操作按钮缺少 mini 类，窄屏下会撑宽")
	}
	if strings.Contains(cell, "<svg") {
		t.Error("操作列仍在渲染图标，应改回文字")
	}
}

// 停用状态的账号，其按钮应当变成「启用」。
func TestDisabledAccountShowsEnableButton(t *testing.T) {
	resetState()
	state.accounts.mu.Lock()
	state.accounts.cached = []workBuddyAccount{
		{Label: "停用的", UID: "uid-off", AuthIndex: "auth-off", Variant: "cn", DisabledByUser: true},
	}
	state.accounts.fetchedAt = time.Now()
	state.accounts.mu.Unlock()

	cell := extractActionCells(renderMainPage())
	if !strings.Contains(cell, "启用") {
		t.Errorf("停用账号应显示「启用」按钮，实际：%s", cell)
	}
}
