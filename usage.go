package main

import (
	"strings"
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
	mu  sync.Mutex
	max int
	// recs holds call records only. Notices live in notices: keeping them apart means
	// the call list is a direct read rather than a scan-and-filter, and a notice can
	// never displace a call from the ring before anyone has seen it.
	recs []callRecord
	// notices holds informational records — scheduler notes, task summaries. They are
	// shown where they belong rather than mixed in with traffic.
	notices []callRecord

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
	daily []usageBucket
	// hourly is the same accounting per hour, so the panel can show a rolling hour.
	hourly []usageBucket
}

// usageBucket is one period of accounting. The same shape serves the hourly and the
// daily trend: the panel picks which series to show, and the arithmetic is identical.
type usageBucket struct {
	// Date is the bucket key: "2006-01-02" for days, "2006-01-02 15" for hours.
	Date       string `json:"date"`
	Calls      int64  `json:"calls"`
	Failed     int64  `json:"failed"`
	Prompt     int64  `json:"prompt_tokens"`
	Completion int64  `json:"completion_tokens"`
}

// dailyUsageKept is how many days the daily trend covers. A week fits the panel without
// horizontal scrolling on a phone.
const dailyUsageKept = 7

// hourlyUsageKept is how many hours the hourly trend covers — a full day, so the
// "last hour" and "today" views can both be cut from it.
const hourlyUsageKept = 24

// rollDaily folds one call into the day bucket, creating it when the date changes.
//
// Called with the lock held. Days with no traffic are not synthesised: the panel
// shows gaps as gaps, and inventing zeros would make a quiet weekend look like a
// provider outage.
// rollDaily folds one call into both trends.
//
// Two series are kept from the same record rather than one: the rolling hour needs
// minute-level recency, the week needs a coarse summary, and deriving either from the
// other would mean either losing detail or keeping far more than a week of it. Both are
// cheap — a few dozen small structs.
//
// Called with the lock held. Periods with no traffic are not synthesised: the panel
// fills the gaps so a bar chart can show an empty slot as empty rather than absent.
func (l *callLog) rollDaily(rec callRecord) {
	// Both the record's own stamp and the fallback are put into the panel's zone before
	// the keys are built. A record written before the zone was pinned still carries an
	// offset, and formatting it as-is would file it under the wrong hour.
	stamp := rec.StartedAt.In(panelLocation)
	if rec.StartedAt.IsZero() {
		stamp = nowPanel()
	}

	l.hourly = foldBucket(l.hourly, stamp.Format("2006-01-02 15"), hourlyUsageKept, rec)
	l.daily = foldBucket(l.daily, stamp.Format("2006-01-02"), dailyUsageKept, rec)
}

// foldBucket adds one record to the bucket for key, appending a new bucket when the key
// changes and trimming the series to keep.
func foldBucket(series []usageBucket, key string, keep int, rec callRecord) []usageBucket {
	last := len(series) - 1
	if last < 0 || series[last].Date != key {
		series = append(series, usageBucket{Date: key})
		last = len(series) - 1
		if len(series) > keep {
			series = series[len(series)-keep:]
			last = len(series) - 1
		}
	}

	bucket := &series[last]
	bucket.Calls++
	// A notice is informational; it must not move the counters. add() already refuses
	// to count one, so this only matters if a caller writes a bucket directly.
	if recordFailed(rec) {
		bucket.Failed++
	}
	bucket.Prompt += rec.PromptTokens
	bucket.Completion += rec.CompletionTokens
	return series
}

// addNotice stores an informational record without touching any counter.
func (l *callLog) addNotice(rec callRecord) {
	rec.Notice = true
	rec.StartedAt = time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	// Notices have their own ring. Writing them into recs would put a non-call in the
	// list the panel reads as traffic, and would let a note push a real call out of the
	// history.
	l.notices = append([]callRecord{rec}, l.notices...)
	if len(l.notices) > l.max {
		l.notices = l.notices[:l.max]
	}
}

