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

	for _, col := range []string{"账号", "状态", "积分", "成功 / 失败", "在途", "用量", "最近成功", "操作"} {
		if !strings.Contains(page, ">"+col+"<") && !strings.Contains(page, col+"</th>") &&
			!strings.Contains(page, col+"<") {
			t.Errorf("账号表缺少列 %s", col)
		}
	}

	for _, want := range []string{
		"credit-ratio",      // 剩余 / 总量
		"credit-bar",        // 进度条
		"upill",             // 用量小标签
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
		"table.accounts { min-width: 1000px; }",
		"table.accounts thead { display: table-header-group; }",
		"table.accounts td { display: table-cell;",
	} {
		if !strings.Contains(phone, want) {
			t.Errorf("手机端账号表缺少 %s", want)
		}
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
	if got.Checkin.Hour != 9 {
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
	log.add(callRecord{ProviderID: "p", StatusCode: 429, Error: "限流", StartedAt: timeNowForTest()})
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

	// 但诊断本身要留下来，面板上能看到。
	if len(log.recent(10)) != 4 {
		t.Errorf("诊断没有进入记录列表：%d 条", len(log.recent(10)))
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
	if len(log.recent(10)) != 1 {
		t.Error("带 Notice 标记的记录应仍然可见")
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
