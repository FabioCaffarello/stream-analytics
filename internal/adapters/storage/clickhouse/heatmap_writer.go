package clickhouse

import (
	"context"
	"strconv"
	"strings"
	"sync"

	insightsdomain "github.com/market-raccoon/internal/core/insights/domain"
	sharedhash "github.com/market-raccoon/internal/shared/hash"
	"github.com/market-raccoon/internal/shared/ids"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/problem"
)

// HeatmapWriter is a cold-path storage writer skeleton for heatmap artifacts.
type HeatmapWriter struct {
	mu      sync.RWMutex
	byKey   map[string]insightsdomain.HeatmapArtifactV1
	commits int
}

func NewHeatmapWriter() *HeatmapWriter {
	return &HeatmapWriter{byKey: make(map[string]insightsdomain.HeatmapArtifactV1)}
}

func (w *HeatmapWriter) Save(_ context.Context, artifact insightsdomain.HeatmapArtifactV1, sourceIdempotencyKey string) *problem.Problem {
	if w == nil {
		return problem.New(problem.ValidationFailed, "clickhouse heatmap writer is nil")
	}
	if p := artifact.Validate(); p != nil {
		return p
	}
	if strings.TrimSpace(sourceIdempotencyKey) == "" {
		return problem.New(problem.ValidationFailed, "clickhouse heatmap source idempotency key must not be empty")
	}
	key := ids.HeatmapArtifactWriteKey(
		artifact.Venue,
		artifact.Instrument,
		artifact.Timeframe,
		artifact.WindowStartTs,
		sourceIdempotencyKey,
	)
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, exists := w.byKey[key]; exists {
		return nil
	}
	w.byKey[key] = artifact
	w.commits++
	return nil
}

func (w *HeatmapWriter) CommitCount() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.commits
}

// ChHeatmapWriter persists heatmap snapshots in ClickHouse cold storage.
type ChHeatmapWriter struct {
	preparer batchPreparer
}

func NewChHeatmapWriter(pool *Pool) *ChHeatmapWriter {
	if pool == nil {
		return &ChHeatmapWriter{}
	}
	return &ChHeatmapWriter{preparer: pool}
}

func NewChHeatmapWriterWithPreparer(preparer BatchPreparer) *ChHeatmapWriter {
	return &ChHeatmapWriter{preparer: preparer}
}

func (w *ChHeatmapWriter) Save(ctx context.Context, artifact insightsdomain.HeatmapArtifactV1, sourceIdempotencyKey string) *problem.Problem {
	if w == nil || w.preparer == nil {
		return problem.New(problem.ValidationFailed, "clickhouse heatmap writer is nil")
	}
	if p := artifact.Validate(); p != nil {
		return p
	}
	if strings.TrimSpace(sourceIdempotencyKey) == "" {
		return problem.New(problem.ValidationFailed, "clickhouse heatmap source idempotency key must not be empty")
	}
	const insertSQL = `
INSERT INTO aggregation_heatmap_cold (
    venue,
    instrument,
    timeframe,
    window_start,
    window_end,
    price_bucket_low,
    price_bucket_high,
    size_bucket,
    bid_liquidity,
    ask_liquidity,
    trade_volume,
    seq_min,
    seq_max,
    samples,
    source_idempotency_key,
    idempotency_key
)`
	batch, p := w.preparer.PrepareInsert(ctx, insertSQL)
	if p != nil {
		return problem.Wrap(p, problem.Unavailable, "clickhouse heatmap prepare batch failed")
	}
	defer func() {
		_ = batch.Close()
	}()

	baseKey := sharedhash.HashFieldsFast(
		artifact.Venue,
		artifact.Instrument,
		artifact.Timeframe,
		strconv.FormatInt(artifact.WindowStartTs, 10),
		sourceIdempotencyKey,
	)
	for _, cell := range artifact.Cells {
		idempotencyKey := sharedhash.HashFieldsFast(
			baseKey,
			strconv.FormatFloat(cell.PriceBucketLow, 'f', -1, 64),
			strconv.FormatFloat(cell.PriceBucketHigh, 'f', -1, 64),
			strings.ToUpper(strings.TrimSpace(cell.SizeBucket)),
		)
		if p := batch.AppendRow(
			ctx,
			artifact.Venue,
			artifact.Instrument,
			artifact.Timeframe,
			artifact.WindowStartTs,
			artifact.WindowEndTs,
			cell.PriceBucketLow,
			cell.PriceBucketHigh,
			cell.SizeBucket,
			cell.BidLiquidity,
			cell.AskLiquidity,
			cell.TradeVolume,
			cell.SeqMin,
			cell.SeqMax,
			cell.Samples,
			sourceIdempotencyKey,
			idempotencyKey,
		); p != nil {
			return problem.Wrap(p, problem.Unavailable, "clickhouse heatmap append failed")
		}
	}
	if _, p := batch.Flush(ctx); p != nil {
		return problem.Wrap(p, problem.Unavailable, "clickhouse heatmap batch send failed")
	}
	metrics.IncProcessorCommit("heatmap_cold")
	return nil
}
