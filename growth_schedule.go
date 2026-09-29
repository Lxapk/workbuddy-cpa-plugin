package main

// Scheduled growth-task runs.
//
// Modelled on the daily check-in scheduler next door: a one-minute tick, at-most-once
// per calendar day, and a catch-up pass at startup when configured. Reusing that shape
// means the two behave the same way in the panel and in the logs, which matters more
// than any cleverness a separate design would buy.

import (
	"sync"
	"time"
)

// growthSettings controls the scheduled task run.
type growthSettings struct {
	// Enabled turns the automatic daily run on or off.
	Enabled bool `json:"enabled" yaml:"enabled"`
	// Hour/Minute is the local time of day for the automatic run.
	Hour   int `json:"hour" yaml:"hour"`
	Minute int `json:"minute" yaml:"minute"`
	// OnStart also runs a catch-up pass when the plugin loads and today's run has not
	// happened yet.
	OnStart bool `json:"on_start" yaml:"on_start"`
}

// defaultGrowthSettings is 09:00 daily, disabled: an operator should opt in to
// something that spends upstream quota on a timer.
func defaultGrowthSettings() growthSettings {
	// OnStart off for the same reason as the check-in: a fresh install should not make a
	// network pass before the operator has enabled anything.
	return growthSettings{Enabled: false, Hour: 9, Minute: 0, OnStart: false}
}

// normalizeGrowthSettings clamps the time fields into range.
func normalizeGrowthSettings(cfg growthSettings) growthSettings {
	if cfg.Hour < 0 || cfg.Hour > 23 {
		cfg.Hour = 9
	}
	if cfg.Minute < 0 || cfg.Minute > 59 {
		cfg.Minute = 0
	}
	return cfg
}

// growthScheduleState tracks whether the loop is running and when it last fired.
type growthScheduleState struct {
	mu           sync.Mutex
	started      bool
	stopCh       chan struct{}
	lastAutoDate string
	running      bool
	lastRunAt    time.Time
	lastSummary  string
}

var growthSchedule growthScheduleState

// startGrowthScheduler launches the background loop exactly once.
func startGrowthScheduler() {
	growthSchedule.mu.Lock()
	if growthSchedule.started {
		growthSchedule.mu.Unlock()
		return
	}
	growthSchedule.started = true
	if growthSchedule.stopCh == nil {
		growthSchedule.stopCh = make(chan struct{})
	}
	growthSchedule.mu.Unlock()

	safeGo("growth-loop", growthLoop)
}

// growthLoop wakes up periodically and runs the task pass when due.
func growthLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	settings := state.settings.get().Growth
	if settings.Enabled && settings.OnStart && !growthRanToday() {
		guardLoop("growth-startup", func() {
			runScheduledGrowth("startup")
		})
	}

	for {
		select {
		case <-growthSchedule.stopCh:
			return
		case <-ticker.C:
			cfg := state.settings.get().Growth
			if !cfg.Enabled || growthRanToday() || !growthDueNow(cfg) {
				continue
			}
			guardLoop("growth-tick", func() {
				runScheduledGrowth("auto")
			})
		}
	}
}

// growthDueNow reports whether the configured time has passed today.
func growthDueNow(cfg growthSettings) bool {
	now := time.Now()
	due := time.Date(now.Year(), now.Month(), now.Day(), cfg.Hour, cfg.Minute, 0, 0, now.Location())
	return !now.Before(due)
}

// growthRanToday reports whether the automatic run already happened today.
func growthRanToday() bool {
	growthSchedule.mu.Lock()
	defer growthSchedule.mu.Unlock()
	return growthSchedule.lastAutoDate == time.Now().Format("2006-01-02")
}

// markGrowthRan records that today's automatic run has happened.
func markGrowthRan() {
	growthSchedule.mu.Lock()
	growthSchedule.lastAutoDate = time.Now().Format("2006-01-02")
	growthSchedule.mu.Unlock()
}

// runScheduledGrowth performs one automatic pass and records a one-line summary.
//
// Guarded by a running flag rather than by stopping the ticker: a pass takes minutes,
// and the tick fires every minute. Without the flag a slow run would overlap itself
// and hammer the upstream with duplicate task calls.
func runScheduledGrowth(trigger string) {
	growthSchedule.mu.Lock()
	if growthSchedule.running {
		growthSchedule.mu.Unlock()
		return
	}
	growthSchedule.running = true
	growthSchedule.mu.Unlock()

	defer func() {
		growthSchedule.mu.Lock()
		growthSchedule.running = false
		growthSchedule.lastRunAt = time.Now()
		growthSchedule.mu.Unlock()
	}()

	results := runGrowthForAll()
	if trigger != "manual" {
		markGrowthRan()
	}

	earned, accounts, failed := 0, 0, 0
	for _, r := range results {
		earned += r.Earned
		accounts++
		if !r.OK {
			failed++
		}
	}

	summary := "定时任务完成：" + itoa(accounts) + " 个账号，+" + itoa(earned) + " 积分"
	if failed > 0 {
		summary += "，" + itoa(failed) + " 个失败"
	}

	growthSchedule.mu.Lock()
	growthSchedule.lastSummary = summary
	growthSchedule.mu.Unlock()

	// A notice: the run is a task pass, not a model call, and counting it would make
	// the usage figures include work that consumed no tokens.
	state.log.addNotice(callRecord{
		ProviderID: workBuddyProviderKey,
		Model:      "growth",
		Error:      summary + "（" + trigger + "）",
	})
}

// growthScheduleSnapshot describes the scheduler for the panel.
func growthScheduleSnapshot() map[string]any {
	cfg := state.settings.get().Growth

	growthSchedule.mu.Lock()
	defer growthSchedule.mu.Unlock()

	out := map[string]any{
		"enabled":      cfg.Enabled,
		"hour":         cfg.Hour,
		"minute":       cfg.Minute,
		"on_start":     cfg.OnStart,
		"running":      growthSchedule.running,
		"ran_today":    growthSchedule.lastAutoDate == time.Now().Format("2006-01-02"),
		"next_run_at":  "",
		"last_run_at":  "",
		"last_summary": growthSchedule.lastSummary,
	}
	if cfg.Enabled {
		now := time.Now()
		next := time.Date(now.Year(), now.Month(), now.Day(), cfg.Hour, cfg.Minute, 0, 0, now.Location())
		if !next.After(now) || growthSchedule.lastAutoDate == now.Format("2006-01-02") {
			next = next.Add(24 * time.Hour)
		}
		out["next_run_at"] = next.Format(time.RFC3339)
	}
	if !growthSchedule.lastRunAt.IsZero() {
		out["last_run_at"] = growthSchedule.lastRunAt.Format(time.RFC3339)
	}
	return out
}
