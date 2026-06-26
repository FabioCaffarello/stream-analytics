// Package infra contains driven-side adapters for the workspace bounded context.
package infra

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/FabioCaffarello/stream-analytics/internal/core/workspace/domain"
)

// workspaceStateDTO is the on-disk JSON representation of a Workspace.
type workspaceStateDTO struct {
	SchemaVersion int               `json:"schema_version"`
	LayoutV6      string            `json:"layout_v6"`
	Fingerprint   string            `json:"fingerprint"`
	Settings      map[string]string `json:"settings"`
	SavedAtMs     int64             `json:"saved_at_ms"`
}

func dtoFromDomain(ws *domain.Workspace) workspaceStateDTO {
	return workspaceStateDTO{
		SchemaVersion: ws.SchemaVersion(),
		LayoutV6:      ws.LayoutV6(),
		Fingerprint:   ws.Fingerprint(),
		Settings:      ws.Settings(),
		SavedAtMs:     ws.SavedAtMs(),
	}
}

func dtoToDomain(dto workspaceStateDTO) *domain.Workspace {
	return domain.Reconstitute(
		dto.SchemaVersion,
		dto.LayoutV6,
		dto.Fingerprint,
		dto.Settings,
		dto.SavedAtMs,
	)
}

// FileWorkspaceStore is a file-backed implementation of ports.WorkspaceRepository.
// State is cached in memory after the first load; saves write atomically
// (temp file + fsync + rename) to avoid partial writes on crash.
type FileWorkspaceStore struct {
	mu      sync.RWMutex
	path    string            // full path to workspace.json
	state   *domain.Workspace // cached in memory after first load
	loaded  bool              // true after first load attempt
	loadErr error             // non-nil if the on-disk file was corrupt
}

// NewFileWorkspaceStore creates a store that reads/writes workspace state
// at "<dir>/workspace.json".  The file is loaded eagerly; if it does not
// exist, Load() will return nil, nil until the first Save.
func NewFileWorkspaceStore(dir string) *FileWorkspaceStore {
	s := &FileWorkspaceStore{
		path: filepath.Join(dir, "workspace.json"),
	}
	if err := s.loadFromDisk(); err != nil {
		slog.Error("workspace store: initial load failed", "path", s.path, "err", err)
	}
	return s
}

// Load returns the cached workspace state.  Returns (nil, nil) when no
// state has been saved yet (first run / file missing).
// Returns (nil, error) when the on-disk file exists but is corrupt.
func (s *FileWorkspaceStore) Load() (*domain.Workspace, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.loadErr != nil {
		return nil, s.loadErr
	}
	if !s.loaded {
		return nil, nil
	}
	return s.state, nil
}

// Save persists state to disk atomically and updates the in-memory cache.
func (s *FileWorkspaceStore) Save(ws *domain.Workspace) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // 0755 is appropriate for a shared state directory.
		return err
	}

	dto := dtoFromDomain(ws)
	data, err := json.MarshalIndent(dto, "", "  ")
	if err != nil {
		return err
	}

	// Write to temp file, fsync, then rename for crash-safe persistence.
	tmp := s.path + ".tmp"
	f, err := os.Create(tmp) //nolint:gosec // tmp path is derived from a fixed configured path; not user-controlled.
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return err
	}

	s.state = ws
	s.loaded = true
	s.loadErr = nil
	return nil
}

// Delete removes the persisted workspace file and clears in-memory state.
func (s *FileWorkspaceStore) Delete() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	err := os.Remove(s.path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	s.state = nil
	s.loaded = true
	s.loadErr = nil
	return nil
}

// loadFromDisk reads the workspace file.  If it does not exist, state is
// set to nil (first run).  The caller must NOT hold mu.
func (s *FileWorkspaceStore) loadFromDisk() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			s.state = nil
			s.loaded = true
			return nil
		}
		return err
	}

	var dto workspaceStateDTO
	if err := json.Unmarshal(data, &dto); err != nil {
		// File exists but is corrupt — record error for Load() to propagate.
		s.loaded = true
		s.loadErr = err
		return err
	}
	s.state = dtoToDomain(dto)
	s.loaded = true
	return nil
}
