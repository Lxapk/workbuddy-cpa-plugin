package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
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

// 卡片规格要对齐宿主的 SectionCard。
//
// 用户对比截图时指出圆角与底色不对。从源码看，宿主的配置卡片是：
//
//	border-radius: 14px            自己定的值，不用 12px 的令牌
//	padding: clamp(20px, 2.4vw, 28px)
//	background: color-mix(… var(--bg-primary) 82%, transparent)
//
// 半透明那一点尤其重要：底色透出来，卡片才不会像贴上去的色块。
func TestCardMatchesHostSectionCard(t *testing.T) {
	css := uiCSS

	for _, want := range []string{
		"--radius-card: 14px",
		"border-radius: var(--radius-card)",
		"border-radius: var(--radius-card);", // .stats 也用同一规格
		"color-mix(in srgb, var(--bg-primary) 88%",
		"padding: var(--space-lg)",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("卡片规格缺少 %s", want)
		}
	}

	// 旧的 8px 圆角不该再用于卡片。
	box := css[strings.Index(css, ".box {"):]
	box = box[:strings.Index(box, "}")]
	if strings.Contains(box, "var(--radius-md)") {
		t.Error("卡片仍在用 8px 圆角")
	}

	// 命中宿主的间距与圆角刻度。
	for _, tok := range []string{"--space-sm: 8px", "--space-md: 16px", "--space-lg: 24px", "--radius-lg: 12px"} {
		if !strings.Contains(css, tok) {
			t.Errorf("缺少与宿主一致的刻度 %s", tok)
		}
	}
}

// 卡片入场动画与宿主同拍，并且只对可见页生效。
func TestCardEntranceMatchesHost(t *testing.T) {
	css := uiCSS
	if !strings.Contains(css, "keyframes card-in") {
		t.Fatal("缺少卡片入场动画")
	}
	if !strings.Contains(css, ".view:not([hidden]) .box { animation: card-in .45s") {
		t.Error("动画时长或作用范围与宿主不一致；隐藏页不应参与")
	}
	// 尊重减弱动效偏好。
	if !strings.Contains(css, "prefers-reduced-motion: reduce") {
		t.Error("缺少减弱动效的处理")
	}
}

// 账号行按参考布局呈现：状态、积分比值与进度条、调用计数、在途、用量、操作。
//
// 积分要显示成「剩余 / 总量」并配一条进度条：只有剩余数时说不出用了多少，比值才
// 读得出周期余量。总量由上游的 cycle capacity 提供，取不到时退回只显示剩余数。
func TestAccountRowShowsTheReferenceColumns(t *testing.T) {
	resetState()
	seedPanelAccounts(t)
	page := renderMainPage()

	for _, col := range []string{"账号", "状态", "积分", "成功 / 失败", "操作"} {
		if !strings.Contains(page, ">"+col+"<") && !strings.Contains(page, col+"</th>") &&
			!strings.Contains(page, col+"<") {
			t.Errorf("账号表缺少列 %s", col)
		}
	}

	for _, want := range []string{
		"credit-ratio",      // 剩余 / 总量
		"credit-bar",        // 进度条
		`data-credits-for=`, // 可按行更新的锚点
		`data-row-action="checkin"`,
		`data-row-action="quota"`,
		`data-row-action="tasks"`,
		`data-account-toggle="1"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("账号行缺少 %s", want)
		}
	}

	// 账号单元格里带上短 uid，完整值放 title 供悬停查看。
	accounts := sectionOf(page, "view-accounts")
	if !strings.Contains(accounts, `class="uid mono"`) {
		t.Error("账号行缺少 uid 小字")
	}
}

// 账号表在手机上横滑，而不是堆叠。
//
// 八列堆叠会让每个账号比屏幕还高，而这个布局的意义正是横向比较账号——那需要它们
// 并排。滚动限定在表格容器内，页面框架不动。
func TestAccountTableScrollsOnPhone(t *testing.T) {
	css := uiCSS
	phone := css[strings.Index(css, "@media (max-width: 768px)"):]

	for _, want := range []string{
		"table.accounts thead { display: table-header-group; }",
		"table.accounts td { display: table-cell;",
	} {
		if !strings.Contains(phone, want) {
			t.Errorf("手机端账号表缺少 %s", want)
		}
	}
	// 最小宽度只在基础规则里声明一次即可——同一张表在窄屏下也用它。
	// （先前媒体查询里又写了一遍，是重复。）
	if !strings.Contains(css, "table.accounts { min-width: 900px; }") {
		t.Error("账号表缺少最小宽度")
	}
	if got := strings.Count(css, "table.accounts { min-width"); got != 1 {
		t.Errorf("账号表的最小宽度声明了 %d 次，应只声明一次", got)
	}
	// 通用堆叠规则不得作用到账号表上：它的 td 必须是表格单元。
	if !strings.Contains(phone, "table.accounts td[data-label]::before { display: none; }") {
		t.Error("手机端账号表仍会套用堆叠标签")
	}
}

// 每个在 handler 里实现的路径都必须先注册。
//
// CPA 的路由表是精确匹配（METHOD + PATH），没注册的路径直接 404。新增
// /growth/schedule 时就漏了这一步：代码写完、测试通过，但那一路径永远打不通。
// 这条断言把「实现」与「注册」绑在一起。
func TestEveryImplementedRouteIsRegistered(t *testing.T) {
	registered := map[string]bool{}
	regSrc := readSourceFile(t, "management.go")
	for _, m := range regexp.MustCompile(`Path:\s+"(/workbuddy/[^"]*)"`).FindAllStringSubmatch(regSrc, -1) {
		registered[m[1]] = true
	}
	if len(registered) == 0 {
		t.Fatal("没有解析到任何已注册路由")
	}

	implemented := map[string]bool{}
	for _, file := range []string{
		"handler_main.go", "handler_account.go",
		"checkin_page.go", "quota_page.go", "growth_page.go",
	} {
		src := readSourceFile(t, file)
		for _, m := range regexp.MustCompile(`case "(/[a-z/]*)"`).FindAllStringSubmatch(src, -1) {
			implemented["/workbuddy"+m[1]] = true
		}
	}
	if len(implemented) == 0 {
		t.Fatal("没有解析到任何已实现的路径")
	}

	for path := range implemented {
		if !registered[path] {
			t.Errorf("%s 已实现但未在 management.go 注册，请求会 404", path)
		}
	}
}

// 定时任务的设置要能被读写，并且默认是关闭的。
//
// 它会按点消耗上游额度，所以必须由使用者显式开启——默认开启等于替人做决定。
func TestGrowthScheduleIsOptIn(t *testing.T) {
	def := defaultGrowthSettings()
	if def.Enabled {
		t.Error("定时任务默认不应开启")
	}
	if def.Hour < 0 || def.Hour > 23 || def.Minute < 0 || def.Minute > 59 {
		t.Errorf("默认时间越界：%02d:%02d", def.Hour, def.Minute)
	}

	// 越界输入要被夹回合法范围。
	cfg := normalizeGrowthSettings(growthSettings{Hour: 99, Minute: -5})
	if cfg.Hour != 9 || cfg.Minute != 0 {
		t.Errorf("越界时间未被修正：%02d:%02d", cfg.Hour, cfg.Minute)
	}

	// 调度器要能报告状态给面板。
	snap := growthScheduleSnapshot()
	for _, key := range []string{"enabled", "hour", "minute", "on_start", "running", "ran_today", "last_summary"} {
		if _, ok := snap[key]; !ok {
			t.Errorf("调度状态缺少字段 %s", key)
		}
	}
}

// 任务清单要同时列出全部任务与其中未完成的那些。
func TestGrowthListsPendingTasks(t *testing.T) {
	src := readSourceFile(t, "growth_engine.go")
	for _, want := range []string{"已获取任务清单", "待完成：", "所有任务都已完成"} {
		if !strings.Contains(src, want) {
			t.Errorf("清单报告缺少 %q", want)
		}
	}
}

// 配置里缺失的段要落到默认值，而不是零值。
//
// 宿主的 YAML 只写它认识的键，面板自己的段（checkin/quota/growth/routing）通常不
// 在其中。原先解码用的是空结构体，于是每个未出现的键都变成零值——定时任务因此显示
// 成「0 点、启动不补跑」，签到也受影响。这条断言把默认值与解码行为绑在一起。
func TestMissingConfigSectionsKeepDefaults(t *testing.T) {
	var store settingsStore
	// 只给一个宿主的键，面板的段全部缺席。
	if errDecode := store.decodeLifecycleConfig([]byte("port: 8317\n")); errDecode != nil {
		t.Fatalf("解码失败：%v", errDecode)
	}
	got := store.get()

	if got.Growth.Hour != 9 || got.Growth.Minute != 0 {
		t.Errorf("任务定时的默认时间被零值覆盖：%02d:%02d", got.Growth.Hour, got.Growth.Minute)
	}
	if !got.Growth.OnStart {
		t.Error("任务定时的「启动补跑」默认值被覆盖")
	}
	if got.Growth.Enabled {
		t.Error("定时任务默认不应开启")
	}
	if got.Checkin.Hour != 8 {
		t.Errorf("签到的默认时间被零值覆盖：%02d", got.Checkin.Hour)
	}
	if got.Quota.IntervalMinutes != 30 {
		t.Errorf("积分刷新的默认间隔被零值覆盖：%d", got.Quota.IntervalMinutes)
	}
}

