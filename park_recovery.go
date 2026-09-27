package main

// park_recovery.go restores credentials that were briefly disabled so CPA would
// route around a throttle.
//
// A model-level throttle has to be published as an account-level disable, because
// CPA decides which credential to use without ever asking the plugin which models a
// credential can still serve. That works, but it also means an account can be left
// switched off by a write that nothing ever undoes — the same failure mode as a
// permanent cooldown, only worse because it is silent.
//
// Recovery therefore does not depend on the plugin still running: each disable
// carries its own deadline in the credential file, and this pass only has to notice
// that the deadline has passed. A restart, a crash or a missed tick all resolve to
// the same outcome — the credential comes back.

import (
	"encoding/json"
	"strings"
	"sync"
	"time"
)

// parkRecoveryInterval is how often expired disables are undone.
//
// The deadlines are minutes to hours away, so the exact cadence does not matter;
// what matters is that it is far shorter than the shortest deadline, so a
// credential is idle for seconds rather than for the rest of its window.
const parkRecoveryInterval = 30 * time.Second

// parkRecoveryOnce guards against starting more than one recovery loop.
var parkRecoveryOnce sync.Once

// startParkRecovery begins the periodic restore pass.
//
// DISABLED: the account-level park this restores is itself disabled (see
// publishModelPark). Two reasons it must stay off:
//
//  1. CPA rewrites auth files from its own in-memory state, so a flag written by
//     the plugin is reverted — and the revert, the re-write and the restore pass
//     then oscillate. That oscillation unregistered and re-registered the
//     credential on every request, which is worse than the problem it set out to
//     solve: requests failed with "no auth available" even though a healthy
//     credential existed.
//  2. A disabled credential is skipped by CPA entirely, so parking one account for
//     a single model's throttle takes its other models offline too.
//
// Kept as a no-op rather than deleted: the deadline logic is correct and is the
// part a future host interface would reuse.
func startParkRecovery() {
	parkRecoveryOnce.Do(func() {})
}

// parkRecoveryLoop restores expired credential disables on a fixed cadence.
func parkRecoveryLoop() {
	ticker := time.NewTicker(parkRecoveryInterval)
	defer ticker.Stop()
	for range ticker.C {
		// Guarded like the other background loops: an unguarded panic here would
		// kill the goroutine before the select loop starts, and no credential would
		// ever be restored again.
		safely(restoreExpiredParks)
	}
}

// restoreExpiredParks clears the disabled flag on every credential whose park has
// expired.
//
// A credential is skipped when it is disabled without a deadline: that flag was not
// written by this plugin — an operator disabled the account by hand — and undoing it
// would override a deliberate decision.
func restoreExpiredParks() {
	raw, errList := callHost("host.auth.list", map[string]any{})
	if errList != nil || len(raw) == 0 {
		return
	}
	var resp hostAuthListResponse
	if errUnmarshal := json.Unmarshal(raw, &resp); errUnmarshal != nil {
		return
	}
	now := time.Now().UTC()
	for _, entry := range hostAuthListEntries(resp) {
		authIndex := strings.TrimSpace(entry.AuthIndex)
		name := strings.TrimSpace(entry.Name)
		if authIndex == "" || name == "" || !strings.HasSuffix(strings.ToLower(name), ".json") {
			continue
		}

		guard := modelStateMutexFor(authIndex)
		guard.Lock()
		restored := restoreParkIfExpired(authIndex, name, now)
		guard.Unlock()

		if restored {
			logModelState("已到期，恢复账号", authIndex, "", name)
		}
	}
}

// clearExpiredPark removes the disable and the model states from a credential file
// whose deadline has passed.
//
// Pure, so the rule is testable without the host: an operator-set disable (no
// deadline) is left alone, and a live deadline is left alone, because both mean the
// credential is meant to stay out.
func clearExpiredPark(file map[string]json.RawMessage, now time.Time) bool {
	deadline, okDeadline := parkedUntil(file)
	if !okDeadline || deadline.After(now) {
		return false
	}
	delete(file, accountParkField)
	// Model states belong to the same incident. Leaving one parked after the
	// credential is back in rotation would keep CPA away from a model the upstream
	// has already released.
	delete(file, "model_states")
	file["disabled"] = json.RawMessage("false")
	return true
}

// restoreParkIfExpired clears one credential's disable when its deadline has passed.
//
// Reports whether the file was rewritten.
func restoreParkIfExpired(authIndex, name string, now time.Time) bool {
	credential, okCredential := fetchAuthCredential(authIndex)
	storage := credential.credentialJSON()
	if !okCredential || len(storage) == 0 {
		return false
	}
	var file map[string]json.RawMessage
	if errUnmarshal := json.Unmarshal(storage, &file); errUnmarshal != nil || file == nil {
		return false
	}
	if !clearExpiredPark(file, now) {
		return false
	}

	updated, errMarshal := json.Marshal(file)
	if errMarshal != nil {
		return false
	}
	if _, errSave := callHost("host.auth.save", map[string]any{
		"name": name,
		"json": json.RawMessage(updated),
	}); errSave != nil {
		logModelState("恢复失败: "+errSave.Error(), authIndex, "", name)
		return false
	}
	return true
}

// safely runs fn, converting a panic into a logged note.
//
// The background loops must outlive any single failure; a panic that escapes ends
// the goroutine permanently, and a recovery pass that has died is indistinguishable
// from one that has nothing to do.
func safely(fn func()) {
	defer func() {
		if recovered := recover(); recovered != nil {
			_, _ = callHost("host.log", map[string]any{
				"level":   "warn",
				"message": "[model-states] 后台恢复 pass 发生 panic，已忽略",
				"fields":  map[string]any{"panic": recovered},
			})
		}
	}()
	fn()
}
