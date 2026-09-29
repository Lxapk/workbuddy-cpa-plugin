package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

// This file implements CPA's Scheduler capability, which lets the plugin decide
// which credential a request uses.
//
// Why this matters
//
// CPA normally picks the auth itself. The Scheduler capability hands the plugin
// the candidate list and lets it return one, which is what makes the account
// selection strategy configurable from the panel.
//
// Three strategies are offered:
//
//	by_credits  largest remaining quota first  — the source app's behaviour
//	              (A0/s.java:585 t() keeps the candidate with the greatest
//	               credits after filtering)
//	round_robin strictly rotating position      — predictable, spreads load
//	random      uniformly at random             — avoids hotspots entirely
//
// All three honour the same availability filter, mirroring A0/s.java:596:
//
//	not disabled, not in a cooldown window, status not failed
//
// and they never pick a credential the request has already tried.

// schedulerStrategy names the supported selection modes.
type schedulerStrategy string

const (
	// strategyByExpiry spends the soonest-expiring credits first, which is the
	// reference implementation's rotation policy.
	strategyByExpiry schedulerStrategy = "by_expiry"
	// strategyByCredits prefers the account with the most remaining quota.
	strategyByCredits schedulerStrategy = "by_credits"
	// strategyRoundRobin rotates through the candidates deterministically.
	strategyRoundRobin schedulerStrategy = "round_robin"
	// strategyRandom picks uniformly at random.
	strategyRandom schedulerStrategy = "random"
)

// allSchedulerStrategies lists the strategies in display order.
//
// A "weighted" strategy (three-factor weighted random) used to be offered here.
// It was removed: it shared this exact call path with by_credits — same
// SchedulerPick RPC, same candidate set, same response shape, only a different
// choice function — and its enable switch was never consulted anywhere, so the
// setting was inert. Keeping two strategies that differ only in arithmetic made
// the panel harder to reason about for no behavioural gain.
var allSchedulerStrategies = []schedulerStrategy{
	strategyByExpiry,
	strategyByCredits,
	strategyRoundRobin,
	strategyRandom,
}

// normalizeStrategy coerces user input, defaulting to by_credits (the app's
// behaviour) when unrecognised.
//
// "weighted" and its Chinese spellings are deliberately absent: a configuration
// saved under the old version falls through to the default rather than matching
// a strategy that no longer exists.
func normalizeStrategy(s string) schedulerStrategy {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case string(strategyRoundRobin), "round-robin", "roundrobin", "rr", "轮巡":
		return strategyRoundRobin
	case string(strategyRandom), "rand", "随机":
		return strategyRandom
	case string(strategyByCredits), "credits", "quota", "按额度", "额度":
		return strategyByCredits
	case "by_expiry", "expiry", "expire", "soonest", "按到期", "到期", "紧迫":
		return strategyByExpiry
	}
	return strategyByCredits
}

func (s schedulerStrategy) label() string {
	switch s {
	case strategyRoundRobin:
		return "轮巡"
	case strategyRandom:
		return "随机"
	case strategyByExpiry:
		return "按到期"
	default:
		return "按额度"
	}
}

// schedulerState holds the rotating cursor used by round_robin.
type schedulerState struct {
	mu sync.Mutex
	// cursor is consumed by round_robin. Keyed by provider so two providers do
	// not advance each other's position.
	cursor map[string]uint64
	// picks counts how many times each auth was chosen, for the panel.
	picks map[string]uint64
	// rng is shared by the random strategy.
	rng *rand.Rand
	// lastSwitch records the most recent by_expiry switch, for the cooldown
	// gate (rotate.rs: last_switch_at_ms).
	lastSwitch time.Time
	// lastPickedID is the account most recently handed to a request, used as
	// the "current account" in the rotation gate chain.
	lastPickedID string
}

