package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// This file provides the single authoritative WorkBuddy account view.
//
// Why this exists
//
// The plugin's credential pool (pool.go) only learns about an account when it
// sees traffic for it (intercept_response.go) or when a quota refresh runs. That
// means a freshly logged-in account stayed invisible until the first request —
// which is not what the source app does: it lists accounts straight from its
// own store (A0.s), independent of any traffic.
//
// CPA's equivalent of that store is the auth-file inventory behind
// host.auth.list / host.auth.get. This file reads it directly, so the account
// list is correct the moment a login finishes.
//
// Filtering is strict: only credentials whose provider is WorkBuddy/codebuddy
// are surfaced. Other providers never appear, even though CPA may hold many.

// workBuddyAccount is one WorkBuddy credential as presented to the UI.
type workBuddyAccount struct {
	// AuthIndex is CPA's storage key (the auth file name).
	AuthIndex string `json:"auth_index"`
	// Label is the user-facing name.
	Label string `json:"label"`
	// UID mirrors the provider-side user id.
	UID string `json:"uid"`
	// Nickname mirrors W1.d's display name when the provider supplied one.
	Nickname string `json:"nickname,omitempty"`
	// Domain is the stored region domain ("" when unset).
	Domain string `json:"domain"`
	// Region is the derived region key: "cn" or "global" (a2/b.java:284).
	Region string `json:"region"`
	// Cooldown is the account-level cooldown expiry, empty when the account is
	// eligible right now. ModelCooldowns lists per-model parks, which is the
	// common case for a throttle: the upstream reports it per model and invites
	// switching, so the account stays usable for other models.
	Cooldown       string            `json:"cooldown,omitempty"`
	CooldownKind   string            `json:"cooldown_kind,omitempty"`
	ModelCooldowns map[string]string `json:"model_cooldowns,omitempty"`
	// ModelCoolReasons carries the upstream wording for each parked model.
	ModelCoolReasons map[string]string `json:"model_cool_reasons,omitempty"`
	// LastError is the most recent failure reason for this account.
	LastError string `json:"last_error,omitempty"`
	// Failures counts failures since the plugin started.
	Failures int64 `json:"failures,omitempty"`
	// Regions lists every region this person holds a credential for, in the
	// canonical order (国内在前). One account legitimately has both: the CN and
	// global endpoints issue separate tokens for the same uid, so collapsing them
	// into a single row loses the fact that both are available.
	Regions []string `json:"regions,omitempty"`
	// AuthIndexes lists every auth file backing this person, so the merged row
	// can still be traced back to the individual credentials.
	AuthIndexes []string `json:"auth_indexes,omitempty"`
	// CredentialCount is how many stored credentials collapsed into this row.
	CredentialCount int `json:"credential_count,omitempty"`
	// EnterpriseID is the tenant, when present.
	EnterpriseID string `json:"enterprise_id,omitempty"`
	// ExpiresAt is the credential expiry as epoch seconds (0 when unknown).
	ExpiresAt int64 `json:"expires_at,omitempty"`
	// Expired reports whether the access token looks expired.
	Expired bool `json:"expired"`
	// Disabled mirrors the host-side disabled flag.
	Disabled bool `json:"disabled"`

	// Credits is the latest known remaining quota (0 when never queried).
	Credits int64 `json:"credits"`
	// CreditsTotal is the cycle capacity the balance is drawn from. Shown next to
	// the remaining figure ("3735 / 4600") because a bare remainder says nothing
	// about how much of the allowance is left.
	CreditsTotal int64 `json:"credits_total"`
	// CreditsUsed is the spent portion, computed as total - remaining.
	CreditsUsed int64 `json:"credits_used"`
	// CreditsKnown reports whether a quota figure is available.
	CreditsKnown bool `json:"credits_known"`
	// CreditsAt is when the quota was last read.
	CreditsAt time.Time `json:"credits_at,omitempty"`

	// CreditsExpireAt is the soonest expiry across this account's credit
	// resources, in epoch seconds (0 = no expiry). Ported from the reference
	// implementation's soonestExpireAt.
	CreditsExpireAt int64 `json:"credits_expire_at,omitempty"`
	// CreditsExpireDays is the whole-day countdown to CreditsExpireAt.
	CreditsExpireDays int64 `json:"credits_expire_days,omitempty"`
	// CreditsExpiringSoon reports CreditsExpireAt within 7 days.
	CreditsExpiringSoon bool `json:"credits_expiring_soon"`
	// CreditsExpired reports that a credit resource has already expired.
	CreditsExpired bool `json:"credits_expired"`
	// CreditPackages lists the package names, for the detail view.
	CreditPackages []string `json:"credit_packages,omitempty"`
	// Variant is "cn" or "ai", shown so the operator can see which service an
	// account belongs to.
	Variant string `json:"variant"`
	// VariantLabel is Variant rendered for display ("国内版"/"国际版").
	VariantLabel string `json:"variant_label"`

	// CoolKind / Reason / CooldownUntil come from the pool when it has seen
	// this credential; they are empty otherwise.
	CoolKind      string    `json:"cool_kind,omitempty"`
	Reason        string    `json:"reason,omitempty"`
	CooldownUntil time.Time `json:"cooldown_until,omitempty"`
	Usable        bool      `json:"usable"`
	// DisabledByUser reports the operator's manual enable/disable toggle.
	DisabledByUser bool `json:"disabled_by_user"`
	// AutoDisabled reports that the pool retired this account itself, after a
	// failure that retrying cannot fix. Distinguished from DisabledByUser so the
	// panel can say who disabled it and why.
	AutoDisabled bool `json:"auto_disabled"`
	// DisabledReason carries the upstream message that caused the retirement.
	DisabledReason string `json:"disabled_reason,omitempty"`

	// credentials is the parsed credential material, kept for internal callers
	// (model catalogue, quota refresh). Unexported so it never reaches JSON.
	credentials *workBuddyCredentials
}

