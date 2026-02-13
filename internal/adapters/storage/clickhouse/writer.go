package clickhouse

import (
	"context"
	"sync"

	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	aggports "github.com/market-raccoon/internal/core/aggregation/ports"
	"github.com/market-raccoon/internal/shared/ids"
	"github.com/market-raccoon/internal/shared/problem"
)

// Writer is a minimal cold-path storage writer skeleton for ClickHouse.
// TODO(m1): replace in-memory map with batch insert pipeline.
type Writer struct {
	mu      sync.RWMutex
	byKey   map[string]aggdomain.SnapshotProduced
	commits int
}

var _ aggports.ColdReadModelStore = (*Writer)(nil)

func NewWriter() *Writer {
	return &Writer{byKey: make(map[string]aggdomain.SnapshotProduced)}
}

func (w *Writer) Save(_ context.Context, snap aggdomain.SnapshotProduced) *problem.Problem {
	if w == nil {
		return problem.New(problem.ValidationFailed, "clickhouse writer is nil")
	}
	key := ids.AggregationSnapshotWriteKey(
		snap.BookID.Venue,
		snap.BookID.Instrument,
		snap.Seq,
		"",
	)

	w.mu.Lock()
	defer w.mu.Unlock()

	if _, exists := w.byKey[key]; exists {
		return nil
	}
	w.byKey[key] = snap
	w.commits++
	return nil
}

func (w *Writer) CommitCount() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.commits
}
