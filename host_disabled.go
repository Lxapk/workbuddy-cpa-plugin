package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The host's per-credential "disabled" flag, read and written on the auth file itself.
//
// CPA builds its candidate list from the top-level "disabled" in each auth file and reloads
// a file when its watcher sees it change. That flag is therefore the one lever this plugin
// has over the host's own routing without touching the host, and the file on disk — not the
// host's listing, which describes the copy it last loaded — is the current truth.

// readAuthFile reads a credential file at the location the host reported.
//
// The host reports it relative to its own working directory ("auths/x.json"). The plugin is
// loaded into the host process, so that is this process's working directory as well and the
// path opens as given.
func readAuthFile(path string) ([]byte, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, false
	}
	raw, errRead := os.ReadFile(path)
	if errRead != nil || len(raw) == 0 {
		return nil, false
	}
	return raw, true
}

// diskDisabled reports the file's top-level "disabled". known is false when the file cannot
// be read or does not carry the flag, so the caller can fall back to what the host said.
func diskDisabled(path string) (disabled, known bool) {
	raw, ok := readAuthFile(path)
	if !ok {
		return false, false
	}
	return disabledInPayload(raw)
}

// disabledInPayload extracts the top-level "disabled" from a credential payload.
func disabledInPayload(raw []byte) (disabled, known bool) {
	if len(raw) == 0 {
		return false, false
	}
	var doc map[string]json.RawMessage
	if json.Unmarshal(raw, &doc) != nil {
		return false, false
	}
	value, has := doc["disabled"]
	if !has {
		return false, false
	}
	if json.Unmarshal(value, &disabled) != nil {
		return false, false
	}
	return disabled, true
}

// setAuthFileDisabled sets the top-level "disabled" in a credential file, changing nothing
// else. It reports whether the file was rewritten.
//
// The file is read fresh and only that one key is replaced. Starting from the host's listing
// instead — its loaded copy — would write that copy back whole, and a token the host had
// refreshed and saved since would be rolled back to the previous one.
//
// The write goes through a temporary file in the same directory and a rename. Writing in
// place truncates first, and the host's watcher can read the file in between and find half a
// credential. The temporary name does not end in ".json", so the watcher ignores it.
func setAuthFileDisabled(path string, disabled bool) (bool, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return false, fmt.Errorf("宿主没有报告凭据文件位置")
	}
	info, errStat := os.Stat(path)
	if errStat != nil {
		return false, errStat
	}
	raw, errRead := os.ReadFile(path)
	if errRead != nil {
		return false, errRead
	}
	var doc map[string]json.RawMessage
	if errUnmarshal := json.Unmarshal(raw, &doc); errUnmarshal != nil || doc == nil {
		return false, fmt.Errorf("%s 不是 JSON 对象", filepath.Base(path))
	}
	if current, known := disabledInPayload(raw); known && current == disabled {
		return false, nil
	}
	doc["disabled"] = json.RawMessage(strconv.FormatBool(disabled))
	encoded, errMarshal := json.Marshal(doc)
	if errMarshal != nil {
		return false, errMarshal
	}

	tmp, errTemp := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if errTemp != nil {
		return false, errTemp
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, errWrite := tmp.Write(encoded); errWrite != nil {
		_ = tmp.Close()
		cleanup()
		return false, errWrite
	}
	if errChmod := tmp.Chmod(info.Mode().Perm()); errChmod != nil {
		_ = tmp.Close()
		cleanup()
		return false, errChmod
	}
	if errSync := tmp.Sync(); errSync != nil {
		_ = tmp.Close()
		cleanup()
		return false, errSync
	}
	if errClose := tmp.Close(); errClose != nil {
		cleanup()
		return false, errClose
	}
	if errRename := os.Rename(tmpName, path); errRename != nil {
		cleanup()
		return false, errRename
	}
	return true, nil
}

// hostDisabled reports whether the host holds this credential as disabled.
//
// The file on disk wins over the host's listing, which lags every change until the watcher
// has reloaded it. A value this plugin has just written is honoured while no source can
// confirm it, and dropped as soon as one does.
func hostDisabled(entry hostAuthEntry, storage []byte) bool {
	keys := authEntryKeys(entry)
	actual, known := hostDisabledFromSources(entry, storage)
	if want, has := pendingFor(keys); has {
		if known && actual == want {
			clearPending(keys)
		}
		if !known {
			return want
		}
	}
	return actual
}

// hostDisabledFromSources reads the flag from the file, then the entry, then its payload.
func hostDisabledFromSources(entry hostAuthEntry, storage []byte) (disabled, known bool) {
	if disabled, known = diskDisabled(entry.Path); known {
		return disabled, true
	}
	if entry.Disabled {
		return true, true
	}
	return disabledInPayload(storage)
}

// pendingTTL bounds how long a written value is trusted without confirmation. After that the
// host's own sources decide again, so a value that was never confirmed cannot mask a change
// made in CPA indefinitely.
const pendingTTL = 30 * time.Second

type pendingValue struct {
	disabled bool
	at       time.Time
}

// pendingDisabled records what this plugin last wrote for a credential, under every
// identifier the host entry carries: the write and the later read come from different host
// calls that do not necessarily fill the same fields.
var pendingDisabled sync.Map // identifier → pendingValue

// authEntryKeys enumerates the identifiers a host entry may be looked up by.
func authEntryKeys(entry hostAuthEntry) []string {
	var out []string
	seen := map[string]bool{}
	add := func(id string) {
		if id = strings.TrimSpace(id); id != "" && id != "." && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	add(entry.Name)
	add(entry.AuthIndex)
	add(entry.ID)
	add(entry.Path)
	if entry.Path != "" {
		add(filepath.Base(entry.Path))
	}
	return out
}

// rememberDisabled records a value that has been written to the credential's file. Call it
// only after the write succeeded.
func rememberDisabled(entry hostAuthEntry, disabled bool) {
	value := pendingValue{disabled: disabled, at: time.Now()}
	for _, id := range authEntryKeys(entry) {
		pendingDisabled.Store(id, value)
	}
}

// pendingFor returns an unexpired pending value stored under any of the keys.
func pendingFor(keys []string) (bool, bool) {
	for _, key := range keys {
		raw, ok := pendingDisabled.Load(key)
		if !ok {
			continue
		}
		value, _ := raw.(pendingValue)
		if time.Since(value.at) > pendingTTL {
			pendingDisabled.Delete(key)
			continue
		}
		return value.disabled, true
	}
	return false, false
}

// clearPending forgets the pending value under every key.
func clearPending(keys []string) {
	for _, key := range keys {
		pendingDisabled.Delete(key)
	}
}

// applyPendingDisabled overlays values this plugin has just written onto an inventory, so a
// table rendered right after a toggle shows the new state even if the host has not caught up.
//
// Usability is recomputed with the same rule the inventory uses, rather than set to the
// inverse of the flag: re-enabling an account that is cooling down or whose balance has
// expired must not paint it as 可用.
func applyPendingDisabled(accounts []workBuddyAccount) []workBuddyAccount {
	if len(accounts) == 0 {
		return accounts
	}
	out := make([]workBuddyAccount, len(accounts))
	copy(out, accounts)
	now := time.Now()
	for i := range out {
		keys := append([]string{out[i].AuthIndex}, out[i].AuthIndexes...)
		disabled, ok := pendingFor(keys)
		if !ok {
			continue
		}
		out[i].Disabled = disabled
		out[i].DisabledByUser = disabled
		out[i].Usable = accountUsable(&out[i], now)
	}
	return out
}
