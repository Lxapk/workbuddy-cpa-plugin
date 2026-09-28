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

// 面板是三段式：标题区、标签栏、内容框架，自上而下。
//
// 用户描述的布局：顶部是「WorkBuddy 控制台」和描述，中间是各项标签，下方是标签下的
// 框架。标题区与标签栏分开，这样面板先说明自己是什么，再提供导航。
func TestThreePartLayout(t *testing.T) {
	resetState()
	page := renderMainPage()

	header := strings.Index(page, `class="page-header"`)
	tabs := strings.Index(page, `class="tabbar"`)
	main := strings.Index(page, `class="main"`)

	if header < 0 || tabs < 0 || main < 0 {
		t.Fatalf("缺少区段：header=%d tabbar=%d main=%d", header, tabs, main)
	}
	if !(header < tabs && tabs < main) {
		t.Errorf("顺序应为 标题区 → 标签栏 → 内容框架，实际 %d / %d / %d", header, tabs, main)
	}

	// 标题区里有标题与描述。
	section := page[header : header+400]
	if !strings.Contains(section, "WorkBuddy 控制台") {
		t.Error("标题区缺少标题")
	}
	if !strings.Contains(section, `class="desc"`) {
		t.Error("标题区缺少描述")
	}

	// 各页不再有重复的标题：顶部已经说明了这是什么。
	for _, gone := range []string{"<h1>账号</h1>", "<h1>任务</h1>", "<h1>用量</h1>", "<h1>设置</h1>"} {
		if strings.Contains(page, gone) {
			t.Errorf("仍存在页面级标题：%s", gone)
		}
	}
}

// 四个标签都要在标签栏里，并且指向存在的页面。
func TestTabBarHoldsEveryPage(t *testing.T) {
	resetState()
	page := renderMainPage()

	bar := page[strings.Index(page, `class="tabbar"`):]
	bar = bar[:strings.Index(bar, "</nav>")]
	for _, view := range []string{"view-accounts", "view-tasks", "view-usage", "view-settings"} {
		if !strings.Contains(bar, `data-view="`+view+`"`) {
			t.Errorf("标签栏缺少 %s", view)
		}
	}
	// 标签栏里不该混入品牌块：标题区已经承担了那个角色。
	if strings.Contains(bar, "brand") {
		t.Error("标签栏里仍有品牌块，标题区已负责说明身份")
	}
}

// 亮色有两套，不能混用。
//
// 宿主在 themes.scss 里定义了两个浅色色板：:root 是「跟随系统」用的纸感暖白
// (#faf9f5)，[data-theme='white'] 是用户在主题切换器里显式选择后用的纯白
// (#ffffff)。给显式选择套上纸感色，面板在宿主旁边就会显得发黄——这正是用户指出
// 「白主题是白色底色」时看到的问题。
func TestWhiteThemeIsPureWhite(t *testing.T) {
	css := uiCSS

	white := css[strings.Index(css, `:root[data-theme="white"]`):]
	white = white[:strings.Index(white, "}")]
	if !strings.Contains(white, "--bg-secondary: #ffffff") {
		t.Error("white 主题的页面底色应为纯白 #ffffff")
	}
	if !strings.Contains(white, "--bg-primary: #ffffff") {
		t.Error("white 主题的卡片底色应为纯白 #ffffff")
	}
	// 去掉注释后再检查：注释里提到 #faf9f5 正是为了说明两者的区别。
	if strings.Contains(stripCSSComments(white), "#faf9f5") {
		t.Error("white 主题混入了 :root 的纸感暖白")
	}

	// 暗色是同一套暖灰体系里的深色，页面底 #151412。
	dark := css[strings.Index(css, `:root[data-theme="dark"]`):]
	dark = dark[:strings.Index(dark, "}")]
	if !strings.Contains(dark, "--bg-secondary: #151412") {
		t.Error("dark 主题的页面底色应为 #151412")
	}

	// 「跟随系统」保持宿主的 :root 值。
	media := css[strings.Index(css, "@media (prefers-color-scheme: light)"):]
	if !strings.Contains(media, "#faf9f5") {
		t.Error("跟随系统的浅色应使用宿主的纸感暖白")
	}
}

// stripCSSComments removes /* … */ blocks so value checks ignore prose.
func stripCSSComments(css string) string {
	for {
		start := strings.Index(css, "/*")
		if start < 0 {
			return css
		}
		end := strings.Index(css[start:], "*/")
		if end < 0 {
			return css[:start]
		}
		css = css[:start] + css[start+end+2:]
	}
}