func newSchedulerState() *schedulerState {
	return &schedulerState{
		cursor: make(map[string]uint64),
		picks:  make(map[string]uint64),
		// Seeded from the clock; the exact sequence does not matter, only that
		// it is not identical across restarts.
		rng: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// ---- candidate filtering ------------------------------------------------

// schedulerCandidate is one selectable auth, already filtered and decorated.
type schedulerCandidate struct {
	ID      string
	Credits int64
	// Known reports whether the pool has a credit reading for this candidate.
	Known bool
	// Matched reports whether the pool recognised this credential at all. Distinct from
	// Known: an account the pool holds but has never queried has no credit figure yet,
	// and conflating the two made a usable account read as "not recognised".
	Matched bool
	// RealmExcluded marks a credential the supplier switch rules out for this request.
	RealmExcluded bool
	Cooldown      time.Time
	HasCool       bool
	// ModelCooled marks a candidate that is parked for the requested model only.
	// It stays a candidate so the pick can fall through to the next one; if every
	// candidate ends up parked for this model, the caller reports that instead of
	// silently answering from a model the upstream is throttling.
	ModelCooled    bool
	ModelCoolUntil time.Time
	ModelCoolModel string
}

// candidateStats describes what happened to the host's offer.
//
// Returned alongside the candidates so the diagnostic can say which of the three reasons
// applied — never seen, cooling down, or marked unusable — instead of reporting the
// filtered list's length as "recognised".
type candidateStats struct {
	Offered  int
	Matched  int
	Cooling  int
	Rejected int
	// RealmExcluded counts credentials the supplier switch rules out. Reported apart from
	// the other exclusions: "全部正在冷却" and "都被供应商开关排除" call for different
	// actions, and the operator would otherwise go looking for a throttle that is not
	// there.
	RealmExcluded int
	// Disabled counts credentials the pool has retired. Its own case, because the remedy is
	// different again: the operator has to re-enable the account, not wait.
	Disabled int
}

// collectCandidates ports A0/s.java:596's availability guard and folds in the
// quota reading so strategies can rank by it.
//
// A candidate is dropped when the host says it is unusable, when the pool has
// it in a cooldown window, or when it has already been tried for this request.
func (s *schedulerState) collectCandidates(req pluginapi.SchedulerPickRequest) []schedulerCandidate {
	out, _ := s.collectCandidatesWithStats(req)
	return out
}

// collectCandidatesWithStats is collectCandidates plus the counters the diagnostic needs.
func (s *schedulerState) collectCandidatesWithStats(req pluginapi.SchedulerPickRequest) ([]schedulerCandidate, candidateStats) {
	tried := triedAuthSet(req.Options.Metadata)
	now := time.Now()
	stats := candidateStats{Offered: len(req.Candidates)}
	// Read once: the supplier switch applies to every candidate in this request.
	gateway := state.settings.get()

	open := make([]pluginapi.SchedulerAuthCandidate, 0, len(req.Candidates))
	for _, c := range req.Candidates {
		if c.ID == "" {
			continue
		}
		if _, seen := tried[c.ID]; seen {
			stats.Rejected++
			continue
		}
		// Host-reported status: skip anything explicitly failed or disabled.
		if isUnusableSchedulerStatus(c.Status) {
			stats.Rejected++
			continue
		}
		// Host may also surface the disabled flag in metadata.
		if boolFromAny(c.Metadata["disabled"]) {
			stats.Rejected++
			continue
		}
		open = append(open, c)
	}
	if len(open) == 0 {
		return nil, stats
	}

	out := make([]schedulerCandidate, 0, len(open))
	for _, c := range open {
		cand := schedulerCandidate{ID: c.ID}

		// Quota: prefer a recorded reading, else fall back to the pool lane.
		state.quota.mu.Lock()
		if q, ok := state.quota.byAuth[c.ID]; ok && q != nil && q.Known {
			cand.Credits = q.Credits
			cand.Known = true
		}
		state.quota.mu.Unlock()

		// The supplier switch is checked here, outside the !Known block below.
		//
		// That block is entered only when the quota reading did not supply a credit
		// figure — it exists to fill in what the reading missed. Putting the realm check
		// inside it meant a candidate whose credits were already known skipped the check
		// entirely, so with the quota cache warm the switch had no effect at all: the
		// request went wherever the host's first candidate led.
		//
		// The realm is resolved now rather than read off the lane: a lane is created by
		// observe() the first time a credential is seen, and at that moment the account
		// table may not be loaded, leaving Variant empty.
		// Realm gating and the disabled check, applied from whatever the host told us about
		// this candidate plus whatever the pool holds.
		//
		// Both must work on the first request. The pool creates a lane when the executor
		// first sees a credential, so on a cold start — or on any account the executor has
		// not yet touched — laneFor finds nothing, and an implementation that hangs the
		// checks off the lane silently skips them. That is how a disabled account kept
		// serving and how the supplier switch had no effect: the lanes were empty.
		if lane, found := state.pool.laneFor(c.ID); found {
			if lane.Disabled || lane.DisabledByUser || lane.AutoDisabled {
				stats.Disabled++
				continue
			}
		}
		realm := wbVariant(variantForAuthIndex(c))
		if realm == "" {
			if lane, found := state.pool.laneFor(c.ID); found {
				realm = wbVariant(lane.Variant)
				if realm == "" {
					realm = wbVariant(variantForUID(lane.UID, lane.Label))
				}
			}
		}
		logf("scheduler: realm id=%s realm=%q attrs=%q switch=%q",
			c.ID, realm, variantForAuthIndex(c), gateway.VariantOverride)
		if realm != "" && !variantAllowedFor(gateway.VariantOverride, realm) {
			stats.RealmExcluded++
			continue
		}

		if !cand.Known {
			// The host identifies a candidate by whatever id its own inventory uses —
			// the runtime auth index for a credential it loaded from disk, the uid for
			// one it learned from the wire. The pool keys by the credential's uid, so a
			// direct comparison misses one of the two and the account looks unknown.
			// Normalising both sides is what makes the two agree.
			wanted := candidateIdentifiers(c)
			for _, lane := range state.pool.snapshot() {
				if !wanted[lane.UID] && !wanted[laneKey(lane.Provider, lane.UID)] {
					continue
				}
				// Matched: the pool knows this credential — counted before the supplier
				// check below, because "recognised" and "eligible for this request" are
				// different questions and the diagnostic reports both.
				cand.Matched = true
				stats.Matched++

				// A model-scoped throttle parks only that model: the account is
				// still a valid candidate for every other model, and skipping it
				// here is what lets a throttled deepseek-v4.1-flash fall through to
				// an account that can still serve it.
				if until, cooled := lane.modelCooled(req.Model, now); cooled {
					cand.ModelCooled = true
					cand.ModelCoolUntil = until
					cand.ModelCoolModel = req.Model
				}
				// Respect a pool-level cooldown even if the host does not know
				// about it (the pool sees failures the host does not).
				if !lane.CooldownUntil.IsZero() && now.Before(lane.CooldownUntil) {
					cand.Cooldown = lane.CooldownUntil
					cand.HasCool = true
				}
				// Matched: the pool knows this credential. Credits are recorded
				// separately because not every account has been queried yet.
				if lane.CreditsKnown {
					cand.Credits = lane.Credits
					cand.Known = true
				}
				break
			}
		}
		if cand.RealmExcluded || cand.HasCool {
			continue
		}
		out = append(out, cand)
	}

	// Drop candidates parked for this specific model, so the pick falls through
	// to an account that can still serve it. Done as a second pass so that when
	// *every* candidate is parked for this model the list comes back empty — the
	// caller turns that into "no account can serve this model right now" rather
	// than answering from an account the upstream is throttling.
	if strings.TrimSpace(req.Model) != "" {
		servable := out[:0]
		for _, cand := range out {
			if cand.ModelCooled {
				stats.Cooling++
				continue
			}
			servable = append(servable, cand)
		}
		out = servable
	}
	return out, stats
}

// modelCooledForRequest reports, for a request whose candidates all came back
// parked, which model is cooling and until when. Used to explain the failure
// instead of returning a bare "no account".
func (s *schedulerState) modelCooledForRequest(req pluginapi.SchedulerPickRequest) (time.Time, bool) {
	model := strings.TrimSpace(req.Model)
	if model == "" {
		return time.Time{}, false
	}
	now := time.Now()
	var earliest time.Time
	for _, c := range req.Candidates {
		if c.ID == "" {
			continue
		}
		for _, lane := range state.pool.snapshot() {
			if lane.UID != c.ID && laneKey(lane.Provider, lane.UID) != c.ID {
				continue
			}
			until, cooled := lane.modelCooled(model, now)
			if !cooled {
				break
			}
			if earliest.IsZero() || until.Before(earliest) {
				earliest = until
			}
			break
		}
	}
	if earliest.IsZero() {
		return time.Time{}, false
	}
	return earliest, true
}

// humanizeUntil renders a cooldown expiry as a short relative phrase plus the
// wall-clock instant, e.g. "4 分 30 秒后（20:03:46）".
func humanizeUntil(until time.Time) string {
	remaining := time.Until(until)
	if remaining <= 0 {
		return "已恢复"
	}
	remaining = remaining.Round(time.Second)
	var phrase string
	switch {
	case remaining >= time.Hour:
		phrase = fmt.Sprintf("%d 小时 %d 分钟后", int(remaining.Hours()), int(remaining.Minutes())%60)
	case remaining >= time.Minute:
		phrase = fmt.Sprintf("%d 分 %d 秒后", int(remaining.Minutes()), int(remaining.Seconds())%60)
	default:
		phrase = fmt.Sprintf("%d 秒后", int(remaining.Seconds()))
	}
	return fmt.Sprintf("%s（%s）", phrase, until.In(panelLocation).Format("15:04:05"))
}

// triedAuthSet reads the already-attempted auth ids from scheduler metadata.
//
// CPA passes request-scoped state through SchedulerOptions.Metadata; the key
// names below cover the shapes the host uses.
func triedAuthSet(meta map[string]any) map[string]struct{} {
	out := map[string]struct{}{}
	if meta == nil {
		return out
	}
	for _, key := range []string{"tried_auth_ids", "tried", "excluded_auth_ids", "tried_auths"} {
		raw, ok := meta[key]
		if !ok {
			continue
		}
		switch v := raw.(type) {
		case []string:
			for _, id := range v {
				out[id] = struct{}{}
			}
		case []any:
			for _, item := range v {
				if s, okString := item.(string); okString {
					out[s] = struct{}{}
				}
			}
		}
	}
	return out
}

// isUnusableSchedulerStatus drops candidates the host already considers bad.
// isUnusableSchedulerStatus reports whether the host has marked a credential as
// unusable for reasons the plugin should respect.
//
// "error" is deliberately absent. CPA sets it together with auth.Unavailable for a
// temporary failure — the status constant is documented as "temporarily unavailable due
// to errors" — and clears it on the next successful refresh. Treating it as a permanent
// verdict made the scheduler refuse every account the host offered: a request for a model
// with four healthy credentials returned "选号无候选：宿主提供 4 个账号，本地只认出 0 个"
// and 503, while the accounts page showed all four as usable.
//
// The candidates reaching this function have already been through CPA's own availability
// screen, so an entry here is one the host considers a candidate. The statuses kept below
// are the ones that mean "do not use this": the host will not serve it until something
// changes outside this request.
func isUnusableSchedulerStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "disabled", "unavailable", "failed", "invalid", "blocked", "revoked":
		return true
	}
	// Everything else — including "error" and any status this plugin does not know — is
	// left to the host and the executor: if the credential really cannot serve, the
	// attempt fails and the pool parks it with a reason that names the cause.
	return false
}

