package main

import (
	"regexp"
	"strings"
	"testing"
)

// 趋势图的拉取函数必须跨越 IIFE 边界可见。
//
// 用户报告：统计页的「用量趋势」一直停在「正在加载」。
//
// showTab 是顶层函数（作为独立脚本片段注入，这样标签栏不依赖页面主体脚本是否
// 执行成功），而 fetch 函数定义在页面主体脚本的 IIFE 里。写成一个裸标识符时，
// typeof 判断恒为 undefined，条件为假，数据永不拉取——图就停在占位文案上。
//
// 这里守住的是：fetch 函数挂在 window 上，且 showTab 通过 window 访问它。
func TestUsageTrendFetchIsReachableFromShowTab(t *testing.T) {
	script := mainPageScript()
	tabs := uiTabsScript

	if !strings.Contains(script, "window.refreshUsageTrend = refreshUsageTrend") {
		t.Error("refreshUsageTrend 未挂到 window，IIFE 外看不到")
	}
	if !strings.Contains(tabs, "window.refreshUsageTrend") {
		t.Error("showTab 未通过 window 访问 fetch 函数")
	}
	// 裸标识符的写法必须不再出现：它在 IIFE 外恒为 undefined。
	if strings.Contains(tabs, "typeof refreshUsageTrend") {
		t.Error("showTab 仍在用裸标识符判断，跨作用域会失败")
	}
}

// 账号表的积分数值要能就地更新。
//
// 用户要求：点某个账号的「积分」后，表格里的数值立刻反映新结果。重载整页也能做到，
// 但会丢掉滚动位置和当前所在的位置——为了一个数字代价太大。
func TestCreditCellCanBeUpdatedInPlace(t *testing.T) {
	resetState()
	seedPanelAccounts(t)
	page := renderMainPage()

	if !strings.Contains(page, `data-credits-for="cn-uid-1"`) {
		t.Error("积分单元格没有携带 uid，无法定位更新")
	}

	script := mainPageScript()
	for _, want := range []string{
		"function updateCreditCell(",
		"data-credits-for=",
		"cssEscape",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("脚本缺少 %s", want)
		}
	}
	// 更新后要有可见反馈，否则数值没变时看不出动作发生过。
	if !strings.Contains(uiCSS, ".flash") {
		t.Error("缺少变化高亮样式")
	}
}

// 任务的排版顺序：任务列表 → 每日签到 → 执行按钮 → 账号状态。
//
// 用户要求把签到放在任务列表下方、「全部执行」上方：它和这一页其它内容一样是
// 被调度的，放在按钮之上才读得出「今天会跑什么」。
func TestTaskTabOrder(t *testing.T) {
	resetState()
	seedPanelAccounts(t)

	page := renderMainPage()
	tasks := sectionOf(page, "view-tasks")
	if tasks == "" {
		t.Fatal("未找到任务面板")
	}

	// Anchor on strings unique to the tasks page, in document order:
	//   the card header        <h3>任务执行</h3>
	//   the run buttons        全部执行
	//   the check-in card      <h3>每日签到</h3>
	//   the account table      <h3>账号任务状态</h3>
	// Anchor on strings unique to each block, in document order:
	//   the run card's header   <h3>任务执行</h3>
	//   its run buttons         全部执行
	//   the check-in card       <h3>每日签到</h3>
	//   the account table       <h3>账号任务状态</h3>
	//
	// The bare words 任务 / 每日签到 also appear in headings and the page subtitle, so
	// the anchors include the tag that only the block header carries.
	indexList := strings.Index(tasks, "任务执行")
	indexRunAll := strings.Index(tasks, "全部执行")
	indexCheckin := strings.Index(tasks, `>每日签到<`)
	indexAccounts := strings.Index(tasks, "参与账号")

	for name, index := range map[string]int{
		"任务执行卡片": indexList, "每日签到": indexCheckin,
		"全部执行": indexRunAll, "参与账号": indexAccounts,
	} {
		if index < 0 {
			t.Fatalf("任务页面缺少 %s", name)
		}
	}

	// One card holds the run controls and the counters; the check-in card follows it,
	// and the per-account table comes last.
	if !(indexList < indexRunAll) {
		t.Errorf("执行按钮应在任务卡片内（%d vs %d）", indexList, indexRunAll)
	}
	if !(indexRunAll < indexCheckin) {
		t.Errorf("每日签到应在执行按钮之后（%d vs %d）", indexRunAll, indexCheckin)
	}
	if !(indexRunAll < indexAccounts) {
		t.Errorf("「全部执行」应在账号状态之前（%d vs %d）", indexRunAll, indexAccounts)
	}
}