// accountStore caches the inventory briefly so a page render does not hit the
// host on every request, while still picking up a fresh login within seconds.
type accountStore struct {
	mu        sync.Mutex
	cached    []workBuddyAccount
	fetchedAt time.Time
	ttl       time.Duration
	lastErr   string
}

func newAccountStore() *accountStore {
	return &accountStore{ttl: 5 * time.Second}
}

// invalidate forces the next read to hit the host.
func (s *accountStore) invalidate() {
	s.mu.Lock()
	s.fetchedAt = time.Time{}
	s.mu.Unlock()
}

// accounts returns the current WorkBuddy inventory, refreshing from the host
// when the cache has expired.
func (s *accountStore) accounts() []workBuddyAccount {
	s.mu.Lock()
	if !s.fetchedAt.IsZero() && time.Since(s.fetchedAt) < s.ttl {
		out := append([]workBuddyAccount(nil), s.cached...)
		s.mu.Unlock()
		return out
	}
	s.mu.Unlock()

	list, errList := loadWorkBuddyAccounts()

	s.mu.Lock()
	defer s.mu.Unlock()
	if errList != nil {
		s.lastErr = errList.Error()
		// Serve the stale copy rather than an empty list on a transient error.
		out := append([]workBuddyAccount(nil), s.cached...)
		return out
	}
	s.cached = list
	s.fetchedAt = time.Now()
	s.lastErr = ""
	// The host's flag is mirrored onto the pool by whoever consumes this list, outside
	// this lock.
	//
	// Doing it here deadlocks: the pool resolves a lane's realm by calling back into this
	// store for the account table, so the order accounts()→pool.mu would meet the order
	// pool.mu→accounts() and the two would wait on each other. The list is returned and the
	// caller — which is not holding either lock — applies it.
	return append([]workBuddyAccount(nil), list...)
}

func (s *accountStore) lastError() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastErr
}