func boolFromAny(v any) bool {
	b, ok := v.(bool)
	return ok && b
}

// ---- strategies ---------------------------------------------------------

// pickByCredits returns the candidate with the greatest remaining quota,
// mirroring A0/s.java:596's strict-greater comparison (ties keep host order).
func pickByCredits(candidates []schedulerCandidate) string {
	if len(candidates) == 0 {
		return ""
	}
	best := candidates[0]
	for _, c := range candidates[1:] {
		if c.Credits > best.Credits {
			best = c
		}
	}
	return best.ID
}

// pickRoundRobin advances a per-provider cursor and returns that position.
//
// The candidate list is sorted first so the rotation is stable regardless of
// the order the host supplies.
func (s *schedulerState) pickRoundRobin(provider string, candidates []schedulerCandidate) string {
	if len(candidates) == 0 {
		return ""
	}
	ids := make([]string, 0, len(candidates))
	for _, c := range candidates {
		ids = append(ids, c.ID)
	}
	sort.Strings(ids)

	s.mu.Lock()
	pos := s.cursor[provider]
	s.cursor[provider] = pos + 1
	s.mu.Unlock()

	return ids[int(pos%uint64(len(ids)))]
}

// pickRandom returns a uniformly random candidate.
func (s *schedulerState) pickRandom(candidates []schedulerCandidate) string {
	if len(candidates) == 0 {
		return ""
	}
	s.mu.Lock()
	idx := s.rng.Intn(len(candidates))
	s.mu.Unlock()
	return candidates[idx].ID
}

