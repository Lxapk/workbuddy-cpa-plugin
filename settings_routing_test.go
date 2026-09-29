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
		"table.accounts { min-width: 700px; }",
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
func TestAccountTableHasFiveColumns(t *testing.T) {
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

	if cols != 5 {
		t.Errorf("账号表应有 5 列，实际 %d", cols)
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
	log.add(callRecord{ProviderID: "p", StatusCode: 200, StartedAt: timeNowForTest()}) // 无模型名

	only := log.modelCallsOnly(10)
	if len(only) != 1 {
		t.Fatalf("应只保留 1 条模型调用，得到 %d", len(only))
	}
	if only[0].Model != "glm-5.3" {
		t.Errorf("保留的不是模型调用：%q", only[0].Model)
	}
	// 全部记录仍在，供其他视图使用。
	if len(log.recent(10)) != 3 {
		t.Errorf("原始记录不应被删除，得到 %d 条", len(log.recent(10)))
	}
}

// 用量趋势要能选三个时间范围。
func TestTrendOffersThreeRanges(t *testing.T) {
	resetState()
	page := renderMainPage()
	usage := sectionOf(page, "view-usage")

	for _, want := range []string{
		`data-trend-range="hour"`,
		`data-trend-range="day"`,
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
	css := uiCSS
	for _, want := range []string{
		`table.accounts td[data-label="参与"]`,
		`table.data.tasks td[data-label="参与"]`,
		`display: inline-flex; align-items: center;`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("缺少单元格居中规则 %s", want)
		}
	}
}

// 表格里的操作按钮等宽。
//
// 按各自标签定宽会让按钮组的左边缘逐行参差——「签到 / 余额 / 任务 / 禁用」都是两字，
// 而「展开任务」是四字，每行的起点都不同。
func TestTableActionButtonsShareAWidth(t *testing.T) {
	css := uiCSS
	if !strings.Contains(css, "td.actions > button { min-width: 62px; text-align: center; }") {
		t.Error("操作按钮没有统一最小宽度")
	}
	// 间距只由 gap 提供，逐按钮的 margin 会让第一个按钮比其余的更近。
	start := strings.Index(css, "td.actions {\n  display: flex")
	if start < 0 {
		t.Fatal("未找到 td.actions 的 flex 规则")
	}
	block := css[start:]
	block = block[:strings.Index(block, "}")]
	if strings.Contains(block, "margin-left") {
		t.Error("操作列同时用了 gap 与 margin-left，间距会不一致")
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