// 积分页的表格必须能被就地替换。
//
// 用户报告：账号页的积分刷新了，但积分页的表格不动。
//
// refreshQuota 把结果写进了 #runResult——那是账号页的元素，积分页的表格从未被
// 更新。现在积分页的表格包在 #quotaResults 里，刷新后替换它，并同步写回账号表的
// 积分单元格。
func TestCreditsTabTableUpdatesInPlace(t *testing.T) {
	resetState()
	seedPanelAccounts(t)
	page := renderMainPage()

	// 积分不再有独立的第二张表：读数就是账号表的「积分」列，刷新时按行就地更新。
	// 所以这里检查的是「逐行更新」的机制，而不是一个汇总容器。
	if !strings.Contains(page, `data-credits-for="`) {
		t.Error("账号表缺少可按行更新的积分单元格")
	}
	if !strings.Contains(page, `id="quotaMsg"`) {
		t.Error("账号卡缺少积分查询的状态位")
	}

	script := mainPageScript()
	// 刷新后同时更新两处：积分页表格与账号表的单元格。
	if !strings.Contains(script, "applyResults") {
		t.Error("refreshQuota 没有就地的更新函数")
	}
	if !strings.Contains(script, "updateCreditCell(hit.uid") {
		t.Error("refreshQuota 没有同步账号表的积分单元格")
	}
	// 不应再用整页重载来做积分刷新本身。
	// 其它动作（执行全部、签到）重载是有意的——它们会同时改变多个面板；这里只
	// 盯着 refreshQuota 的函数体。
	if body := functionBody(script, "window.refreshQuota"); body != "" {
		if strings.Contains(body, "location.reload") {
			t.Error("积分刷新仍在整页重载，会丢掉滚动位置")
		}
		if !strings.Contains(body, "quotaResults") {
			t.Error("积分刷新没有更新积分页的表格")
		}
	} else {
		t.Fatal("未找到 window.refreshQuota")
	}
	// JS 里的表格结构要与服务端一致，否则替换时面板会跳。
	if !strings.Contains(script, `tbl-wrap`) {
		t.Error("renderQuota 未复用滚动容器结构")
	}
}

// 每个标签页面板的 div 深度必须一致。
//
// 两次「标签页空白」都源于同一个形状：某个面板多闭（或少闭）一个 div，于是从它
// 之后的每个面板都嵌套错位，内容落到不显示的位置。这里直接量深度——比数总量可靠，
// 因为它能指出是哪一个面板开始出问题。
func TestEveryPanelSitsAtTheSameDepth(t *testing.T) {
	resetState()
	seedPanelAccounts(t)

	page := stripScriptBlocks(renderMainPage())
	page = stripStyleBlocks(page)

	depths := map[string]int{}
	for _, tab := range []string{
		"view-accounts", "view-usage", "view-tasks", "view-settings",
	} {
		marker := `id="` + tab + `"`
		index := strings.Index(page, marker)
		if index < 0 {
			t.Fatalf("缺少面板 %s", tab)
		}
		// 数到该标记为止的 div 净值。
		depth := 0
		for _, token := range tokenizeDivs(page[:index]) {
			if token == "<div" {
				depth++
			} else {
				depth--
			}
			if depth < 0 {
				t.Fatalf("页面 %s 之前 depth 已经变负，说明前面的容器多闭了", tab)
			}
		}
		depths[tab] = depth
	}

	want := depths["view-accounts"]
	for tab, depth := range depths {
		if depth != want {
			t.Errorf("%s 深度 %d，与 view-accounts 的 %d 不一致", tab, depth, want)
		}
	}
	// 整页也要收平。
	if problem := unbalancedMarkup(renderMainPage()); problem != "" {
		t.Errorf("整页标记未闭合：%s", problem)
	}
}