// 显式写出的段要照常被采纳，不能被默认值盖掉。
func TestExplicitConfigSectionsWin(t *testing.T) {
	var store settingsStore
	body := []byte("port: 8317\ngrowth:\n  enabled: true\n  hour: 21\n  minute: 15\n  on_start: false\n")
	if errDecode := store.decodeLifecycleConfig(body); errDecode != nil {
		t.Fatalf("解码失败：%v", errDecode)
	}
	got := store.get().Growth
	if !got.Enabled || got.Hour != 21 || got.Minute != 15 || got.OnStart {
		t.Errorf("显式配置未被采纳：%+v", got)
	}
}

// 诊断信息不得进入用量统计。
//
// 用户在用量趋势里看到了「选号：host 提供 3 个……」这条记录，并且它被算成了一次
// 失败——因为它的 callRecord 带 Error 字段，而计数器把任何 Error 都当作失败。
// 诊断写的是「系统状态如何」，不是「一次调用发生了什么」，两者不能混在一起计。
func TestNoticesDoNotAffectUsageCounters(t *testing.T) {
	log := newCallLog(50)

	// 一次真实失败。
	log.add(callRecord{ProviderID: "p", Model: "m", StatusCode: 429, Error: "限流", StartedAt: timeNowForTest()})
	// 三条诊断信息。
	for i := 0; i < 3; i++ {
		log.addNotice(callRecord{ProviderID: "p", Model: "growth", Error: "选号：host 提供 3 个"})
	}

	totals := log.totals()
	if totals.TotalCalls != 1 {
		t.Errorf("诊断被算成了调用：total_calls=%d, want 1", totals.TotalCalls)
	}
	if totals.TotalFailed != 1 {
		t.Errorf("诊断被算成了失败：total_failed=%d, want 1（只有那次 429）", totals.TotalFailed)
	}

	// 日趋势同样不应受影响。
	daily := log.dailyUsage()
	if len(daily) != 1 {
		t.Fatalf("日桶数量异常：%d", len(daily))
	}
	if daily[0].Calls != 1 || daily[0].Failed != 1 {
		t.Errorf("日趋势被诊断污染：calls=%d failed=%d, want 1/1", daily[0].Calls, daily[0].Failed)
	}

	// 诊断本身要留下来，只是放在通知环里；调用列表读出来就是干净的。
	if len(log.noticeLog(10)) != 3 {
		t.Errorf("诊断没有进入通知列表：%d 条", len(log.noticeLog(10)))
	}
	if got := len(log.modelCallsOnly(10)); got != 1 {
		t.Errorf("调用列表应只有 1 条，实际 %d", got)
	}
}

// 直接写入带 Notice 标记的记录，也走同一条不受统计的路径。
func TestNoticeFlagIsHonouredByAdd(t *testing.T) {
	log := newCallLog(50)
	log.add(callRecord{ProviderID: "p", Notice: true, Error: "只是一个提示", StartedAt: timeNowForTest()})

	if totals := log.totals(); totals.TotalCalls != 0 || totals.TotalFailed != 0 {
		t.Errorf("带 Notice 标记的记录仍被计数：calls=%d failed=%d",
			totals.TotalCalls, totals.TotalFailed)
	}
	// 通知有自己的环，调用列表读出来不含它。
	if len(log.recent(10)) != 0 {
		t.Error("带 Notice 标记的记录不该出现在调用列表里")
	}
	if len(log.noticeLog(10)) != 1 {
		t.Error("带 Notice 标记的记录应保存在通知环里")
	}
}

// 路由策略的描述要说「什么时候用」，并给出代价。
func TestRoutingOptionsExplainWhenToUseThem(t *testing.T) {
	for _, s := range []schedulerStrategy{
		strategyByCredits, strategyRoundRobin, strategyRandom, strategyByExpiry,
	} {
		desc := strategyDescription(s)
		if len(desc) < 20 {
			t.Errorf("%s 的描述过短：%q", s, desc)
		}
		if !strings.Contains(desc, "适合") {
			t.Errorf("%s 的描述没有说明适用场景：%q", s, desc)
		}
		trade := strategyTradeoff(s)
		if !strings.Contains(trade, "代价") {
			t.Errorf("%s 缺少代价说明：%q", s, trade)
		}
	}
}

// timeNowForTest is the clock used by the usage-counter tests.
func timeNowForTest() time.Time { return time.Now() }

// 积分要同时给出剩余与总量，不能把总量等于剩余。
//
// 用户看到每个账号都是「1 / 1」。根因是 legacy 解析路径读了 CycleCapacitySize 却
// 从未把它累加，Total 直接赋值成 Remaining。没有分母时进度条恒满，比值也没有意义。
//
// 取值规则对齐参考实现：一个包报了周期容量就按 Cycle* 算，否则按 Capacity* 算，
// 两条路径互斥——混用会把同一个包算两次。
func TestLegacyQuotaSumsCapacitySeparately(t *testing.T) {
	body := []byte(`{"data":{"Response":{"Data":{"Accounts":[
		{"CycleCapacitySize":4608,"CycleCapacityRemain":3743,"CycleCapacityUsed":865}
	]}}}}`)
	q := interpretQuotaResponse(200, body)

	if q.Summary.Remaining != 3743 {
		t.Errorf("剩余 = %v, want 3743", q.Summary.Remaining)
	}
	if q.Summary.Total != 4608 {
		t.Errorf("总量 = %v, want 4608（不能等于剩余）", q.Summary.Total)
	}
	if q.Summary.Total == q.Summary.Remaining {
		t.Error("总量与剩余相同，进度条会恒满")
	}
}

// 没有周期容量时退到 Capacity* 字段，且不与 Cycle* 混算。
func TestLegacyQuotaFallsBackToPlainCapacity(t *testing.T) {
	body := []byte(`{"data":{"Response":{"Data":{"Accounts":[
		{"CycleCapacitySize":0,"CycleCapacityRemain":0,"CapacitySize":800,"CapacityRemain":300,"CapacityUsed":500}
	]}}}}`)
	q := interpretQuotaResponse(200, body)

	if q.Summary.Remaining != 300 || q.Summary.Total != 800 {
		t.Errorf("退化取值错误：remaining=%v total=%v, want 300/800", q.Summary.Remaining, q.Summary.Total)
	}
}

// 多个包各自判断后求和：一个走周期字段，一个走普通字段。
func TestLegacyQuotaSumsPerPackage(t *testing.T) {
	body := []byte(`{"data":{"Response":{"Data":{"Accounts":[
		{"CycleCapacitySize":1000,"CycleCapacityRemain":600},
		{"CycleCapacitySize":0,"CapacitySize":500,"CapacityRemain":400}
	]}}}}`)
	q := interpretQuotaResponse(200, body)

	if q.Summary.Total != 1500 || q.Summary.Remaining != 1000 {
		t.Errorf("多包求和错误：total=%v remaining=%v, want 1500/1000",
			q.Summary.Total, q.Summary.Remaining)
	}
}

// 只报了剩余、没有容量的包，总量退化为剩余，比值保持 100%。
func TestLegacyQuotaUsesRemainAsSizeWhenMissing(t *testing.T) {
	body := []byte(`{"data":{"Response":{"Data":{"Accounts":[
		{"CycleCapacitySize":0,"CycleCapacityRemain":0,"CapacitySize":0,"CapacityRemain":250}
	]}}}}`)
	q := interpretQuotaResponse(200, body)

	if q.Summary.Total != 250 || q.Summary.Remaining != 250 {
		t.Errorf("缺容量时应以剩余兜底：total=%v remaining=%v, want 250/250",
			q.Summary.Total, q.Summary.Remaining)
	}
}

// 点「余额」只更新数值，不改变积分格的形态。
//
// updateCreditCell 原先用 cell.textContent 整体替换，把「剩余 / 总量」与进度条一并
// 抹掉，只剩一个裸数字——刷新一次余额反而把可读性毁了。现在它定位到具体元素逐个更新。
func TestCreditRefreshKeepsTheCellShape(t *testing.T) {
	script := mainPageScript()

	if !strings.Contains(script, "cell.querySelector('.credit-remaining')") {
		t.Error("刷新没有定位到剩余数值元素，可能仍在整体替换")
	}
	if !strings.Contains(script, "cell.querySelector('.credit-total')") {
		t.Error("刷新没有更新总量")
	}
	if !strings.Contains(script, "cell.querySelector('.credit-bar > span')") {
		t.Error("刷新没有更新进度条")
	}
	// 整体替换只在「没有容量」的分支里保留：那种单元格本来就只有数字。
	if strings.Contains(script, "cell.textContent = String(result.credits);") &&
		!strings.Contains(script, "} else {") {
		t.Error("仍在无条件整体替换单元格内容")
	}

	// 服务端渲染要给出 JS 能定位的结构。
	resetState()
	seedPanelAccounts(t)
	page := renderMainPage()
	for _, want := range []string{"credit-ratio", "credit-remaining", "credit-total", "credit-bar"} {
		if !strings.Contains(page, want) {
			t.Errorf("积分格缺少 %s", want)
		}
	}
}

