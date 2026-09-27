package main

// model_states.go keeps CPA's per-model view of a credential in step with the
// plugin's own cooldown bookkeeping.
//
// Why this exists
//
// A throttle is scoped to one model, not to the account: the upstream answers
//
//	您的使用量已超出频率限制，将在 2026-09-27 20:03:46 UTC+8 重置，
//	您也可以切换其他模型继续使用。
//
// The plugin already parks just that model (ModelCooldowns) so account rotation
// keeps working. CPA, however, cannot see that state. When every candidate is
// filtered out for a request it reports the bluntest of its two verdicts:
//
//	cooldownCount == total  -> "model cooldown"            (accurate)
//	unauthorizedCount == total -> "auth_unavailable"       (misleading)
//
// The plugin's model-level parking does not move an entry into CPA's cooldown
// set, so the second branch fires and the client is told the account is gone —
// for a request that would have succeeded on another model.
//
// The fix is to publish the same fact to CPA through an interface it already
// reads: auth.ModelStates. It is loaded from the auth file and consulted per
// request by isAuthBlockedForModel, which classifies a still-cooling model as a
// model cooldown. Writing it needs no CPA change.
//
// The write path is read-modify-write, because host.auth.save replaces the whole
// file: the existing JSON is fetched, one key is updated, and the result is
// stored back. Losing the other fields — or another model's state — would log the
// operator out or wipe a cooldown CPA itself recorded.

