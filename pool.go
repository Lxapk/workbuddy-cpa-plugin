package main

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// coolKind ports W1.b (A0/s.java:969 parses it from the persisted "coolKind"):
//
//	QUOTA(0)  credits exhausted
//	SOFT(1)   transient failure, short cooldown
//	ERROR(2)  hard error
//
// RATE is this plugin's addition. The app folds a 429 into QUOTA, but the two
// have opposite meanings for the remaining balance, so the pool tracks them
// separately instead of zeroing Credits on every throttle.
type coolKind int

const (
	coolKindNone  coolKind = -1
	coolKindQuota coolKind = iota
	coolKindSoft
	coolKindError
	coolKindRate
)

// credentialLane mirrors one entry of the app's account pool.
//
// In the APK each credential lives in A0.s (a LinkedHashMap keyed by
// providerId + "/" + uid) and carries the fields of W1.d:
//
//	f3827b credits       -> Credits          (selection weight, A0/s.java:596)
//	f3828c creditsKnown  -> CreditsKnown
//	f3829d detail        -> Detail
//	f3830e disabled      -> Disabled
//	f          enabled   -> Enabled
//	f3831g reason        -> StatusMessage
//	f3832h untilMillis   -> CooldownUntil
//	f3833i errorCount    -> ConsecutiveErrors
//	f3834j coolKind      -> CoolKind
//
// V1.k.c() mutates it on failure:
//
//	case 0 (auth/invalid):  mark; cooldown = quotaCooldownMillis  (hard)
//	case 1,3 (rate/quota):  mark; cooldown = quotaCooldownMillis
//	case 2 (disabled):      dVar.e = true; dVar.g = reason        (permanent)
//	case 4,5,6:             A0.s.p(errThreshold, errCooldown, ...)  (soft)
type credentialLane struct {
	// Provider is the provider id owning this credential.
	Provider string `json:"provider"`
	// UID is the stable credential identifier (runtime auth index in CPA).
	UID string `json:"uid"`
	// Label mirrors V1.n.c (user-facing account label).
	Label string `json:"label"`
	// CreditsTotal is the cycle capacity the balance is drawn from, so the panel can
	// show a ratio instead of a bare remainder.
	CreditsTotal int64 `json:"credits_total"`
	// Disabled mirrors V1.n.e.
	Disabled bool `json:"disabled"`
	// DisabledByUser tracks manual toggles from the panel.
	DisabledByUser bool `json:"disabled_by_user"`
	// AutoDisabled reports that the pool retired this account itself, after a
	// failure that cannot be recovered by retrying (dead credential, revoked
	// token). It is what the面板 shows as 原因「自动禁用」 and what lets the
	// operator re-enable the account knowingly.
	AutoDisabled bool `json:"auto_disabled"`
	// DisabledReason explains why the account was retired.
	DisabledReason string `json:"disabled_reason,omitempty"`
	// DisabledAt records when that happened.
	DisabledAt time.Time `json:"disabled_at,omitempty"`
	// hostDisabledSeen records that the last host read found the credential disabled, so a
	// later "enabled" can be told apart from a file that was never disabled at all.
	hostDisabledSeen bool
	// Variant is the credential's realm, shown in the grouped account list.
	Variant string `json:"variant,omitempty"`
	// Enabled mirrors W1.d.f (defaults to true in the app).
	Enabled bool `json:"enabled"`
	// StatusMessage mirrors V1.n.g / W1.d.f3831g.
	StatusMessage string `json:"status_message"`
	// Detail mirrors W1.d.f3829d.
	Detail string `json:"detail"`

	// Credits is the remaining quota, used as the selection weight
	// (W1.d.f3827b / A0/s.java:596 picks the largest).
	Credits int64 `json:"credits"`
	// CreditsKnown mirrors W1.d.f3828c.
	CreditsKnown bool `json:"credits_known"`

	// ConsecutiveErrors counts successive failures since the last success.
	ConsecutiveErrors int `json:"consecutive_errors"`
	// CooldownUntil is the wall-clock instant this lane may be retried.
	CooldownUntil time.Time `json:"cooldown_until"`
	// CoolKind mirrors W1.d.f3834j.
	CoolKind coolKind `json:"cool_kind"`
	// ModelCooldowns parks individual models without benching the account.
	//
	// The upstream throttles per model and says so in its own wording
	// ("您也可以切换其他模型继续使用"), but a lane-level cooldown takes the whole
	// account out of rotation, so one throttled model would knock out the models
	// that still work. Keyed by model id, valued with the expiry instant.
	ModelCooldowns map[string]time.Time `json:"model_cooldowns,omitempty"`
	// ModelCoolKinds records why each model was parked, for the panel.
	ModelCoolKinds map[string]coolKind `json:"model_cool_kinds,omitempty"`
	// ModelCoolReasons keeps the upstream wording, for the panel.
	ModelCoolReasons map[string]string `json:"model_cool_reasons,omitempty"`
	// LastError is the most recent failure reason.
	LastError string `json:"last_error"`
	// LastUsed is the most recent successful use.
	LastUsed time.Time `json:"last_used"`
	// Successes / Failures are lifetime counters.
	Successes int64 `json:"successes"`
	Failures  int64 `json:"failures"`
}

