// Package ports defines storage write interfaces for the insights context.
package ports

import (
	"context"
	"strings"

	"github.com/market-raccoon/internal/core/insights/domain"
	"github.com/market-raccoon/internal/shared/naming"
	"github.com/market-raccoon/internal/shared/problem"
)

// VolumeProfileBucketUpsert carries one deterministic VPVR bucket aggregate.
// Storage key invariant (VPVR-STO-1):
// (venue, instrument, timeframe, window_start_ts, bucket_low, bucket_high)
// seqMax is intentionally not part of the storage key.
type VolumeProfileBucketUpsert struct {
	Venue         string
	Instrument    string
	Timeframe     string
	WindowStartTs int64
	BucketLow     float64
	BucketHigh    float64
	BuyVolume     float64
	SellVolume    float64
	TotalVolume   float64
	SeqMin        int64
	SeqMax        int64
}

func (u VolumeProfileBucketUpsert) Validate() *problem.Problem {
	if strings.TrimSpace(u.Venue) == "" {
		return problem.New(problem.ValidationFailed, "volume profile upsert venue must not be empty")
	}
	if strings.TrimSpace(u.Instrument) == "" {
		return problem.New(problem.ValidationFailed, "volume profile upsert instrument must not be empty")
	}
	if _, ok := domain.VPVRTimeframes[naming.NormalizeTimeframe(u.Timeframe)]; !ok {
		return problem.New(problem.ValidationFailed, "volume profile upsert timeframe is unsupported")
	}
	if u.WindowStartTs <= 0 {
		return problem.New(problem.ValidationFailed, "volume profile upsert window_start_ts must be > 0")
	}
	if u.BucketHigh <= u.BucketLow {
		return problem.New(problem.ValidationFailed, "volume profile upsert bucket bounds are invalid")
	}
	if u.BuyVolume < 0 || u.SellVolume < 0 || u.TotalVolume < 0 {
		return problem.New(problem.ValidationFailed, "volume profile upsert volumes must be non-negative")
	}
	if u.TotalVolume != u.BuyVolume+u.SellVolume {
		return problem.New(problem.ValidationFailed, "volume profile upsert total_volume must equal buy_volume + sell_volume")
	}
	if u.SeqMin <= 0 || u.SeqMax < u.SeqMin {
		return problem.New(problem.ValidationFailed, "volume profile upsert seq bounds are invalid")
	}
	return nil
}

// VolumeProfileHotWriter persists VPVR hot-path bucket aggregates.
// Implementations must return *problem.Problem (VPVR-STO-3).
type VolumeProfileHotWriter interface {
	UpsertVolumeProfileBucket(ctx context.Context, upsert VolumeProfileBucketUpsert) *problem.Problem
}
