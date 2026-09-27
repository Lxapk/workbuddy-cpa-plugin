package main

import (
	"strings"
	"testing"
)

// 账号表的每个数据单元格都要带 data-label。
//
// 手机上表格被改成块级布局：表头整行隐藏，每个字段独占一行，靠 data-label 生成的
// ::before 告诉读者这一行是什么。少了这个属性，用户看到的是一列没有名字的值。
func TestAccountCellsCarryDataLabels(t *testing.T) {
	resetState()
	seedPanelAccounts(t)
	page := renderMainPage()

	for _, label := range []string{
		`data-label="账号"`,
		`data-label="UID"`,
		`data-label="积分"`,
		`data-label="到期"`,
		`data-label="状态"`,
	} {
		if !strings.Contains(page, label) {
			t.Errorf("账号表缺少 %s", label)
		}
	}
}

// 表格必须包在滚动容器里。
//
// 六个列的表格在窄屏上放不下；让页面本身横向滚动会把标签栏一起带走，所以溢出
// 必须限定在表格容器内。
func TestTablesAreWrapped(t *testing.T) {
	resetState()
	seedPanelAccounts(t)
	page := renderMainPage()

	opens := strings.Count(page, `<div class="table-wrap">`)
	closes := strings.Count(page, `</table></div>`)
	if opens == 0 {
		t.Fatal("没有表格包上滚动容器")
	}
	if opens != closes {
		t.Errorf("table-wrap 未配对：开 %d，闭 %d", opens, closes)
	}
}

// 操作列的按钮必须在 data-label 体系之外单独可点。
//
// 三个按钮挤在最后一列时只有第一个能点到，其余两个被屏幕边缘裁掉。它们现在是
// 独立的一行，这个测试守住 td.actions 这个钩子一直存在。
func TestActionCellIsMarked(t *testing.T) {
	resetState()
	seedPanelAccounts(t)
	page := renderMainPage()

	if !strings.Contains(page, `<td class="actions">`) {
		t.Error("操作列缺少 class=\"actions\"，窄屏布局无从生效")
	}
	if !strings.Contains(page, `<th class="actions">操作</th>`) {
		t.Error("操作列表头缺少 class")
	}
	// 三个操作都要在
	for _, action := range []string{
		`data-account-toggle="1"`,
		`data-account-checkin="1"`,
		`data-account-quota="1"`,
	} {
		if !strings.Contains(page, action) {
			t.Errorf("操作列缺少 %s", action)
		}
	}
}

// 窄屏样式必须存在，并且覆盖了表格、触摸目标与提示条。
//
// 表格在窄屏保持横向（一行一个账号），只把溢出限制在表格内部：把它拆成每个
// 字段一行的堆叠布局会让列表高得离谱，而找某个账号时需要的正是整行一眼扫过。
func TestNarrowViewportStylesExist(t *testing.T) {
	css := uiCSS

	for _, want := range []string{
		"@media (max-width: 720px)",
		"@media (max-width: 400px)",
		".table-wrap",          // 溢出受控的容器
		"button.icon",          // 图标按钮
		".filter-bar",          // 筛选条
		"min-height: 42px",     // 触摸目标下限
		"font-size: 16px",      // 避免 iOS 聚焦时自动缩放
		"#toasts { left: 12px", // 提示条不越界
	} {
		if !strings.Contains(css, want) {
			t.Errorf("窄屏样式缺少 %s", want)
		}
	}

	// 表格不能再被拆成块级堆叠。
	if strings.Contains(css, ".table-wrap tbody { display: block; }") {
		t.Error("表格被改成块级堆叠，横向紧凑布局丢失")
	}
}