// anotherLaneCanServe reports whether any credential other than the parked ones is
// still eligible for a model.
//
// A lane parked for this model is not a candidate; that is the whole point. When
// every eligible lane is parked for it, the only honest advice is to wait for the
// reset or use another model — telling the caller to retry, or that the account is
// gone, both point at the wrong remedy.
func (p *credentialPool) anotherLaneCanServe(model string) bool {
	model = strings.TrimSpace(model)
	if model == "" {
		return true
	}
	now := time.Now()
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, lane := range p.lanes {
		if lane == nil || !lane.isUsable() {
			continue
		}
		if _, cooled := lane.modelCooled(model, now); !cooled {
			return true
		}
	}
	return false
}

// isUsable reports whether a lane may currently serve traffic.
func (l *credentialLane) isUsable() bool {
	if l == nil {
		return false
	}
	if l.Disabled || l.DisabledByUser || l.AutoDisabled {
		return false
	}
	// Lanes created by older code paths may leave Enabled unset; treat "not
	// explicitly disabled" as usable rather than silently dropping the lane.
	return l.Enabled || !l.Disabled
}

// poolOrphanLock documents the lock the pool's accessors take.

// disableAccount marks a credential for manual enable/disable from the panel.
//
// This is orthogonal to the host's own disabled flag — it persists across
// restarts in the pool state and is checked by usable().
func (p *credentialPool) disableAccount(uid string, disabled bool) {
	p.disableAccountKeyed(uid, "", disabled)
}

// disableAccountKeyed marks a credential for manual enable/disable from the
// panel, matching lanes by uid AND the CPA auth index.
//
// The two identifiers differ by code path: the panel keys accounts by the
// provider-side uid (creds.UID), while the pool lane seen by interception is
// keyed by the CPA auth index (metadata auth_id / AuthIndex). Matching only
// one of them silently created an orphan disabled lane while the live lane
// stayed enabled — the "账号禁用不了" report. Both are applied so the toggle
// always lands on the lane that actually serves traffic.
// disableAccountKeyed applies the panel's enable/disable toggle.
//
// Enabling must also clear the pool's own retirement (Disabled /
// AutoDisabled / cooldown): otherwise a manually re-enabled account stayed
// unusable because the internal bit was still set, and the toggle looked
// broken.
func (p *credentialPool) disableAccountKeyed(uid, authIndex string, disabled bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	matched := false
	for _, lane := range p.lanes {
		if (uid != "" && lane.UID == uid) || (authIndex != "" && lane.UID == authIndex) {
			p.applyToggleLocked(lane, disabled)
			matched = true
		}
	}
	if matched {
		return
	}
	key := firstNonEmpty(uid, authIndex)
	if key == "" {
		return
	}
	lane := p.laneLocked(workBuddyProviderKey, key)
	p.applyToggleLocked(lane, disabled)
}

// applyToggleLocked writes one enable/disable decision. Caller holds p.mu.
func (p *credentialPool) applyToggleLocked(lane *credentialLane, disabled bool) {
	lane.DisabledByUser = disabled
	if disabled {
		// Record it the same way an automatic retirement is recorded: the operator's
		// manual action belongs in the request log beside the plugin's own decisions,
		// otherwise pressing 禁用 leaves no trace and the log looks like it missed the
		// event.
		p.autoDisables = append(p.autoDisables, autoDisableEvent{
			UID:     lane.UID,
			Label:   lane.Label,
			Reason:  "操作者手动操作",
			At:      nowPanel(),
			Manual:  true,
			Variant: lane.Variant,
		})
		if len(p.autoDisables) > autoDisableHistoryMax {
			p.autoDisables = p.autoDisables[len(p.autoDisables)-autoDisableHistoryMax:]
		}
		return
	}
	// Re-enabling lifts every internal retirement this lane accumulated.
	lane.Disabled = false
	lane.AutoDisabled = false
	lane.DisabledReason = ""
	lane.DisabledAt = time.Time{}
	lane.ConsecutiveErrors = 0
	lane.CooldownUntil = time.Time{}
	lane.CoolKind = coolKindNone
	lane.StatusMessage = ""
	p.autoDisables = append(p.autoDisables, autoDisableEvent{
		UID:       lane.UID,
		Label:     lane.Label,
		Reason:    "操作者手动操作",
		At:        nowPanel(),
		Manual:    true,
		Recovered: true,
		Variant:   lane.Variant,
	})
	if len(p.autoDisables) > autoDisableHistoryMax {
		p.autoDisables = p.autoDisables[len(p.autoDisables)-autoDisableHistoryMax:]
	}
	p.markRecoveredLocked(lane.UID)
}