// loadWorkBuddyAccounts reads and filters the host's auth inventory.
func loadWorkBuddyAccounts() ([]workBuddyAccount, error) {
	raw, errList := callHost("host.auth.list", map[string]any{})
	if errList != nil {
		return nil, errList
	}

	entries := decodeAuthEntries(raw)
	out := make([]workBuddyAccount, 0, len(entries))

	for _, entry := range entries {
		// Strict provider filter: only WorkBuddy/codebuddy entries are shown.
		// The host may expose "provider" or "type", and either may carry the
		// internal key or the display name.
		if !isWorkBuddyAuthEntry(entry) {
			continue
		}
		logf("auth entry: index=%q name=%q id=%q path=%q disabled=%v",
			entry.AuthIndex, entry.Name, entry.ID, entry.Path, entry.Disabled)

		storage := entry.StorageJSON
		if len(storage) == 0 && entry.AuthIndex != "" {
			storage = fetchAuthStorage(entry.AuthIndex)
		}
		creds, errParse := parseWorkBuddyCredentials(storage)
		if errParse != nil {
			// A codebuddy file we cannot parse is still worth showing, so the
			// operator can see something is wrong with it.
			//
			// Recover whatever identity we can from the raw blob. Without this the
			// row has no uid, so its dedupe key falls back to the auth index and it
			// can never merge with the healthy record for the same person — which
			// is what made one account appear several times in the panel.
			recovered := recoverIdentityFromStorage(storage)
			out = append(out, workBuddyAccount{
				AuthIndex:      entry.AuthIndex,
				Label:          firstNonEmpty(entry.Label, entry.Name, recovered.nickname, recovered.uid, entry.AuthIndex),
				UID:            recovered.uid,
				Nickname:       recovered.nickname,
				Domain:         recovered.domain,
				Region:         workBuddyRegion(recovered.domain),
				Usable:         false,
				Reason:         "凭据无法解析",
				Disabled:       hostDisabledFor(entry, recovered),
				DisabledByUser: hostDisabledFor(entry, recovered),
			})
			continue
		}

		account := workBuddyAccount{
			credentials:  creds,
			AuthIndex:    entry.AuthIndex,
			Label:        firstNonEmpty(entry.Label, entry.Name, creds.Nickname, creds.UID, entry.AuthIndex),
			UID:          creds.UID,
			Nickname:     creds.Nickname,
			Domain:       creds.Domain,
			Region:       workBuddyRegionForCredentials(creds),
			EnterpriseID: creds.EnterpriseID,
			ExpiresAt:    creds.ExpiresAt,
			Expired:      creds.expired(),
			// The flag lives inside the credential blob; host.auth.list does not surface it
			// as a field of its own, so entry.Disabled is empty for a file-based credential
			// and the blob is what carries the operator's choice.
			Disabled:       hostDisabled(entry, storage),
			DisabledByUser: hostDisabled(entry, storage),
			Usable:         !hostDisabled(entry, storage),
		}
		out = append(out, account)
	}

	// Collapse duplicates.
	//
	// host.auth.list can surface the same credential twice — once from the auth
	// file on disk and once from the runtime index — which showed up as a doubled
	// account row in the panel. A credential is the same when its uid matches, or,
	// when the uid is unavailable, its auth_index does.
	out = dedupeAccounts(out)

	// Stable, useful ordering: most credits first (mirrors A0/s.java:596),
	// then by label for ties.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Credits != out[j].Credits {
			return out[i].Credits > out[j].Credits
		}
		return out[i].Label < out[j].Label
	})
	return out, nil
}

// recoveredIdentity carries whatever identifying fields can be salvaged from a
// credential blob that failed to parse.
type recoveredIdentity struct {
	uid      string
	nickname string
	domain   string
	// disabled is the host's own flag, which lives inside the credential blob rather than
	// beside it — host.auth.list does not surface it as a field of its own.
	disabled bool
	// hasDisabled distinguishes "the file says enabled" from "the file does not say".
	hasDisabled bool
}

// recoverIdentityFromStorage extracts uid/nickname/domain from a blob that
// parseWorkBuddyCredentials rejected.
//
// It works on the raw JSON rather than a struct so that a partial or
// differently-shaped file still yields an identity. The uid in particular is
// what lets dedupeAccounts merge this row with the healthy record for the same
// person; without it a broken file shows up as an extra account forever.
//
// When the body carries no uid, the JWT payload is consulted, mirroring the
// fallback parseWorkBuddyCredentials already performs (a2/b.t()).
func recoverIdentityFromStorage(storage []byte) recoveredIdentity {
	if len(storage) == 0 {
		return recoveredIdentity{}
	}
	var doc map[string]any
	if errUnmarshal := json.Unmarshal(storage, &doc); errUnmarshal != nil {
		return recoveredIdentity{}
	}
	out := recoveredIdentity{
		uid:      pickString(doc, "uid", "userId", "user_id"),
		nickname: pickString(doc, "nickname", "nickName", "name"),
		domain:   pickString(doc, "domain"),
	}
	if raw, present := doc["disabled"]; present {
		out.hasDisabled = true
		out.disabled, _ = raw.(bool)
	}
	if out.uid == "" {
		if token := pickString(doc, "accessToken", "access_token"); token != "" {
			out.uid = jwtClaim(token, "user_id", "userId", "uid", "sub")
		}
	}
	return out
}

