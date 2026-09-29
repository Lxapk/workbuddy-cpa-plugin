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
	return managementResponse{
		StatusCode: http.StatusOK,
		Headers:    jsonResponseHeaders(),
		Body:       mustJSON(map[string]any{"ok": true}),
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

	var doc map[string]any
	if errUnmarshal := json.Unmarshal(storage, &doc); errUnmarshal != nil {
		logf("disable sync: %s is not an object: %v", entry.AuthIndex, errUnmarshal)
		return
	}
	if was, _ := doc["disabled"].(bool); was == disabled && doc["disabled"] != nil {
		return // already in the requested state
	}
	doc["disabled"] = disabled

	encoded, errMarshal := json.Marshal(doc)
	if errMarshal != nil {
		logf("disable sync: cannot encode %s: %v", entry.AuthIndex, errMarshal)
		return
	}
	name := entry.AuthIndex
	if !strings.HasSuffix(strings.ToLower(name), ".json") {
		name += ".json"
	}
	if _, errSave := callHost("host.auth.save", map[string]any{
		"name": name,
		"json": json.RawMessage(encoded),
	}); errSave != nil {
		logf("disable sync: save %s failed: %v", name, errSave)
		return
	}
	logf("disable sync: %s disabled=%v", name, disabled)
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
			continue
		}
		storage := entry.StorageJSON
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
