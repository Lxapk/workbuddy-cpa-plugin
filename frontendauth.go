package main

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

// frontendAuth is the port of V1/o.j(Session) from AI 聚合网关 0.1.18.
//
// # IMPORTANT — authentication belongs to CPA, not to this plugin
//
// This plugin does exactly one job: it rewrites CPA's OpenAI-compatible endpoints
// onto the WorkBuddy backend. Authentication is *not* its business, and it must
// never decide that a request is allowed.
//
// The reason is how CPA's authentication chain works: it walks its providers in
// order and stops at the first one that reports success
// (sdk/access/manager.go). Registering a provider that answers
// `Authenticated: true` therefore removes every check behind it — including
// CPA's own `api-keys` list. That is exactly what an earlier version of this
// file did, and it left /v1/* callable with no key at all.
//
// So this function always answers `Authenticated: false`, in every
// configuration. CPA reads that as "this provider does not handle the request"
// (sdk/access/manager.go -> NewNotHandledError, surfaced through
// internal/pluginhost/adapters_auth.go) and continues with its own providers,
// whose verdict is the only one that counts.
//
// There is deliberately no setting that changes this. A plugin that can be
// switched into authorizing requests is a plugin that can be misconfigured into
// disabling the host's authentication, so the ability does not exist at all.
// Configure credentials in CPA.
func frontendAuth(request []byte) ([]byte, error) {
	var req pluginapi.FrontendAuthRequest
	if len(request) > 0 {
		if errUnmarshal := json.Unmarshal(request, &req); errUnmarshal != nil {
			return nil, errUnmarshal
		}
	}

	// Always defer. The only thing reported is why, for the log.
	return deferToHost("鉴权由 CPA 负责")
}

// deferToHost reports that this provider declines to authenticate a request.
//
// `Authenticated:false` becomes sdkaccess.NotHandledError, so the remaining
// access providers — CPA's api-keys among them — still run. Returning a hard 401
// would veto the whole chain, which is not this plugin's call to make either.
func deferToHost(reason string) ([]byte, error) {
	return okEnvelope(pluginapi.FrontendAuthResponse{
		Authenticated: false,
		Metadata:      map[string]string{"workbuddy_auth": "delegated_to_cpa", "workbuddy_note": reason},
	})
}

// isOpenPath mirrors the unauthenticated routes of V1/o.e(Session) in the source
// app. It is informational only: the plugin does not grant access to these paths,
// CPA decides.
func isOpenPath(path string) bool {
	p := strings.TrimSuffix(strings.TrimSpace(path), "/")
	switch p {
	case "/healthz", "/authorize", "":
		return true
	}
	return false
}

// headerValue reads one header, tolerating the non-canonical maps CPA may hand over.
func headerValue(headers http.Header, key string) string {
	if headers == nil {
		return ""
	}
	// http.Header.Get is already case-insensitive.
	if v := headers.Get(key); v != "" {
		return v
	}
	for k, vs := range headers {
		if strings.EqualFold(k, key) && len(vs) > 0 {
			return vs[0]
		}
	}
	return ""
}
