package timescale

import (
	"context"
	"math"
	"strconv"
	"sync"

	adapterstorage "github.com/market-raccoon/internal/adapters/storage"
	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	aggports "github.com/market-raccoon/internal/core/aggregation/ports"
	"github.com/market-raccoon/internal/shared/codec"
	sharedhash "github.com/market-raccoon/internal/shared/hash"
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

	bidsJSON, err := codec.Marshal(snap.Bids)
	if err != nil {
		return problem.Wrap(err, problem.Internal, "marshal bids failed")
	}
	asksJSON, err := codec.Marshal(snap.Asks)
	if err != nil {
		return problem.Wrap(err, problem.Internal, "marshal asks failed")
	}

	const upsertSQL = `
INSERT INTO aggregation_orderbook_snapshot (
    venue,
    instrument,
    seq,
    bids_json,
    asks_json,
    created_at
) VALUES ($1, $2, $3, $4, $5, NOW())
ON CONFLICT (venue, instrument, seq) DO NOTHING`

	if _, p := w.exec.Exec(
		ctx,
		upsertSQL,
		snap.BookID.Venue,
		snap.BookID.Instrument,
		snap.Seq,
		bidsJSON,
		asksJSON,
	); p != nil {
		return p
	}
	return nil
}

func snapshotFingerprint(snap aggdomain.SnapshotProduced) string {
	// Pre-allocate: venue + instrument + seq + 3 fields per level (side + price + qty).
	fields := make([]string, 0, 3+3*len(snap.Bids)+3*len(snap.Asks))
	fields = append(fields,
		snap.BookID.Venue,
		snap.BookID.Instrument,
		strconv.FormatInt(snap.Seq, 10),
	)
	for _, l := range snap.Bids {
		fields = append(fields,
			"b",
			strconv.FormatUint(math.Float64bits(float64(l.Price)), 36),
			strconv.FormatUint(math.Float64bits(float64(l.Quantity)), 36),
		)
	}
	for _, l := range snap.Asks {
		fields = append(fields,
			"a",
			strconv.FormatUint(math.Float64bits(float64(l.Price)), 36),
			strconv.FormatUint(math.Float64bits(float64(l.Quantity)), 36),
		)
	}
	return sharedhash.HashFields(fields...)
}
