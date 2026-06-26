// Package domain contains the workspace bounded context domain model.
//
// A Workspace represents the persisted client layout and settings.
// It is the root aggregate — all validation and integrity invariants
// (schema version range, layout prefix, fingerprint computation) live here.
package domain

import (
	"fmt"
	"hash/fnv"
	"sort"
	"strings"

	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

// MaxSchemaVersion is the maximum workspace schema version this server
// understands.  Payloads with a higher version are rejected to prevent
// silent data loss from a newer client talking to an older server.
const MaxSchemaVersion = 12

// layoutPrefix is the required prefix for layout_v6 strings.
const layoutPrefix = "V6"

// Workspace is the root aggregate for client workspace state.
//
// Invariants:
//   - SchemaVersion is in [1, MaxSchemaVersion].
//   - LayoutV6 starts with "V6".
//   - Fingerprint is deterministically derived from LayoutV6 + Settings.
//   - SavedAtMs is positive after persistence.
type Workspace struct {
	schemaVersion int
	layoutV6      string
	fingerprint   string
	settings      map[string]string
	savedAtMs     int64
}

// NewFromPayload creates a Workspace from an incoming client payload,
// validating all invariants.  Returns a *problem.Problem on failure.
func NewFromPayload(schemaVersion int, layoutV6 string, settings map[string]string, savedAtMs int64) (*Workspace, *problem.Problem) {
	if schemaVersion <= 0 {
		return nil, problem.New(problem.ValidationFailed, "schema_version is required and must be > 0")
	}
	if schemaVersion > MaxSchemaVersion {
		return nil, problem.New(problem.Conflict, "schema_version is from a newer client; server cannot downgrade")
	}
	if layoutV6 == "" || !strings.HasPrefix(layoutV6, layoutPrefix) {
		return nil, problem.New(problem.ValidationFailed, `layout_v6 is required and must start with "V6"`)
	}

	w := &Workspace{
		schemaVersion: schemaVersion,
		layoutV6:      layoutV6,
		settings:      settings,
		savedAtMs:     savedAtMs,
	}
	w.fingerprint = w.computeFingerprint()
	return w, nil
}

// Reconstitute creates a Workspace from persisted state without re-validating.
// Used by repositories when loading from storage.
func Reconstitute(schemaVersion int, layoutV6, fingerprint string, settings map[string]string, savedAtMs int64) *Workspace {
	return &Workspace{
		schemaVersion: schemaVersion,
		layoutV6:      layoutV6,
		fingerprint:   fingerprint,
		settings:      settings,
		savedAtMs:     savedAtMs,
	}
}

// SchemaVersion returns the workspace schema version.
func (w *Workspace) SchemaVersion() int { return w.schemaVersion }

// LayoutV6 returns the serialized layout string.
func (w *Workspace) LayoutV6() string { return w.layoutV6 }

// Fingerprint returns the deterministic FNV-1a fingerprint of layout + settings.
func (w *Workspace) Fingerprint() string { return w.fingerprint }

// Settings returns a copy of the settings map.
func (w *Workspace) Settings() map[string]string {
	if w.settings == nil {
		return nil
	}
	cp := make(map[string]string, len(w.settings))
	for k, v := range w.settings {
		cp[k] = v
	}
	return cp
}

// SavedAtMs returns the persistence timestamp in epoch milliseconds.
func (w *Workspace) SavedAtMs() int64 { return w.savedAtMs }

// StampSaveTime sets the save timestamp if not already set.
// Returns the (possibly updated) timestamp.
func (w *Workspace) StampSaveTime(nowMs int64) int64 {
	if w.savedAtMs == 0 {
		w.savedAtMs = nowMs
	}
	return w.savedAtMs
}

// HasSameFingerprint returns true if this workspace's fingerprint matches another.
func (w *Workspace) HasSameFingerprint(other *Workspace) bool {
	if other == nil {
		return false
	}
	return w.fingerprint == other.fingerprint
}

// computeFingerprint computes a stable FNV-1a fingerprint from layout + settings.
// Deterministic for the same inputs regardless of map iteration order.
func (w *Workspace) computeFingerprint() string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(w.layoutV6))
	keys := make([]string, 0, len(w.settings))
	for k := range w.settings {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		_, _ = h.Write([]byte(k))
		_, _ = h.Write([]byte(w.settings[k]))
	}
	return fmt.Sprintf("%016x", h.Sum64())
}
