package clickhouse

import (
	"context"
	"sync"

	insightsdomain "github.com/market-raccoon/internal/core/insights/domain"
	"github.com/market-raccoon/internal/shared/ids"
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
	seqMax := artifact.Cells[len(artifact.Cells)-1].SeqMax
	key := ids.HeatmapArtifactWriteKey(
		artifact.Venue,
		artifact.Instrument,
		artifact.Timeframe,
		artifact.WindowStartTs,
		seqMax,
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
