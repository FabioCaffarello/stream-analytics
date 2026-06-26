package app

import (
	"context"
	"sort"

	"github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/ports"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

// GapReport describes a contiguous gap in candle coverage.
type GapReport struct {
	Venue      string
	Instrument string
	Timeframe  string
	GapStartMs int64
	GapEndMs   int64
	Missing    int
}

// GapDetectorConfig controls the detection parameters.
type GapDetectorConfig struct {
	Venue          string
	Instrument     string
	Timeframe      string
	FromMs         int64
	ToMs           int64
	ExpectedStepMs int64
}

// DetectCandleGaps queries cold storage timestamps and identifies gaps.
//
//nolint:gocyclo // explicit branching keeps range anchoring and gap semantics readable.
func DetectCandleGaps(ctx context.Context, reader ports.CandleReader, cfg GapDetectorConfig) ([]GapReport, *problem.Problem) {
	if reader == nil {
		return nil, problem.New(problem.ValidationFailed, "candle reader must not be nil")
	}
	if cfg.ExpectedStepMs <= 0 {
		return nil, problem.New(problem.ValidationFailed, "expected_step_ms must be > 0")
	}

	fromMs := cfg.FromMs
	toMs := cfg.ToMs

	if fromMs == 0 {
		first, p := reader.GetFirstCandle(ctx, cfg.Venue, cfg.Instrument, cfg.Timeframe)
		if p != nil {
			return nil, p
		}
		if first == nil {
			return nil, nil
		}
		fromMs = first.WindowStartTs
	}
	if toMs == 0 {
		last, p := reader.GetLastCandle(ctx, cfg.Venue, cfg.Instrument, cfg.Timeframe)
		if p != nil {
			return nil, p
		}
		if last == nil {
			return nil, nil
		}
		toMs = last.WindowStartTs
	}
	if toMs < fromMs {
		return nil, problem.Newf(problem.ValidationFailed, "invalid range: from_ms=%d to_ms=%d", fromMs, toMs)
	}

	ts, p := reader.GetCandleTimestamps(ctx, cfg.Venue, cfg.Instrument, cfg.Timeframe, fromMs, toMs)
	if p != nil {
		return nil, p
	}

	if len(ts) == 0 {
		missing := int(((toMs - fromMs) / cfg.ExpectedStepMs) + 1)
		if missing <= 0 {
			return nil, nil
		}
		return []GapReport{{
			Venue:      cfg.Venue,
			Instrument: cfg.Instrument,
			Timeframe:  cfg.Timeframe,
			GapStartMs: fromMs,
			GapEndMs:   toMs,
			Missing:    missing,
		}}, nil
	}

	sorted := append([]int64(nil), ts...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	unique := sorted[:0]
	var prev int64
	for i, v := range sorted {
		if i == 0 || v != prev {
			unique = append(unique, v)
		}
		prev = v
	}

	reports := make([]GapReport, 0, 8)
	if unique[0] > fromMs {
		reports = append(reports, buildGap(cfg, fromMs, unique[0]-cfg.ExpectedStepMs, cfg.ExpectedStepMs))
	}
	for i := 1; i < len(unique); i++ {
		delta := unique[i] - unique[i-1]
		if delta <= cfg.ExpectedStepMs {
			continue
		}
		gapStart := unique[i-1] + cfg.ExpectedStepMs
		gapEnd := unique[i] - cfg.ExpectedStepMs
		reports = append(reports, buildGap(cfg, gapStart, gapEnd, cfg.ExpectedStepMs))
	}
	if unique[len(unique)-1] < toMs {
		reports = append(reports, buildGap(cfg, unique[len(unique)-1]+cfg.ExpectedStepMs, toMs, cfg.ExpectedStepMs))
	}

	return reports, nil
}

func buildGap(cfg GapDetectorConfig, startMs, endMs, stepMs int64) GapReport {
	missing := 0
	if endMs >= startMs {
		missing = int(((endMs - startMs) / stepMs) + 1)
	}
	return GapReport{
		Venue:      cfg.Venue,
		Instrument: cfg.Instrument,
		Timeframe:  cfg.Timeframe,
		GapStartMs: startMs,
		GapEndMs:   endMs,
		Missing:    missing,
	}
}