// autoDisableEvent records one automatic retirement, for the panel's history.
type autoDisableEvent struct {
	UID     string    `json:"uid"`
	Label   string    `json:"label"`
	Reason  string    `json:"reason"`
	Variant string    `json:"variant,omitempty"`
	At      time.Time `json:"at"`
	// Recovered marks the entry that lifts a retirement.
	Recovered bool `json:"recovered"`
	// Manual marks an action the operator took, as opposed to one the pool decided on
	// its own. Both belong in the request log, but the wording differs and so does what
	// the reader should do about it.
	Manual bool `json:"manual,omitempty"`
}

// autoDisableHistoryMax bounds the audit trail. It is a display list — enough to answer
// "why did this account stop" without growing without limit.
const autoDisableHistoryMax = 200

// recordAutoDisableLocked appends an audit entry. Caller holds p.mu.
//
// A wrong retirement is otherwise invisible: the account simply stops being
// used and nothing says why or when.
func (p *credentialPool) recordAutoDisableLocked(lane *credentialLane, reason string) {
	p.autoDisables = append(p.autoDisables, autoDisableEvent{
		UID:     lane.UID,
		Label:   lane.Label,
		Reason:  reason,
		Variant: lane.Variant,
		At:      nowPanel(),
	})
	if len(p.autoDisables) > autoDisableHistoryMax {
		p.autoDisables = p.autoDisables[len(p.autoDisables)-autoDisableHistoryMax:]
	}
}

// autoDisableHistory returns the recorded retirements, newest last.
func (p *credentialPool) autoDisableHistory() []autoDisableEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]autoDisableEvent, len(p.autoDisables))
	copy(out, p.autoDisables)
	return out
}

// markRecoveredLocked flags the latest retirement of a uid as undone.
// Caller holds p.mu.
func (p *credentialPool) markRecoveredLocked(uid string) {
	for i := len(p.autoDisables) - 1; i >= 0; i-- {
		if p.autoDisables[i].UID == uid && !p.autoDisables[i].Recovered {
			p.autoDisables[i].Recovered = true
			return
		}
	}
}

// findAccount returns a lane by uid, or nil.
func (p *credentialPool) findAccount(uid string) *credentialLane {
	return p.findAccountKeyed(uid, "")
}

