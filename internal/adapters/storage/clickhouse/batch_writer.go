package clickhouse

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	aggdomain "github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/domain"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/config"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/metrics"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

// SnapshotWriter is the minimal write interface consumed by BatchWriter.
// Both *Writer and test decorators (e.g. slow-writer) satisfy this contract.
type SnapshotWriter interface {
	SaveIdempotent(ctx context.Context, snap aggdomain.SnapshotProduced, sourceIdempotencyKey string) *problem.Problem
}

// batchItem holds a single enqueued snapshot with its dedup key.
type batchItem struct {
	snap     aggdomain.SnapshotProduced
	dedupKey string
}

// estimatePayloadSize returns a rough byte size for batch-size accounting.
// Uses O(1) arithmetic instead of JSON marshaling to avoid hot-path allocations.
// Each Level ≈ 40 bytes JSON (two float fields + formatting); base overhead
// covers BookID strings, Seq, and JSON structure.
func estimatePayloadSize(snap aggdomain.SnapshotProduced) int {
	const (
		baseOverhead  = 80 // BookID (venue+instrument) + seq + JSON structure
		bytesPerLevel = 40 // {"price":12345.67,"quantity":0.123} ≈ 40 bytes
	)
	return baseOverhead + (len(snap.Bids)+len(snap.Asks))*bytesPerLevel
}

// BatchWriter accumulates snapshots and flushes them to the underlying writer
// when any configured threshold is met.  With the default max_rows=1, every
// Write call flushes immediately — preserving ack-on-commit semantics.  The
// infrastructure is ready for batch-size>1 when concurrent dispatch arrives.
//
// Timer/interval logic is confined to this adapter layer; domain code never
// references time-based flush.
type BatchWriter struct {
	writer SnapshotWriter
	cfg    config.StoreBatchConfig

	closed       atomic.Bool
	mu           sync.Mutex
	pending      []batchItem
	pendingBytes int
	lastFlush    time.Time
}

// NewBatchWriter creates a batch writer with the given config.
func NewBatchWriter(writer SnapshotWriter, cfg config.StoreBatchConfig) *BatchWriter {
	return &BatchWriter{
		writer:    writer,
		cfg:       cfg,
		lastFlush: time.Now(),
	}
}

// Write enqueues a snapshot and flushes synchronously if any threshold is met.
// Returns nil only after the batch containing this item has been flushed.
func (b *BatchWriter) Write(ctx context.Context, snap aggdomain.SnapshotProduced, dedupKey string) *problem.Problem {
	if b == nil || b.writer == nil {
		return problem.New(problem.ValidationFailed, "batch writer or underlying writer is nil")
	}
	b.mu.Lock()
	if b.closed.Load() {
		b.mu.Unlock()
		return problem.New(problem.ValidationFailed, "batch writer is closed")
	}

	b.pending = append(b.pending, batchItem{snap: snap, dedupKey: dedupKey})
	b.pendingBytes += estimatePayloadSize(snap)

	if b.shouldFlush() {
		items := b.pending
		b.pending = nil
		b.pendingBytes = 0
		b.lastFlush = time.Now()
		b.mu.Unlock()
		return b.flush(ctx, items)
	}

	b.mu.Unlock()
	return nil
}

// shouldFlush returns true when any configured threshold is met.
// Caller must hold b.mu.
func (b *BatchWriter) shouldFlush() bool {
	if len(b.pending) >= b.cfg.MaxRows {
		return true
	}
	if b.cfg.MaxBytes > 0 && b.pendingBytes >= b.cfg.MaxBytes {
		return true
	}
	if time.Since(b.lastFlush) >= b.cfg.FlushIntervalDuration() {
		return true
	}
	return false
}

// flush writes all items to the underlying writer and emits metrics.
// Returns on the first error; remaining items will be redelivered by JetStream.
func (b *BatchWriter) flush(ctx context.Context, items []batchItem) *problem.Problem {
	started := time.Now()
	metrics.ObserveStoreBatchSize(len(items))

	for _, item := range items {
		if p := b.writer.SaveIdempotent(ctx, item.snap, item.dedupKey); p != nil {
			metrics.IncStoreFlush("failed")
			metrics.ObserveStoreFlushLatency(time.Since(started))
			return p
		}
	}

	metrics.IncStoreFlush("ok")
	metrics.ObserveStoreFlushLatency(time.Since(started))
	return nil
}

// Close flushes any remaining pending items (shutdown drain).
// After Close returns, any subsequent Write call returns an error.
func (b *BatchWriter) Close(ctx context.Context) *problem.Problem {
	b.closed.Store(true)
	b.mu.Lock()
	items := b.pending
	b.pending = nil
	b.pendingBytes = 0
	b.mu.Unlock()

	if len(items) == 0 {
		return nil
	}
	return b.flush(ctx, items)
}
