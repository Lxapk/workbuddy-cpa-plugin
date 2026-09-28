package main

import (
	"os"
	"strings"
	"testing"
)

// 路由策略必须在设置页，而不是账号页。
//
// 用户要求把它移过去。理由是它属于策略——请求如何分配到池子里的账号——而账号页
// 关心的是有哪些账号。两者混在一起会让账号页带着一个和上方列表无关的控件。
func TestRoutingLivesOnSettingsPage(t *testing.T) {
	resetState()
	seedPanelAccounts(t)
	page := renderMainPage()

	settings := sectionOf(page, "view-settings")
	accounts := sectionOf(page, "view-accounts")

	if !strings.Contains(settings, "路由策略") {
		t.Error("设置页缺少路由策略")
	}
	if strings.Contains(accounts, "路由策略") {
		t.Error("账号页不应再有路由策略")
	}
}

// 设置页的顺序预览要显示账号信息，而路由策略不应携带它。
//
// 用户要求「不用显示账号信息」。顺序预览会列出账号名与积分，那正是账号页已有的内容，
// 而且会让一张设置卡依赖实时的池子状态。
func TestRoutingHasNoAccountInfo(t *testing.T) {
	resetState()
	seedPanelAccounts(t)
	settings := sectionOf(renderMainPage(), "view-settings")

	// 「选择顺序预览」整块都应消失。data-credits-for 只出现在账号页的表格里，
	// sectionOf 的边界未必能完全隔离，所以用块标题判断。
	if strings.Contains(settings, "选择顺序预览") {
		t.Error("设置页仍保留了顺序预览（含账号信息）")
	}
	// 策略本身的两个操作要在。
	if !strings.Contains(settings, `data-call="saveStrategy"`) {
		t.Error("路由卡缺少应用按钮")
	}
	if !strings.Contains(settings, `data-call="resetRotation"`) {
		t.Error("路由卡缺少重置按钮")
	}
}

// 成长任务必须先拿到任务清单，再把清单内容报出来。
//
// 用户要求「先获取到每个任务，之后再进行任务」。后端本来就是先拉清单（fetch 在
// accept 之前），但过程完全静默：清单为空和「还在跑」看起来一模一样。现在拉取成功
// 后会先报出数量与任务名，之后逐条处理。
func TestGrowthReportsTheFetchedTaskList(t *testing.T) {
	src := readSourceFile(t, "growth_engine.go")

	fetch := strings.Index(src, "tasks, errFetch := r.fetch(ctx, creds)")
	report := strings.Index(src, "已获取任务清单")
	accept := strings.Index(src, "r.acceptPending(ctx, creds, tasks, logger)")

	if fetch < 0 {
		t.Fatal("找不到拉取任务清单的调用")
	}
	if report < 0 {
		t.Fatal("拉取清单后没有报告内容，过程仍然是静默的")
	}
	if accept < 0 {
		t.Fatal("找不到接取任务的调用")
	}
	// 顺序：拉取 → 报告 → 接取。报告夹在中间，读者才知道后面在做什么。
	if !(fetch < report && report < accept) {
		t.Errorf("顺序不对：fetch=%d report=%d accept=%d", fetch, report, accept)
	}
}

// 面板主题跟随宿主的 data-theme，并覆盖「跟随系统」这一档。
//
// 宿主（CPAMC）在 <html> 上写 data-theme="dark" 或 "white"，选「跟随系统」时则把
// 属性去掉。所以样式表要同时处理两件事：属性存在时按属性，不存在时交给
// prefers-color-scheme。亮色用 "white" 这个字面量对齐宿主。
func TestThemeFollowsHost(t *testing.T) {
	css := uiCSS
	script := uiTabsScript

	for _, want := range []string{
		`:root[data-theme="white"]`,
		`:root[data-theme="dark"]`,
		`@media (prefers-color-scheme: light)`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("样式表缺少 %s", want)
		}
	}
	// 读取宿主属性的脚本，以及对跨域读取失败的处理。
	if !strings.Contains(script, "adoptHostTheme") {
		t.Error("缺少读取宿主主题的脚本")
	}
	if !strings.Contains(script, "attach('data-theme', hostTheme)") &&
		!strings.Contains(script, "setAttribute('data-theme'") {
		t.Error("脚本没有把宿主的主题应用到本页")
	}
	if !strings.Contains(script, "} catch (e) {") {
		t.Error("跨域读取父文档会抛异常，必须捕获后才能回退到媒体查询")
	}
	// 宿主换主题不会重载 iframe，所以要定期复查。
	if !strings.Contains(script, "setInterval(adoptHostTheme") {
		t.Error("没有定期复查宿主主题，宿主切换后本页不会跟着变")
	}
}

// 页面不得写死主题：它必须由脚本决定。
func TestPageDoesNotHardcodeTheme(t *testing.T) {
	resetState()
	page := renderMainPage()
	// 只看 <html> 标签本身：CSS 里当然会出现这些选择器，那是给脚本设定用的。
	open := strings.Index(page, "<html")
	if open < 0 {
		t.Fatal("找不到 <html> 标签")
	}
	end := strings.Index(page[open:], ">")
	tag := page[open : open+end+1]
	if strings.Contains(tag, "data-theme") {
		t.Errorf("<html> 写死了主题，无法跟随宿主：%s", tag)
	}
}

// readSourceFile reads one of the plugin's own source files.
//
// Used where an assertion is about the order of statements in a function rather than
// about rendered output: the growth pass is a sequence of stages, and the order is the
// contract.
func readSourceFile(t *testing.T, name string) string {
	t.Helper()
	data, errRead := os.ReadFile(name)
	if errRead != nil {
		t.Fatalf("读取 %s: %v", name, errRead)
	}
	return string(data)
}