// findAccountKeyedCopy returns a snapshot copy of the first lane matching uid
// or the CPA auth index. See disableAccountKeyed for why both keys are
// consulted.
//
// It returns a value rather than the pooled *credentialLane on purpose: lanes
// are mutated in place under p.mu by success/failure/observe, so handing the
// pointer to a caller that reads fields after the lock is dropped is a data
// race. Callers that only need to read state must use this copy.
func (p *credentialPool) findAccountKeyedCopy(uid, authIndex string) (credentialLane, bool) {
	if uid == "" && authIndex == "" {
		return credentialLane{}, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, lane := range p.lanes {
		if (uid != "" && lane.UID == uid) || (authIndex != "" && lane.UID == authIndex) {
			return *lane, true
		}
	}
	return credentialLane{}, false
}

// findAccountKeyed returns the first lane matching uid or the CPA auth index,
// or nil. See disableAccountKeyed for why both keys are consulted.
//
// Deprecated: the returned pointer aliases pool state and must not be read
// after p.mu is released. Prefer findAccountKeyedCopy.
func (p *credentialPool) findAccountKeyed(uid, authIndex string) *credentialLane {
	if uid == "" && authIndex == "" {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, lane := range p.lanes {
		if (uid != "" && lane.UID == uid) || (authIndex != "" && lane.UID == authIndex) {
			return lane
		}
	}
	return nil
}

// isAccountDisabled reports whether any pool lane matching uid/authIndex is
// manually disabled by the operator (or permanently parked by the host).
//
// The check runs entirely under p.mu via findAccountKeyedCopy so it never
// races with the writers that mutate lane fields in place.
func (p *credentialPool) isAccountDisabled(uid, authIndex string) bool {
	lane, ok := p.findAccountKeyedCopy(uid, authIndex)
	return ok && (lane.DisabledByUser || lane.Disabled)
}

//	!d.disabled && d.enabled && now >= d.untilMillis
//
// and also checks the operator's manual toggle.
func (c *credentialLane) usable(now time.Time) bool {
	if c.Disabled || c.DisabledByUser || !c.Enabled {
		return false
	}
	return !now.Before(c.CooldownUntil)
}

// credentialPool is the port of A0.s's health bookkeeping plus V1.k.c()'s
// cooldown policy. It is keyed by "provider/uid", exactly like A0.s.m().
type credentialPool struct {
	mu    sync.Mutex
	lanes map[string]*credentialLane
	order []string
	// autoDisables is the audit trail of accounts the pool retired itself.
	autoDisables []autoDisableEvent
}

func newCredentialPool() *credentialPool {
	return &credentialPool{lanes: make(map[string]*credentialLane)}
}

func laneKey(provider, uid string) string { return provider + "/" + uid }

// canonicalUID maps a credential identifier onto the uid the account table shows.
//
// The wire gives us CPA's auth id (the auth file name, e.g.
// "codebuddy-42213638-….json"); the account table keys on the credential's own uid
// (e.g. "42213638-…"). Treating them as different identifiers created two lanes for one
// account — the pool reported four credentials while the host offered two — and the
// call log named an account the table did not list.
//
// Falls back to the input when the store has no row for it, which is the case for a
// credential the host has offered but the plugin has not yet read.
func canonicalUID(identifier string) string {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return ""
	}
	for _, account := range listWorkBuddyAccounts() {
		if account.AuthIndex == identifier && account.UID != "" {
			return account.UID
		}
		if account.UID == identifier {
			return identifier
		}
	}
	return identifier
}

// observe registers (or refreshes) a credential seen on the wire.
func (p *credentialPool) observe(provider, uid, label string) *credentialLane {
	if provider == "" || uid == "" {
		return nil
	}
	// Normalise before keying: the wire's identifier and the table's identifier must
	// land on the same lane, or one account becomes two.
	uid = canonicalUID(uid)
	key := laneKey(provider, uid)
	p.mu.Lock()
	lane, ok := p.lanes[key]
	if !ok {
		// A new lane starts enabled with no known credits, matching W1.d's
		// field initialisers (f = true, f3827b = 0, f3828c = false).
		lane = &credentialLane{Provider: provider, UID: uid, Enabled: true, CoolKind: coolKindNone}
		p.lanes[key] = lane
		p.order = append(p.order, key)
	}
	if label != "" {
		lane.Label = label
	}
	needVariant := lane.Variant == ""
	p.mu.Unlock()

	// Resolve the realm without the lock held: the lookup walks the account
	// store, which reads the pool back through the account view, so calling it
	// under p.mu deadlocks.
	if needVariant {
		if variant := variantForUID(uid, label); variant != "" {
			p.mu.Lock()
			if lane.Variant == "" {
				lane.Variant = variant
			}
			p.mu.Unlock()
		}
	}
	return lane
}

// variantForUID resolves a realm from the account store, without touching the
// pool (so it is safe to call while holding the pool lock).
func variantForUID(uid, label string) string {
	for _, account := range listWorkBuddyAccounts() {
		if account.Variant == "" {
			continue
		}
		if (uid != "" && (account.UID == uid || account.AuthIndex == uid)) ||
			(label != "" && account.Label == label) {
			return account.Variant
		}
	}
	return ""
}

// setCredits records a quota reading, mirroring A0/s.java:947:
//
//	dVar.f3827b = credits
//	dVar.f3828c = creditsKnown
func (p *credentialPool) setCredits(provider, uid string, credits int64, known bool) {
	if provider == "" && uid == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	lane := p.laneLocked(provider, uid)
	lane.Credits = credits
	lane.CreditsKnown = known
}

// laneLocked finds a lane by provider+uid, creating it if needed.
func (p *credentialPool) laneLocked(provider, uid string) *credentialLane {
	key := laneKey(provider, uid)
	lane, ok := p.lanes[key]
	if !ok {
		lane = &credentialLane{Provider: provider, UID: uid, Enabled: true, CoolKind: coolKindNone}
		p.lanes[key] = lane
		p.order = append(p.order, key)
	}
	return lane
}

// setCreditsByAuthID records a reading when only the auth id is known.
// It matches on uid first, then on the auth id itself.
// authIndexFor returns the CPA auth identifier recorded for a credential.
//
// The pool keys lanes by credential uid (or by whatever identifier the host offered),
// so this walks the lanes and reports the key: a call record written before the executor
// stamped identifiers names the account by that key, and a tally that cannot recognise it
// shows zero.
func (p *credentialPool) authIndexFor(uid string) string {
	uid = strings.TrimSpace(uid)
	if uid == "" {
		return ""
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, key := range p.order {
		lane := p.lanes[key]
		if lane == nil {
			continue
		}
		if lane.UID == uid || key == uid || strings.HasSuffix(key, "/"+uid) {
			return key
		}
	}
	return ""
}

// creditsTotalFor returns the cycle capacity the pool recorded for a credential.
//
// The pool learns it whenever a refresh runs — including the background one — while the
// panel's own quota cache is only updated by its queries. Reading through the pool means
// the account row has a denominator even before the first manual refresh.
func (p *credentialPool) creditsTotalFor(authID, uid string) int64 {
	authID = strings.TrimSpace(authID)
	uid = strings.TrimSpace(uid)
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, key := range p.order {
		lane := p.lanes[key]
		if lane == nil {
			continue
		}
		if (uid != "" && lane.UID == uid) || lane.UID == authID || key == workBuddyProviderKey+"/"+authID {
			return lane.CreditsTotal
		}
	}
	return 0
}

func (p *credentialPool) setCreditsByAuthID(authID, uid string, credits, total int64, known bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, key := range p.order {
		lane := p.lanes[key]
		if lane == nil {
			continue
		}
		if (uid != "" && lane.UID == uid) || lane.UID == authID || key == workBuddyProviderKey+"/"+authID {
			lane.Credits = credits
			lane.CreditsTotal = total
			lane.CreditsKnown = known
			return
		}
	}
	// No existing lane: create one so the reading is not lost.
	lane := p.laneLocked(workBuddyProviderKey, firstNonEmpty(uid, authID))
	lane.Credits = credits
	lane.CreditsKnown = known
}

// success ports A0.s.q(providerId, uid): a good call resets the failure state.
func (p *credentialPool) success(provider, uid string) {
	if provider == "" || uid == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	lane := p.lanes[laneKey(provider, uid)]
	if lane == nil {
		// A success can be observed before any interception (e.g. CPA's usage
		// hook is the first place the credential surfaces). Register it, the
		// same way A0.s materialises an entry on first sight: W1.d initialises
		// enabled=true, so the lane must be usable immediately.
		lane = &credentialLane{Provider: provider, UID: uid, Enabled: true, CoolKind: coolKindNone}
		p.lanes[laneKey(provider, uid)] = lane
		p.order = append(p.order, laneKey(provider, uid))
	}
	// Guard against lanes created by older code paths without Enabled set.
	if !lane.Enabled && !lane.Disabled {
		lane.Enabled = true
	}
	lane.ConsecutiveErrors = 0
	lane.CooldownUntil = time.Time{}
	lane.LastError = ""
	lane.LastUsed = time.Now()
	lane.Successes++
}

// failureKind mirrors V1.j / the Y1.j enum used by V1.k.c().
type failureKind int

const (
	// failureAuth maps to V1.j "auth" (credential invalid/expired).
	failureAuth failureKind = iota
	// failureRate maps to V1.j "rate" (rate limited).
	failureRate
	// failureQuota maps to V1.j "quota" (balance/permission exhausted).
	failureQuota
	// failureTransient maps to V1.j "transient" (5xx, connection reset).
	failureTransient
)

// resetTimePattern finds "将在 <timestamp> 重置" / "resets at <timestamp>".
var resetTimePattern = regexp.MustCompile(
	`(?:将在|将于|重置时间[:：]?|resets?\s+(?:at|on)?)\s*([0-9]{4}-[0-9]{2}-[0-9]{2}[ T][0-9]{2}:[0-9]{2}:[0-9]{2})(?:\s*(?:UTC|GMT)\s*([-+])?\s*([0-9]{1,2})(?::([0-9]{2}))?)?`)

// parseUpstreamResetTime pulls the reset instant out of an upstream message.
//
// The hint is worth honouring: a fixed cooldown either releases the model while
// it is still throttled (every retry fails) or keeps it parked after it recovered
// (a working model sits idle). Returns false when no timestamp is present, which
// leaves the configured cooldown in charge.
//
// The offset is applied by hand rather than through a Go layout. The upstream
// writes "UTC+8", but time.Parse's Z07:00/Z0700 escapes only accept the colon and
// two-digit spellings ("+08:00", "+0800"), so no single layout covers it.
func parseUpstreamResetTime(message string, loc *time.Location) (time.Time, bool) {
	match := resetTimePattern.FindStringSubmatch(message)
	if len(match) < 2 {
		return time.Time{}, false
	}
	stamp := strings.TrimSpace(match[1])

	base := time.Time{}
	var parsed bool
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02T15:04:05"} {
		if candidate, errParse := time.Parse(layout, stamp); errParse == nil {
			base, parsed = candidate, true
			break
		}
	}
	if !parsed {
		return time.Time{}, false
	}

	if len(match) < 4 || strings.TrimSpace(match[3]) == "" {
		// No offset given: read the stamp as wall-clock time in the server's zone,
		// which is how the Chinese wording is meant.
		return time.Date(base.Year(), base.Month(), base.Day(),
			base.Hour(), base.Minute(), base.Second(), 0, loc), true
	}

	sign := 1
	if match[2] == "-" {
		sign = -1
	}
	hours := 0
	if _, errScan := fmt.Sscanf(match[3], "%d", &hours); errScan != nil {
		return time.Time{}, false
	}
	minutes := 0
	if len(match) > 4 && match[4] != "" {
		_, _ = fmt.Sscanf(match[4], "%d", &minutes)
	}
	offset := sign * (hours*3600 + minutes*60)

	// The stamp is local time at that offset; convert to UTC.
	return time.Date(base.Year(), base.Month(), base.Day(),
		base.Hour(), base.Minute(), base.Second(), 0, time.UTC).
		Add(-time.Duration(offset) * time.Second), true
}

// parkModelLocked parks one model on one lane.
//
// The expiry is derived from the upstream's own reset hint when it gives one
// ("将在 2026-09-27 20:03:46 UTC+8 重置"), because a fixed cooldown either gives up
// too early (wasting a working account) or too late. Falls back to the configured
// rate cooldown when no hint is present, capped so a misparsed date cannot park a
// model indefinitely.
func (p *credentialPool) parkModelLocked(lane *credentialLane, model string, kind coolKind, reason string, now time.Time, settings gatewaySettings) {
	model = strings.TrimSpace(model)
	if model == "" {
		return
	}

	until := now.Add(time.Duration(settings.RateCooldownMillis) * time.Millisecond)
	if hinted, okHint := parseUpstreamResetTime(reason, now.Location()); okHint {
		// A hint that is already in the past means the upstream's clock and ours
		// disagree; a short cooldown is the safer reading than "not throttled".
		if hinted.After(now) {
			until = hinted
		}
	}
	// Never park longer than the hard quota cooldown: a throttled model coming
	// back is the normal case, and a bad parse must not look like dead credit.
	if maxPark := now.Add(time.Duration(settings.QuotaCooldownMillis) * time.Millisecond); until.After(maxPark) {
		until = maxPark
	}

	if lane.ModelCooldowns == nil {
		lane.ModelCooldowns = make(map[string]time.Time)
	}
	if lane.ModelCoolKinds == nil {
		lane.ModelCoolKinds = make(map[string]coolKind)
	}
	if lane.ModelCoolReasons == nil {
		lane.ModelCoolReasons = make(map[string]string)
	}
	lane.ModelCooldowns[model] = until
	lane.ModelCoolKinds[model] = kind
	lane.ModelCoolReasons[model] = reason
	lane.LastError = reason
	lane.Failures++
}

// modelCooled reports whether a lane currently has this model parked, and until
// when. An empty model (caller does not know it) is never considered parked, so
// the caller falls back to the lane-level cooldown.
func (lane *credentialLane) modelCooled(model string, now time.Time) (time.Time, bool) {
	model = strings.TrimSpace(model)
	if model == "" || len(lane.ModelCooldowns) == 0 {
		return time.Time{}, false
	}
	until, okFound := lane.ModelCooldowns[model]
	if !okFound {
		return time.Time{}, false
	}
	if !now.Before(until) {
		// Expired; the entry is left in place so the panel can still show what
		// happened, and is ignored from here on.
		return time.Time{}, false
	}
	return until, true
}

// Returns whether the lane was actually retired by this call, so the caller can
// log the retirement once instead of inferring it.
//
// model is the model id the failed request asked for, and may be empty when the
// caller does not know it. When it is known and the failure is a throttle, only
// that model is parked — the upstream's own message invites switching models, and
// benching the account would also disable the models that still work.
func (p *credentialPool) failure(provider, uid string, kind failureKind, reason string, settings gatewaySettings, permanent bool) bool {
	return p.failureForModel(provider, uid, "", kind, reason, settings, permanent)
}

// failureForModel is failure with the model id, so a throttle can be scoped to
// one model instead of the whole account.
func (p *credentialPool) failureForModel(provider, uid, model string, kind failureKind, reason string, settings gatewaySettings, permanent bool) bool {
	if provider == "" || uid == "" {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	lane := p.lanes[laneKey(provider, uid)]
	if lane == nil {
		lane = &credentialLane{Provider: provider, UID: uid, Enabled: true, CoolKind: coolKindNone}
		p.lanes[laneKey(provider, uid)] = lane
		p.order = append(p.order, laneKey(provider, uid))
	}
	if !lane.Enabled && !lane.Disabled {
		lane.Enabled = true
	}

	now := time.Now()
	lane.LastError = reason
	lane.Failures++

	if permanent {
		// V1.k.c() case 2: permanent disable.
		//
		// Also stamp the user-visible flag and reason. Previously only the
		// internal Disabled bit was set, so an account the pool had retired for
		// a bad credential still rendered as 启用 in the panel and offered no
		// explanation — the operator had to infer it from the call log.
		lane.Disabled = true
		lane.DisabledByUser = true
		lane.AutoDisabled = true
		lane.DisabledReason = reason
		lane.DisabledAt = now
		lane.StatusMessage = reason
		p.recordAutoDisableLocked(lane, reason)
		return true
	}

	switch kind {
	case failureAuth, failureRate, failureQuota:
		// A throttle is scoped to the model that was asked for when the caller
		// knows which one it was: the upstream reports it per model and tells the
		// caller to switch models, so benching the account would take out the
		// models that are still fine. Quota exhaustion and auth failures are not
		// model-specific and keep the lane-level cooldown.
		if kind == failureRate && strings.TrimSpace(model) != "" {
			p.parkModelLocked(lane, model, coolKindRate, reason, now, settings)
			return false
		}

		// V1.k.c() cases 0,1,3: immediate hard cooldown.
		// The app labels a quota/rate rejection as QUOTA
		// (V1/k.java:164 passes W1.b.f3822d with quotaCooldownMillis).
		lane.ConsecutiveErrors++
		lane.StatusMessage = reason
		switch kind {
		case failureQuota:
			// A quota rejection means the credits are gone; reflecting that
			// keeps the selection order honest (A0/s.java:596).
			lane.CoolKind = coolKindQuota
			lane.CooldownUntil = now.Add(time.Duration(settings.QuotaCooldownMillis) * time.Millisecond)
			lane.Credits = 0
			lane.CreditsKnown = true
		case failureRate:
			// A 429 is transient: it says nothing about the remaining balance.
			// Zeroing Credits here permanently demoted a merely throttled
			// account under by_credits selection and, because CreditsKnown
			// stayed true, made it look "known empty" rather than unknown.
			// Keep the last known figure and use the rate cooldown.
			lane.CoolKind = coolKindRate
			lane.CooldownUntil = now.Add(time.Duration(settings.RateCooldownMillis) * time.Millisecond)
		default:
			lane.CoolKind = coolKindError
			lane.CooldownUntil = now.Add(time.Duration(settings.QuotaCooldownMillis) * time.Millisecond)
		}
	default:
		// V1.k.c() cases 4,5,6: soft failure. Park only after errorThreshold
		// consecutive failures, for errorCooldownMillis.
		// V1/k.java:168 labels this SOFT.
		lane.ConsecutiveErrors++
		lane.CoolKind = coolKindSoft
		if lane.ConsecutiveErrors >= settings.ErrorThreshold {
			lane.CooldownUntil = now.Add(time.Duration(settings.ErrorCooldownMillis) * time.Millisecond)
			lane.StatusMessage = reason
			lane.ConsecutiveErrors = 0
			return false
		}
		lane.CooldownUntil = now.Add(time.Duration(settings.SoftCooldownMillis) * time.Millisecond)
		lane.StatusMessage = reason
	}
	return false
}

// pick ports A0/s.java:585 t(providerId, exclude):
//
//	now := clock()
//	best := nil
//	for key, d := range accounts {           // LinkedHashMap insertion order
//	    if d.providerId != providerId { continue }
//	    if exclude.contains(d.uid) { continue }
//	    if !d.disabled && d.enabled && now >= d.untilMillis {
//	        if best == nil || d.credits > best.credits { best = d }
//	    }
//	}
//	return best?.account
//
// The selection weight is the remaining quota, so the richest account is used
// first. Ties keep the earlier insertion order, which is why the comparison is
// strict (>).
func (p *credentialPool) pick(provider string, tried map[string]struct{}, now time.Time) *credentialLane {
	p.mu.Lock()
	defer p.mu.Unlock()
	var best *credentialLane
	for _, key := range p.order {
		lane := p.lanes[key]
		if lane == nil || lane.Provider != provider {
			continue
		}
		if tried != nil {
			if _, ok := tried[lane.UID]; ok {
				continue
			}
		}
		if !lane.usable(now) {
			continue
		}
		if best == nil || lane.Credits > best.Credits {
			best = lane
		}
	}
	return best
}

// snapshot returns a stable copy of every lane for the management endpoint.
func (p *credentialPool) snapshot() []credentialLane {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]credentialLane, 0, len(p.lanes))
	for _, key := range p.order {
		if lane := p.lanes[key]; lane != nil {
			out = append(out, *lane)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Provider != out[j].Provider {
			return out[i].Provider < out[j].Provider
		}
		return out[i].UID < out[j].UID
	})
	return out
}

