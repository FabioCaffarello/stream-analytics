package domain

import (
	"math"
	"slices"
	"strings"

	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/validation"
)

// FusionMode controls how multi-venue data is combined.
type FusionMode string

const (
	FusionSingleVenue FusionMode = "single"
	FusionWeighted    FusionMode = "weighted"
	FusionMerge       FusionMode = "merge"
)

var validFusionModes = map[FusionMode]struct{}{
	FusionSingleVenue: {},
	FusionWeighted:    {},
	FusionMerge:       {},
}

func ValidFusionMode(m FusionMode) bool {
	_, ok := validFusionModes[m]
	return ok
}

const (
	FusedDepthMaxLevels   = 50
	FusedSourceMixMaxLen  = 8
	FusedFeatureTagMaxLen = 10
)

// SourceEntry describes one venue's contribution to a fused result.
type SourceEntry struct {
	Venue      string  `json:"venue"`
	WeightPct  float64 `json:"weight_pct"`
	LastSeenMs int64   `json:"last_seen_ms"`
	IsStale    bool    `json:"is_stale"`
}

// StalenessReport summarizes freshness across sources.
type StalenessReport struct {
	FreshCount int   `json:"fresh_count"`
	StaleCount int   `json:"stale_count"`
	OldestMs   int64 `json:"oldest_ms"`
	NewestMs   int64 `json:"newest_ms"`
}

// FusionMeta carries evidence metadata for any fused payload.
type FusionMeta struct {
	Reason      string          `json:"reason"`
	Confidence  float64         `json:"confidence"`
	SourceMix   []SourceEntry   `json:"source_mix"`
	Staleness   StalenessReport `json:"staleness"`
	FeatureTags []string        `json:"feature_tags"`
}

// Validate checks FusionMeta invariants.
func (m FusionMeta) Validate() *problem.Problem {
	if strings.TrimSpace(m.Reason) == "" {
		return problem.New(problem.ValidationFailed, "fusion_meta reason must not be empty")
	}
	if !isFinite(m.Confidence) || m.Confidence < 0 || m.Confidence > 1 {
		return problem.New(problem.ValidationFailed, "fusion_meta confidence must be in [0,1]")
	}
	if len(m.SourceMix) == 0 {
		return problem.New(problem.ValidationFailed, "fusion_meta source_mix must not be empty")
	}
	if len(m.SourceMix) > FusedSourceMixMaxLen {
		return problem.New(problem.ValidationFailed, "fusion_meta source_mix exceeds max length")
	}
	if len(m.FeatureTags) > FusedFeatureTagMaxLen {
		return problem.New(problem.ValidationFailed, "fusion_meta feature_tags exceeds max length")
	}
	if !slices.IsSorted(m.FeatureTags) {
		return problem.New(problem.ValidationFailed, "fusion_meta feature_tags must be sorted")
	}
	return nil
}

// FusedLevel is one price level in a fused depth snapshot.
type FusedLevel struct {
	PriceFP int64    `json:"price_fp"`
	SizeFP  int64    `json:"size_fp"`
	Venues  []string `json:"venues"`
}

// FusedDepthSnapshotV1 is a deterministic merged orderbook across venues.
type FusedDepthSnapshotV1 struct {
	Instrument      string       `json:"instrument"`
	TsServerMs      int64        `json:"ts_server_ms"`
	Mode            FusionMode   `json:"mode"`
	Bids            []FusedLevel `json:"bids"`
	Asks            []FusedLevel `json:"asks"`
	SourceVenues    []string     `json:"source_venues"`
	GlobalSpreadBPS float64      `json:"global_spread_bps"`
	Meta            FusionMeta   `json:"meta"`
}

