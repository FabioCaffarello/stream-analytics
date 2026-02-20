package timescale

import (
	"context"
	"strings"
	"sync"

	adapterstorage "github.com/market-raccoon/internal/adapters/storage"
	insightsdomain "github.com/market-raccoon/internal/core/insights/domain"
	"github.com/market-raccoon/internal/shared/ids"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/problem"
)

// HeatmapWriter persists heatmap artifacts.
// When exec is configured, writes go to Timescale; otherwise it falls back to
// in-memory idempotent storage for tests/dev.
type HeatmapWriter struct {
	exec adapterstorage.SQLExecutor

	mu      sync.RWMutex
	byKey   map[string]insightsdomain.HeatmapArtifactV1
	commits int
}

func NewHeatmapWriter(pool ...*Pool) *HeatmapWriter {
	w := &HeatmapWriter{byKey: make(map[string]insightsdomain.HeatmapArtifactV1)}
	if len(pool) > 0 && pool[0] != nil {
		w.exec = pool[0]
	}
	return w
}

func NewHeatmapWriterWithExecutor(exec adapterstorage.SQLExecutor) *HeatmapWriter {
	return &HeatmapWriter{
		exec:  exec,
		byKey: make(map[string]insightsdomain.HeatmapArtifactV1),
	}
}

func NewPgHeatmapWriter(pool *Pool) *HeatmapWriter {
	if pool == nil {
		return &HeatmapWriter{byKey: make(map[string]insightsdomain.HeatmapArtifactV1)}
	}
	return &HeatmapWriter{
		exec:  pool,
		byKey: make(map[string]insightsdomain.HeatmapArtifactV1),
	}
}

func (w *HeatmapWriter) Save(ctx context.Context, artifact insightsdomain.HeatmapArtifactV1, sourceIdempotencyKey string) *problem.Problem {
	if w == nil {
		return problem.New(problem.ValidationFailed, "timescale heatmap writer is nil")
	}
	if p := artifact.Validate(); p != nil {
		return p
	}
	if strings.TrimSpace(sourceIdempotencyKey) == "" {
		return problem.New(problem.ValidationFailed, "timescale heatmap source idempotency key must not be empty")
	}
	if w.exec != nil {
		if p := w.saveSQL(ctx, artifact, sourceIdempotencyKey); p != nil {
			return p
		}
		w.mu.Lock()
		w.commits++
		w.mu.Unlock()
		metrics.IncProcessorCommit("heatmap_hot")
		return nil
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

func (w *HeatmapWriter) saveSQL(ctx context.Context, artifact insightsdomain.HeatmapArtifactV1, sourceIdempotencyKey string) *problem.Problem {
	const upsertSQL = `
INSERT INTO aggregation_heatmap (
    venue,
    instrument,
    timeframe,
    window_start_ts,
    window_end_ts,
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
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
ON CONFLICT (
    venue,
    instrument,
    timeframe,
    window_start_ts,
    price_bucket_low,
    price_bucket_high,
    size_bucket,
    source_idempotency_key
) DO NOTHING`

	rowsArgs, p := adapterstorage.MarshalHeatmapCells(ctx, artifact, sourceIdempotencyKey)
	if p != nil {
		return problem.Wrap(p, problem.Unavailable, "timescale heatmap marshal failed")
	}
	for _, args := range rowsArgs {
		if _, p := w.exec.Exec(ctx, upsertSQL, args...); p != nil {
			return problem.Wrap(p, problem.Unavailable, "timescale heatmap upsert failed")
		}
	}
	return nil
}

func (w *HeatmapWriter) CommitCount() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.commits
}