// dailyUsage returns a copy of the per-day trend, oldest first.
func (l *callLog) dailyUsage() []usageBucket {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]usageBucket, len(l.daily))
	copy(out, l.daily)
	return out
}

// hourlyUsage returns a copy of the per-hour trend, oldest first.
func (l *callLog) hourlyUsage() []usageBucket {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]usageBucket, len(l.hourly))
	copy(out, l.hourly)
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

	// A notice goes to its own ring and touches nothing else. Keeping it out of recs
	// means the call list holds calls and only calls, so reading it is a copy rather
	// than a scan, and a note cannot push a call out of the ring.
	if rec.Notice {
		l.notices = append([]callRecord{rec}, l.notices...)
		if len(l.notices) > l.max {
			l.notices = l.notices[:l.max]
		}
		return
	}

	// A record without a model is not a call. Nothing should reach here that way — the
	// call list is read as traffic, and a nameless row in it would be counted in the
	// totals while telling the reader nothing. Dropping it keeps the ring honest.
	if strings.TrimSpace(rec.Model) == "" {
		return
	}

	l.recs = append([]callRecord{rec}, l.recs...)
	if len(l.recs) > l.max {
		l.recs = l.recs[:l.max]
	}

	l.totalCalls++
	l.totalPrompt += rec.PromptTokens
	l.totalCompl += rec.CompletionTokens
	if recordFailed(rec) {
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

// modelCallsOnly returns the most recent calls, newest first.
//
// Calls have their own ring, so this is a bounded copy rather than a scan of every
// record followed by a filter. The list is fetched on a timer; walking the whole history
// each time to discard notices was work with nothing to show for it.
func (l *callLog) modelCallsOnly(limit int) []callRecord {
	if limit < 1 {
		limit = 1
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	if limit > len(l.recs) {
		limit = len(l.recs)
	}
	out := make([]callRecord, limit)
	copy(out, l.recs[:limit])
	return out
}

// recordFailed reports whether a record represents a failure worth counting.
//
// One definition for the three places that tally calls — the totals, the daily buckets
// and the per-account column — so they cannot disagree.
//
// A caller hanging up is excluded. It is a real bad outcome for the person who pressed
// stop, but it is not evidence about the credential: the account was answering, the same
// one serves the next request, and a tally that counts it shows failures the account
// never had.
func recordFailed(rec callRecord) bool {
	if rec.Notice {
		return false
	}
	if isClientAbortFailure(rec.StatusCode, rec.Error) {
		return false
	}
	return rec.Error != "" || rec.StatusCode >= 400
}

// isWorkBuddyRecord reports whether a record came from this plugin's provider.
//
// The provider key is the constant the plugin registers with; a record carrying anything
// else was produced by a different part of CPA and is not this panel's business.
func isWorkBuddyRecord(provider string) bool {
	key := strings.ToLower(strings.TrimSpace(provider))
	// Empty means the record predates the field or came from a path that does not fill
	// it; those are this plugin's own records, so they stay.
	if key == "" {
		return true
	}
	return key == workBuddyProviderKey || strings.Contains(key, workBuddyProviderKey)
}

// logListLimit caps both lists on the records page.
//
// A hundred is what the operator can scroll through, and it keeps the JSON the page
// fetches small enough to be sent on a timer without noticing.
const logListLimit = 100

// noticeLog returns the most recent notices, newest first.
//
// Notices are the plugin's own operational events — sign-in results, task runs, an
// account being parked or disabled. They live in their own ring and are read directly,
// with no filtering.
func (l *callLog) noticeLog(limit int) []callRecord {
	if limit < 1 {
		limit = 1
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	if limit > len(l.notices) {
		limit = len(l.notices)
	}
	out := make([]callRecord, limit)
	copy(out, l.notices[:limit])
	return out
}

// clearNotices drops the notice ring and reports how many went.
//
// Only notices. The call list is the accounting the totals and the trend are computed
// from; discarding it would leave those figures describing records that are no longer
// there.
func (l *callLog) clearNotices() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := len(l.notices)
	l.notices = nil
	return n
}