// dedupeAccounts collapses repeated credentials for the same person.
//
// The host legitimately surfaces the same uid more than once: the CN
// (copilot.tencent.com) and global (www.workbuddy.ai) endpoints issue separate
// tokens, and a credential can additionally appear both from its auth file and
// from the runtime index. Both cases looked like duplicated rows before.
//
// The surviving row keeps the richest credential but accumulates every region
// and auth index it saw, so nothing about the merged credentials is lost.
func dedupeAccounts(in []workBuddyAccount) []workBuddyAccount {
	if len(in) < 2 {
		return in
	}
	index := make(map[string]int, len(in))
	out := make([]workBuddyAccount, 0, len(in))

	for _, a := range in {
		key := accountIdentity(a)
		if key == "" {
			out = append(out, a)
			continue
		}
		pos, seen := index[key]
		if !seen {
			a.Regions = appendRegion(a.Regions, a.Region)
			a.AuthIndexes = appendAuthIndex(a.AuthIndexes, a.AuthIndex)
			a.CredentialCount = 1
			index[key] = len(out)
			out = append(out, a)
			continue
		}

		// Merge into the existing row: union of regions and auth indexes, and
		// prefer the richer credential body.
		merged := out[pos]
		merged.Regions = appendRegion(merged.Regions, a.Region)
		merged.AuthIndexes = appendAuthIndex(merged.AuthIndexes, a.AuthIndex)
		merged.CredentialCount++
		if accountScore(a) > accountScore(merged) {
			// Keep the identity of the better record but carry over the union.
			regions, indexes, count := merged.Regions, merged.AuthIndexes, merged.CredentialCount
			a.Regions, a.AuthIndexes, a.CredentialCount = regions, indexes, count
			merged = a
		}
		// The row is usable if any of its credentials is.
		merged.Usable = merged.Usable || a.Usable
		if merged.Credits < a.Credits {
			merged.Credits = a.Credits
			merged.CreditsKnown = merged.CreditsKnown || a.CreditsKnown
		}
		merged.CreditsKnown = merged.CreditsKnown || a.CreditsKnown
		// Prefer a real region over the default when the survivor had none.
		if merged.Region == "" && a.Region != "" {
			merged.Region = a.Region
		}
		out[pos] = merged
	}
	return out
}

// appendRegion adds a region to the list once, keeping the canonical order so
// the panel's grouping is stable.
func appendRegion(list []string, region string) []string {
	region = strings.TrimSpace(region)
	if region == "" {
		return list
	}
	for _, existing := range list {
		if existing == region {
			return list
		}
	}
	return append(list, region)
}

// appendAuthIndex adds an auth file name to the list once.
func appendAuthIndex(list []string, index string) []string {
	index = strings.TrimSpace(index)
	if index == "" {
		return list
	}
	for _, existing := range list {
		if existing == index {
			return list
		}
	}
	return append(list, index)
}

// accountIdentity derives the dedupe key: the provider uid when present, else
// the auth index.
func accountIdentity(a workBuddyAccount) string {
	if strings.TrimSpace(a.UID) != "" {
		return "uid:" + strings.TrimSpace(a.UID)
	}
	if strings.TrimSpace(a.AuthIndex) != "" {
		return "idx:" + strings.TrimSpace(a.AuthIndex)
	}
	return ""
}

// accountScore ranks how much useful information a record carries.
func accountScore(a workBuddyAccount) int {
	score := 0
	if a.credentials != nil && a.credentials.AccessToken != "" {
		score += 100
	}
	if a.CreditsKnown {
		score += 10
	}
	if a.Nickname != "" {
		score += 2
	}
	// A label equal to the auth index is the least useful fallback.
	if a.Label != "" && a.Label != a.AuthIndex {
		score += 5
	}
	return score
}

