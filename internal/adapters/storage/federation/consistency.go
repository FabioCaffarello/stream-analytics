package federation

import (
	"context"

	aggports "github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/ports"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

// ConsistencyReport summarises hot vs cold alignment for a single artifact slice.
type ConsistencyReport struct {
	Artifact      string `json:"artifact"`
	Venue         string `json:"venue"`
	Instrument    string `json:"instrument"`
	Timeframe     string `json:"timeframe"`
	HotCount      int    `json:"hot_count"`
	ColdCount     int    `json:"cold_count"`
	OverlapCount  int    `json:"overlap_count"`
	MissingInCold int    `json:"missing_in_cold"`
	MissingInHot  int    `json:"missing_in_hot"`
	HotMinTs      int64  `json:"hot_min_ts"`
	HotMaxTs      int64  `json:"hot_max_ts"`
	ColdMinTs     int64  `json:"cold_min_ts"`
	ColdMaxTs     int64  `json:"cold_max_ts"`
}

// ConsistencyChecker compares row counts and timestamp coverage across tiers.
type ConsistencyChecker struct {
	hotCandles aggports.CandleReader
	hotStats   aggports.StatsReader

	coldCandles aggports.CandleReader
	coldStats   aggports.StatsReader
}

func NewConsistencyChecker(
	hotCandles aggports.CandleReader,
	coldCandles aggports.CandleReader,
	hotStats aggports.StatsReader,
	coldStats aggports.StatsReader,
) *ConsistencyChecker {
	return &ConsistencyChecker{
		hotCandles:  hotCandles,
		coldCandles: coldCandles,
		hotStats:    hotStats,
		coldStats:   coldStats,
	}
}

// CheckCandles compares candle timestamps in [fromMs, toMs] across hot and cold.
func (c *ConsistencyChecker) CheckCandles(ctx context.Context, venue, instrument, timeframe string, fromMs, toMs int64) (*ConsistencyReport, *problem.Problem) {
	var hotTs, coldTs []int64
	if c.hotCandles != nil {
		ts, p := c.hotCandles.GetCandleTimestamps(ctx, venue, instrument, timeframe, fromMs, toMs)
		if p != nil {
			return nil, p
		}
		hotTs = ts
	}
	if c.coldCandles != nil {
		ts, p := c.coldCandles.GetCandleTimestamps(ctx, venue, instrument, timeframe, fromMs, toMs)
		if p != nil {
			return nil, p
		}
		coldTs = ts
	}
	return buildReport("candle", venue, instrument, timeframe, hotTs, coldTs), nil
}

// CheckStats compares stats timestamps in [fromMs, toMs] across hot and cold.
func (c *ConsistencyChecker) CheckStats(ctx context.Context, venue, instrument, timeframe string, fromMs, toMs int64) (*ConsistencyReport, *problem.Problem) {
	var hotTs, coldTs []int64
	if c.hotStats != nil {
		ts, p := c.hotStats.GetStatsTimestamps(ctx, venue, instrument, timeframe, fromMs, toMs)
		if p != nil {
			return nil, p
		}
		hotTs = ts
	}
	if c.coldStats != nil {
		ts, p := c.coldStats.GetStatsTimestamps(ctx, venue, instrument, timeframe, fromMs, toMs)
		if p != nil {
			return nil, p
		}
		coldTs = ts
	}
	return buildReport("stats", venue, instrument, timeframe, hotTs, coldTs), nil
}

func buildReport(artifact, venue, instrument, timeframe string, hotTs, coldTs []int64) *ConsistencyReport {
	r := &ConsistencyReport{
		Artifact:   artifact,
		Venue:      venue,
		Instrument: instrument,
		Timeframe:  timeframe,
		HotCount:   len(hotTs),
		ColdCount:  len(coldTs),
	}
	if len(hotTs) > 0 {
		r.HotMinTs = hotTs[0]
		r.HotMaxTs = hotTs[len(hotTs)-1]
	}
	if len(coldTs) > 0 {
		r.ColdMinTs = coldTs[0]
		r.ColdMaxTs = coldTs[len(coldTs)-1]
	}

	hotSet := make(map[int64]struct{}, len(hotTs))
	for _, ts := range hotTs {
		hotSet[ts] = struct{}{}
	}
	coldSet := make(map[int64]struct{}, len(coldTs))
	for _, ts := range coldTs {
		coldSet[ts] = struct{}{}
	}

	for ts := range hotSet {
		if _, ok := coldSet[ts]; ok {
			r.OverlapCount++
		} else {
			r.MissingInCold++
		}
	}
	for ts := range coldSet {
		if _, ok := hotSet[ts]; !ok {
			r.MissingInHot++
		}
	}
	return r
}