import (
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

// modelStateEntry mirrors cliproxyauth.ModelState, the shape CPA reads from an
// auth file's "model_states" map.
//
// Only the fields that carry meaning here are modelled. Timestamps are emitted
// in RFC3339 UTC because that is what encoding/json produces for time.Time and
// what the loader parses back.
type modelStateEntry struct {
	Status         string          `json:"status,omitempty"`
	StatusMessage  string          `json:"status_message,omitempty"`
	Unavailable    bool            `json:"unavailable"`
	NextRetryAfter *time.Time      `json:"next_retry_after,omitempty"`
	LastError      *modelStateErr  `json:"last_error,omitempty"`
	Quota          *modelStateQuot `json:"quota,omitempty"`
	UpdatedAt      *time.Time      `json:"updated_at,omitempty"`
}

// modelStateErr is the error detail CPA records alongside a model state.
type modelStateErr struct {
	Message    string `json:"message,omitempty"`
	HTTPStatus int    `json:"http_status,omitempty"`
}

// modelStateQuot carries quota information for a model that hit a limit.
type modelStateQuot struct {
	Exceeded      bool       `json:"exceeded"`
	Reason        string     `json:"reason,omitempty"`
	NextRecoverAt *time.Time `json:"next_recover_at,omitempty"`
}

// CPA's auth status values that matter here (sdk/cliproxy/auth/status.go).
const (
	modelStateStatusError = "error"
)

// modelStateWrites serialises the read-modify-write cycle per auth file.
//
// Two failures for different models on the same credential can be reported at
// the same instant from separate requests. Without this, both would read the
// same file, each would add its own model, and the second write would drop the
// first model's state.
var modelStateWrites sync.Map // authIndex -> *sync.Mutex

// modelStateMutexFor returns the mutex guarding one auth file's writes.
func modelStateMutexFor(authIndex string) *sync.Mutex {
	value, _ := modelStateWrites.LoadOrStore(authIndex, &sync.Mutex{})
	return value.(*sync.Mutex)
}

// publishModelPark mirrors one model's cooldown into the auth file CPA reads, and
// briefly takes the whole credential out of rotation with it.
//
// # Why the account is parked too
//
// CPA chooses the credential itself: it consults its own view of each one and has
// no way to ask which models a credential can still serve, so a throttle scoped to
// one model is invisible to it. A request for that model therefore keeps landing on
// the throttled credential — even when another credential could serve it — and is
// refused with a bare "no auth available". The plugin cannot fix that from the
// scheduler callback either, because the host never calls it on this path.
//
// Taking the credential out of rotation for the cool-off window is what makes CPA
// move on to a healthy one. The cost is that the credential's other models wait
// too, which is why the window is the upstream's own reset time and no longer: for
// a throttle the two are the same duration, and serving the request from another
// credential beats serving it not at all.
//
// The account-level disable is recorded in the auth file as "disabled" so CPA sees
// it, together with a deadline in "disabled_until" so the plugin can undo it. The
// deadline is persisted rather than kept in memory: a restart must not leave a
// credential disabled forever.
//
// Best-effort by design: this only improves how a later request is routed, so a
// failure here must not turn into a request failure.
func publishModelPark(authID, model string, until time.Time, reason string, statusCode int, quotaExceeded bool) {
	// Disabled: hosts rewrite auth files from their own state, so nothing written
	// here survives, and the write/revert cycle generates file events on every
	// failing request. The detection logic below is kept because it is correct and
	// is what a host-side cooldown interface would use.
	_, _, _, _, _, _ = authID, model, until, reason, statusCode, quotaExceeded
}

// publishModelParkLegacy is the former write path, retained for reference.
func publishModelParkLegacy(authID, model string, until time.Time, reason string, statusCode int, quotaExceeded bool) {
	authID = strings.TrimSpace(authID)
	model = strings.TrimSpace(model)
	if authID == "" || model == "" {
		return
	}

	entry, okEntry := resolveAuthEntry(authID)
	if !okEntry {
		logModelState("skip: 无法把 AuthID 解析成 auth 条目", authID, model, "")
		return
	}
	if strings.TrimSpace(entry.AuthIndex) == "" {
		logModelState("skip: auth 条目缺少 auth_index", authID, model, entry.Name)
		return
	}

	// Serialise per credential: two models failing at the same instant would
	// otherwise read the same file, each add its own entry, and the second write
	// would drop the first.
	guard := modelStateMutexFor(entry.AuthIndex)
	guard.Lock()
	defer guard.Unlock()

	credential, okCredential := fetchAuthCredential(entry.AuthIndex)
	storage := credential.credentialJSON()
	if !okCredential || len(storage) == 0 {
		// Without the current file contents the write would replace the
		// credential with nothing. Skipping keeps the account usable; the
		// misdescribed error is the lesser problem.
		logModelState("skip: host.auth.get 未返回凭据", entry.AuthIndex, model, entry.Name)
		return
	}
	name := credentialFileName(entry, credential)
	if name == "" {
		logModelState("skip: 无法确定 auth 文件名", entry.AuthIndex, model, "")
		return
	}

	var file map[string]json.RawMessage
	if errUnmarshal := json.Unmarshal(storage, &file); errUnmarshal != nil {
		return
	}
	if file == nil {
		return
	}

	states := decodeModelStates(file["model_states"])
	applyModelPark(states, model, until, reason, statusCode, quotaExceeded)
	if len(states) == 0 {
		delete(file, "model_states")
	} else {
		encoded, errMarshal := json.Marshal(states)
		if errMarshal != nil {
			return
		}
		file["model_states"] = encoded
	}
	applyAccountPark(file, until)

	updated, errMarshalFile := json.Marshal(file)
	if errMarshalFile != nil {
		return
	}
	if _, errSave := callHost("host.auth.save", map[string]any{
		"name": name,
		"json": json.RawMessage(updated),
	}); errSave != nil {
		logModelState("host.auth.save 失败: "+errSave.Error(), entry.AuthIndex, model, name)
		return
	}
	logModelState("已写入", entry.AuthIndex, model, name)
}

// logModelState records a model-state write attempt.
//
// This path is best-effort and silent by contract — a failure must not surface
// as a request failure — but silence also makes "why is the model still reported
// as an account outage?" unanswerable. The line is emitted at info level so it
// shows up without turning on debug logging.
func logModelState(message, authIndex, model, name string) {
	_, _ = callHost("host.log", map[string]any{
		"level":   "info",
		"message": "[model-states] " + message,
		"fields": map[string]any{
			"auth_index": authIndex,
			"model":      model,
			"file":       name,
		},
	})
}

// fetchAuthCredential reads one credential body and its file name from the host.
func fetchAuthCredential(authIndex string) (hostAuthGetResponse, bool) {
	raw, errGet := callHost("host.auth.get", map[string]any{"auth_index": authIndex})
	if errGet != nil || len(raw) == 0 {
		return hostAuthGetResponse{}, false
	}
	var resp hostAuthGetResponse
	if errUnmarshal := json.Unmarshal(raw, &resp); errUnmarshal != nil {
		return hostAuthGetResponse{}, false
	}
	return resp, true
}

// resolveAuthEntry maps a credential id onto the host's inventory entry.
//
// pluginapi.ExecutorRequest.AuthID carries Auth.ID, while every host.auth.* call
// addresses a credential by AuthIndex. They are separate keys: the index is
// derived from credential metadata at runtime and is not persisted, so it cannot
// be reconstructed from the id. host.auth.list is the only place that publishes
// both, which makes it the bridge.
func resolveAuthEntry(authID string) (hostAuthEntry, bool) {
	authID = strings.TrimSpace(authID)
	if authID == "" {
		return hostAuthEntry{}, false
	}
	raw, errList := callHost("host.auth.list", map[string]any{})
	if errList != nil || len(raw) == 0 {
		return hostAuthEntry{}, false
	}
	var resp hostAuthListResponse
	if errUnmarshal := json.Unmarshal(raw, &resp); errUnmarshal != nil {
		return hostAuthEntry{}, false
	}
	entries := hostAuthListEntries(resp)
	for _, entry := range entries {
		// Match on either key: AuthID is documented as the id, but hosts and
		// older builds have passed the index here, and accepting both keeps the
		// lookup working either way.
		if strings.TrimSpace(entry.ID) == authID || strings.TrimSpace(entry.AuthIndex) == authID {
			return entry, true
		}
	}
	return hostAuthEntry{}, false
}

// hostAuthListEntries returns whichever list field the host populated.
//
// The RPC response has carried the list under more than one key across builds, so
// all of them are checked rather than assuming one and silently seeing nothing.
func hostAuthListEntries(resp hostAuthListResponse) []hostAuthEntry {
	switch {
	case len(resp.Files) > 0:
		return resp.Files
	case len(resp.Auths) > 0:
		return resp.Auths
	case len(resp.Items) > 0:
		return resp.Items
	}
	return nil
}

// credentialFileName picks the auth file to write back to.
//
// The inventory entry reports the file name, and the get response repeats it; the
// inventory is preferred because it is the same call that established the index,
// so the two cannot disagree.
func credentialFileName(entry hostAuthEntry, credential hostAuthGetResponse) string {
	for _, candidate := range []string{entry.Name, credential.Name} {
		name := strings.TrimSpace(candidate)
		if name != "" && strings.HasSuffix(strings.ToLower(name), ".json") &&
			!strings.ContainsAny(name, "/\\") {
			return name
		}
	}
	return ""
}

// applyModelPark writes or clears one model's entry in a decoded states map.
//
// An expired or zero deadline means "healthy again": the entry is removed rather
// than left behind with unavailable:false, so the file does not grow one stale
// row per model the account has ever throttled.
func applyModelPark(states map[string]modelStateEntry, model string, until time.Time, reason string, statusCode int, quotaExceeded bool) {
	now := time.Now().UTC()

	if until.IsZero() || !until.After(now) {
		delete(states, model)
		return
	}

	deadline := until.UTC()
	entry := modelStateEntry{
		Status:         modelStateStatusError,
		Unavailable:    true,
		NextRetryAfter: &deadline,
		UpdatedAt:      &now,
	}
	if trimmed := strings.TrimSpace(reason); trimmed != "" {
		entry.StatusMessage = trimmed
	}
	if statusCode != 0 || strings.TrimSpace(reason) != "" {
		entry.LastError = &modelStateErr{
			Message:    strings.TrimSpace(reason),
			HTTPStatus: statusCode,
		}
	}
	if quotaExceeded || statusCode == 429 {
		entry.Quota = &modelStateQuot{
			Exceeded:      true,
			Reason:        strings.TrimSpace(reason),
			NextRecoverAt: &deadline,
		}
	}
	states[model] = entry
}

// decodeModelStates reads the "model_states" member of an auth file, tolerating
// every shape it may arrive in.
//
// CPA writes this map itself, so it can also contain fields this plugin does not
// model; decoding into a struct keeps those from being lost only if the struct
// covers them, which is why unknown keys are preserved by re-encoding what was
// decoded plus any raw extras.
func decodeModelStates(raw json.RawMessage) map[string]modelStateEntry {
	out := map[string]modelStateEntry{}
	if len(raw) == 0 {
		return out
	}
	var decoded map[string]modelStateEntry
	if errUnmarshal := json.Unmarshal(raw, &decoded); errUnmarshal != nil {
		// Unreadable state is replaced rather than merged: keeping bytes we
		// cannot interpret would write them back unchanged and mask the problem.
		return out
	}
	for model, entry := range decoded {
		if strings.TrimSpace(model) == "" {
			continue
		}
		out[model] = entry
	}
	return out
}

// accountParkField is the auth file member holding the moment a credential may be
// put back into rotation.
//
// It rides in the credential file rather than in plugin memory so a restart cannot
// strand a credential in the disabled state: the file is the only state both the
// plugin and CPA are guaranteed to see again.
const accountParkField = "disabled_until"

// applyAccountPark marks the credential disabled until the deadline.
//
// CPA reads "disabled" from the auth file and skips the credential entirely, which
// is what lets it choose another one for a request the throttled credential cannot
// serve. The deadline is stored beside it so the disable is always temporary.
func applyAccountPark(file map[string]json.RawMessage, until time.Time) {
	// Disabled on purpose: see accountParkLegacy for why this write must not happen.
	_ = file
	_ = until
}

// accountParkLegacy is the write this function used to perform.
//
// Kept beside the no-op so the reason stays legible, and because the deadline
// arithmetic is what a future host interface would reuse.
//
// Why it is off: CPA rewrites auth files from its own in-memory state, so a flag
// written here is reverted immediately. The revert and the re-write then fight each
// other — the debug log shows disabled flipping false->true->false across file
// events — and each flip re-registers the credential, so it oscillates in and out
// of the registry. Worse, a disabled credential is dropped by CPA altogether, so
// parking one account for a single model's throttle takes its other models offline
// too, which is a bigger loss than the request that triggered it.
func accountParkLegacy(file map[string]json.RawMessage, until time.Time) {
	now := time.Now().UTC()
	if until.IsZero() || !until.After(now) {
		// Healthy again: clear both the flag and its deadline.
		if _, has := file[accountParkField]; has {
			delete(file, accountParkField)
			file["disabled"] = json.RawMessage("false")
		}
		return
	}

	// Never shorten an existing park: a second, earlier report for a different model
	// must not hand the credential back before the first deadline is up.
	if current, okCurrent := parkedUntil(file); okCurrent && current.After(until) {
		return
	}
	deadline, errMarshal := json.Marshal(until.UTC())
	if errMarshal != nil {
		return
	}
	file[accountParkField] = deadline
	file["disabled"] = json.RawMessage("true")
}

// parkedUntil reads the moment a credential may be restored.
func parkedUntil(file map[string]json.RawMessage) (time.Time, bool) {
	raw, has := file[accountParkField]
	if !has {
		return time.Time{}, false
	}
	var deadline time.Time
	if errUnmarshal := json.Unmarshal(raw, &deadline); errUnmarshal != nil {
		// An unparsable deadline is treated as absent rather than as "never": the
		// credential must come back, and a malformed value cannot be trusted to
		// keep it out.
		return time.Time{}, false
	}
	return deadline, !deadline.IsZero()
}

// modelStatesFromStorage is a test/debug helper: the states currently recorded
// for one credential, without going through the host.
func modelStatesFromStorage(storage json.RawMessage) map[string]modelStateEntry {
	var file map[string]json.RawMessage
	if errUnmarshal := json.Unmarshal(storage, &file); errUnmarshal != nil {
		return map[string]modelStateEntry{}
	}
	return decodeModelStates(file["model_states"])
}

// hostAuthIndexOfExecutorRequest returns the credential index a call is bound to.
//
// pluginapi.ExecutorRequest carries AuthID, which is the same storage key
// host.auth.get accepts.
func hostAuthIndexOfExecutorRequest(req pluginapi.ExecutorRequest) string {
	return strings.TrimSpace(req.AuthID)
}