// isWorkBuddyAuthEntry applies the strict provider filter.
// isWorkBuddyAuthEntry reports whether a host auth entry belongs to this plugin.
//
// It accepts the internal key or the display name in either the provider or type
// field, and falls back to the file name for hosts that only expose that. All
// comparisons are case-insensitive because CPA lower-cases the identifier before
// persisting it.
func isWorkBuddyAuthEntry(entry hostAuthEntry) bool {
	// Accept the internal key or the display name, in either field.
	for _, candidate := range []string{entry.Provider, entry.Type} {
		if isWorkBuddyProvider(candidate) {
			return true
		}
	}
	// Some hosts only expose the file name; a "codebuddy-"/"workbuddy-" prefix
	// is ours. Both spellings appear depending on which identifier CPA stored.
	name := strings.ToLower(firstNonEmpty(entry.AuthIndex, entry.Name))
	for _, prefix := range []string{workBuddyProviderKey, workBuddyDisplayNameLower} {
		if strings.HasPrefix(name, prefix+"-") || strings.HasPrefix(name, prefix+"_") {
			return true
		}
	}
	// A bare file name (the auth index itself, no prefix) and an empty provider
	// field reach here. Dropping the entry blind was how a working account went
	// missing: the call list kept showing its runtime auth id because the
	// account table had no row to translate it with. The credential's storage
	// JSON decides — a WorkBuddy token file is unlike anything the other
	// providers store.
	return looksLikeWorkBuddyStorage(entry.StorageJSON)
}

// looksLikeWorkBuddyStorage reports whether a stored credential carries the
// WorkBuddy token shape.
//
// Two signals, because one is not enough: an access token alone appears in other
// providers' files too. Either the full triple (access + refresh + uid) — the shape this
// plugin writes — or the realm field naming one of the WorkBuddy domains is decisive.
// Anything else is another provider's file and stays out of the account table.
func looksLikeWorkBuddyStorage(storage json.RawMessage) bool {
	if len(storage) == 0 {
		return false
	}
	var probe struct {
		AccessToken  *string `json:"accessToken"`
		RefreshToken *string `json:"refreshToken"`
		UID          *string `json:"uid"`
		Domain       *string `json:"domain"`
	}
	if err := json.Unmarshal(storage, &probe); err != nil {
		return false
	}
	if probe.AccessToken != nil && probe.RefreshToken != nil && probe.UID != nil {
		return true
	}
	if probe.Domain == nil {
		return false
	}
	switch variantOfDomain(*probe.Domain) {
	case string(variantCn), string(variantAi):
		return true
	}
	return false
}

// decodeAuthEntries tolerates the shapes host.auth.list may return.
//
// CPA nests the entries under "files" (rpcHostAuthListResponse); the other keys
// are fallbacks for hosts that spell it differently. A bare array is also
// accepted.
func decodeAuthEntries(raw json.RawMessage) []hostAuthEntry {
	if len(raw) == 0 {
		return nil
	}
	var wrapper hostAuthListResponse
	if errUnmarshal := json.Unmarshal(raw, &wrapper); errUnmarshal == nil {
		switch {
		case len(wrapper.Files) > 0:
			return wrapper.Files
		case len(wrapper.Auths) > 0:
			return wrapper.Auths
		case len(wrapper.Items) > 0:
			return wrapper.Items
		}
	}
	var list []hostAuthEntry
	if errUnmarshal := json.Unmarshal(raw, &list); errUnmarshal == nil {
		return list
	}
	return nil
}

