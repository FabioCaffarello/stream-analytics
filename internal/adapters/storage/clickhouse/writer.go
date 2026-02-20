package clickhouse

import (
	"context"
	"strconv"
	"sync"

	adapterstorage "github.com/market-raccoon/internal/adapters/storage"
	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	aggports "github.com/market-raccoon/internal/core/aggregation/ports"
	sharedhash "github.com/market-raccoon/internal/shared/hash"
	"github.com/market-raccoon/internal/shared/ids"
	"github.com/market-raccoon/internal/shared/problem"
)

// Writer is a cold-path storage writer for ClickHouse snapshots.
// The in-memory map is used for tests and local dev; production deployments
// should use the real ClickHouse driver.  BatchWriter wraps this type to
// provide flush-on-size and shutdown-drain semantics.
type Writer struct {
	mu      sync.RWMutex
	byKey   map[string]aggdomain.SnapshotProduced
	commits int
}

var _ aggports.ColdReadModelStore = (*Writer)(nil)
var _ aggports.ColdReadModelStore = (*ChWriter)(nil)

func NewWriter() *Writer {
	return &Writer{byKey: make(map[string]aggdomain.SnapshotProduced)}
}

func (w *Writer) Save(ctx context.Context, snap aggdomain.SnapshotProduced) *problem.Problem {
	return w.SaveIdempotent(ctx, snap, "")
}

// SaveIdempotent persists a snapshot using a deterministic write key that
// includes sourceIdempotencyKey (typically envelope.IdempotencyKey).  Same
// (venue, instrument, seq, sourceKey) → idempotent skip; same key with
// different payload → conflict error.
func (w *Writer) SaveIdempotent(_ context.Context, snap aggdomain.SnapshotProduced, sourceIdempotencyKey string) *problem.Problem {
	if w == nil {
		return problem.New(problem.ValidationFailed, "clickhouse writer is nil")
	}
	key := ids.AggregationSnapshotWriteKey(
		snap.BookID.Venue,
		snap.BookID.Instrument,
		snap.Seq,
		sourceIdempotencyKey,
	)

	w.mu.Lock()
	defer w.mu.Unlock()

	if existing, exists := w.byKey[key]; exists {
		if snapshotFingerprint(existing) != snapshotFingerprint(snap) {
			return problem.New(problem.ValidationFailed, "clickhouse duplicate key conflict for (venue,instrument,seq,source_idempotency_key)")
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

// BatchPreparer abstracts ClickHouse batch preparation for writers.
type BatchPreparer interface {
	PrepareInsert(ctx context.Context, query string) (adapterstorage.BatchInserter, *problem.Problem)
}

type batchPreparer = BatchPreparer

// ChWriter is the production ClickHouse writer for cold snapshot persistence.
type ChWriter struct {
	preparer batchPreparer
}

func NewChWriter(pool *Pool) *ChWriter {
	if pool == nil {
		return &ChWriter{}
	}
	return &ChWriter{preparer: pool}
}

func NewChWriterWithPreparer(preparer BatchPreparer) *ChWriter {
	return &ChWriter{preparer: preparer}
}

func (w *ChWriter) Save(ctx context.Context, snap aggdomain.SnapshotProduced) *problem.Problem {
	return w.SaveIdempotent(ctx, snap, "")
}

func (w *ChWriter) SaveIdempotent(ctx context.Context, snap aggdomain.SnapshotProduced, sourceIdempotencyKey string) *problem.Problem {
	if w == nil || w.preparer == nil {
		return problem.New(problem.ValidationFailed, "clickhouse writer is nil")
	}

	bidsJSON, asksJSON, p := adapterstorage.MarshalAggregationSnapshot(ctx, snap)
	if p != nil {
		return p
	}

	const insertSQL = `
INSERT INTO aggregation_orderbook_snapshot_cold (
    venue,
    instrument,
    seq,
    bids_json,
    asks_json,
    source_idempotency_key
)`

	batch, p := w.preparer.PrepareInsert(ctx, insertSQL)
	if p != nil {
		return p
	}
	defer func() {
		_ = batch.Close()
	}()

	if p := batch.AppendRow(
		ctx,
		snap.BookID.Venue,
		snap.BookID.Instrument,
		snap.Seq,
		string(bidsJSON),
		string(asksJSON),
		sourceIdempotencyKey,
	); p != nil {
		return p
	}
	if _, p := batch.Flush(ctx); p != nil {
		return p
	}
	return nil
}

func snapshotFingerprint(snap aggdomain.SnapshotProduced) string {
	// Pre-size: 3 base fields + 3 per bid ("b", price, qty) + 3 per ask ("a", price, qty).
	fields := make([]string, 0, 3+3*len(snap.Bids)+3*len(snap.Asks))
	fields = append(fields,
		snap.BookID.Venue,
		snap.BookID.Instrument,
		strconv.FormatInt(snap.Seq, 10),
	)
	for _, l := range snap.Bids {
		fields = append(fields,
			"b",
			strconv.FormatFloat(float64(l.Price), 'f', -1, 64),
			strconv.FormatFloat(float64(l.Quantity), 'f', -1, 64),
		)
	}
	for _, l := range snap.Asks {
		fields = append(fields,
			"a",
			strconv.FormatFloat(float64(l.Price), 'f', -1, 64),
			strconv.FormatFloat(float64(l.Quantity), 'f', -1, 64),
		)
	}
	return sharedhash.HashFieldsFast(fields...)
}
