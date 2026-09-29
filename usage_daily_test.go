package main

import (
	"net/http"
	"testing"
	"time"
)

// 按天聚合必须只累计当天，并在跨天时开新桶。
//
// 面板的趋势图靠这份数据。若实现把不同日期折进同一个桶，图上是平的；若跨天时
// 忘了开新桶，昨天的量会被算进今天。两者都不会报错，只会让运营看到错误的走势。
func TestDailyUsageBucketsByCalendarDay(t *testing.T) {
	log := newCallLog(100)
	// 桶按面板展示的时区（北京时间）归类，所以测试也用那个时区构造时刻。若用 UTC 构造，
	// 23:00 UTC 已经是次日 07:00，三条会落进同一天——那是正确行为，只是测不到跨天。
	base := time.Date(2026, 3, 10, 23, 0, 0, 0, panelLocation)

	log.add(callRecord{ProviderID: "p", Model: "m", StartedAt: base, PromptTokens: 10, CompletionTokens: 1})
	log.add(callRecord{ProviderID: "p", Model: "m", StartedAt: base.Add(30 * time.Minute), PromptTokens: 20, CompletionTokens: 2})
	// 跨天：这条必须落到新的桶里。
	log.add(callRecord{ProviderID: "p", Model: "m", StartedAt: base.Add(2 * time.Hour), PromptTokens: 30, CompletionTokens: 3})

	daily := log.dailyUsage()
	if len(daily) != 2 {
		t.Fatalf("应有两个日桶，got %d: %+v", len(daily), daily)
	}

	if daily[0].Date != "2026-03-10" || daily[1].Date != "2026-03-11" {
		t.Fatalf("日期不对: %+v", daily)
	}
	// 桶按时间升序，趋势图直接按顺序画。
	if daily[0].Calls != 2 || daily[0].Prompt != 30 {
		t.Errorf("3-10 桶 = %+v, want 2 calls / 30 prompt", daily[0])
	}
	if daily[1].Calls != 1 || daily[1].Prompt != 30 {
		t.Errorf("3-11 桶 = %+v, want 1 call / 30 prompt", daily[1])
	}
}

// 失败的调用要单独计数：重试放大靠这一列才看得出来。
func TestDailyUsageCountsFailures(t *testing.T) {
	log := newCallLog(100)
	now := time.Now()

	log.add(callRecord{ProviderID: "p", Model: "m", StartedAt: now, StatusCode: http.StatusOK})
	log.add(callRecord{ProviderID: "p", Model: "m", StartedAt: now, StatusCode: http.StatusTooManyRequests})
	log.add(callRecord{ProviderID: "p", Model: "m", StartedAt: now, Error: "boom"})

	daily := log.dailyUsage()
	if len(daily) != 1 {
		t.Fatalf("应有一个日桶，got %d", len(daily))
	}
	if daily[0].Calls != 3 {
		t.Errorf("calls = %d, want 3", daily[0].Calls)
	}
	if daily[0].Failed != 2 {
		t.Errorf("failed = %d, want 2（4xx 与 Error 各算一次）", daily[0].Failed)
	}
}

// 桶数有上限，且保留的是最近的若干天。
func TestDailyUsageIsBounded(t *testing.T) {
	log := newCallLog(100)
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	for day := 0; day < dailyUsageKept+5; day++ {
		log.add(callRecord{ProviderID: "p", Model: "m", StartedAt: start.AddDate(0, 0, day)})
	}

	daily := log.dailyUsage()
	if len(daily) != dailyUsageKept {
		t.Fatalf("桶数应为 %d，got %d", dailyUsageKept, len(daily))
	}
	// 保留的是最后几天，最早的应已被丢弃。
	if daily[0].Date == "2026-01-01" {
		t.Errorf("最早的桶应被丢弃，got %+v", daily[0])
	}
	want := start.AddDate(0, 0, 5).Format("2006-01-02")
	if daily[0].Date != want {
		t.Errorf("首个桶 = %s, want %s", daily[0].Date, want)
	}
}

// 没有流量的一天不应被凭空插入 0 值——安静的周末和上游故障必须能区分开。
func TestDailyUsageDoesNotInventEmptyDays(t *testing.T) {
	log := newCallLog(100)
	start := time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)

	log.add(callRecord{ProviderID: "p", Model: "m", StartedAt: start})
	log.add(callRecord{ProviderID: "p", Model: "m", StartedAt: start.AddDate(0, 0, 3)})

	daily := log.dailyUsage()
	if len(daily) != 2 {
		t.Fatalf("应只有两个有流量的桶，got %d: %+v", len(daily), daily)
	}
}

// dailyUsage 返回副本，调用方改动不得影响内部状态。
func TestDailyUsageReturnsCopy(t *testing.T) {
	log := newCallLog(100)
	log.add(callRecord{ProviderID: "p", Model: "m", StartedAt: time.Now()})

	first := log.dailyUsage()
	first[0].Calls = 999

	second := log.dailyUsage()
	if second[0].Calls == 999 {
		t.Error("dailyUsage 返回了内部切片，调用方改动泄漏进状态")
	}
}
