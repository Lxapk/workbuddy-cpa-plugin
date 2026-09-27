package main

import (
	"strings"
	"testing"
)

// 任务结果必须按等级分组，而不是倒进一个 <pre>。
//
// 改造前所有日志行（成功、跳过、失败）混在同一个 <pre> 里，失败条目还带着完整
// 的上游 JSON。一次被同一个前置条件挡住 16 个任务的运行，于是产生十六屏几乎相同
// 的文字，而真正解释原因的那一行埋在里面。
//
// 这里守住的是形状：分组存在、图例存在、条目没有被丢弃。
func TestPanelScriptGroupsGrowthResult(t *testing.T) {
	script := mainPageScript()

	for _, want := range []string{
		"function renderGrowthResult(",
		"function renderGrowthSection(",
		`class="legend"`,
		`<details class="log-group"`,
		`'未成功', 'err'`,
		`'跳过', 'skip'`,
		`'完成', 'ok'`,
		"log-scroll",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("脚本缺少 %s", want)
		}
	}

	// 旧做法：把日志行直接拼进 <pre class="log">。它已经不该存在。
	if strings.Contains(script, `'<div class="card"><h2>成长任务结果`) &&
		strings.Contains(script, `pre class="log"`) {
		// 允许 <pre class="log"> 出现在别处，但不允许它再承载成长任务的结果。
		if strings.Contains(script, "escapeHTML(lines[i].message)") {
			t.Error("成长任务结果仍在拼进 <pre>")
		}
	}
}

// 条目内容必须经 esc 转义后再插入。上游文案里带引号是常态（错误 JSON），
// 未转义就会破坏结构，甚至是注入点。
func TestGrowthResultEscapesEntries(t *testing.T) {
	script := mainPageScript()
	// 匹配不依赖缩进：源码里这些行嵌在模板字符串中，前导空格会一起进入输出。
	if !strings.Contains(script, `'<span class="log-text">' + esc(message) + '</span></div>'`) {
		t.Error("日志条目未经 esc 转义")
	}
	if !strings.Contains(script, `esc(title)`) {
		t.Error("分组标题未经 esc 转义")
	}
	if !strings.Contains(script, `esc(entries.length)`) {
		t.Error("分组计数未经 esc 转义")
	}
	if !strings.Contains(script, `esc(accountCount)`) || !strings.Contains(script, `esc(earned)`) {
		t.Error("标题里的数值未经 esc 转义")
	}
	// 组名是代码里的字面量，同样要过一层——它虽然是常量，但保持一致的写法能
	// 避免以后有人把变量塞进来时忘了转义。
	if !strings.Contains(script, `esc(pair[1])`) {
		t.Error("统计卡数值未经 esc 转义")
	}
}

// 等级到分组的映射必须覆盖后端可能给出的全部取值，否则日志会静默丢失。
//
// growthRunLog.Level 的取值是 info | ok | warn | skip | error。
func TestGrowthResultHandlesEveryLevel(t *testing.T) {
	script := mainPageScript()
	start := strings.Index(script, "function renderGrowthResult(")
	if start < 0 {
		t.Fatal("未找到 renderGrowthResult")
	}
	end := strings.Index(script[start:], "\n  }")
	if end < 0 {
		t.Fatal("renderGrowthResult 未闭合")
	}
	body := script[start : start+end]

	for _, level := range []string{"ok", "skip", "error", "warn", "info"} {
		if !strings.Contains(body, level+":") {
			t.Errorf("未处理等级 %q", level)
		}
	}
	// 未知等级必须落到某个分组，不能丢掉。
	if !strings.Contains(body, "if (!groups[level]) level = 'info'") {
		t.Error("未知等级没有兜底归组，日志会丢失")
	}
}

// 「未成功」是唯一需要行动的分组，必须有条件地默认展开；过长时要限高。
func TestGrowthResultErrorSectionDefaultsOpenAndScrolls(t *testing.T) {
	script := mainPageScript()
	if !strings.Contains(script, `groups.error.length > 0`) {
		t.Error("「未成功」未按条件默认展开")
	}
	if !strings.Contains(script, `kind === 'err' && entries.length > 6`) {
		t.Error("过长的失败分组未限高")
	}
}
