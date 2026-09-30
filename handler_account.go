package main

// Handlers for the two per-account actions the panel offers: switching the provider
// and toggling an account in or out of rotation.

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

func handleVariantRequest(req pluginapi.ManagementRequest) (managementResponse, bool) {
	method := strings.ToUpper(strings.TrimSpace(req.Method))

	// GET reports the current selections; the panel reads them on load.
	if method == http.MethodGet {
		current := state.settings.get()
		return managementResponse{
			StatusCode: http.StatusOK,
			Headers:    jsonResponseHeaders(),
			Body: mustJSON(map[string]any{
				"ok":             true,
				"variant":        current.VariantOverride,
				"label":          callScopeLabel(current.VariantOverride),
				"auth_supplier":  current.AuthSupplier,
				"auth_label":     authSupplierLabel(current),
				"auth_effective": string(current.authSupplierOrDefault()),
			}),
		}, true
	}

	if method != http.MethodPost {
		return managementResponse{StatusCode: http.StatusMethodNotAllowed}, true
	}

	var body struct {
		Variant      *string `json:"variant"`
		AuthSupplier *string `json:"auth_supplier"`
	}
	if len(req.Body) > 0 {
		if errUnmarshal := json.Unmarshal(req.Body, &body); errUnmarshal != nil {
			return managementResponse{
				StatusCode: http.StatusBadRequest,
				Headers:    jsonResponseHeaders(),
				Body:       mustJSON(map[string]any{"error": errUnmarshal.Error()}),
			}, true
		}
	}

	// A field the caller omitted keeps its current value: the panel sends only
	// the switch the operator touched.
	if body.Variant != nil {
		v := strings.TrimSpace(*body.Variant)
		if !validVariantChoice(v) {
			v = ""
		}
		state.settings.setVariantOverride(v)
	}
	if body.AuthSupplier != nil {
		v := strings.TrimSpace(*body.AuthSupplier)
		if !validVariantChoice(v) {
			v = ""
		}
		state.settings.setAuthSupplier(v)
	}

	current := state.settings.get()
	return managementResponse{
		StatusCode: http.StatusOK,
		Headers:    jsonResponseHeaders(),
		Body: mustJSON(map[string]any{
			"ok":             true,
			"variant":        current.VariantOverride,
			"label":          callScopeLabel(current.VariantOverride),
			"auth_supplier":  current.AuthSupplier,
			"auth_label":     authSupplierLabel(current),
			"auth_effective": string(current.authSupplierOrDefault()),
		}),
	}, true
}

func handleAccountToggleRequest(req pluginapi.ManagementRequest) (managementResponse, bool) {
	if method := strings.ToUpper(strings.TrimSpace(req.Method)); method != http.MethodPost {
		return managementResponse{StatusCode: http.StatusMethodNotAllowed}, true
	}
	var body struct {
		UID       string `json:"uid"`
		AuthIndex string `json:"auth_index"`
		Action    string `json:"action"`
		Disabled  bool   `json:"disabled"`
	}
	if len(req.Body) > 0 {
		if errUnmarshal := json.Unmarshal(req.Body, &body); errUnmarshal != nil {
			return managementResponse{
				StatusCode: http.StatusBadRequest,
				Headers:    jsonResponseHeaders(),
				Body:       mustJSON(map[string]any{"error": errUnmarshal.Error()}),
			}, true
		}
	}
	if body.UID == "" && body.AuthIndex == "" {
		return managementResponse{
			StatusCode: http.StatusBadRequest,
			Headers:    jsonResponseHeaders(),
			Body:       mustJSON(map[string]any{"error": "缺少 uid"}),
		}, true
	}
	switch body.Action {
	case "disable":
		// The panel only sends uid/auth_index/action — never "disabled" — so
		// the disable branch must force-disable. Passing body.Disabled through
		// would silently no-op (false default) and leave the account enabled:
		// the "账号禁用没解开" report.
		state.pool.disableAccountKeyed(body.UID, body.AuthIndex, true)
		syncAccountDisabledToHost(body.UID, body.AuthIndex, true)
	case "enable":
		state.pool.disableAccountKeyed(body.UID, body.AuthIndex, false)
		syncAccountDisabledToHost(body.UID, body.AuthIndex, false)
	case "toggle":
		lane, found := state.pool.findAccountKeyedCopy(body.UID, body.AuthIndex)
		if !found {
			state.pool.disableAccountKeyed(body.UID, body.AuthIndex, true)
			syncAccountDisabledToHost(body.UID, body.AuthIndex, true)
		} else {
			next := !lane.DisabledByUser
			state.pool.disableAccountKeyed(body.UID, body.AuthIndex, next)
			syncAccountDisabledToHost(body.UID, body.AuthIndex, next)
		}
	default:
		return managementResponse{
			StatusCode: http.StatusBadRequest,
			Headers:    jsonResponseHeaders(),
			Body:       mustJSON(map[string]any{"error": "未知操作"}),
		}, true
	}
	// The repainted table and stat cards travel back with the acknowledgement.
	//
	// The panel repaints from them instead of reloading, so the row's button flips on the
	// spot. A reload raced the plugin's own state — the page came back before the switch it
	// had just written showed up in the data it renders — and the button then displayed the
	// state the operator had just left.
	accounts := listWorkBuddyAccounts()
	// The inventory is rebuilt from the host's listing, which lags the write we just made —
	// so the row would come back describing the state the operator just left, and the
	// repainted button would point the wrong way. Apply the value that was just written
	// before rendering.
	accounts = applyPendingDisabled(accounts)
	return managementResponse{
		StatusCode: http.StatusOK,
		Headers:    jsonResponseHeaders(),
		Body: mustJSON(map[string]any{
			"ok":           true,
			"count":        len(accounts),
			"table_html":   renderAccountTable(accounts),
			"summary_html": renderAccountSummary(accounts),
		}),
	}, true
}

