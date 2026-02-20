package timescale

import (
	"context"
	"sync"

	adapterstorage "github.com/market-raccoon/internal/adapters/storage"
	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	aggports "github.com/market-raccoon/internal/core/aggregation/ports"
	"github.com/market-raccoon/internal/shared/ids"
	"github.com/market-raccoon/internal/shared/problem"
)

// Writer is an in-memory fallback hot-path storage writer for Timescale.
type Writer struct {
	mu      sync.RWMutex
	byKey   map[string]aggdomain.SnapshotProduced
	commits int
}

var _ aggports.HotReadModelStore = (*Writer)(nil)
var _ aggports.HotReadModelStore = (*PgWriter)(nil)

func NewWriter() *Writer {
	return &Writer{byKey: make(map[string]aggdomain.SnapshotProduced)}
}

func (w *Writer) Save(_ context.Context, snap aggdomain.SnapshotProduced) *problem.Problem {
	if w == nil {
		return problem.New(problem.ValidationFailed, "timescale writer is nil")
	}
	key := ids.AggregationSnapshotWriteKey(
		snap.BookID.Venue,
		snap.BookID.Instrument,
		snap.Seq,
		"",
	)

	w.mu.Lock()
	defer w.mu.Unlock()

	if existing, exists := w.byKey[key]; exists {
		if snapshotFingerprint(existing) != snapshotFingerprint(snap) {
			return problem.New(problem.ValidationFailed, "timescale duplicate key conflict for (venue,instrument,seq)")
		}
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

// PgWriter is the production Timescale writer for orderbook snapshots.
type PgWriter struct {
	exec adapterstorage.SQLExecutor
}

func NewPgWriter(pool *Pool) *PgWriter {
	if pool == nil {
		return &PgWriter{}
	}
	return &PgWriter{exec: pool}
}

func NewPgWriterWithExecutor(exec adapterstorage.SQLExecutor) *PgWriter {
	return &PgWriter{exec: exec}
}

func (w *PgWriter) Save(ctx context.Context, snap aggdomain.SnapshotProduced) *problem.Problem {
	if w == nil || w.exec == nil {
		return problem.New(problem.ValidationFailed, "timescale pg writer is nil")
	}
	return adapterstorage.UpsertAggregationSnapshot(ctx, w.exec, snap)
}

func snapshotFingerprint(snap aggdomain.SnapshotProduced) string {
	return adapterstorage.SnapshotFingerprint(snap)
}