// ---- RPC ----------------------------------------------------------------

// schedulerPick answers scheduler.pick.
func schedulerPick(request []byte) ([]byte, error) {
	var req pluginapi.SchedulerPickRequest
	if len(request) > 0 {
		if errUnmarshal := json.Unmarshal(request, &req); errUnmarshal != nil {
			return nil, errUnmarshal
		}
	}
	// Refusals are recorded, not every call.
	//
	// "No auth available" looks identical whether the host offered no candidates,
	// offered some that were all parked, or never asked at all. Only a refusal
	// needs that explained, so the trace is written on the empty-candidate path.

	if !schedulerOwnsProvider(req) {
		return okEnvelope(pluginapi.SchedulerPickResponse{Handled: false})
	}

	// Log the host's offer when debugging is on: the identifier it uses has to be
	// reconciled with the pool's uid, and a failure there shows up only as a request that
	// silently picked the wrong realm.
	debugLogCandidates(req)

	candidates, stats := state.scheduler.collectCandidatesWithStats(req)
	// Record the host's offer when it changes.
	//
	// Whether a request can survive one throttled credential depends on how many
	// the host offered and what it said about each, so this is worth keeping —
	// but only while it is informative. Writing it on every call would push the
	// failures that actually need reading out of the call history, and the offer
	// is the same on almost every call: the host re-sends the same credential set
	// and only the per-candidate status drifts.
	// Record the host's offer only when it says something is wrong.
	//
	// This was written on every change of the candidate set while I was diagnosing a
	// report that "three accounts were only ever using one". The diagnosis is done —
	// the host was offering all three correctly — and the line kept appearing in the
	// call log where it reads like an error rather than a note. Now it is emitted
	// only when the two counts disagree, which is the case that would actually need
	// investigating: the host handed us credentials the pool has no lane for.
	// The candidate set is no longer recorded here.
	//
	// It was added while diagnosing a report that "three accounts were only ever using
	// one"; the diagnosis is done (the host was offering them correctly, and the real
	// fault was two identifiers for one account, fixed in canonicalUID). Keeping a
	// note that fires on every mismatch only added lines to the log that read like
	// errors, so the branch is gone rather than merely quietened.
	if len(candidates) == 0 {
		// All candidates may have been parked for this model specifically. Say so,
		// because "handled: false" sends the request back to the host, which then
		// retries the same throttled account and surfaces a bare "no auth
		// available" with no hint about when it clears.
		if until, cooled := state.scheduler.modelCooledForRequest(req); cooled {
			state.log.add(callRecord{
				ProviderID: req.Provider,
				Model:      req.Model,
				StatusCode: http.StatusTooManyRequests,
				Error: fmt.Sprintf("模型 %s 已被上游限流，%s 后恢复；其他模型不受影响",
					req.Model, humanizeUntil(until)),
			})
		}
		// Report that nothing was usable, in one sentence.
		//
		// The previous version dumped the host's candidate list and the full credential
		// inventory into the message — a hundred-account wall that filled the call list
		// and read like a crash. What the operator needs from this row is that the model
		// went unserved and, when it did, why: every account is parked for this model, or
		// the pool has nothing this request could use. The detail is available from the
		// accounts page; it does not belong in a table cell.
		reason := describeNoCandidateReason(stats.Offered, stats.Matched, stats.Cooling, stats.Rejected, stats.RealmExcluded, stats.Disabled)
		state.log.add(callRecord{
			ProviderID: req.Provider,
			Model:      req.Model,
			StatusCode: http.StatusServiceUnavailable,
			Error:      reason,
		})
		// Handled:false so the host falls back to its own scheduler rather than failing the
		// request outright.
		//
		// This is a deliberate limit, and worth stating because it bounds what the supplier
		// switch can guarantee: CPA treats an empty AuthID together with Handled:true as
		// "the plugin did not decide" and runs its own selection, which does not know about
		// the switch. There is no value of this response that means "no account is
		// acceptable" — the only two outcomes the host understands are a named account and
		// a delegate. So the switch governs the pick this plugin makes; a host-side retry
		// after that pick fails may land elsewhere.
		//
		// The alternative — writing disabled onto the other realm's auth files — would
		// reach the host but turn a per-request preference into a persistent state change,
		// which is not what the setting means.
		return okEnvelope(pluginapi.SchedulerPickResponse{Handled: false})
	}

	strategy := state.settings.get().Routing.Strategy
	var chosen string
	var delegate string

	switch strategy {
	case strategyByExpiry:
		chosen, _ = pickByExpiryScheduler(req, candidates)
	case strategyRoundRobin:
		delegate = pluginapi.SchedulerBuiltinRoundRobin
	case strategyRandom:
		chosen = state.scheduler.pickRandom(candidates)
	default:
		chosen = pickByCredits(candidates)
	}

	if delegate != "" {
		return okEnvelope(pluginapi.SchedulerPickResponse{
			Handled:         true,
			DelegateBuiltin: delegate,
		})
	}
	if chosen == "" {
		return okEnvelope(pluginapi.SchedulerPickResponse{Handled: false})
	}

	state.scheduler.recordPick(chosen)
	return okEnvelope(pluginapi.SchedulerPickResponse{Handled: true, AuthID: chosen})
}