// enrichWithRuntime folds in the pool's cooldown state and the latest quota
// reading, so the list shows operational detail without being dependent on it.
// formatModelCooldowns renders per-model parks for the panel, keeping only the
// ones still in force and pairing each with the upstream wording.
func formatModelCooldowns(parks map[string]time.Time, reasons map[string]string) map[string]string {
	if len(parks) == 0 {
		return nil
	}
	now := time.Now()
	out := make(map[string]string, len(parks))
	for model, until := range parks {
		if !now.Before(until) {
			continue
		}
		out[model] = until.Format(time.RFC3339)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func enrichWithRuntime(accounts []workBuddyAccount) []workBuddyAccount {
	for i := range accounts {
		a := &accounts[i]

		// Variant for the UI: the machine key ("cn"/"ai") and its label.
		//
		// Re-derived from the credential or its domain when either is present, because
		// those are the authoritative sources. When neither is — an inventory entry
		// whose storage could not be read — the value already on the record is kept:
		// defaulting to 国内 there relabels an international account as domestic, which
		// is not a cosmetic mistake. The badge decides whether the account is offered
		// growth tasks, and those only exist for domestic accounts.
		switch {
		case a.credentials != nil:
			a.Variant = string(variantForCredentials(a.credentials))
		case strings.TrimSpace(a.Domain) != "":
			a.Variant = string(variantForDomain(a.Domain))
		case strings.TrimSpace(a.Variant) == "":
			// Nothing to go on and nothing recorded: say so rather than guess.
			a.Variant = ""
		}
		a.VariantLabel = wbVariant(a.Variant).label()

		// Quota: look the reading up by every identifier the credential has.
		//
		// The quota writer keys by the auth index it received from
		// host.auth.list, while the executor path keys by the credential's uid
		// (auth.AuthIndex). Looking up only one of them silently loses the
		// figure — the panel then showed credits 0 despite a successful query.
		authKey := ""
		if a.credentials != nil {
			authKey = a.credentials.AuthKey()
		}
		if q, ok := lookupQuotaAny(a.AuthIndex, a.UID, authKey); ok {
			a.Credits = q.Credits
			a.CreditsKnown = q.Known
			a.CreditsExpireAt = q.soonestExpireAt()
			a.CreditsExpiringSoon = q.expiringSoon()
			a.CreditsExpired = q.expired()
			a.CreditPackages = q.Labels
			// The cycle capacity, so the row can show a ratio and a progress bar.
			// Total is a float upstream; the panel works in whole credits.
			a.CreditsTotal = int64(q.Summary.Total)
			a.CreditsUsed = int64(q.Summary.Used)
			// The pool keeps its own copy of the capacity (it is written on every
			// refresh, including the background one, while this lookup only sees the
			// panel's own cache). Take whichever is present.
			if a.CreditsTotal <= 0 {
				if total := state.pool.creditsTotalFor(a.AuthIndex, a.UID); total > 0 {
					a.CreditsTotal = total
					if a.CreditsTotal > a.Credits {
						a.CreditsUsed = a.CreditsTotal - a.Credits
					}
				}
			}
			// A summary with no capacity is a reading, not a budget: leave the
			// total at zero and the row falls back to the bare remainder.
			if a.CreditsTotal <= 0 {
				a.CreditsUsed = 0
			}
		}
		if a.CreditsExpireAt > 0 {
			days := (a.CreditsExpireAt - time.Now().Unix()) / 86400
			if days < 0 {
				days = 0
			}
			a.CreditsExpireDays = days
		}

		// Pool: cooldown / cool kind when this credential has been exercised.
		for _, lane := range state.pool.snapshot() {
			if lane.UID != a.UID && lane.UID != a.AuthIndex {
				continue
			}
			if !a.CreditsKnown && lane.CreditsKnown {
				a.Credits = lane.Credits
				a.CreditsKnown = true
			}
			a.CoolKind = coolKindName(lane.CoolKind)
			if lane.StatusMessage != "" {
				a.Reason = lane.StatusMessage
			}
			a.CooldownUntil = lane.CooldownUntil
			// Per-model parks. A throttle only benches the model that was asked
			// for, so the panel has to show these separately from the
			// account-level cooldown — otherwise an account that is serving every
			// other model still looks unavailable.
			a.ModelCooldowns = formatModelCooldowns(lane.ModelCooldowns, lane.ModelCoolReasons)
			a.LastError = lane.LastError
			a.Failures = lane.Failures
			a.DisabledByUser = lane.DisabledByUser
			a.AutoDisabled = lane.AutoDisabled
			if lane.AutoDisabled {
				a.DisabledReason = firstNonEmpty(lane.DisabledReason, lane.StatusMessage)
			}
			break
		}

		// Usability mirrors A0/s.java:596's guard, plus the reference
		// implementation's rule that an expired balance is not a valid target.
		// A credential flagged with a reason we already set (e.g. an unparsable
		// file) stays unusable.
		//
		// DisabledByUser must be part of this test: the panel toggle only sets
		// the pool lane, so leaving it out made a disabled account keep
		// reporting 可用 in the very column the operator looks at, which read as
		// "禁用没有生效" even though the flag was stored.
		now := time.Now()
		blockedByReason := a.Reason != "" && !a.CreditsKnown && a.UID == "" && a.CoolKind == ""
		a.Usable = !a.Disabled && !a.DisabledByUser && !a.Expired && !a.CreditsExpired &&
			!blockedByReason && (a.CooldownUntil.IsZero() || !now.Before(a.CooldownUntil))
	}
	return accounts
}

// listWorkBuddyAccounts is the entry point used by the UI.
// listWorkBuddyAccounts reads the inventory and mirrors the host's disabled flag.
//
// The mirror happens after the store's lock is released — accounts() returns a copy and
// holds nothing — and before the pool's lock is taken, so neither store is ever entered
// while the other is held. That ordering is the whole point: the pool resolves a lane's
// realm by asking this store for the account table, so a call the other way round from
// inside accounts() met itself and both sides waited.
func listWorkBuddyAccounts() []workBuddyAccount {
	accounts := enrichWithRuntime(state.accounts.accounts())
	state.pool.applyHostDisabledFlags(accounts)
	return accounts
}

// expired reports whether the stored access token is past its expiry, allowing
// a small clock skew.
func (c *workBuddyCredentials) expired() bool {
	if c == nil || c.ExpiresAt <= 0 {
		return false
	}
	return time.Now().After(time.Unix(c.ExpiresAt, 0).Add(60 * time.Second))
}

// refreshAccountsAfterLogin invalidates the inventory cache so a just-finished
// login appears immediately.
func refreshAccountsAfterLogin() {
	state.accounts.invalidate()
}

// accountSummary counts accounts for the header line.
func accountSummary(accounts []workBuddyAccount) (total, usable, known int, totalCredits int64) {
	total = len(accounts)
	for _, a := range accounts {
		if a.Usable {
			usable++
		}
		if a.CreditsKnown {
			known++
			if a.Credits > 0 {
				totalCredits += a.Credits
			}
		}
	}
	return total, usable, known, totalCredits
}

// contextWithTimeout is a small helper shared by the refresh paths.
func contextWithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

// hostDisabled reports whether the host holds this credential as disabled.
//
// The flag is read from the file on disk when its location is known, and from the entry
// and its payload otherwise.
//
// Reading the file matters because the host's own listing is a snapshot: it answers from
// the copy it loaded, so a change written moments ago — by this plugin, when the operator
// disables an account — is not in it yet. Consulting the listing first made that change
// invisible, and the mirror then treated the host's stale "enabled" as the operator's
// intent and undid it.
// resolveAuthPath turns the host's reported location into an absolute one.
//
// The host reports it relative to its own working directory ("auths/x.json"), which is not
// necessarily this process's — the plugin runs inside the host, but a path is only usable
// if it is either absolute or relative to the process that opens it. Resolving against the
// host's directory is what makes the read reliable; without it the file appears
// unreadable and the caller falls back to the host's stale snapshot of it.
func resolveAuthPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		return path
	}
	if base := strings.TrimSpace(hostWorkingDirectory()); base != "" {
		return filepath.Join(base, path)
	}
	return path
}