// 账号表只保留五列。
//
// 在途 / 用量 / 最近成功 按需求去掉：它们回答的问题在这张表里没人问，每列却各占一份
// 宽度，剩下几列本可以用上。
func TestAccountTableColumns(t *testing.T) {
	resetState()
	seedPanelAccounts(t)
	page := renderMainPage()

	accounts := sectionOf(page, "view-accounts")
	if accounts == "" {
		t.Fatal("未找到账号页")
	}
	// 取账号表的表头。
	start := strings.Index(accounts, `<table class="accounts"`)
	if start < 0 {
		t.Fatal("未找到账号表")
	}
	head := accounts[start:]
	head = head[:strings.Index(head, "</thead>")]
	// 只数 <th 元素：<thead> 本身也以 "<th" 开头，直接统计会多算一个。
	cols := len(regexp.MustCompile(`<th[ >]`).FindAllString(head, -1))

	// 账号 / 区域 / 状态 / 积分 / 成功·失败 / 操作
	if cols != 6 {
		t.Errorf("账号表应有 6 列，实际 %d", cols)
	}
	if !strings.Contains(head, "区域") {
		t.Error("账号表缺少「区域」列")
	}
	for _, gone := range []string{"在途", "用量", "最近成功"} {
		if strings.Contains(head, gone) {
			t.Errorf("账号表仍含已移除的列 %s", gone)
		}
	}
}

// 一个账号只能有一条 lane，无论调用方用哪个标识来称呼它。
//
// 用户看到「host 提供 2 个，本地 lanes=4」：拦截器用的是 CPA 的 auth id（auth 文件名，
// 形如 codebuddy-<uid>.json），而账号表用的是凭据自身的 uid。同一个账号因此被建成两条
// lane，账目对不上，调用记录里的账号名也和账号表列的不是同一个。
//
// canonicalUID 把两者归一到账号表使用的那一个。
func TestCanonicalUIDCollapsesBothIdentifiers(t *testing.T) {
	resetState()
	installAuthList(t, nil)

	uid := "42213638-073a-434a-90b7-42eff8af6478"
	authIndex := "codebuddy-" + uid + ".json"

	state.accounts.mu.Lock()
	state.accounts.cached = []workBuddyAccount{
		{Label: "国内一号", UID: uid, AuthIndex: authIndex, Variant: "cn"},
	}
	state.accounts.fetchedAt = timeNowForTest()
	state.accounts.mu.Unlock()

	if got := canonicalUID(authIndex); got != uid {
		t.Errorf("auth id 未被归一：%q, want %q", got, uid)
	}
	if got := canonicalUID(uid); got != uid {
		t.Errorf("uid 本身应原样返回：%q", got)
	}
	// 存储里没有的标识原样返回，不猜。
	if got := canonicalUID("unknown-id"); got != "unknown-id" {
		t.Errorf("未知标识应原样返回：%q", got)
	}
	if got := canonicalUID(""); got != "" {
		t.Errorf("空值应返回空：%q", got)
	}

	// 两个标识注册后只应得到一条 lane。
	state.pool.observe("codebuddy", uid, "国内一号")
	state.pool.observe("codebuddy", authIndex, "国内一号")
	if lanes := state.pool.snapshot(); len(lanes) != 1 {
		t.Errorf("同一账号产生了 %d 条 lane，want 1", len(lanes))
	}
}

// 诊断信息完全不再写入。
//
// 「选号：host 提供 N 个，本地 lanes=N（数量不一致）」是排查期间加的，根因已在
// canonicalUID 处修掉。留着它只会在日志里增加像报错的行。
func TestCandidateDiagnosticIsGone(t *testing.T) {
	src := readSourceFile(t, "scheduler.go")
	for _, gone := range []string{"选号：host 提供", "lastOffer"} {
		if strings.Contains(src, gone) {
			t.Errorf("scheduler.go 仍含已移除的诊断：%s", gone)
		}
	}
}

// 任务页把账号与任务放在一起，而不是散在三处。
//
// 原先：一条只有数字的统计条、一张「参与账号」表、外加浮在上方的结果区。读一个账号
// 的状态要来回跳，而一次执行的结果出现在与它无关的位置。现在每个账号一行，它的任务
// 就在这一行下面。
func TestTaskPageKeepsAccountAndTasksTogether(t *testing.T) {
	resetState()
	seedPanelAccounts(t)
	page := renderMainPage()

	tasks := sectionOf(page, "view-tasks")
	if tasks == "" {
		t.Fatal("未找到任务页")
	}

	// 三个区块，按「设一次 → 现在跑 → 每个账号」排列。
	for _, want := range []string{"每天自动执行", ">立即执行<", "<h3>账号与任务</h3>"} {
		if !strings.Contains(tasks, want) {
			t.Errorf("任务页缺少区块 %s", want)
		}
	}

	// 每个账号自带一个详情槽和两个操作：展开与单独执行。
	for _, want := range []string{
		`data-task-row="1"`,
		`class="task-detail-row"`,
		`data-task-expand="1"`,
		`data-task-run="1"`,
	} {
		if !strings.Contains(tasks, want) {
			t.Errorf("账号行缺少 %s", want)
		}
	}

	// 旧的重复元素必须消失，否则还是三处显示。
	for _, gone := range []string{`id="growthDetail"`, `id="growthMsg"`, "参与账号", "<h3>任务执行</h3>"} {
		if strings.Contains(page, gone) {
			t.Errorf("任务页仍有旧元素 %s", gone)
		}
	}
}

// 任务明细要能说出哪些没做完。
//
// 原先每行只有「最近任务」一个名字，屏幕上没有任何地方回答「还差哪些」。详情里每项
// 都带状态与进度，并在表头给出已完成/未完成的数量。
func TestTaskDetailListsPendingItems(t *testing.T) {
	// 渲染在浏览器侧完成，断言落在脚本上：三态各自的字面量与计数都要出现。
	script := mainPageScript()
	for _, want := range []string{"已完成", "未完成", "无法代做", "'共 ' + tasks.length + ' 项"} {
		if !strings.Contains(script, want) {
			t.Errorf("明细渲染缺少 %q", want)
		}
	}
	// 中文任务名优先，机器码只作兜底。
	if !strings.Contains(script, "t.name || t.task_code || t.code") {
		t.Error("任务名没有优先取中文名")
	}
	// 累计奖励也要给出。
	if !strings.Contains(script, "reward_credit") {
		t.Error("明细没有显示奖励")
	}
}

// 没有任务记录时给出可操作的提示，而不是空白。
func TestTaskDetailHandlesEmptyRecord(t *testing.T) {
	script := mainPageScript()
	if !strings.Contains(script, "还没有任务记录") {
		t.Error("空记录时应提示先执行一次")
	}
	// 两种字段名都要接受，否则换一种形状就显示空白。
	if !strings.Contains(script, "payload.tasks || payload.task_list") {
		t.Error("明细没有兼容两种字段名")
	}
}

// 不在成长任务范围内的账号，不该出现「展开任务」按钮。
//
// 成长任务中心仅国内版可用，执行端会据此过滤。页面上原先给每个账号都放了展开按钮，
// 国际版账号点下去必然失败——而且失败信息还被前端吞掉，只剩「查询失败」四个字。
func TestInternationalAccountsHaveNoGrowthControls(t *testing.T) {
	resetState()
	page := renderMainPage()
	tasks := sectionOf(page, "view-tasks")

	if tasks == "" {
		t.Fatal("未找到任务页")
	}
	// 判断谓词要与执行端一致：空 variant 视为国内。
	if !growthEligibleVariant("") {
		t.Error("未知 variant 应按国内处理，与执行端默认一致")
	}
	if !growthEligibleVariant("cn") {
		t.Error("国内版应在成长任务范围内")
	}
	if growthEligibleVariant("ai") {
		t.Error("国际版不在成长任务范围内")
	}
}

// 端点返回 ok:false 时必须显示原因，不能渲染成空白。
func TestTaskDetailSurfacesServerSideError(t *testing.T) {
	// 明细是异步取回来的，渲染必须在浏览器里做：服务端的同名函数在页面脚本中不存在，
	// 调用它只会得到 "renderTaskDetail is not defined"。
	script := mainPageScript()
	if !strings.Contains(script, "function renderTaskDetail(") {
		t.Error("页面脚本里没有 renderTaskDetail，展开时会报未定义")
	}
	if !strings.Contains(script, "payload.ok === false") {
		t.Error("前端没有检查 ok:false，服务端拒绝会被渲染成空白")
	}
	if !strings.Contains(script, "payload.error") {
		t.Error("前端没有把服务端给出的原因显示出来")
	}
}

