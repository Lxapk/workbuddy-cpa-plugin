package main

import (
	"sync"
	"time"
)

// callRecord ports V1.f2.C0541a, the per-request log entry written by V1/o.r().
//
// Original constructor arguments (in order):
//
//	providerID, startedAt, uid, model, label, requestedModel, stream,
//	kind(f2.b), statusCode, promptTokens, completionTokens, totalTokens,
//	latencyMillis, errorText, responsePreview(<=8192), requestPreview(<=8192)
type callRecord struct {
	ProviderID string `json:"provider_id"`
	// Variant is the supplier realm that served the call ("cn" / "ai").
	// ProviderID alone cannot distinguish them: it is the constant "codebuddy"
	// for both realms.
	Variant          string `json:"variant,omitempty"`
	UID              string `json:"uid"`
	Label            string `json:"label"`
	Model            string `json:"model"`
	RequestedModel   string `json:"requested_model"`
	Stream           bool   `json:"stream"`
	StatusCode       int    `json:"status_code"`
	PromptTokens     int64  `json:"prompt_tokens"`
	CompletionTokens int64  `json:"completion_tokens"`
	TotalTokens      int64  `json:"total_tokens"`
	LatencyMillis    int64  `json:"latency_millis"`
	Error            string `json:"error,omitempty"`
	// Notice marks a record that is informational rather than the outcome of a call.
	//
	// Diagnostic lines ("the host offered 3 credentials") were being written as call
	// records with an Error string, and the counters treat any Error as a failure — so
	// a note about scheduling showed up in the failure column and in the usage trend.
	// Records carrying this flag are stored and displayed but never counted.
	Notice    bool      `json:"notice,omitempty"`
	StartedAt time.Time `json:"started_at"`
}

// callLog ports V1.f2.C1121t: a bounded, newest-first ring of call records
// (the APK keeps the newest 100 per provider shard and prunes older entries).
type callLog struct {
	mu   sync.Mutex
	max  int
	recs []callRecord

	totalCalls  int64
	totalFailed int64
	totalPrompt int64
	totalCompl  int64
	todayCalls  int64
	todayDate   string
	// daily keeps one bucket per calendar day so the panel can show a trend.
	//
	// The running totals answer "how much in total" but not "is it getting
	// worse", which is the question an operator actually has when a provider
	// starts throttling. Only the last few days are kept: enough to draw a week
	// of bars, bounded so the history cannot grow without limit.
	daily []dailyUsage
}

// dailyUsage is one day of accounting.
type dailyUsage struct {
	Date       string `json:"date"`
	Calls      int64  `json:"calls"`
	Failed     int64  `json:"failed"`
	Prompt     int64  `json:"prompt_tokens"`
	Completion int64  `json:"completion_tokens"`
}

// dailyUsageKept is how many days the trend covers. A week fits the panel without
// horizontal scrolling on a phone.
const dailyUsageKept = 7

// rollDaily folds one call into the day bucket, creating it when the date changes.
//
// Called with the lock held. Days with no traffic are not synthesised: the panel
// shows gaps as gaps, and inventing zeros would make a quiet weekend look like a
// provider outage.
func (l *callLog) rollDaily(rec callRecord) {
	day := rec.StartedAt.Format("2006-01-02")
	if day == "" || rec.StartedAt.IsZero() {
		day = time.Now().Format("2006-01-02")
	}

	last := -1
	if len(l.daily) > 0 {
		last = len(l.daily) - 1
	}
	if last < 0 || l.daily[last].Date != day {
		l.daily = append(l.daily, dailyUsage{Date: day})
		last = len(l.daily) - 1
		if len(l.daily) > dailyUsageKept {
			l.daily = l.daily[len(l.daily)-dailyUsageKept:]
			last = len(l.daily) - 1
		}
	}

	bucket := &l.daily[last]
	bucket.Calls++
	if rec.Error != "" || rec.StatusCode >= 400 {
		bucket.Failed++
	}
	bucket.Prompt += rec.PromptTokens
	bucket.Completion += rec.CompletionTokens
}