// usableCount reports how many lanes are currently usable for a provider.
func (p *credentialPool) usableCount(provider string, now time.Time) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, lane := range p.lanes {
		if lane.Provider == provider && lane.usable(now) {
			n++
		}
	}
	return n
}

func (p *credentialPool) totalCount(provider string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, lane := range p.lanes {
		if lane.Provider == provider {
			n++
		}
	}
	return n
}

// clearAutoDisableHistory drops the retirement audit and reports how many went.
//
// Used by the records page's clear action, which is about the operator's view: an entry
// they have read and acknowledged does not need to stay. It does not touch the lanes
// themselves — those still carry the disabled flag and the reason, so clearing the log
// never silently re-enables an account.
func (p *credentialPool) clearAutoDisableHistory() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := len(p.autoDisables)
	p.autoDisables = nil
	return n
}

// laneFor returns a copy of the lane for an identifier the host may spell either way.
//
// The host names a credential by its runtime auth index when it loaded it from disk and by
// the credential's own uid when it learned it from the wire; the pool keys by uid, so both
// spellings are accepted along with each one's normalised form.
//
// A copy, because snapshot() hands back values: the caller reads Variant and Label only,
// and returning a pointer into a slice that is rebuilt on every observe would be a race
// waiting to happen.
func (p *credentialPool) laneFor(identifier string) (credentialLane, bool) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return credentialLane{}, false
	}
	wanted := map[string]bool{identifier: true}
	if canonical := canonicalUID(identifier); canonical != "" {
		wanted[canonical] = true
	}
	for _, lane := range p.snapshot() {
		if wanted[lane.UID] {
			return lane, true
		}
	}
	return credentialLane{}, false
}