// pickByExpiryScheduler adapts scheduler candidates to the rotation gate chain.
//
// The "current account" is whatever the plugin last handed out; the first pick
// has none, so the chain switches unconditionally (subject to the gates).
func pickByExpiryScheduler(req pluginapi.SchedulerPickRequest, candidates []schedulerCandidate) (string, rotateDecision) {
	rot := make([]rotateCandidate, 0, len(candidates))
	for _, c := range candidates {
		rc := rotateCandidate{
			AccountID:      c.ID,
			DisplayName:    c.ID,
			TotalRemaining: float64(c.Credits),
			// Default to valid. A candidate with no quota record still has to
			// be selectable, otherwise by_expiry would refuse to serve any
			// request until a refresh had run for every account.
			Valid: true,
		}
		// Attach the expiry from the recorded credit summary, if any.
		state.quota.mu.Lock()
		q, hasQuota := state.quota.byAuth[c.ID]
		state.quota.mu.Unlock()
		if !hasQuota {
			// Fall back to a uid-keyed reading.
			if alt, okAlt := lookupQuotaByUID(c.ID); okAlt {
				q, hasQuota = alt, true
			}
		}
		if hasQuota && q != nil {
			rc.SoonestExpireAt = q.soonestExpireAt()
			// An expired balance disqualifies the account as a target, matching
			// Candidate::valid in the reference implementation. A missing
			// reading does not: it only means the expiry is unknown.
			if q.expired() || !q.Known {
				rc.Valid = false
			}
		}
		rot = append(rot, rc)
	}

	current := state.scheduler.lastPicked()
	target, decision := pickByExpiry(rot, current, time.Now())
	if decision.Kind == rotateSwitch {
		state.scheduler.noteSwitch(time.Now())
	}
	return target, decision
}

