package timescale

import (
	"context"
	"strconv"
	"sync"

	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	aggports "github.com/market-raccoon/internal/core/aggregation/ports"
	sharedhash "github.com/market-raccoon/internal/shared/hash"
	"github.com/market-raccoon/internal/shared/ids"
	"github.com/market-raccoon/internal/shared/problem"
)

// Writer is a minimal hot-path storage writer skeleton for Timescale.
// TODO(m1): replace in-memory map with real INSERT/UPSERT transaction.
type Writer struct {
	mu      sync.RWMutex
	byKey   map[string]aggdomain.SnapshotProduced
	commits int
}

var _ aggports.HotReadModelStore = (*Writer)(nil)

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

func snapshotFingerprint(snap aggdomain.SnapshotProduced) string {
	fields := []string{
		snap.BookID.Venue,
		snap.BookID.Instrument,
		strconv.FormatInt(snap.Seq, 10),
	}
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
	return sharedhash.HashFields(fields...)
}