// applyHostDisabledFlags mirrors the host's disabled flag onto the pool's lanes.
//
// The flag belongs to the host: it is what CPA reads when building its candidate list, and
// what the panel's own enable/disable writes. Treating it as one-way — plugin writes,
// host reads — left the pool holding its own opinion, so an account switched off in CPA
// still looked usable here and could be named by this plugin's pick.
//
// Only the host-owned bit is touched. DisabledByUser and AutoDisabled belong to this
// plugin and are made to agree with it rather than overwritten: with the host saying
// enabled, those two cannot remain set, and with the host saying disabled the pool has to
// treat the lane as unavailable regardless of who turned it off.
func (p *credentialPool) applyHostDisabledFlags(accounts []workBuddyAccount) {
	if len(accounts) == 0 {
		return
	}
	byUID := make(map[string]bool, len(accounts))
	for _, a := range accounts {
		// Raw identifiers only. canonicalUID resolves through the account table, and we are
		// being called from it — see the note on the loop below.
		key := strings.TrimSpace(a.UID)
		if key == "" {
			key = strings.TrimSpace(a.AuthIndex)
		}
		if key == "" {
			continue
		}
		// An account duplicated across files is disabled only if every copy is.
		if prev, seen := byUID[key]; seen {
			byUID[key] = prev && a.Disabled
			continue
		}
		byUID[key] = a.Disabled
		// The auth index is registered too, so a lane keyed by either spelling is found.
		if idx := strings.TrimSpace(a.AuthIndex); idx != "" && idx != key {
			byUID[idx] = a.Disabled
		}
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	for _, lane := range p.lanes {
		if lane == nil {
			continue
		}
		// Matched on the raw uid, deliberately.
		//
		// canonicalUID resolves an identifier through the account table, and the table is
		// what called us — listWorkBuddyAccounts reads the inventory and then mirrors it
		// here. Going through the resolver re-enters that path and recurses until the stack
		// runs out. The uid in a lane and the uid in an inventory row are the same field
		// from the same source, so no resolution is needed.
		want, known := byUID[lane.UID]
		if !known {
			continue
		}
		if want {
			lane.Disabled = true
			lane.hostDisabledSeen = true
			continue
		}
		// Enabled at the host. Only a transition — the host said disabled on an earlier read
		// and now says enabled — is the operator re-enabling it there, and only that lifts
		// the plugin's own reasons as well.
		//
		// An automatic retirement is never written to the file, so the file reads "enabled"
		// for it on every listing. Clearing it whenever the file said so undid the retirement
		// on the next read, which made auto-disable a no-op.
		reenabled := lane.hostDisabledSeen
		lane.hostDisabledSeen = false
		if lane.AutoDisabled && !reenabled {
			continue
		}
		lane.Disabled = false
		lane.DisabledByUser = false
		lane.AutoDisabled = false
		lane.DisabledReason = ""
		lane.DisabledAt = time.Time{}
	}
}