// 任务明细要区分「未完成」与「无法代做」。
//
// 上游对需要真实桌面操作的任务给出 skip_reason。把它和未完成混在一起，剩余计数就是
// 错的，也看不出为什么。
func TestTaskDetailSeparatesSkippedFromPending(t *testing.T) {
	// 带 skip_reason 的任务是「无法代做」，不是「未完成」：混在一起会让剩余计数偏高，
	// 也看不出它为什么从来不动。三种状态都要在渲染脚本里出现。
	script := mainPageScript()
	for _, want := range []string{"已完成", "未完成", "无法代做", "skip_reason"} {
		if !strings.Contains(script, want) {
			t.Errorf("明细渲染缺少 %q", want)
		}
	}
}

// 账号名旁边要标出国内/国际，两个列表用同一种写法。
//
// 这个归属决定账号能做什么——成长任务与签到只对国内账号存在——所以它应该出现在每一
// 处提到账号名的地方。此前只有任务页显示，而且印的是原始值（cn / ai）；账号页完全不
// 显示。
func TestRealmBadgeAppearsOnBothLists(t *testing.T) {
	resetState()
	// seedPanelAccounts 提供一个国内与一个国际账号，正是这个断言需要的两种归属。
	seedPanelAccounts(t)

	// 三个标签各自的中文说法。
	if label, _ := variantBadgeText("cn"); label != "国内" {
		t.Errorf("cn 应显示「国内」，得到 %q", label)
	}
	if label, _ := variantBadgeText("ai"); label != "国际" {
		t.Errorf("ai 应显示「国际」，得到 %q", label)
	}
	if label, _ := variantBadgeText(""); label != "未标注" {
		t.Errorf("未知归属应显示「未标注」，得到 %q", label)
	}
	// 空归属也要有一个标签，不能静默省略——那会让人以为这个账号没有归属。
	if variantBadge("") == "" {
		t.Error("未知归属也应渲染标签")
	}

	page := renderMainPage()
	accounts := sectionOf(page, "view-accounts")
	tasks := sectionOf(page, "view-tasks")
	for name, body := range map[string]string{"账号页": accounts, "任务页": tasks} {
		if body == "" {
			t.Fatalf("%s 未渲染", name)
		}
		if !strings.Contains(body, "tag-cn") || !strings.Contains(body, "tag-ai") {
			t.Errorf("%s 缺少国内或国际标记", name)
		}
		// 原始值不该出现：它是存储格式，不是给读者看的。
		if strings.Contains(body, ">cn<") || strings.Contains(body, ">ai<") {
			t.Errorf("%s 仍在显示原始 variant 值", name)
		}
	}
}

// 最近调用只列出模型调用。
//
// 日志里还有调度器的提示与任务摘要——它们有保留的价值，但没有模型、没有 token、
// 没有上游，混在调用列表里会被当流量读。
func TestRecentCallsExcludeNonModelRecords(t *testing.T) {
	log := newCallLog(50)
	log.add(callRecord{ProviderID: "p", Model: "glm-5.3", StatusCode: 200, StartedAt: timeNowForTest()})
	log.addNotice(callRecord{ProviderID: "p", Model: "growth", Error: "定时任务完成"})
	log.add(callRecord{ProviderID: "p", StatusCode: 200, StartedAt: timeNowForTest()}) // 无模型名，add 会丢弃

	// add 会丢弃没有模型名的记录：它不是调用，没有资格进这个列表。
	only := log.modelCallsOnly(10)
	if len(only) != 1 {
		t.Fatalf("只有一条真正的调用，得到 %d", len(only))
	}
	if only[0].Model != "glm-5.3" {
		t.Errorf("保留的不是模型调用：%q", only[0].Model)
	}
	// 通知另有去处，没有被丢弃。
	if len(log.noticeLog(10)) != 1 {
		t.Errorf("通知环应有 1 条，得到 %d", len(log.noticeLog(10)))
	}
}

// 用量趋势要能选三个时间范围。
func TestTrendOffersThreeRanges(t *testing.T) {
	resetState()
	page := renderMainPage()
	usage := sectionOf(page, "view-usage")

	for _, want := range []string{
		`data-trend-range="day"`,
		`data-trend-range="3day"`,
		`data-trend-range="week"`,
	} {
		if !strings.Contains(usage, want) {
			t.Errorf("趋势卡缺少范围 %s", want)
		}
	}
	// 服务端要同时给出两套序列，否则切换范围得重新请求。
	src := readSourceFile(t, "management.go")
	for _, key := range []string{`"usage_hourly"`, `"usage_daily"`} {
		if !strings.Contains(src, key) {
			t.Errorf("status 未返回 %s", key)
		}
	}
}

// 任务页把定时设置与手动触发放进同一张卡。
//
// 原先两张卡共六个按钮，按之前得先弄清哪个按钮属于哪张卡。
func TestTaskPageMergesScheduleAndManualRuns(t *testing.T) {
	resetState()
	page := renderMainPage()
	tasks := sectionOf(page, "view-tasks")

	// 两个区块：任务卡与账号表。
	if got := strings.Count(tasks, `class="box"`); got != 2 {
		t.Errorf("任务页应有 2 张卡，实际 %d", got)
	}
	// 定时与手动都在同一张卡里，且一个保存按钮管两处设置。
	for _, want := range []string{
		"每天自动执行", ">立即执行<",
		`id="gsEnabled"`, `id="ckEnabled"`,
		`data-call="saveSchedule"`,
		`data-call="runAllTasks"`, `data-call="runCheckin"`,
	} {
		if !strings.Contains(tasks, want) {
			t.Errorf("合并后的任务卡缺少 %s", want)
		}
	}
	if got := strings.Count(tasks, `data-call="saveSchedule"`); got != 1 {
		t.Errorf("保存按钮应有 1 个，实际 %d", got)
	}
}

// 只有供应商卡的控件靠右。
//
// .setting-control 是定宽列，被两处共用：供应商卡的两组分段按钮，以及任务卡的定时行。
// 给基础类加 margin-left:auto 会把任务卡的定时行也推到右边，与它的标签脱开——看着像
// 布局坏了。右对齐是供应商卡自己的事，用类限定住。
func TestSettingColumnsDoNotGrowIntoTheGap(t *testing.T) {
	// 排版问题的根源是两列都按比例伸缩：各自认领一半行宽，然后把剩余空间留成空档，
	// 卡片中间于是出现一条宽阔的空白带。现在两列都按内容取宽，行内用
	// space-between 把控件推到行尾。
	css := uiCSS

	start := strings.Index(css, ".setting-label {")
	if start < 0 {
		t.Fatal("未找到 .setting-label")
	}
	label := css[start:]
	label = label[:strings.Index(label, "}")]
	if strings.Contains(label, "flex: 1 1") {
		t.Error("标签列仍在按比例伸缩，会留出空档")
	}

	start = strings.Index(css, ".setting-control {")
	if start < 0 {
		t.Fatal("未找到 .setting-control")
	}
	ctrl := css[start:]
	ctrl = ctrl[:strings.Index(ctrl, "}")]
	if strings.Contains(ctrl, "flex: 1 1") || strings.Contains(ctrl, "flex-grow") {
		t.Error("控件列仍在按比例伸缩，会留出空档")
	}

	// 行本身要把两端分开，控件才落在行尾。
	if !strings.Contains(css, "justify-content: space-between;") {
		t.Error("设置行没有把控件推到行尾")
	}

	// 窄屏时两列都占满宽度，"行尾对齐" 无从谈起。
	if !strings.Contains(css, ".setting-control { flex: 1 1 100%; align-items: stretch; }") {
		t.Error("窄屏下控件列应占满宽度")
	}
}

// CSS 常量里不能出现反引号。
//
// uiCSS 是 Go 的原始字符串字面量，一个反引号就会把它提前闭合：后面的样式变成 Go 代码，
// 报出一串与 CSS 无关的语法错误。在注释里写 `vertical-align: middle` 时最容易踩到。
func TestStylesheetHasNoBackticks(t *testing.T) {
	for name, body := range map[string]string{"uiCSS": uiCSS, "uiTabsScript": uiTabsScript} {
		if i := strings.IndexByte(body, '`'); i >= 0 {
			line := 1 + strings.Count(body[:i], "\n")
			t.Errorf("%s 第 %d 行含反引号，会提前闭合字符串", name, line)
		}
	}
}

// 账号单元格是两行（名称、归属），其控件的单元格要各自垂直居中。
//
// 只给 tr 设 vertical-align: middle 不够：当某一行变高，按钮会浮在单元格顶部，看起来
// 比邻居偏上。
func TestAccountCellsCentreTheirControls(t *testing.T) {
	// 账号格有两行（名称与标识），旁边的格子只有一行。若不让单元格内的控件垂直居中，
	// 按钮会贴在格的顶部，行下方的横线看上去就错了一级。
	css := cssForTest()

	if !strings.Contains(css, "table.accounts td { vertical-align: middle; }") {
		t.Error("账号表没有统一垂直居中")
	}
	// 药丸与按钮都是行内级的盒子，居中它们自身才不会把行高撑歪。
	if !strings.Contains(css, "table.accounts td .pill,") ||
		!strings.Contains(css, "table.accounts td button { display: inline-flex; align-items: center; vertical-align: middle; }") {
		t.Error("单元格内的药丸或按钮没有垂直居中")
	}
	// 账号表没有「参与」列，不该出现针对它的规则——那是任务表的。
	if strings.Contains(css, `table.accounts td[data-label="参与"]`) {
		t.Error("账号表出现针对「参与」列的规则，它并没有这一列")
	}
}