// hostWorkingDirectory returns the host process's working directory, if it reports one.
var hostWorkingDirectory = sync.OnceValue(func() string {
	if wd, errGet := os.Getwd(); errGet == nil {
		return wd
	}
	return ""
})

// readAuthFile reads a credential file, resolving a relative location first.
func readAuthFile(path string) ([]byte, bool) {
	resolved := resolveAuthPath(path)
	if resolved == "" {
		return nil, false
	}
	raw, errRead := os.ReadFile(resolved)
	if errRead != nil {
		return nil, false
	}
	return raw, true
}

func hostDisabled(entry hostAuthEntry, storage []byte) bool {
	// A write made by this plugin is authoritative for the account table until the host's
	// own listing catches up.
	//
	// The host assembles its listing from what it has loaded, and it reloads on a file
	// watcher — asynchronously. Immediately after a toggle the listing therefore still
	// describes the previous state, and reading it back showed the operator the opposite of
	// what they had just asked for: disable appeared to do nothing, then enable appeared to
	// disable. This map holds what was written until the host publishes the same value.
	for _, key := range disabledKeys(entry) {
		pending, hasPending := pendingDisabled.Load(key)
		if !hasPending {
			continue
		}
		want, _ := pending.(bool)
		if want {
			// Written as disabled. The host may still list it as enabled until its watcher
			// catches up, but the operator's instruction is unambiguous.
			if hostSays, hasHost := hostDisabledFromSources(entry, storage); !hasHost || hostSays {
				if hasHost {
					pendingDisabled.Delete(key)
				}
			}
			logf("disabled read: key=%q pending=true → true", key)
			return true
		}
		pendingDisabled.Delete(key)
		logf("disabled read: key=%q pending=false → false", key)
		return false
	}
	value, hasHost := hostDisabledFromSources(entry, storage)
	logf("disabled read: keys=%v pending=none file=%v hasHost=%v", disabledKeys(entry), value, hasHost)
	return value
}