// lookupQuotaByUID finds a recorded credit summary by uid or auth id.
func lookupQuotaByUID(id string) (*workBuddyQuota, bool) {
	return lookupQuotaAny(id)
}

// lookupQuotaAny finds a recorded credit summary under any of the identifiers a
// credential may be keyed by.
//
// Three keys are in play and they differ by code path:
//
//	host.auth.list -> entry.AuthIndex   (the runtime index, e.g. e420b8fe...)
//	executor       -> auth.AuthIndex    (the credential's uid)
//	models         -> creds.AuthKey()   ("<domain>/<uid>")
//
// Accepting all of them keeps a successful query from being invisible to the
// panel, which is exactly what happened before this helper existed: the quota
// refresh stored the reading under the auth index while the account table looked
// it up by uid, so the panel showed credits 0 despite a successful query.
func lookupQuotaAny(ids ...string) (*workBuddyQuota, bool) {
	state.quota.mu.Lock()
	defer state.quota.mu.Unlock()

	for _, id := range ids {
		id = trimSpace(id)
		if id == "" {
			continue
		}
		if q, ok := state.quota.byAuth[id]; ok && q != nil {
			return q, true
		}
	}
	// Fall back to matching the "<domain>/<uid>" keys.
	for _, id := range ids {
		id = trimSpace(id)
		if id == "" {
			continue
		}
		for key, q := range state.quota.byAuth {
			if q == nil {
				continue
			}
			if strings.HasSuffix(key, "/"+id) {
				return q, true
			}
		}
	}
	return nil, false
}