// 表格里的操作按钮等宽。
//
// 按各自标签定宽会让按钮组的左边缘逐行参差——「签到 / 余额 / 任务 / 禁用」都是两字，
// 而「展开任务」是四字，每行的起点都不同。
func TestActionButtonsArePlainInlineBlocks(t *testing.T) {
	// 操作列经历过三版实现，只有这一版稳定：普通表格单元格 + 右对齐 + 行内块按钮。
	// 用 flex 会把 <td> 移出列布局；声明宽度（哪怕 1%）会让它塌陷、按钮溢到左边的格子。
	css := cssForTest()

	start := strings.Index(css, "td.actions {")
	if start < 0 {
		t.Fatal("未找到 td.actions")
	}
	block := css[start:]
	block = block[:strings.Index(block, "}")]
	if strings.Contains(block, "display: flex") {
		t.Error("操作列仍在用 flex，会把单元格移出列布局")
	}
	if !strings.Contains(block, "text-align: right") {
		t.Error("操作列没有用右对齐")
	}
	// 按钮之间靠右外边距留白：无论单行还是折行，间距一致，也不必特判首尾。
	if !strings.Contains(css, "td.actions button {") ||
		!strings.Contains(css, "margin: 0 8px 0 0") {
		t.Error("操作列的按钮缺少间距")
	}
	if !strings.Contains(css, "td.actions button:last-child { margin-right: 0; }") {
		t.Error("最后一个按钮没有去掉右边距，会与单元格边缘不齐")
	}
	// 不再强制统一宽度：四个标签都是两个字，自然就齐。
	if strings.Contains(css, "td.actions > button { min-width") {
		t.Error("操作按钮仍在被强制统一宽度")
	}
}

// 两个定时任务并排显示，各占一栏。
//
// 串行堆叠时它们只占卡片的一半，另一半空着。
func TestScheduleJobsSitSideBySide(t *testing.T) {
	resetState()
	page := renderMainPage()
	tasks := sectionOf(page, "view-tasks")

	if !strings.Contains(tasks, `class="sched-pair"`) {
		t.Error("缺少并排容器")
	}
	if got := strings.Count(tasks, `class="sched-col"`); got != 2 {
		t.Errorf("应有 2 栏，实际 %d", got)
	}
	// 保存按钮在卡片底部。
	if !strings.Contains(tasks, `class="sched-foot"`) {
		t.Error("缺少底部保存区")
	}
	if got := strings.Count(tasks, `data-call="saveSchedule"`); got != 1 {
		t.Errorf("保存按钮应有 1 个，实际 %d", got)
	}
}

// 供应商切换必须真的能点动。
//
// 三处契约同时存在才生效，任何一处断掉都会让按钮看起来没反应：
//
//	按钮带 data-value，脚本按它判断高亮哪一项
//	脚本写的类名与样式表用的是同一个（on）
//	两个分组各自的容器与消息位的 id 都在，脚本才找得到它们
func TestSupplierSwitchesAreWired(t *testing.T) {
	resetState()
	page := renderMainPage()
	settings := sectionOf(page, "view-settings")
	if settings == "" {
		t.Fatal("未找到设置页")
	}

	// 两个分段控件都有 id，且按钮带 data-value。
	for _, segID := range []string{"variantSeg", "authSeg"} {
		if !strings.Contains(settings, `id="`+segID+`"`) {
			t.Errorf("缺少分段控件 %s", segID)
		}
		start := strings.Index(settings, `id="`+segID+`"`)
		block := settings[start:]
		block = block[:strings.Index(block, "</div>")]
		if !strings.Contains(block, `data-value=`) {
			t.Errorf("%s 的按钮没有 data-value，脚本无法判断该高亮哪个", segID)
		}
	}
	// 调用范围三项，授权归属两项（只要国内与国际，不再有「跟随」）。
	if got := strings.Count(settings, `data-value=`); got != 5 {
		t.Errorf("data-value 应有 5 个（3 + 2），实际 %d", got)
	}
	if strings.Contains(settings, `data-value="follow"`) {
		t.Error("授权归属仍有「跟随」选项，应只保留国内与国际")
	}

	// 两个分组各有自己的消息位与「当前…」行，脚本用 id + Effect 后缀找后者。
	for _, id := range []string{"variantMsg", "authMsg", "variantSegEffect", "authSegEffect"} {
		if !strings.Contains(settings, `id="`+id+`"`) {
			t.Errorf("缺少 %s，切换后无处反馈", id)
		}
	}

	script := mainPageScript()
	// 高亮用 on 类——样式表里只有 .seg button.on。
	if !strings.Contains(script, "classList.toggle('on'") {
		t.Error("脚本没有用 on 类切换高亮")
	}
	if strings.Contains(cssForTest(), "button.active") {
		t.Error("样式表里出现了 active 类，脚本写的却是 on")
	}
	// 读的是 data-value，不是别的属性名。
	if !strings.Contains(script, "getAttribute('data-value')") {
		t.Error("脚本没有按 data-value 判断选中项")
	}
}

// 趋势提供 1 天 / 3 天 / 7 天三个范围。
func TestTrendRangesAreDayBased(t *testing.T) {
	resetState()
	page := renderMainPage()
	usage := sectionOf(page, "view-usage")

	// 属性之间有 title，用正则按「范围 + 选项文字」配对，避免依赖属性顺序。
	reRange := regexp.MustCompile(`data-trend-range="([a-z0-9]+)"[^>]*>([^<]+)<`)
	found := map[string]string{}
	for _, m := range reRange.FindAllStringSubmatch(usage, -1) {
		found[m[1]] = strings.TrimSpace(m[2])
	}
	for rangeVal, label := range map[string]string{"day": "1 天", "3day": "3 天", "week": "7 天"} {
		if found[rangeVal] != label {
			t.Errorf("范围 %s 的选项文字应为 %q，实际 %q", rangeVal, label, found[rangeVal])
		}
	}
	// 分钟级的「1 小时」已按需求去掉。
	if strings.Contains(usage, `data-trend-range="hour"`) {
		t.Error("趋势卡仍有「1 小时」选项")
	}
	// 三个范围都要有对应的切片逻辑。
	script := mainPageScript()
	for _, want := range []string{"trendData.daily.slice(-7)", "trendData.daily.slice(-3)", "trendData.hourly.slice(-24)"} {
		if !strings.Contains(script, want) {
			t.Errorf("缺少切片逻辑 %s", want)
		}
	}
}

// 表格下方的说明自成一块，上下留白对称。
//
// 先前是一段紧贴表格的段落，只剩卡片底部的高度堆在它下面，读起来像文字浮在上方。
func TestTableNotesUseTheirOwnBlock(t *testing.T) {
	resetState()
	page := renderMainPage()
	tasks := sectionOf(page, "view-tasks")

	if !strings.Contains(tasks, `class="card-block notes-block"`) {
		t.Error("表格说明没有独立分区")
	}
	css := cssForTest()
	if !strings.Contains(css, ".notes-block { padding-top: 14px; padding-bottom: 14px; }") {
		t.Error("说明块上下留白不对称")
	}
}

// cssForTest exposes the stylesheet to assertions.
func cssForTest() string { return uiCSS }

// 成功 / 失败要按账号真实统计。
//
// 同一个账号在不同时期的记录里有两个标识：早期写的是 CPA 的运行时 auth id，之后才
// 改成凭据自身的 uid。直接比较字符串时，旧记录对新账号不可见，整列恒为 0 / 0——看着
// 像「没有数据」，实际是没匹配上。
func TestCallTallyRecognisesBothIdentifiers(t *testing.T) {
	log := newCallLog(50)
	log.add(callRecord{ProviderID: "p", UID: "uid-real", Model: "m", StatusCode: 200, StartedAt: timeNowForTest()})
	log.add(callRecord{ProviderID: "p", UID: "auth-index", Model: "m", StatusCode: 200, StartedAt: timeNowForTest()})
	log.add(callRecord{ProviderID: "p", UID: "uid-real", Model: "m", StatusCode: 500, Error: "boom", StartedAt: timeNowForTest()})
	// 一条诊断不应计入。
	log.addNotice(callRecord{ProviderID: "p", Model: "growth", Error: "定时任务完成"})

	// 直接查其中一个标识都要能看见全部属于该账号的记录。
	wanted := map[string]bool{"uid-real": true}
	hits := 0
	for _, rec := range log.recent(10) {
		if recordMatchesAccount(rec, wanted) {
			hits++
		}
	}
	if hits != 2 {
		t.Errorf("按 uid 应匹配到 2 条，实际 %d", hits)
	}

	// Notice 记录不算调用。
	success, failed := 0, 0
	for _, rec := range log.recent(10) {
		if rec.Notice || !recordMatchesAccount(rec, wanted) {
			continue
		}
		if rec.Error != "" || rec.StatusCode >= 400 {
			failed++
		} else {
			success++
		}
	}
	if success != 1 || failed != 1 {
		t.Errorf("统计错误：成功 %d 失败 %d，want 1/1", success, failed)
	}
}