// Validate checks FusedDepthSnapshotV1 invariants.
func (s FusedDepthSnapshotV1) Validate() *problem.Problem {
	if p := validation.Collect(
		validation.NonEmptyString("instrument", s.Instrument),
		validation.PositiveInt("ts_server_ms", s.TsServerMs),
	); p != nil {
		return p
	}
	if !ValidFusionMode(s.Mode) {
		return problem.New(problem.ValidationFailed, "fused depth mode must be a recognized value")
	}
	if len(s.Bids) > FusedDepthMaxLevels {
		return problem.New(problem.ValidationFailed, "fused depth bids exceed max levels")
	}
	if len(s.Asks) > FusedDepthMaxLevels {
		return problem.New(problem.ValidationFailed, "fused depth asks exceed max levels")
	}
	if len(s.SourceVenues) == 0 {
		return problem.New(problem.ValidationFailed, "fused depth source_venues must not be empty")
	}
	if !slices.IsSorted(s.SourceVenues) {
		return problem.New(problem.ValidationFailed, "fused depth source_venues must be sorted")
	}
	return s.Meta.Validate()
}

// VenueContribution describes one venue's volume in a fused bucket.
type VenueContribution struct {
	Venue     string  `json:"venue"`
	Volume    float64 `json:"volume"`
	WeightPct float64 `json:"weight_pct"`
}

// DeriveFusionConfidence computes confidence from source freshness.
func DeriveFusionConfidence(sources []SourceEntry) float64 {
	if len(sources) == 0 {
		return 0
	}
	fresh := 0
	for i := range sources {
		if !sources[i].IsStale {
			fresh++
		}
	}
	ratio := float64(fresh) / float64(len(sources))
	if fresh == 1 {
		ratio *= 0.7
	}
	if ratio > 1 {
		ratio = 1
	}
	return ratio
}

// DeriveFeatureTags produces sorted, deduplicated feature tags.
func DeriveFeatureTags(confidence float64, sources []SourceEntry, divergenceBPS float64, depthCapped bool) []string {
	var tags []string
	if confidence >= 0.9 {
		tags = append(tags, "high_confidence")
	}
	if confidence < 0.5 {
		tags = append(tags, "degraded")
	}
	if depthCapped {
		tags = append(tags, "depth_capped")
	}
	stale := 0
	for i := range sources {
		if sources[i].IsStale {
			stale++
		}
	}
	if stale > 0 && stale < len(sources) {
		tags = append(tags, "partial_sources")
	}
	fresh := len(sources) - stale
	if fresh == 1 {
		tags = append(tags, "single_source")
	}
	if divergenceBPS > 50 {
		tags = append(tags, "venue_divergent")
	}
	slices.Sort(tags)
	return slices.Compact(tags)
}

// BuildSourceMix creates SourceEntry slice from venue timestamps.
func BuildSourceMix(venues map[string]int64, nowMs, staleThresholdMs int64) []SourceEntry {
	entries := make([]SourceEntry, 0, len(venues))
	for venue, lastSeen := range venues {
		isStale := nowMs-lastSeen > staleThresholdMs
		entries = append(entries, SourceEntry{
			Venue:      venue,
			WeightPct:  0,
			LastSeenMs: lastSeen,
			IsStale:    isStale,
		})
	}
	slices.SortFunc(entries, func(a, b SourceEntry) int {
		return strings.Compare(a.Venue, b.Venue)
	})
	fresh := 0
	for i := range entries {
		if !entries[i].IsStale {
			fresh++
		}
	}
	if fresh > 0 {
		w := 100.0 / float64(fresh)
		for i := range entries {
			if !entries[i].IsStale {
				entries[i].WeightPct = w
			}
		}
	}
	return entries
}

// BuildStalenessReport creates a staleness summary from source entries.
func BuildStalenessReport(sources []SourceEntry, nowMs int64) StalenessReport {
	report := StalenessReport{}
	var oldest, newest int64
	for i := range sources {
		age := nowMs - sources[i].LastSeenMs
		if sources[i].IsStale {
			report.StaleCount++
		} else {
			report.FreshCount++
		}
		if i == 0 || age > oldest {
			oldest = age
		}
		if i == 0 || age < newest {
			newest = age
		}
	}
	report.OldestMs = oldest
	report.NewestMs = newest
	return report
}

func isFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
