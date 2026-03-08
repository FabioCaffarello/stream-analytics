package ports

import (
	"context"

	"github.com/market-raccoon/internal/core/insights/domain"
	"github.com/market-raccoon/internal/shared/problem"
)

// VolumeProfileSnapshotQuery specifies a range query for persisted VPVR snapshots.
type VolumeProfileSnapshotQuery struct {
	Venue      string
	Instrument string
	Timeframe  string
	FromMs     int64 // inclusive window_start_ts lower bound
	ToMs       int64 // exclusive window_start_ts upper bound
	Limit      int   // max results (0 = default cap)
}

// VolumeProfileReader reads persisted VPVR snapshots from storage.
type VolumeProfileReader interface {
	GetVolumeProfileSnapshots(ctx context.Context, q VolumeProfileSnapshotQuery) ([]domain.VolumeProfileSnapshotV1, *problem.Problem)
}

// HeatmapSnapshotQuery specifies a range query for persisted heatmap artifacts.
type HeatmapSnapshotQuery struct {
	Venue      string
	Instrument string
	Timeframe  string
	FromMs     int64
	ToMs       int64
	Limit      int
}

// HeatmapReader reads persisted heatmap artifacts from storage.
type HeatmapReader interface {
	GetHeatmapSnapshots(ctx context.Context, q HeatmapSnapshotQuery) ([]domain.HeatmapArtifactV1, *problem.Problem)
}

// SessionVolumeProfileQuery specifies a query for persisted SVP snapshots.
type SessionVolumeProfileQuery struct {
	Venue       string
	Instrument  string
	AnchorLabel string
	FromMs      int64
	ToMs        int64
	Limit       int
}

// SessionVolumeProfileReader reads persisted session volume profile snapshots.
type SessionVolumeProfileReader interface {
	GetSessionVolumeProfiles(ctx context.Context, q SessionVolumeProfileQuery) ([]domain.SessionVolumeProfileV1, *problem.Problem)
}

// TPOProfileQuery specifies a query for persisted TPO profiles.
type TPOProfileQuery struct {
	Venue       string
	Instrument  string
	AnchorLabel string
	FromMs      int64
	ToMs        int64
	Limit       int
}

// TPOProfileReader reads persisted TPO profiles from storage.
type TPOProfileReader interface {
	GetTPOProfiles(ctx context.Context, q TPOProfileQuery) ([]domain.TPOProfileV1, *problem.Problem)
}
