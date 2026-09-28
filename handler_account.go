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
	case "enable":
		state.pool.disableAccountKeyed(body.UID, body.AuthIndex, false)
	case "toggle":
		lane, found := state.pool.findAccountKeyedCopy(body.UID, body.AuthIndex)
		if !found {
			state.pool.disableAccountKeyed(body.UID, body.AuthIndex, true)
		} else {
			state.pool.disableAccountKeyed(body.UID, body.AuthIndex, !lane.DisabledByUser)
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
