package main

import (
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
	tasks := sectionOf(page, "tab-tasks")
	if tasks == "" {
		t.Fatal("未找到任务面板")
	}

	indexList := strings.Index(tasks, "任务列表")
	indexCheckin := strings.Index(tasks, "每日签到")
	indexRunAll := strings.Index(tasks, "全部执行")
	indexAccounts := strings.Index(tasks, "账号任务状态")

	for name, index := range map[string]int{
		"任务列表": indexList, "每日签到": indexCheckin,
		"全部执行": indexRunAll, "账号任务状态": indexAccounts,
	} {
		if index < 0 {
			t.Fatalf("任务面板缺少 %s", name)
		}
	}

	if !(indexList < indexCheckin) {
		t.Errorf("每日签到应在任务列表之后（%d vs %d）", indexList, indexCheckin)
	}
	if !(indexCheckin < indexRunAll) {
		t.Errorf("每日签到应在「全部执行」之前（%d vs %d）", indexCheckin, indexRunAll)
	}
	if !(indexRunAll < indexAccounts) {
		t.Errorf("「全部执行」应在账号状态之前（%d vs %d）", indexRunAll, indexAccounts)
	}
}
