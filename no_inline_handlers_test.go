package main

import (
	"regexp"
	"strings"
	"testing"
)

// 面板不得包含任何内联事件处理器。
//
// 用户报告：账号页正常，但点「积分/统计/任务/设置」后一片空白。
//
// 根因是标签按钮用内联 onclick 调用 showTab。当承载页面下发的内容安全策略禁止
// 内联脚本时，这些处理器全部被拦下，且不会有任何可见错误——点击什么也不发生，
// 页面就停在标记里已经标为 active 的那个面板上。而标记里只有账号面板是 active
// 的，所以症状恰好是「只有账号页有内容」。
//
// 因此这里守住的是：所有交互都靠 data-* 属性加上本文件里的委托监听器。
func TestNoInlineEventHandlers(t *testing.T) {
	resetState()
	seedPanelAccounts(t)
	page := renderMainPage()

	// 覆盖常见的内联处理器属性名，而不只是 onclick。
	pattern := regexp.MustCompile(`(?i)\bon(click|change|input|submit|load|focus|blur|keydown|keyup|mouseover|mouseout)\s*=`)
	if found := pattern.FindAllString(page, -1); len(found) > 0 {
		t.Errorf("页面包含内联事件处理器 %v；CSP 会拦下它们", found)
	}
}

// 面板脚本本身也不该出现内联处理器字符串（它是被拼接进页面的）。
func TestPanelScriptDeclaresNoInlineHandlers(t *testing.T) {
	script := mainPageScript()
	if strings.Contains(script, "onclick=") {
		t.Error("脚本里仍在构造 onclick 属性")
	}
	// 委托监听器本身必须存在，否则 data-call 永远不会被分发。
	for _, want := range []string{
		"function dispatchDataCall(",
		"data-call",
		"data-tab",
		"addEventListener('click'",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("脚本缺少 %s", want)
		}
	}
}

// 标签按钮必须能被委托监听器识别：带 tab 类与 data-tab。
//
// 少了任一个，点击就不会落到 showTab，面板也就不会切换。
func TestTabButtonsAreWiredByDataAttributes(t *testing.T) {
	resetState()
	seedPanelAccounts(t)
	page := renderMainPage()

	tabs := []string{
		"tab-accounts", "tab-switch", "tab-checkin",
		"tab-credits", "tab-usage", "tab-tasks", "tab-settings",
	}
	for _, tab := range tabs {
		marker := `class="tab active" data-tab="` + tab + `"`
		plain := `class="tab" data-tab="` + tab + `"`
		if !strings.Contains(page, marker) && !strings.Contains(page, plain) {
			t.Errorf("标签 %s 缺少 class=tab + data-tab 的组合", tab)
		}
	}
}

// 每个 data-call 都必须指向脚本里真实存在的全局函数。
//
// 拼错的函数名不会报错，只会让按钮点了没反应——和这次的事故同一种表现，
// 所以值得在测试里挡住。
func TestDataCallNamesResolveToFunctions(t *testing.T) {
	resetState()
	seedPanelAccounts(t)
	page := renderMainPage()
	script := mainPageScript()

	pattern := regexp.MustCompile(`data-call="([A-Za-z_][A-Za-z0-9_]*)"`)
	seen := map[string]bool{}
	for _, match := range pattern.FindAllStringSubmatch(page, -1) {
		name := match[1]
		if seen[name] {
			continue
		}
		seen[name] = true
		// 允许两种定义形式：window.name = function 或 function name(。
		if !strings.Contains(script, "window."+name+" = function") &&
			!strings.Contains(script, "function "+name+"(") {
			t.Errorf("data-call=%q 在脚本里没有定义", name)
		}
	}
	if len(seen) == 0 {
		t.Fatal("页面上没有任何 data-call，交互全部失效")
	}
}