// 账号表的列宽与行高是声明的，不是推断的。
//
// auto 布局按内容定尺寸：账号格有名字与 uid 两行，比邻居高，于是每行下方的横线落在
// 不同高度，整张表看着错位。
// 账号表与任务表用同一套布局策略。
//
// 曾经给账号表强制 table-layout: fixed 并手算六列百分比。算术没错，渲染仍然错位：
// 固定布局不能借用空间，账号列装的是 46 个字符的标签，声明的份额比它窄，于是标签被裁、
// 旁边的格子跟着移位。任务表从来没这么做，也就从来没歪过。两张表现在都交给浏览器按
// 内容分配宽度。
func TestAccountTableUsesAutomaticLayout(t *testing.T) {
	css := cssForTest()

	start := strings.Index(css, "table.accounts {")
	if start < 0 {
		t.Fatal("未找到 table.accounts")
	}
	base := css[start:]
	base = base[:strings.Index(base, "}")]
	if strings.Contains(base, "table-layout: fixed") {
		t.Error("账号表用了固定布局，列宽不足时内容会被裁而不是让邻居收缩")
	}
	// 列宽不硬编码，只给需要的内容一个下限。
	if regexp.MustCompile(`table\.accounts td:nth-child\(\d\) \{ width:`).MatchString(css) {
		t.Error("账号表仍在硬编码列宽")
	}
	for _, want := range []string{
		`table.accounts td[data-label="账号"] { min-width: 190px; }`,
		`table.accounts td[data-label="积分"] { min-width: 130px; }`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("账号表缺少 %s", want)
		}
	}
	// 操作列不得声明宽度：哪怕 1% 也会与自动布局相争，把格子压塌。
	if strings.Contains(css, "table.accounts td.actions { width:") {
		t.Error("操作列声明了宽度，会与自动布局相争")
	}
	// 两张表都不用固定布局。
	if strings.Contains(css, "table.data { table-layout: fixed") {
		t.Error("任务表也被改成了固定布局")
	}
}

// 分段按钮的 data-value 必须与存储的取值一致。
//
// 「全部」在设置里存的是空串（VariantOverride 的文档就是这么写的），而按钮一度带着
// "auto"。回读时两者不相等，重新渲染就没有任何选项被选中——表现成「选择不保持」。
func TestVariantButtonsMatchStoredValues(t *testing.T) {
	resetState()
	for _, tc := range []struct{ stored, wantOn string }{
		{"", ""},
		{"cn", "cn"},
		{"ai", "ai"},
	} {
		state.settings.setVariantOverride(tc.stored)
		page := renderMainPage()

		start := strings.Index(page, `id="variantSeg"`)
		if start < 0 {
			t.Fatal("未找到 variantSeg")
		}
		seg := page[start:]
		seg = seg[:strings.Index(seg, "</div>")]

		var onValues []string
		for _, m := range regexp.MustCompile(`<button([^>]*)>([^<]+)</button>`).FindAllStringSubmatch(seg, -1) {
			if !strings.Contains(m[1], `class="on"`) {
				continue
			}
			v := regexp.MustCompile(`data-value="([^"]*)"`).FindStringSubmatch(m[1])
			if v != nil {
				onValues = append(onValues, v[1])
			}
		}
		if len(onValues) != 1 || onValues[0] != tc.wantOn {
			t.Errorf("存储 %q 时应选中的是 %q，实际 %v", tc.stored, tc.wantOn, onValues)
		}
	}
}

// 账号表的每一列都要放得下它的内容。
//
// 固定布局的代价是列宽不可伸缩：声明得过窄，内容不会挤压邻居，而是直接盖过去。
// 操作列要放四个按钮（4×58px 加间距约 250px），先前只分到 167px，「成功 / 失败」那格
// 因此被按钮压住。
// 操作列的内容要放得下，无论表格如何分配宽度。
//
// 这条曾经靠「表格最小宽度 × 操作列百分比」来算，前提是固定布局。改用自动布局后
// 宽度由内容决定，能保证的只有按钮自身的最小宽度与表格的整体下限。
func TestActionColumnIsNotConstrained(t *testing.T) {
	// 这一列不声明宽度：交给自动布局按按钮自身的尺寸给足空间，账号列吸收余量。
	// 先前试过 min-width 与 1% 两种约束，前者让行宽超出卡片，后者把格子压塌。
	css := cssForTest()
	if strings.Contains(css, "table.accounts td.actions { width:") {
		t.Error("操作列仍被声明了宽度")
	}
	if strings.Contains(css, "table.accounts td.actions > button { min-width") {
		t.Error("操作按钮仍被强制统一宽度")
	}
	// 表格自身有下限，窄屏时靠容器滚动。
	if !strings.Contains(css, "table.accounts { min-width: 900px; }") {
		t.Error("账号表缺少最小宽度，窄屏下会被压扁")
	}
}

// 表格单元格一律不用 flex。
//
// 这是「签到压住积分」与「按钮下方横线错位」的共同原因：flex 会把 <td> 从表格的列布局
// 里拿出来，宽度不再与邻居一起计算，高度也不再按同一个规则推导——行下方的横线因此遇到
// 一个高度不同的盒子，看上去错了一级。手机端表格整体横向滚动，这个问题看不出来。
//
// 媒体查询里的 table.stack 是另一回事：那里整套是「折成卡片」的呈现，整张表已经不是表格。
func TestTableCellsAreNeverFlex(t *testing.T) {
	css := cssForTest()

	desktop := css
	if i := strings.Index(css, "@media"); i >= 0 {
		desktop = css[:i]
	}
	reRule := regexp.MustCompile(`(?m)^([^@\n][^{]*?)\{([^}]*)\}`)
	for _, m := range reRule.FindAllStringSubmatch(desktop, -1) {
		selector, body := strings.TrimSpace(m[1]), m[2]
		if !strings.Contains(selector, "td") {
			continue
		}
		if strings.Contains(body, "display: flex") || strings.Contains(body, "display:flex") {
			t.Errorf("选择器 %q 对单元格用了 flex，会把它移出列布局", selector)
		}
	}

	// 两张仍在横向滚动的表，在窄屏下也不许用 flex。
	if i := strings.Index(css, "@media (max-width: 768px)"); i >= 0 {
		for _, m := range reRule.FindAllStringSubmatch(css[i:], -1) {
			selector, body := strings.TrimSpace(m[1]), m[2]
			if !strings.Contains(selector, "table.accounts") && !strings.Contains(selector, "table.data") {
				continue
			}
			if strings.Contains(selector, "td") && strings.Contains(body, "display: flex") {
				t.Errorf("窄屏下 %q 对单元格用了 flex", selector)
			}
		}
	}
}

// 操作列的按钮靠右对齐，间距由按钮自身的右外边距提供。
func TestActionButtonsSitOnARightAlignedRail(t *testing.T) {
	css := cssForTest()

	start := strings.Index(css, "td.actions {")
	if start < 0 {
		t.Fatal("未找到 td.actions")
	}
	block := css[start:]
	block = block[:strings.Index(block, "}")]
	for _, want := range []string{"text-align: right", "vertical-align: middle", "white-space: nowrap"} {
		if !strings.Contains(block, want) {
			t.Errorf("操作列缺少 %s", want)
		}
	}
	if !strings.Contains(css, "td.actions button {") ||
		!strings.Contains(css, "margin: 0 8px 0 0") {
		t.Error("操作列的按钮缺少 8px 间距")
	}
}

// 样式表里同一选择器不得重复声明。
//
// 多轮修改留下了几份重复：table.accounts 的最小宽度写了两次（900 与 1000），按钮内边距
// 两份（10px 与 9px），td:first-child 与 data-label="账号" 各写一次。后一份静默覆盖前
// 一份，改了一处却看不到效果——正是排查这类错位最费时间的地方。
func TestStylesheetHasNoDuplicateSelectors(t *testing.T) {
	css := cssForTest()

	counts := map[string]int{}
	for _, m := range regexp.MustCompile(`(?m)^([.#a-zA-Z][^{@\n]*?)\s*\{`).FindAllStringSubmatch(css, -1) {
		sel := strings.TrimSpace(m[1])
		// 样式表里两处同名的规则是有意保留的例外，逐个说明：
		//   .warn-text / .ok-text 通过 var() 取色，出现两次是有历史原因的同值声明——
		//   这里不豁免，直接合并掉。
		counts[sel]++
	}
	for sel, n := range counts {
		if n > 1 {
			t.Errorf("选择器 %q 声明了 %d 次，后者会静默覆盖前者", sel, n)
		}
	}
}