// tokenizeDivs returns "<div" and "</div>" tokens in order.
func tokenizeDivs(markup string) []string {
	var out []string
	for i := 0; i < len(markup); {
		if markup[i] != '<' {
			i++
			continue
		}
		end := strings.IndexByte(markup[i:], '>')
		if end < 0 {
			break
		}
		token := strings.TrimSpace(markup[i+1 : i+end])
		i += end + 1
		if strings.HasPrefix(token, "/div") {
			out = append(out, "</div>")
		} else if token == "div" || strings.HasPrefix(token, "div ") {
			out = append(out, "<div")
		}
	}
	return out
}

// stripStyleBlocks removes <style> blocks so tag counting sees only markup.
func stripStyleBlocks(page string) string {
	return stripBlock(page, "<style", "</style>")
}

// functionBody returns the source of one function, from its name to the line that
// closes it at the same brace depth.
//
// Scoped assertions need this: the page script has eight location.reload calls, and
// most of them are deliberate (a full run changes several panels at once). Only the
// one inside a specific function is a problem.
func functionBody(script, name string) string {
	start := strings.Index(script, name)
	if start < 0 {
		return ""
	}
	open := strings.Index(script[start:], "{")
	if open < 0 {
		return ""
	}
	open += start
	depth := 0
	for i := open; i < len(script); i++ {
		switch script[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return script[start : i+1]
			}
		}
	}
	return script[start:]
}

// 导航链接的 data-view 必须与页面 id 完全一致。
//
// 用户报告：点任何一个导航项页面都变空白。
//
// 点击处理器一度写成 showTab('view-' + dataView)，而 data-view 的值本身已经是
// "view-accounts"，拼出 "view-view-accounts"，一次都匹配不上。showTab 把每个页面
// 都设为 hidden，于是什么都没显示——看起来像「点了没反应」。
func TestNavTargetsMatchPageIDs(t *testing.T) {
	resetState()
	seedPanelAccounts(t)
	page := renderMainPage()

	pageIDs := map[string]bool{}
	for _, m := range regexp.MustCompile(`<section class="view" id="([^"]+)"`).FindAllStringSubmatch(page, -1) {
		pageIDs[m[1]] = true
	}
	if len(pageIDs) == 0 {
		t.Fatal("没有找到页面容器")
	}

	targets := regexp.MustCompile(`data-view="([^"]+)"`).FindAllStringSubmatch(page, -1)
	if len(targets) == 0 {
		t.Fatal("导航没有任何 data-view")
	}
	for _, m := range targets {
		if !pageIDs[m[1]] {
			t.Errorf("导航指向 %q，但没有这个页面", m[1])
		}
	}

	script := mainPageScript()
	if strings.Contains(script, "'view-' + node.getAttribute('data-view')") {
		t.Error("点击处理器给 data-view 又加了一次前缀")
	}
	if !strings.Contains(script, "showTab(node.getAttribute('data-view')") {
		t.Error("点击处理器没有把 data-view 原样传给 showTab")
	}
}

// showTab 必须容错：id 匹配不到任何页面时回退到第一页，而不是全部隐藏。
func TestShowTabNeverLeavesThePageBlank(t *testing.T) {
	tabs := uiTabsScript
	if !strings.Contains(tabs, "if (!shown && pages.length)") {
		t.Error("showTab 在 id 匹配失败时会把所有页面隐藏，留下空白")
	}
	if !strings.Contains(tabs, "id.indexOf('view-') !== 0") {
		t.Error("showTab 未对 id 做前缀归一化")
	}
}
