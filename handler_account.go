package main

// Handlers for the two per-account actions the panel offers: switching the provider
// and toggling an account in or out of rotation.

import (
	"encoding/json"
	"net/http"
	"os"
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

// syncAccountDisabledToHost mirrors a manual disable onto the credential's auth file.
//
// The pool's own flag only governs this plugin's choices. CPA keeps its own list of
// credentials and hands candidates from it, so an account disabled here stayed selectable
// there — the executor could still be asked to use it, and any path that does not run
// through this plugin's pick (a retry, a different provider key) would go straight to it.
//
// The auth file already carries a top-level "disabled" that CPA honours when building its
// candidate list, so writing it is the one change that reaches the host without patching
// it. StorageJSON is rewritten whole, which is how host.auth.save works; the credential's
// own fields are preserved by starting from what the host already holds.
func syncAccountDisabledToHost(uid, authIndex string, disabled bool) {
	entry, storage, errLookup := findAuthEntryForAccount(uid, authIndex)
	if errLookup != nil || entry == nil || len(storage) == 0 {
		logf("disable sync: no auth file for uid=%q index=%q err=%v", uid, authIndex, errLookup)
		return
	}
	logf("disable sync: uid=%q found index=%q name=%q path=%q want=%v",
		uid, entry.AuthIndex, entry.Name, entry.Path, disabled)

	var doc map[string]any
	if errUnmarshal := json.Unmarshal(storage, &doc); errUnmarshal != nil {
		logf("disable sync: %s is not an object: %v", entry.AuthIndex, errUnmarshal)
		return
	}
	// "Already in the requested state" is decided from the file, not from the payload the
	// host handed over.
	//
	// That payload is the host's loaded copy, so it lags every write made since the host
	// read the file — including the one this function made a moment ago. Judging by it made
	// a second toggle in the opposite direction a no-op: the stale copy said "disabled",
	// the caller asked for "enabled", the two disagreed, and the write was skipped,
	// leaving the account in the state the operator had just tried to leave.
	if raw, ok := readAuthFile(entry.Path); ok {
		var onDisk map[string]any
		if errDisk := json.Unmarshal(raw, &onDisk); errDisk == nil {
			if was, _ := onDisk["disabled"].(bool); was == disabled && onDisk["disabled"] != nil {
				rememberDisabled(*entry, disabled)
				return
			}
		}
	}
	doc["disabled"] = disabled

	encoded, errMarshal := json.Marshal(doc)
	if errMarshal != nil {
		logf("disable sync: cannot encode %s: %v", entry.AuthIndex, errMarshal)
		return
	}
	// Written to the file the host reports, not through host.auth.save.
	//
	// That call re-serialises the payload into the host's schema, and a top-level flag
	// beside the credential is dropped on the way through — the call returns a path and
	// the watcher fires, while the file still says disabled:false. Its answer is used for
	// the path only.
	target, errTarget := hostAuthFileFor(*entry, storage)
	if errTarget != nil {
		logf("disable sync: cannot locate %s: %v", entry.AuthIndex, errTarget)
		return
	}
	rememberAuthFilePath(*entry, target)
	rememberDisabled(*entry, disabled)
	if errWrite := os.WriteFile(target, encoded, 0o600); errWrite != nil {
		logf("disable sync: write %s failed: %v", target, errWrite)
		return
	}
	logf("disable sync: %s disabled=%v → %s", entry.AuthIndex, disabled, target)
}

// findAuthEntryForAccount locates the host's auth entry for an account, by uid or by the
// auth index the panel sent.
func findAuthEntryForAccount(uid, authIndex string) (*hostAuthEntry, []byte, error) {
	raw, errList := callHost("host.auth.list", map[string]any{})
	if errList != nil {
		return nil, nil, errList
	}
	entries := decodeAuthEntries(raw)
	wanted := map[string]bool{}
	for _, id := range []string{uid, authIndex, canonicalUID(uid), canonicalUID(authIndex)} {
		if strings.TrimSpace(id) != "" {
			wanted[id] = true
		}
	}
	for i := range entries {
		entry := entries[i]
		if !wanted[entry.AuthIndex] && !wanted[entry.ID] && !wanted[entry.Name] {
			// The panel addresses an account by its uid, while the host's entries are keyed
			// by file name or runtime index — neither contains the uid. The credential
			// payload does, so it is consulted before giving up: without this, a toggle
			// by uid found no entry and the write never happened.
			storage := credentialPayload(entry)
			if len(storage) == 0 {
				continue
			}
			recovered := recoverIdentityFromStorage(storage)
			if !wanted[recovered.uid] && !wanted[canonicalUID(recovered.uid)] {
				continue
			}
			return &entries[i], storage, nil
		}
		// The host reports the file's location, so read it rather than its snapshot.
		storage := credentialPayload(entry)
		if path := strings.TrimSpace(entry.Path); path != "" {
			if raw, errRead := os.ReadFile(path); errRead == nil {
				rememberAuthFilePath(entry, path)
				return &entries[i], raw, nil
			}
		}
		if len(storage) == 0 && entry.AuthIndex != "" {
			storage = fetchAuthStorage(entry.AuthIndex)
		}
		if len(storage) == 0 {
			continue
		}
		return &entries[i], storage, nil
	}
	return nil, nil, nil
}
