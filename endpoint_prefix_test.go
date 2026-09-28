package main

import (
	"strings"
	"testing"
)

// 逐账号操作的请求必须带上管理端点前缀。
//
// 用户报告：账号表里每行的「签到」「积分」按钮点下去是 404。
//
// 原因是这两处用了 call('/checkin/run?uid=…')，漏掉了 BASE 前缀。BASE 的值是
// "/v0/management/workbuddy"（由 CPA 在渲染时替换），少了它请求就打到了 CPA 的
// 根路径上，而那里没有这个路由，于是 404。其它调用都写了 BASE + '…'，只有这两处
// 是新加的，复制代码时漏了。
//
// 这里守住的是：脚本里不存在任何以 '/' 开头的 call() 字面量。
func TestCallsAlwaysUseTheManagementPrefix(t *testing.T) {
	script := mainPageScript()

	for _, line := range strings.Split(script, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.Contains(trimmed, "call(") {
			continue
		}
		// call(BASE + '…') 是正确的形式；call('…') 是错的。
		if strings.Contains(trimmed, "call('") && !strings.Contains(trimmed, "call(BASE") {
			t.Errorf("call() 缺少 BASE 前缀：%s", trimmed)
		}
	}
}

// 签到不再是独立标签页，它的内容在任务页里。
//
// 用户要求把「签到」这个主标签并入「任务」界面——签到本身就是一类任务，看「今天
// 会跑什么」时不该还要切标签。
func TestCheckinLivesOnTheTasksTab(t *testing.T) {
	resetState()
	seedPanelAccounts(t)
	page := renderMainPage()

	// 有 tasks 标签，没有 checkin 标签。
	if !strings.Contains(page, `data-view="view-tasks"`) {
		t.Error("缺少任务标签页")
	}
	if strings.Contains(page, `id="view-checkin"`) {
		t.Error("签到不应再有独立页面")
	}

	// 签到的卡片出现在任务页面内部。
	tasks := sectionOf(page, "view-tasks")
	if tasks == "" {
		t.Fatal("未找到任务面板")
	}
	if !strings.Contains(tasks, "每日签到") {
		t.Error("任务面板里没有签到卡片")
	}
	// 签到的两个按钮也要在。
	for _, want := range []string{
		`data-call="saveCheckinSettings"`,
		`data-call="runCheckin"`,
	} {
		if !strings.Contains(tasks, want) {
			t.Errorf("任务面板缺少 %s", want)
		}
	}
}

// 存过旧标签 id 的浏览器要落到任务页，而不是被丢回第一个标签。
func TestRestoreTabMigratesCheckinToTasks(t *testing.T) {
	script := uiTabsScript
	if !strings.Contains(script, "'view-checkin'") {
		t.Error("restoreTab 未把旧的签到视图名迁移到任务页")
	}
	if !strings.Contains(script, "moved[saved]") {
		t.Error("restoreTab 缺少迁移映射")
	}
}