func callScopeLabel(v string) string {
	switch v {
	case "cn":
		return "仅国内供应商"
	case "ai":
		return "仅国际供应商"
	}
	return "全部供应商"
}

func authSupplierLabel(g gatewaySettings) string {
	switch g.AuthSupplier {
	case "cn":
		return "国内授权"
	case "ai":
		return "国际授权"
	}
	switch g.VariantOverride {
	case "cn":
		return "跟随调用设置（国内授权）"
	case "ai":
		return "跟随调用设置（国际授权）"
	}
	return "跟随调用设置（默认国内授权）"
}

// syncAccountDisabledToHost writes the operator's enable/disable onto the credential's auth
// file — the top-level "disabled" CPA reads when building its own candidate list.
//
// Every file that belongs to the account is updated: one person can have several credentials
// (a re-login, both realms), and leaving one of them enabled would keep the account routable
// at the host. Only that key changes, read fresh from disk (see setAuthFileDisabled), and the
// value is remembered for the panel only after the write succeeded.
//
// A manual choice also takes the file out of the supplier switch's hands: 自动 must not undo
// it later.
func syncAccountDisabledToHost(uid, authIndex string, disabled bool) {
	// The account table caches the host's listing for a few seconds; a read right after
	// this write must see the new flag, not the cached one.
	defer state.accounts.invalidate()
	entries, errLookup := findAuthEntriesForAccount(uid, authIndex)
	if errLookup != nil || len(entries) == 0 {
		logf("disable sync: no auth file for uid=%q index=%q err=%v", uid, authIndex, errLookup)
		return
	}
	for _, entry := range entries {
		path := strings.TrimSpace(entry.Path)
		if path == "" {
			logf("disable sync: host reported no path for %s", entry.AuthIndex)
			continue
		}
		changed, errWrite := setAuthFileDisabled(path, disabled)
		if errWrite != nil {
			logf("disable sync: write %s failed: %v", path, errWrite)
			continue
		}
		rememberDisabled(entry, disabled)
		regionHold.release(path)
		if changed {
			logf("disable sync: %s disabled=%v", path, disabled)
		}
	}
}

// findAuthEntriesForAccount returns the host's WorkBuddy auth entries that belong to an
// account, matched by the identifiers the panel sent or by the uid inside the credential.
//
// Entries of other providers are skipped before anything is read, so a toggle never fetches
// payloads it has no use for.
func findAuthEntriesForAccount(uid, authIndex string) ([]hostAuthEntry, error) {
	entries := listHostAuthEntries()
	if entries == nil {
		if _, errList := callHost("host.auth.list", map[string]any{}); errList != nil {
			return nil, errList
		}
	}
	wanted := map[string]bool{}
	for _, id := range []string{uid, authIndex} {
		if id = strings.TrimSpace(id); id != "" {
			wanted[id] = true
		}
	}
	var out []hostAuthEntry
	for _, entry := range entries {
		if !isWorkBuddyAuthEntry(entry) {
			continue
		}
		matched := false
		for _, id := range authEntryKeys(entry) {
			if wanted[id] {
				matched = true
				break
			}
		}
		if !matched {
			// The panel addresses an account by its uid, which none of the entry's own
			// identifiers carry; the credential does.
			if payload := currentPayload(entry); len(payload) > 0 {
				matched = wanted[recoverIdentityFromStorage(payload).uid]
			}
		}
		if matched {
			out = append(out, entry)
		}
	}
	return out, nil
}

// currentPayload returns a credential's content: the file when the host reported one, the
// host's copy otherwise.
func currentPayload(entry hostAuthEntry) []byte {
	if raw, ok := readAuthFile(entry.Path); ok {
		return raw
	}
	if len(entry.StorageJSON) > 0 {
		return entry.StorageJSON
	}
	if strings.TrimSpace(entry.AuthIndex) != "" {
		return fetchAuthStorage(entry.AuthIndex)
	}
	return nil
}