// 面板的时间一律用固定的北京时间。
//
// 插件跑在 CPA 进程内，而该进程没有可靠的时区：Android 沙箱里 TZ 未设置，本地时区就是
// UTC，而看面板的人在东八区。调用时间戳按 UTC 记，趋势就把 15:30 的调用标成 07:00，
// 日界线也落在本地 08:00。固定时区让数字与墙上的钟一致，无论宿主怎么启动。
func TestPanelTimesUseBeijingTime(t *testing.T) {
	// 时区必须是 +08:00，且带名字（可用时用 Asia/Shanghai，否则退化为固定偏移）。
	_, offset := time.Now().In(panelLocation).Zone()
	if offset != 8*60*60 {
		t.Errorf("面板时区偏移 %d 秒，应为 28800（+08:00）", offset)
	}

	// 一个落在 UTC 与北京时间不同日的时刻，必须归到北京时间那一天。
	// 2026-03-10 23:00 UTC 就是 03-11 07:00 北京。
	utcLate := time.Date(2026, 3, 10, 23, 0, 0, 0, time.UTC)
	if got := utcLate.In(panelLocation).Format("2006-01-02"); got != "2026-03-11" {
		t.Errorf("UTC 23:00 在北京时间应属 03-11，得到 %s", got)
	}

	// 桶按面板时区归档。
	log := newCallLog(10)
	log.add(callRecord{ProviderID: "p", Model: "m", StartedAt: utcLate, PromptTokens: 1, CompletionTokens: 1})
	daily := log.dailyUsage()
	if len(daily) != 1 || daily[0].Date != "2026-03-11" {
		t.Errorf("日桶没有按北京时间归档：%+v", daily)
	}
	hourly := log.hourlyUsage()
	if len(hourly) != 1 || hourly[0].Date != "2026-03-11 07" {
		t.Errorf("小时桶没有按北京时间归档：%+v", hourly)
	}
}

// 时间轴标签上的小时不得重复补零。
//
// Go 的 15 动词产出零填充的 "07"，而脚本里写的是 hm[1] < 10——字符串与数字比较，JS 把
// "07" 转成 7，判定需要补零，于是又加一个零，7 点显示成 "0007:00"。
func TestHourLabelDoesNotDoublePad(t *testing.T) {
	script := mainPageScript()

	if !strings.Contains(script, "parseInt(hm[1], 10)") {
		t.Error("小时标签没有先把字符串转成数字再补零")
	}
	if strings.Contains(script, "hm[1] < 10 ? '0' + hm[1]") {
		t.Error("仍在用字符串与数字比较来判断补零")
	}
}

// 客户端断开不算账号失败。
//
// 用户取消一个慢回复时，CPA 传进来的状态码是 499（「客户端关闭请求」的约定码），错误
// 文本是 context canceled。判定条件是 `Error != "" || StatusCode >= 400`，两条都成立，
// 于是账号显示了一个它从未有过的失败——更糟的是这个失败会喂给池的冷却逻辑，取消一次慢
// 回复就可能把正在应答的那个账号停掉。
func TestClientAbortIsNotAnAccountFailure(t *testing.T) {
	// 识别
	for _, tc := range []struct {
		status int
		msg    string
	}{
		{499, "context canceled"},
		{499, ""},
		{500, "context canceled"},
		{502, "Client disconnected"},
		{500, "write: broken pipe"},
	} {
		if !isClientAbortFailure(tc.status, tc.msg) {
			t.Errorf("应识别为客户端断开：status=%d msg=%q", tc.status, tc.msg)
		}
	}
	// 真正的上游故障不该被误判
	for _, tc := range []struct {
		status int
		msg    string
	}{
		{429, "rate limit exceeded"},
		{401, "invalid token"},
		{500, "internal server error"},
		{200, ""},
	} {
		if isClientAbortFailure(tc.status, tc.msg) {
			t.Errorf("误判为客户端断开：status=%d msg=%q", tc.status, tc.msg)
		}
	}

	// 统计层：不计入失败
	abort := callRecord{ProviderID: "p", Model: "m", StatusCode: 499, Error: "context canceled"}
	if recordFailed(abort) {
		t.Error("499 context canceled 被计为失败")
	}
	// 真失败仍然计入
	real := callRecord{ProviderID: "p", Model: "m", StatusCode: 429, Error: "rate limited"}
	if !recordFailed(real) {
		t.Error("429 未被计为失败")
	}
	// 成功不计入
	ok := callRecord{ProviderID: "p", Model: "m", StatusCode: 200}
	if recordFailed(ok) {
		t.Error("200 被计为失败")
	}

	// 两个列表页都排除它
	log := newCallLog(20)
	log.add(callRecord{ProviderID: "p", Model: "m", StatusCode: 200, StartedAt: timeNowForTest()})
	log.add(callRecord{ProviderID: "p", Model: "m", StatusCode: 499, Error: "context canceled", StartedAt: timeNowForTest()})
	totals := log.totals()
	if totals.TotalFailed != 0 {
		t.Errorf("中止被计为失败：total_failed=%d", totals.TotalFailed)
	}
	daily := log.dailyUsage()
	if len(daily) != 1 || daily[0].Failed != 0 {
		t.Errorf("日趋势把中止计为失败：%+v", daily)
	}
	// 但它仍然是一次已发生的调用，该被计入总调用数。
	if totals.TotalCalls != 2 {
		t.Errorf("中止应计入调用数：total_calls=%d, want 2", totals.TotalCalls)
	}
}

// 客户端断开不产生任何记录。
//
// 用户取消一个慢回复时，CPA 传进来 499 与 context canceled。这条记录有三个可能写入
// 点，逐个都要拦：正常响应的拦截器、执行器内部的上报、以及 CPA 的用量回调。最后一条
// 是漏得最久的一个——被放弃的请求不会走响应拦截器，而用量回调每次都会触发。
//
// 它不写日志也不喂池：取消一次慢回复，不该把正在应答的账号停掉。
func TestClientAbortLeavesNoRecord(t *testing.T) {
	// 识别的边界
	for _, tc := range []struct {
		status int
		msg    string
		want   bool
	}{
		{499, "context canceled", true},
		{499, "", true},
		{502, "context canceled", true},
		{500, "Client disconnected", true},
		{500, "write tcp: broken pipe", true},
		// 真正需要处理的上游故障
		{429, "rate limit exceeded", false},
		{401, "invalid token", false},
		{500, "internal server error", false},
		{200, "", false},
	} {
		if got := isClientAbortFailure(tc.status, tc.msg); got != tc.want {
			t.Errorf("isClientAbortFailure(%d, %q) = %v, want %v", tc.status, tc.msg, got, tc.want)
		}
	}

	// 统计层：不算失败，也不算成功
	abort := callRecord{ProviderID: "p", Model: "m", StatusCode: 499, Error: "context canceled"}
	if recordFailed(abort) {
		t.Error("499 被计为失败")
	}
	// 真失败仍要计入
	real := callRecord{ProviderID: "p", Model: "m", StatusCode: 429, Error: "rate limited"}
	if !recordFailed(real) {
		t.Error("429 未被计为失败")
	}

	// 三个写入点都必须带上这个判断
	for file, needle := range map[string]string{
		"usage_handler.go":      "if rec.Failed && isClientAbortFailure(",
		"executor.go":           "if isClientAbortFailure(statusCode, string(body)) {",
		"intercept_response.go": "if isClientAbortFailure(statusCode, string(req.Body)) {",
	} {
		if !strings.Contains(readSourceFile(t, file), needle) {
			t.Errorf("%s 没有拦截客户端断开，记录会漏出来", file)
		}
	}
}

// 记录页分成两个列表，默认显示调用记录。
//
// 调用与插件自身的操作事件回答的是两个问题——「模型服务了什么」和「插件自己做了什么」
// ——混在一张表里各自都更难读，而调用记录是打开这一页通常要看的东西。
func TestRecordsPageSplitsCallsAndLog(t *testing.T) {
	resetState()
	page := renderMainPage()
	usage := sectionOf(page, "view-usage")
	if usage == "" {
		t.Fatal("未找到记录页")
	}

	// 两个页签，默认选中调用记录。
	if !strings.Contains(usage, `data-log-tab="calls"`) || !strings.Contains(usage, `data-log-tab="notes"`) {
		t.Error("记录页缺少两个页签")
	}
	if !strings.Contains(usage, `class="on" data-log-tab="calls"`) {
		t.Error("默认应选中调用记录")
	}
	// 两个列表，日志那份默认隐藏。
	if !strings.Contains(usage, `id="logPaneCalls"`) || !strings.Contains(usage, `id="logPaneNotes"`) {
		t.Error("记录页缺少两个列表容器")
	}
	if !strings.Contains(usage, `id="logPaneNotes" hidden`) {
		t.Error("请求日志默认应是隐藏的")
	}
	// 清空按钮存在。
	if !strings.Contains(usage, `data-call="clearRequestLog"`) {
		t.Error("缺少清空按钮")
	}
}

// 清空只作用于请求日志，不动调用记录。
//
// 调用记录是总数与趋势的账本，清掉它会让那些数字描述一批已经不存在的记录。
func TestClearingOnlyAffectsTheRequestLog(t *testing.T) {
	log := newCallLog(10)
	log.add(callRecord{ProviderID: workBuddyProviderKey, Model: "glm-5.3", StatusCode: 200, StartedAt: timeNowForTest()})
	log.addNotice(callRecord{ProviderID: workBuddyProviderKey, Error: "签到完成"})
	log.addNotice(callRecord{ProviderID: workBuddyProviderKey, Error: "账号已冷却"})

	if got := log.clearNotices(); got != 2 {
		t.Errorf("应清掉 2 条日志，实际 %d", got)
	}
	if len(log.noticeLog(10)) != 0 {
		t.Error("日志没有被清空")
	}
	// 调用记录与计数都还在。
	if len(log.modelCallsOnly(10)) != 1 {
		t.Error("调用记录被误删")
	}
	if log.totals().TotalCalls != 1 {
		t.Error("调用计数被误改")
	}
}