// schedulerOwnsProvider reports whether this plugin should schedule the request.
//
// When the provider list is empty the host has not resolved a provider yet, so
// the plugin answers only for its own provider; if none of the listed providers
// is ours, the request belongs to someone else.
func schedulerOwnsProvider(req pluginapi.SchedulerPickRequest) bool {
	if isWorkBuddyProvider(req.Provider) {
		return true
	}
	for _, p := range req.Providers {
		if isWorkBuddyProvider(p) {
			return true
		}
	}
	// A single provider that is not ours, or an explicit list without ours.
	if req.Provider != "" || len(req.Providers) > 0 {
		return false
	}
	// No provider information: decide from the candidates.
	for _, c := range req.Candidates {
		if isWorkBuddyProvider(c.Provider) {
			return true
		}
	}
	return false
}

// schedulerProviderKey returns a stable key for the round-robin cursor.
func schedulerProviderKey(req pluginapi.SchedulerPickRequest) string {
	if req.Provider != "" {
		return req.Provider
	}
	for _, p := range req.Providers {
		if isWorkBuddyProvider(p) {
			return p
		}
	}
	return workBuddyProviderKey
}

func (s *schedulerState) recordPick(authID string) {
	if authID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.picks[authID]++
	s.lastPickedID = authID
}

// lastPicked returns the account most recently selected.
func (s *schedulerState) lastPicked() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastPickedID
}

// pickCounts returns a copy of the per-auth selection counters.
func (s *schedulerState) pickCounts() map[string]uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]uint64, len(s.picks))
	for k, v := range s.picks {
		out[k] = v
	}
	return out
}

// resetCursor clears the round-robin position so the next pick starts at the
// top. Exposed on the panel.
func (s *schedulerState) resetCursor() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cursor = make(map[string]uint64)
	s.lastSwitch = time.Time{}
}

// lastSwitchAt returns when by_expiry last changed account.
func (s *schedulerState) lastSwitchAt() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastSwitch
}

// noteSwitch records that by_expiry moved to a different account.
func (s *schedulerState) noteSwitch(at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastSwitch = at
}

// usableCandidates counts the candidates that are not parked for the requested model.
//
// The message needs to distinguish "the accounts exist but are cooling down" from "the
// host offered accounts the pool cannot match", so it is told how many of each there are
// rather than being handed the lists.
func usableCandidates(candidates []schedulerCandidate) []schedulerCandidate {
	out := make([]schedulerCandidate, 0, len(candidates))
	for _, c := range candidates {
		if c.HasCool && time.Now().Before(c.Cooldown) {
			continue
		}
		out = append(out, c)
	}
	return out
}

// describeNoCandidateReason explains why a request could not be served, in one sentence.
//
// The figures come from the pool's own view, not from the length of the filtered list: a
// candidate can be absent because the pool never saw it, because it is cooling down, or
// because the host marked it unusable, and "本地只认出 0 个" was printed for all three.
// Reporting the wrong cause sends the reader looking in the wrong place.
//
// The account inventories are deliberately omitted. Listing every candidate and every
// credential CPA holds filled the cell, read like a crash, and buried the one line that
// matters.
func describeNoCandidateReason(offered, matched, cooling, rejected, realmExcluded, disabled int) string {
	switch {
	case offered == 0:
		return "选号无候选：宿主没有为该模型提供任何账号（这个模型可能不由本插件服务）"
	case disabled >= offered && disabled > 0:
		return fmt.Sprintf("选号无候选：宿主提供的 %d 个账号都已被禁用；在账号页重新启用即可", offered)
	case realmExcluded >= offered && realmExcluded > 0:
		// Every account the host offered belongs to the other supplier. Checked before the
		// "not recognised" case: with the switch on, an excluded credential never reaches
		// the match, so matched is zero and the reader would be told the credentials were
		// missing when in fact they were deliberately ruled out.
		return fmt.Sprintf("选号无候选：宿主提供的 %d 个账号都被「供应商切换」排除；改用「自动」或切到另一侧", offered)
	case matched == 0:
		return fmt.Sprintf("选号无候选：宿主提供 %d 个账号，本地一个也没认出（凭据可能尚未载入）", offered)
	case cooling == matched:
		return fmt.Sprintf("选号无候选：宿主提供 %d 个账号，全部正在为该模型冷却中；稍后会自动恢复", offered)
	case realmExcluded > 0 || disabled > 0 || matched < offered:
		return fmt.Sprintf("选号无候选：宿主提供 %d 个账号，本地认出 %d 个，其中 %d 个已禁用、%d 个被供应商开关排除、%d 个正在冷却、%d 个被宿主标记为不可用",
			offered, matched, disabled, realmExcluded, cooling, rejected)
	default:
		return fmt.Sprintf("选号无候选：宿主提供的 %d 个账号中，%d 个正在冷却、%d 个被宿主标记为不可用",
			offered, cooling, rejected)
	}
}