// 筛选条要能占满卡片宽度，不能把搜索框钉死成固定宽度。
//
// 之前用内联 flex:1 + 固定宽度的下拉，在手机宽度下右侧会剩下约 40px 死区。
func TestFilterBarFillsWidth(t *testing.T) {
	css := uiCSS
	resetState()
	seedPanelAccounts(t)
	page := renderMainPage()

	// 搜索框用弹性宽度，下拉按内容定宽。
	if !strings.Contains(css, ".filter-search") || !strings.Contains(css, "flex: 1 1 200px") {
		t.Error("筛选条缺少弹性搜索框，无法吞满剩余宽度")
	}
	if !strings.Contains(css, ".filter-select") || !strings.Contains(css, "flex: 0 0 auto") {
		t.Error("状态下拉未按内容定宽")
	}

	// 结构：搜索框 + 清除按钮 + 下拉 + 计数，都在 .filter-bar 里。
	for _, want := range []string{
		`class="filter-bar"`,
		`class="filter-search"`,
		`id="accountFilter"`,
		`id="accountFilterClear"`,
		`id="accountStatusFilter"`,
		`class="filter-select"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("筛选条缺少 %s", want)
		}
	}
	// 计数靠右而不是硬塞在中间。
	if !strings.Contains(css, ".filter-count { flex: 0 0 auto; margin-left: auto; }") {
		t.Error("筛选计数未靠右")
	}
}

// 图标按钮必须带 title 与 aria-label。
//
// 图标本身是有歧义的：没有可访问名，读屏软件只能念出「按钮」。
func TestIconButtonsAreLabelled(t *testing.T) {
	resetState()
	seedPanelAccounts(t)
	page := renderMainPage()

	for _, want := range []string{
		`title="停用" aria-label="停用"`,
		`title="签到" aria-label="签到"`,
		`title="刷新积分" aria-label="刷新积分"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("图标按钮缺少可访问名：期望 %s", want)
		}
	}
	// 旧的可视文字版本不应残留在账号表里（它撑宽了操作列）。
	// 只检查按钮的文本节点，别处（任务页）的文字按钮是另一回事；属性里出现
	// 「签到」是正常的（title / aria-label 就是给它命名的）。
	actionCell := extractActionCells(page)
	if hasButtonText(actionCell) {
		t.Errorf("账号表的操作列仍在渲染文字节点，会撑宽最后一列：%s", actionCell)
	}
}

// hasButtonText reports whether any button in the markup has a text node.
//
// The icon buttons carry their label in title/aria-label, so a plain substring
// search would match the attribute and report a problem that is not there; the
// icon itself is markup, not text. Only the text left after removing every tag
// counts — that is what widens the column.
func hasButtonText(markup string) bool {
	for _, inner := range buttonInnerHTML(markup) {
		if stripTags(inner) != "" {
			return true
		}
	}
	return false
}

// buttonInnerHTML returns the content between each <button …> and its </button>.
func buttonInnerHTML(markup string) []string {
	var out []string
	rest := markup
	for {
		start := strings.Index(rest, ">")
		if start < 0 {
			return out
		}
		rest = rest[start+1:]
		end := strings.Index(rest, "</button>")
		if end < 0 {
			return out
		}
		out = append(out, rest[:end])
		rest = rest[end:]
	}
}

// stripTags removes every <…> tag and collapses whitespace.
func stripTags(markup string) string {
	var out strings.Builder
	depth := 0
	for _, r := range markup {
		switch {
		case r == '<':
			depth++
		case r == '>':
			if depth > 0 {
				depth--
			}
		case depth == 0:
			out.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(out.String()), "")
}

// extractActionCells concatenates the contents of every action cell on the page.
//
// Scoped on purpose: the task tab legitimately has labelled buttons, so a
// page-wide search for the label would fail for the wrong reason.
func extractActionCells(page string) string {
	const open = `<td class="actions">`
	var out strings.Builder
	rest := page
	for {
		start := strings.Index(rest, open)
		if start < 0 {
			break
		}
		rest = rest[start+len(open):]
		end := strings.Index(rest, `</td>`)
		if end < 0 {
			break
		}
		out.WriteString(rest[:end])
		rest = rest[end:]
	}
	return out.String()
}