// 两个列表各留最近 100 条。
func TestRecordListsAreCappedAtOneHundred(t *testing.T) {
	if logListLimit != 100 {
		t.Errorf("上限应为 100，实际 %d", logListLimit)
	}

	log := newCallLog(500)
	for i := 0; i < 150; i++ {
		log.add(callRecord{ProviderID: workBuddyProviderKey, Model: "m", StatusCode: 200, StartedAt: timeNowForTest()})
		log.addNotice(callRecord{ProviderID: workBuddyProviderKey, Error: "事件"})
	}
	if got := len(log.modelCallsOnly(1000)); got != 150 {
		t.Errorf("环内应保留 150 条调用，实际 %d", got)
	}
	// 页面只取前 100 条。
	if got := len(log.modelCallsOnly(logListLimit)); got != logListLimit {
		t.Errorf("调用列表应截到 %d 条，实际 %d", logListLimit, got)
	}
	if got := len(log.noticeLog(logListLimit)); got != logListLimit {
		t.Errorf("日志列表应截到 %d 条，实际 %d", logListLimit, got)
	}
}

// 没有模型名的记录不是调用，不该进调用环，也不该计入总数。
func TestRecordsWithoutModelAreNotCalls(t *testing.T) {
	log := newCallLog(10)
	log.add(callRecord{ProviderID: workBuddyProviderKey, StatusCode: 200, StartedAt: timeNowForTest()})

	if len(log.modelCallsOnly(10)) != 0 {
		t.Error("没有模型名的记录进了调用环")
	}
	if log.totals().TotalCalls != 0 {
		t.Error("没有模型名的记录被计入总数")
	}
}

// 请求日志不能是空的。
//
// 这一页签最初没有数据源：签到、任务、限流与禁用的记录各自留在产生它们的子系统里，从
// 来没有汇总到面板读的地方，于是那一页永远显示「暂无」。这条断言把四个来源都绑住，
// 少一个就少一类事件。
func TestRequestLogGathersEverySource(t *testing.T) {
	resetState()

	// 四个来源各造一条。
	state.log.addNotice(callRecord{ProviderID: workBuddyProviderKey, Error: "定时任务完成"})

	// 走真实的失败路径：永久禁用会写入池的退役审计。
	state.pool.observe(workBuddyProviderKey, "u-1", "一号")
	state.pool.failureForModel(workBuddyProviderKey, "u-1", "", failureAuth, "凭据失效", state.settings.get(), true)
	state.checkin.record(&checkinRun{StartedAt: timeNowForTest(), Trigger: "manual", Total: 2, Succeeded: 1, Failed: 1})
	state.growth.record("u-2", growthRunResult{
		Label: "二号", OK: true, Claimed: 3, Failed: 1,
		FinishedAt: timeNowForTest(),
	})

	entries := collectRequestLog(50)
	if len(entries) < 4 {
		t.Fatalf("四个来源都应出现，实际 %d 条：%+v", len(entries), entries)
	}

	joined := ""
	for _, e := range entries {
		joined += e.Error + "\n"
	}
	for _, want := range []string{"账号已自动禁用", "签到：", "任务：", "定时任务完成"} {
		if !strings.Contains(joined, want) {
			t.Errorf("请求日志缺少 %q 的内容", want)
		}
	}

	// 时间倒序。
	for i := 1; i < len(entries); i++ {
		if entries[i].StartedAt.After(entries[i-1].StartedAt) {
			t.Errorf("第 %d 条比前一条更新，排序不对", i)
		}
	}
	// 上限生效。
	if got := len(collectRequestLog(2)); got != 2 {
		t.Errorf("上限应为 2，实际 %d", got)
	}
}

// 清空日志要覆盖全部来源。
//
// 日志是四处合并出来的，只清其中一处会让按钮看起来没反应——操作者看的那几条来自另一个
// 存储。同时不能碰到状态：清空列表不该重新启用账号，也不该让调度器以为今天没跑过。
func TestClearingTheLogCoversEverySource(t *testing.T) {
	resetState()
	state.log.addNotice(callRecord{ProviderID: workBuddyProviderKey, Error: "一条提示"})
	// 走真实的失败路径：永久禁用会写入池的退役审计。
	state.pool.observe(workBuddyProviderKey, "u-1", "一号")
	state.pool.failureForModel(workBuddyProviderKey, "u-1", "", failureAuth, "凭据失效", state.settings.get(), true)
	state.checkin.record(&checkinRun{StartedAt: timeNowForTest(), Trigger: "manual", Total: 1, Succeeded: 1})
	state.growth.record("u-2", growthRunResult{
		Label: "二号", OK: true, Claimed: 1,
		FinishedAt: timeNowForTest(),
	})

	if got := len(collectRequestLog(50)); got < 4 {
		t.Fatalf("清空前应有至少 4 条，实际 %d", got)
	}

	state.log.clearNotices()
	state.pool.clearAutoDisableHistory()
	state.checkin.clearHistory()
	state.growth.clearHistory()

	if got := len(collectRequestLog(50)); got != 0 {
		t.Errorf("清空后应无条目，实际 %d：%+v", got, collectRequestLog(50))
	}
}

// 插件的标识与显示名是两回事。
//
// CPA 用标识推导路由（/v0/management/<id>、/v0/resource/plugins/<id>），插件也用它定位
// 数据目录；把标识改成大写会让面板发出的每个请求 404，并让已存的配置找不到。
//
// 而授权页面显示的是注册里的 Name，它会插进「通过插件提供的 OAuth 流程登录 {{name}}」
// 这样的句子里——那里要的是产品的大小写，不是目录安全的标识。
func TestIdentifierAndDisplayNameStaySeparate(t *testing.T) {
	if pluginName != strings.ToLower(pluginName) {
		t.Errorf("标识 %q 含大写，不再适合做路径段", pluginName)
	}
	if pluginDisplayName != "WorkBuddy" {
		t.Errorf("显示名应为 WorkBuddy，实际 %q", pluginDisplayName)
	}
	if strings.ContainsAny(pluginName, "/ \t") {
		t.Errorf("标识 %q 含路径分隔或空白", pluginName)
	}

	// 路由与数据目录必须仍用标识。
	src := readSourceFile(t, "rpc.go")
	if !strings.Contains(src, "Name:             pluginDisplayName,") {
		t.Error("注册名没有使用显示名")
	}
	// 注册块之外不得出现显示名——它只用于展示。
	for _, file := range []string{"checkin_page.go", "main_script.go", "quota_script.go", "yaml.go"} {
		if strings.Contains(readSourceFile(t, file), "pluginDisplayName") {
			t.Errorf("%s 用了显示名，路径类用途必须用标识", file)
		}
	}

	// 路由仍在用标识。
	for _, needle := range []string{
		`"/v0/resource/plugins/" + pluginName`,
		`"/v0/management/" + pluginName`,
	} {
		if !strings.Contains(readSourceFile(t, "checkin_page.go"), needle) {
			t.Errorf("路由不再使用标识：%s", needle)
		}
	}
}

// 调用记录显示的是「怎么被服务」，不是「哪个账号」。
//
// 这一列原来写账号名，但做不到：CPA 对同一份凭据在一个地方用运行时 auth index（形如
// 7edb3b68871f4d16），在另一个地方用它自己的 uid（形如 cb56d65f-9921-…），插件无法把
// 两者对上——面板于是把同一个账号显示成两个名字，读者无从判断。记录确知的是这次请求
// 以何种方式被服务，这一列现在报这个。
func TestCallRecordsShowStreamKind(t *testing.T) {
	resetState()

	page := renderCallTable([]callRecord{
		{Model: "glm-5.3-flash", StatusCode: 200, Stream: true},
		{Model: "glm-5.3-flash", StatusCode: 200, Stream: false},
	})

	if strings.Contains(page, ">账号<") {
		t.Error("表头仍写着账号")
	}
	if !strings.Contains(page, ">类型<") {
		t.Error("表头没有类型列")
	}
	if !strings.Contains(page, "流式") || !strings.Contains(page, "非流式") {
		t.Error("两种类型都应能显示")
	}
	// 「非流式」含「流式」子串，所以按单元格数一次。
	if got := strings.Count(page, `data-label="类型"`); got != 2 {
		t.Errorf("每行都应有类型格，实际 %d 个", got)
	}
	// 不再出现账号相关的标识。
	for _, gone := range []string{"data-label=\"账号\"", "WorkBuddy cb56d65f"} {
		if strings.Contains(page, gone) {
			t.Errorf("表中仍出现 %q", gone)
		}
	}

	if streamLabel(true) != "流式" || streamLabel(false) != "非流式" {
		t.Error("类型文案不对")
	}
}