// hostSays is the second return: whether any source could be consulted at all.
func hostDisabledFromSources(entry hostAuthEntry, storage []byte) (bool, bool) {
	for _, path := range []string{entry.Path, knownAuthFilePath(entry)} {
		if strings.TrimSpace(path) == "" {
			continue
		}
		raw, ok := readAuthFile(path)
		if !ok {
			logf("disabled: cannot open %q", path)
			continue
		}
		recovered := recoverIdentityFromStorage(raw)
		if recovered.hasDisabled {
			return recovered.disabled, true
		}
	}
	if entry.Disabled {
		return true, true
	}
	recovered := recoverIdentityFromStorage(storage)
	if recovered.hasDisabled {
		return recovered.disabled, true
	}
	return false, false
}

// pendingDisabled records what this plugin last wrote for a credential.
var pendingDisabled sync.Map // key → bool

// rememberDisabled records the value just written to a credential's file.
//
// Stored under every identifier the entry carries, because the entry that writes and the
// entry that later reads come from two different host calls — host.auth.list for the write
// path and the account inventory for the read — and they do not necessarily populate the
// same fields. Keying on one field meant the write and the read disagreed about the key,
// the pending value was never found, and the panel showed the host's stale copy.
func rememberDisabled(entry hostAuthEntry, disabled bool) {
	for _, id := range []string{entry.Name, entry.AuthIndex, entry.ID, entry.Path} {
		if id = strings.TrimSpace(id); id != "" {
			pendingDisabled.Store(id, disabled)
		}
	}
}

// disabledKeys enumerates every identifier a credential may be pending under.
func disabledKeys(entry hostAuthEntry) []string {
	var out []string
	for _, id := range []string{entry.Name, entry.AuthIndex, entry.ID, entry.Path} {
		if id = strings.TrimSpace(id); id != "" {
			out = append(out, id)
		}
	}
	return out
}

// hostDisabledFor is hostDisabled for a credential already reduced to its recovered fields.
func hostDisabledFor(entry hostAuthEntry, recovered recoveredIdentity) bool {
	for _, path := range []string{entry.Path, knownAuthFilePath(entry)} {
		if strings.TrimSpace(path) == "" {
			continue
		}
		raw, errRead := os.ReadFile(path)
		if errRead != nil {
			continue
		}
		fresh := recoverIdentityFromStorage(raw)
		if fresh.hasDisabled {
			return fresh.disabled
		}
	}
	if entry.Disabled {
		return true
	}
	return recovered.hasDisabled && recovered.disabled
}

// authPathCache remembers where a credential's file lives.
//
// The location is only discoverable by asking the host to write the file, which is far too
// heavy to do on every read. It does not change in practice — a credential keeps its file
// — so the answer is cached under the identifiers the host may use for it.
var authPathCache sync.Map // identifier → string

// rememberAuthFilePath records a resolved location.
func rememberAuthFilePath(entry hostAuthEntry, path string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	for _, id := range []string{entry.Name, entry.AuthIndex, entry.ID} {
		if id = strings.TrimSpace(id); id != "" {
			authPathCache.Store(id, path)
		}
	}
}

// knownAuthFilePath returns a previously resolved location, if any.
func knownAuthFilePath(entry hostAuthEntry) string {
	for _, id := range []string{entry.Name, entry.AuthIndex, entry.ID} {
		if id = strings.TrimSpace(id); id == "" {
			continue
		}
		if path, ok := authPathCache.Load(id); ok {
			if s, _ := path.(string); s != "" {
				return s
			}
		}
	}
	return ""
}
