package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// regionHoldStore remembers which auth files the supplier switch (仅国内 / 仅国际) disabled.
//
// The switch works through the same top-level "disabled" the operator's own toggle writes,
// so the flag alone cannot say who set it. Without this record, going back to 自动 re-enabled
// every credential — including ones the operator had switched off by hand in CPA or in this
// panel. The switch now only lifts what it put there itself.
//
// Persisted beside the panel's other choices, so a restart between 仅国际 and 自动 still
// knows which files to restore.
type regionHoldStore struct {
	mu     sync.Mutex
	path   string
	loaded bool
	held   map[string]bool
}

func newRegionHoldStore(path string) *regionHoldStore {
	return &regionHoldStore{path: path, held: map[string]bool{}}
}

// regionHold is the process-wide record. Tests replace it with an in-memory one.
var regionHold = newRegionHoldStore(filepath.Join(pluginStateDir(), "region_hold.json"))

// regionHoldKey normalises an auth file location so relative and absolute spellings match.
func regionHoldKey(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if abs, errAbs := filepath.Abs(path); errAbs == nil {
		return abs
	}
	return filepath.Clean(path)
}

func (s *regionHoldStore) loadLocked() {
	if s.loaded {
		return
	}
	s.loaded = true
	if s.path == "" {
		return
	}
	raw, errRead := os.ReadFile(s.path)
	if errRead != nil {
		return
	}
	var disk struct {
		Held []string `json:"held"`
	}
	if json.Unmarshal(raw, &disk) != nil {
		return
	}
	for _, p := range disk.Held {
		if p = strings.TrimSpace(p); p != "" {
			s.held[p] = true
		}
	}
}

func (s *regionHoldStore) saveLocked() {
	if s.path == "" {
		return
	}
	list := make([]string, 0, len(s.held))
	for p := range s.held {
		list = append(list, p)
	}
	sort.Strings(list)
	raw, errMarshal := json.Marshal(map[string]any{"held": list})
	if errMarshal != nil {
		return
	}
	// Best effort: a read-only state dir must not break the switch itself.
	_ = atomicWriteFile(s.path, raw)
}

// isHeld reports whether the supplier switch disabled this file.
func (s *regionHoldStore) isHeld(path string) bool {
	key := regionHoldKey(path)
	if key == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
	return s.held[key]
}

// hold records that the supplier switch disabled this file.
func (s *regionHoldStore) hold(path string) {
	s.set(path, true)
}

// release forgets the record, after the switch restored the file or the operator took it over.
func (s *regionHoldStore) release(path string) {
	s.set(path, false)
}

func (s *regionHoldStore) set(path string, held bool) {
	key := regionHoldKey(path)
	if key == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
	if s.held[key] == held {
		return
	}
	if held {
		s.held[key] = true
	} else {
		delete(s.held, key)
	}
	s.saveLocked()
}

// regionHoldReason explains a disable the supplier switch put on this credential.
func regionHoldReason(entry hostAuthEntry) string {
	if regionHold.isHeld(entry.Path) {
		return "供应商切换暂停（切回「全部供应商」后自动恢复）"
	}
	return ""
}