// addNotice stores an informational record without touching any counter.
func (l *callLog) addNotice(rec callRecord) {
	rec.Notice = true
	rec.StartedAt = time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.recs = append([]callRecord{rec}, l.recs...)
	if len(l.recs) > l.max {
		l.recs = l.recs[:l.max]
	}
}

// dailyUsage returns a copy of the per-day trend, oldest first.
func (l *callLog) dailyUsage() []dailyUsage {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]dailyUsage, len(l.daily))
	copy(out, l.daily)
	return out
}

func newCallLog(max int) *callLog {
	if max < 1 {
		max = 100
	}
	return &callLog{max: max}
}

// add ports V1.o.r()'s accounting block: append newest-first, prune the tail,
// and roll the daily counter when the day changes.
func (l *callLog) add(rec callRecord) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// A notice is not a call: it is stored so the panel can show it, but it must not
	// move any counter. Writing it as a zero-value record would otherwise inflate the
	// call count, and giving it an Error string would put it in the failure column —
	// which is how a note about the scheduler ended up in the failure trend.
	notice := rec.Notice

	l.recs = append([]callRecord{rec}, l.recs...)
	if len(l.recs) > l.max {
		l.recs = l.recs[:l.max]
	}
	if notice {
		return
	}

	l.totalCalls++
	l.totalPrompt += rec.PromptTokens
	l.totalCompl += rec.CompletionTokens
	if rec.Error != "" || rec.StatusCode >= 400 {
		l.totalFailed++
	}

	day := rec.StartedAt.Format("2006-01-02")
	if day == "" {
		day = time.Now().Format("2006-01-02")
	}
	if l.todayDate != day {
		l.todayDate = day
		l.todayCalls = 0
	}
	l.todayCalls++

	l.rollDaily(rec)
}

func (l *callLog) recent(limit int) []callRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	if limit <= 0 || limit > len(l.recs) {
		limit = len(l.recs)
	}
	out := make([]callRecord, limit)
	copy(out, l.recs[:limit])
	return out
}

type usageTotals struct {
	TotalCalls      int64 `json:"total_calls"`
	TotalFailed     int64 `json:"total_failed"`
	TodayCalls      int64 `json:"today_calls"`
	TotalPrompt     int64 `json:"total_prompt_tokens"`
	TotalCompletion int64 `json:"total_completion_tokens"`
}

func (l *callLog) totals() usageTotals {
	l.mu.Lock()
	defer l.mu.Unlock()
	return usageTotals{
		TotalCalls:      l.totalCalls,
		TotalFailed:     l.totalFailed,
		TodayCalls:      l.todayCalls,
		TotalPrompt:     l.totalPrompt,
		TotalCompletion: l.totalCompl,
	}
}

// usagePayload is the subset of an OpenAI-compatible usage object the gateway
// reads in V1/o.r() / V1/o.p().
type usagePayload struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
	// Anthropic-style aliases, since CPA can serve /v1/messages too.
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

func (u usagePayload) normalized() (prompt, completion, total int64) {
	prompt = u.PromptTokens
	if prompt == 0 {
		prompt = u.InputTokens
	}
	completion = u.CompletionTokens
	if completion == 0 {
		completion = u.OutputTokens
	}
	total = u.TotalTokens
	if total == 0 {
		total = prompt + completion
	}
	return prompt, completion, total
}

// globalState is the plugin-wide singleton set, populated on register.
type globalState struct {
	settings   *settingsStore
	pool       *credentialPool
	log        *callLog
	checkin    *checkinState
	quota      *quotaState
	accounts   *accountStore
	scheduler  *schedulerState
	taskEngine *taskEngine
	growth     *growthStore
}

var state = &globalState{
	settings:   newSettingsStore(),
	pool:       newCredentialPool(),
	log:        newCallLog(100),
	checkin:    newCheckinState(),
	quota:      newQuotaState(),
	accounts:   newAccountStore(),
	scheduler:  newSchedulerState(),
	taskEngine: newTaskEngine(),
	growth:     newGrowthStore(),
}

func shutdownPlugin() {
	stopTaskScheduler()
	stopCheckinScheduler()
	stopQuotaScheduler()
}
