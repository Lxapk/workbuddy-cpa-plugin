package main

// The combined page's management endpoints.
//
// One handler covers the page itself plus the JSON the page talks to, because they
// share the same prefix and the same authentication; splitting them would only add
// indirection.

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

// handleMainRequest serves the combined page and its JSON endpoints.
func handleMainRequest(req pluginapi.ManagementRequest) (managementResponse, bool) {
	path := normaliseManagementPath(req.Path)
	method := strings.ToUpper(strings.TrimSpace(req.Method))

	switch path {
	case "", "/", "/home", "/dashboard":
		return managementResponse{
			StatusCode: http.StatusOK,
			Headers:    htmlResponseHeaders(),
			Body:       []byte(renderMainPage()),
		}, true

	case "/accounts":
		accounts := listWorkBuddyAccounts()
		total, usable, known, credits := accountSummary(accounts)
		return managementResponse{
			StatusCode: http.StatusOK,
			Headers:    jsonResponseHeaders(),
			Body: mustJSON(map[string]any{
				"accounts":      accounts,
				"total":         total,
				"usable":        usable,
				"credits_known": known,
				"total_credits": credits,
				"fetched_at":    time.Now().Format(time.RFC3339),
				"warning":       state.accounts.lastError(),
				"table_html":    renderAccountTable(accounts),
				"summary_html":  renderAccountSummary(accounts),
			}),
		}, true

	case "/accounts/table":
		// Just the table, for a panel that wants to repaint it in place.
		accounts := listWorkBuddyAccounts()
		return managementResponse{
			StatusCode: http.StatusOK,
			Headers:    jsonResponseHeaders(),
			Body: mustJSON(map[string]any{
				"table_html":   renderAccountTable(accounts),
				"summary_html": renderAccountSummary(accounts),
			}),
		}, true

	case "/calls/clear":
		if method != http.MethodPost {
			return managementResponse{
				StatusCode: http.StatusMethodNotAllowed,
				Headers:    jsonResponseHeaders(),
				Body:       mustJSON(map[string]any{"error": "POST required"}),
			}, true
		}
		// The call records and the figures derived from them. Clearing the ring without
		// the counters would leave the page reporting totals for a list that is now empty.
		removed := state.log.clearCalls()
		return managementResponse{
			StatusCode: http.StatusOK,
			Headers:    jsonResponseHeaders(),
			Body:       mustJSON(map[string]any{"ok": true, "removed": removed}),
		}, true

	case "/log/clear":
		if method != http.MethodPost {
			return managementResponse{
				StatusCode: http.StatusMethodNotAllowed,
				Headers:    jsonResponseHeaders(),
				Body:       mustJSON(map[string]any{"error": "POST required"}),
			}, true
		}
		// The request log only. The call records have their own endpoint now, because the
		// button's scope follows the tab: on 调用记录 it clears calls, on 请求日志 it clears
		// this. The log is assembled from four sources, so all four go — otherwise pressing
		// the button left the page looking unchanged because the entries the operator was
		// reading came from a different store.
		removed := state.log.clearNotices()
		removed += state.pool.clearAutoDisableHistory()
		removed += state.checkin.clearHistory()
		removed += state.growth.clearHistory()
		return managementResponse{
			StatusCode: http.StatusOK,
			Headers:    jsonResponseHeaders(),
			Body:       mustJSON(map[string]any{"ok": true, "removed": removed}),
		}, true

	case "/debug":
		// Reports whether verbose logging is on, and emits one line so the operator can
		// confirm the host actually relays it.
		//
		// The setting is read from the plugin's config section, which CPA flattens from
		// the instance YAML; without this endpoint there is no way to tell "the setting did
		// not arrive" from "the setting arrived but the log callback is not wired".
		gateway := state.settings.get()
		logf("debug probe: debug=%v routing=%s variant=%q",
			gateway.Debug, gateway.Routing.Strategy, gateway.VariantOverride)
		return managementResponse{
			StatusCode: http.StatusOK,
			Headers:    jsonResponseHeaders(),
			Body: mustJSON(map[string]any{
				"ok":               true,
				"debug":            gateway.Debug,
				"variant_override": gateway.VariantOverride,
				"routing":          gateway.Routing.Strategy,
			}),
		}, true

	case "/models":
		return handleModelsRequest(req)

	case "/routing/status":
		return managementResponse{
			StatusCode: http.StatusOK,
			Headers:    jsonResponseHeaders(),
			Body: mustJSON(map[string]any{
				"routing":   routingStatusJSON(),
				"scheduler": schedulerSnapshot(),
			}),
		}, true

	case "/variant":
		return handleVariantRequest(req)

	case "/account/toggle":
		return handleAccountToggleRequest(req)

	case "/growth/tasks":
		return handleGrowthTasksRequest(req), true

	case "/growth/summary":
		return handleGrowthTasksRequest(req), true

	case "/growth/schedule":
		if method == http.MethodPost {
			var body struct {
				Enabled *bool `json:"enabled"`
				Hour    *int  `json:"hour"`
				Minute  *int  `json:"minute"`
				OnStart *bool `json:"on_start"`
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
			// Merge onto the current values so a partial body is a valid update.
			cfg := state.settings.get().Growth
			if body.Enabled != nil {
				cfg.Enabled = *body.Enabled
			}
			if body.Hour != nil {
				cfg.Hour = *body.Hour
			}
			if body.Minute != nil {
				cfg.Minute = *body.Minute
			}
			if body.OnStart != nil {
				cfg.OnStart = *body.OnStart
			}
			cfg = normalizeGrowthSettings(cfg)
			state.settings.setGrowth(cfg)

			// Start the loop the first time it is switched on, so it exists in this
			// process without needing a restart.
			if cfg.Enabled {
				startGrowthScheduler()
			}
			return managementResponse{
				StatusCode: http.StatusOK,
				Headers:    jsonResponseHeaders(),
				Body:       mustJSON(map[string]any{"ok": true, "schedule": growthScheduleSnapshot()}),
			}, true
		}
		return managementResponse{
			StatusCode: http.StatusOK,
			Headers:    jsonResponseHeaders(),
			Body:       mustJSON(map[string]any{"schedule": growthScheduleSnapshot()}),
		}, true
	}

	if method == http.MethodPost {
		switch path {
		case "/routing/config":
			var body struct {
				Strategy string `json:"strategy"`
				Routing  *struct {
					Strategy string `json:"strategy"`
				} `json:"routing"`
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
			value := body.Strategy
			if value == "" && body.Routing != nil {
				value = body.Routing.Strategy
			}
			applied := applyRoutingConfig(routingSettings{Strategy: normalizeStrategy(value)})
			return managementResponse{
				StatusCode: http.StatusOK,
				Headers:    jsonResponseHeaders(),
				Body: mustJSON(map[string]any{
					"ok":      true,
					"routing": routingStatusJSON(),
					"applied": string(applied.Strategy),
				}),
			}, true

		case "/routing/reset":
			state.scheduler.resetCursor()
			return managementResponse{
				StatusCode: http.StatusOK,
				Headers:    jsonResponseHeaders(),
				Body:       mustJSON(map[string]any{"ok": true, "routing": routingStatusJSON()}),
			}, true

		case "/run":
			// Combined action: check in, then refresh credits.
			//
			// The growth pass is opt-in ("growth": true) because it is far slower
			// than the other two stages: a bare /run keeps the quick behaviour the
			// existing buttons rely on.
			var runBody struct {
				Growth bool `json:"growth"`
			}
			if len(req.Body) > 0 {
				_ = json.Unmarshal(req.Body, &runBody)
			}
			run := runFromManagement()
			results, errQuota := runQuotaRefresh("manual")
			refreshAccountsAfterLogin()
			payload := map[string]any{"checkin": run}
			if errQuota == nil {
				payload["quota"] = results
			} else {
				payload["quota_error"] = errQuota.Error()
			}
			if runBody.Growth {
				payload["growth"] = runGrowthForAll()
			}
			return managementResponse{
				StatusCode: http.StatusOK,
				Headers:    jsonResponseHeaders(),
				Body:       mustJSON(payload),
			}, true

		case "/growth/run":
			return handleGrowthRunRequest(req), true

		case "/growth/travel":
			return handleGrowthTravelRequest(req), true
		}
	}

	return managementResponse{}, false
}
