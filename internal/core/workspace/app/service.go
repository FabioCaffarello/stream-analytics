// Package app contains the workspace bounded context application services.
package app

import (
	"log/slog"

	"github.com/FabioCaffarello/stream-analytics/internal/core/workspace/domain"
	"github.com/FabioCaffarello/stream-analytics/internal/core/workspace/ports"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/clock"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/result"
)

// SaveResult is the output of a successful SaveWorkspace operation.
type SaveResult struct {
	Saved       bool
	Fingerprint string
	SavedAtMs   int64
}

// WorkspaceService orchestrates workspace use cases.
type WorkspaceService struct {
	repo ports.WorkspaceRepository
	clk  clock.Clock
}

// NewWorkspaceService creates a service backed by the given repository.
func NewWorkspaceService(repo ports.WorkspaceRepository) *WorkspaceService {
	return &WorkspaceService{repo: repo, clk: clock.NewSystemClock()}
}

// LoadWorkspace retrieves the current workspace state.
// Returns Ok(nil) when no state has been saved yet (first run).
func (s *WorkspaceService) LoadWorkspace() result.Result[*domain.Workspace] {
	ws, err := s.repo.Load()
	if err != nil {
		slog.Error("workspace load failed", "err", err)
		return result.FailProblem[*domain.Workspace](
			problem.New(problem.Internal, "failed to load workspace state"),
		)
	}
	return result.Ok(ws)
}

// SaveWorkspace validates, fingerprints, and persists a workspace.
// Implements idempotency: skips the write if the fingerprint matches
// the currently stored state.
func (s *WorkspaceService) SaveWorkspace(schemaVersion int, layoutV6 string, settings map[string]string, savedAtMs int64) result.Result[SaveResult] {
	ws, p := domain.NewFromPayload(schemaVersion, layoutV6, settings, savedAtMs)
	if p != nil {
		return result.FailProblem[SaveResult](p)
	}

	// Idempotency check: skip write if fingerprint matches.
	existing, err := s.repo.Load()
	if err != nil {
		slog.Warn("workspace idempotency check failed", "err", err)
		// Non-fatal; proceed with save.
	} else if ws.HasSameFingerprint(existing) {
		return result.Ok(SaveResult{
			Saved:       true,
			Fingerprint: ws.Fingerprint(),
			SavedAtMs:   existing.SavedAtMs(),
		})
	}

	// Stamp server-side save time if client didn't provide one.
	ws.StampSaveTime(s.clk.NowUnixMilli())

	if err := s.repo.Save(ws); err != nil {
		slog.Error("workspace save failed", "err", err)
		return result.FailProblem[SaveResult](
			problem.New(problem.Internal, "failed to persist workspace state"),
		)
	}

	slog.Info("workspace saved",
		"schema_version", ws.SchemaVersion(),
		"fingerprint", ws.Fingerprint(),
	)

	return result.Ok(SaveResult{
		Saved:       true,
		Fingerprint: ws.Fingerprint(),
		SavedAtMs:   ws.SavedAtMs(),
	})
}

// ResetWorkspace deletes persisted workspace state, returning the system
// to a first-run state.
func (s *WorkspaceService) ResetWorkspace() result.Result[bool] {
	if err := s.repo.Delete(); err != nil {
		slog.Error("workspace reset failed", "err", err)
		return result.FailProblem[bool](
			problem.New(problem.Internal, "failed to delete workspace state"),
		)
	}
	slog.Info("workspace reset")
	return result.Ok(true)
}
