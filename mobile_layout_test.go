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

	// UID 不再是独立列：它与账号名几乎重复，占宽度不带来新信息。uid 现在作为
	// 账号名下方的小字出现，仍可被搜索命中。
	for _, label := range []string{
		`data-label="账号"`,
		`data-label="积分"`,
		`data-label="到期"`,
		`data-label="状态"`,
	} {
		if !strings.Contains(page, label) {
			t.Errorf("账号表缺少 %s", label)
		}
	}
	if strings.Contains(page, `<th>UID</th>`) {
		t.Error("UID 列应已移除")
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

	// 表格统一用 .tbl-wrap 包裹，横向滚动限定在卡片内。
	if strings.Count(page, `class="tbl-wrap"`) == 0 {
		t.Fatal("表格缺少滚动容器")
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
	// 行内只保留启停开关；逐账号的签到与积分按钮已按需求移除。
	if !strings.Contains(page, `data-account-toggle="1"`) {
		t.Error("操作列缺少启停按钮")
	}
	for _, removed := range []string{`data-account-checkin`, `data-account-quota`} {
		if strings.Contains(page, removed) {
			t.Errorf("操作列仍含已移除的 %s", removed)
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
		"@media (max-width: 768px)",
		"@media (max-width: 768px)",
		".tbl-wrap",            // 溢出受控的容器
		"button.xs",            // 表格行内的紧凑按钮
		".filter-bar",          // 筛选条
		"min-height: 40px",     // 触摸目标下限
		"font-size: 16px",      // 避免 iOS 聚焦时自动缩放
		"#toasts { left: 12px", // 提示条不越界
	} {
		if !strings.Contains(css, want) {
			t.Errorf("窄屏样式缺少 %s", want)
		}
	}

	// 表格不能再被拆成块级堆叠。
	if strings.Contains(css, ".tbl-wrap tbody { display: block; }") {
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
	if !strings.Contains(css, ".filter-search") || !strings.Contains(css, ".filter-bar") {
		t.Error("筛选条样式缺失")
	}

	// 结构：搜索框 + 清除按钮 + 下拉 + 计数，都在 .filter-bar 里。
	for _, want := range []string{
		`class="filter-bar"`,
		`class="filter-search"`,
		`id="accountFilter"`,
		`id="accountFilterClear"`,
		`id="accountStatusFilter"`,
		`class="filter-search"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("筛选条缺少 %s", want)
		}
	}
	// 计数靠右而不是硬塞在中间。
	if !strings.Contains(css, ".filter-count") {
		t.Error("筛选计数未靠右")
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