// candidateIdentifiers returns every identifier a host candidate might be known by.
//
// CPA names a credential by its runtime auth index when it loaded it from disk, and by
// the credential's own uid when it learned it from the wire — the same account, two
// strings. The pool keys by uid, so both spellings are accepted, along with the
// normalised form of each.
func candidateIdentifiers(c pluginapi.SchedulerAuthCandidate) map[string]bool {
	out := map[string]bool{}
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		out[v] = true
		add2 := canonicalUID(v)
		if add2 != "" {
			out[add2] = true
		}
	}
	add(c.ID)
	if c.Metadata != nil {
		add(stringFromAny(c.Metadata["uid"]))
		add(stringFromAny(c.Metadata["auth_id"]))
		add(stringFromAny(c.Metadata["auth_index"]))
	}
	return out
}

// stringFromAny reads a string out of an untyped metadata value.
func stringFromAny(v any) string {
	s, _ := v.(string)
	return s
}

// candidateDebugString renders a candidate for the debug log.
//
// Used while tracking down why the supplier switch had no effect: the candidates CPA
// offers are keyed by an identifier that has to be reconciled with the pool's uid, and
// when that reconciliation fails the candidate is silently treated as unrecognised.
func candidateDebugString(c pluginapi.SchedulerAuthCandidate) string {
	keys := make([]string, 0, len(c.Metadata)+2)
	for k := range c.Metadata {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+stringFromAny(c.Metadata[k]))
	}
	// Attributes carry the immutable routing properties the host knows about a credential
	// — which is where the realm may be, since the pool's own lane has no domain field.
	attrKeys := make([]string, 0, len(c.Attributes))
	for k := range c.Attributes {
		attrKeys = append(attrKeys, k)
	}
	sort.Strings(attrKeys)
	attrs := make([]string, 0, len(attrKeys))
	for _, k := range attrKeys {
		attrs = append(attrs, k+"="+c.Attributes[k])
	}
	return "id=" + c.ID + " provider=" + c.Provider + " status=" + c.Status +
		" attrs{" + strings.Join(attrs, ", ") + "} metadata{" + strings.Join(parts, ", ") + "}"
}

// debugLogCandidates writes the host's offer when the debug setting is on.
func debugLogCandidates(req pluginapi.SchedulerPickRequest) {
	if !state.settings.get().Debug {
		return
	}
	for _, c := range req.Candidates {
		logf("scheduler: candidate %s", candidateDebugString(c))
	}
}

// variantForAuthIndex resolves a realm from the host's candidate attributes.
//
// The attribute "source" (or "path") names the auth file, e.g.
// "auths/codebuddy-42213638-….json". The file name carries the credential's uid, which is
// what the account table is keyed by — so this reaches the table even when the pool's lane
// has no realm recorded yet.
func variantForAuthIndex(c pluginapi.SchedulerAuthCandidate) string {
	for _, key := range []string{"uid", "source", "path"} {
		raw := strings.TrimSpace(c.Attributes[key])
		if raw == "" {
			continue
		}
		candidate := raw
		if idx := strings.LastIndex(candidate, "/"); idx >= 0 {
			candidate = candidate[idx+1:]
		}
		candidate = strings.TrimSuffix(candidate, ".json")
		candidate = strings.TrimPrefix(candidate, workBuddyProviderKey+"-")
		candidate = strings.TrimPrefix(candidate, workBuddyDisplayNameLower+"-")
		if realm := variantForUID(candidate, ""); realm != "" {
			return realm
		}
	}
	return ""
}
