package app

import (
	"context"

	"github.com/market-raccoon/internal/core/insights/domain"
	"github.com/market-raccoon/internal/shared/problem"
	"github.com/market-raccoon/internal/shared/result"
)

// InsightsServiceConfig configures all use cases exposed by InsightsService.
type InsightsServiceConfig struct {
	VolumeProfile        BuildVolumeProfileConfig
	Heatmap              BuildHeatmapConfig
	JoinTrades           JoinCrossVenueTradesConfig
	OverloadDecide       OverloadDecideFunc
	SessionVolumeProfile BuildSessionVolumeProfileConfig
	TPOProfile           BuildTPOProfileConfig
	SessionEmitDecide    SessionEmitDecideFunc
}

// InsightsService is the entrypoint facade for the insights bounded context.
type InsightsService struct {
	VolumeProfile        *BuildVolumeProfile
	Heatmap              *BuildHeatmap
	JoinTrades           *JoinCrossVenueTrades
	OverloadPolicy       *VPVREmitPolicy
	SessionVolumeProfile *BuildSessionVolumeProfile
	TPOProfile           *BuildTPOProfile
	SessionEmitPolicy    *SessionEmitPolicy
}

type HeatmapSnapshotKey struct {
	Venue      string
	Instrument string
	Timeframe  string
}

type VolumeProfileSnapshotKey struct {
	Venue      string
	Instrument string
	Timeframe  string
}

// NewInsightsService creates all insights use cases from a single config.
func NewInsightsService(cfg InsightsServiceConfig) *InsightsService {
	return &InsightsService{
		VolumeProfile:        NewBuildVolumeProfileWithConfig(cfg.VolumeProfile),
		Heatmap:              NewBuildHeatmapWithConfig(cfg.Heatmap),
		JoinTrades:           NewJoinCrossVenueTradesWithConfig(cfg.JoinTrades),
		OverloadPolicy:       NewVPVREmitPolicy(cfg.OverloadDecide),
		SessionVolumeProfile: NewBuildSessionVolumeProfileWithConfig(cfg.SessionVolumeProfile),
		TPOProfile:           NewBuildTPOProfileWithConfig(cfg.TPOProfile),
		SessionEmitPolicy:    NewSessionEmitPolicy(cfg.SessionEmitDecide),
	}
}

// SnapshotHeatmap returns the latest in-memory heatmap snapshot for a key.
func (s *InsightsService) SnapshotHeatmap(
	_ context.Context,
	key HeatmapSnapshotKey,
) result.Result[domain.HeatmapArtifactV1] {
	if s == nil || s.Heatmap == nil {
		return result.FailProblem[domain.HeatmapArtifactV1](
			problem.New(problem.ValidationFailed, "insights heatmap snapshot query is not configured"),
		)
	}
	snap, p := s.Heatmap.Snapshot(key.Venue, key.Instrument, key.Timeframe)
	if p != nil {
		return result.FailProblem[domain.HeatmapArtifactV1](p)
	}
	return result.Ok(snap)
}

// SnapshotVolumeProfile returns the latest in-memory volume profile snapshot for a key.
func (s *InsightsService) SnapshotVolumeProfile(
	_ context.Context,
	key VolumeProfileSnapshotKey,
) result.Result[domain.VolumeProfileSnapshotV1] {
	if s == nil || s.VolumeProfile == nil {
		return result.FailProblem[domain.VolumeProfileSnapshotV1](
			problem.New(problem.ValidationFailed, "insights volume profile snapshot query is not configured"),
		)
	}
	snap, p := s.VolumeProfile.Snapshot(key.Venue, key.Instrument, key.Timeframe)
	if p != nil {
		return result.FailProblem[domain.VolumeProfileSnapshotV1](p)
	}
	return result.Ok(snap)
}
