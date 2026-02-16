package main

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	"github.com/market-raccoon/internal/shared/config"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/problem"
)

// StoreWriter is the minimal interface for the cold-path writer used by the batcher.
type StoreWriter interface {
	SaveIdempotent(ctx context.Context, snap aggdomain.SnapshotProduced, sourceIdempotencyKey string) *problem.Problem
}

// batchItem holds a single enqueued snapshot with its dedup key.
type batchItem struct {
	snap     aggdomain.SnapshotProduced
	dedupKey string
}

// estimatePayloadSize returns a rough byte size for batch-size accounting.
func estimatePayloadSize(snap aggdomain.SnapshotProduced) int {
	data, _ := json.Marshal(snap)
	return len(data)
}

// StoreBatcher accumulates snapshots and flushes them to the ClickHouse writer
// when any configured threshold is met.  With the current serial JetStream
// consumer (Fetch(1)), the default max_rows=1 makes every Write call flush
// immediately — preserving ack-on-commit semantics.  The infrastructure is
// ready for batch-size>1 when concurrent dispatch arrives.
type StoreBatcher struct {
	writer StoreWriter
	cfg    config.StoreBatchConfig

	mu           sync.Mutex
	pending      []batchItem
	pendingBytes int
	lastFlush    time.Time
}

// NewStoreBatcher creates a batcher with the given config.
func NewStoreBatcher(writer StoreWriter, cfg config.StoreBatchConfig) *StoreBatcher {
	return &StoreBatcher{
		writer:    writer,
		cfg:       cfg,
		lastFlush: time.Now(),
	}
}

// Write enqueues a snapshot and flushes synchronously if any threshold is met.
// Returns nil only after the batch containing this item has been flushed.
func (b *StoreBatcher) Write(ctx context.Context, snap aggdomain.SnapshotProduced, dedupKey string) *problem.Problem {
	if b == nil || b.writer == nil {
		return problem.New(problem.ValidationFailed, "store batcher or writer is nil")
	}
	b.mu.Lock()

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
func (b *StoreBatcher) shouldFlush() bool {
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

// flush writes all items to the ClickHouse writer and emits metrics.
// Returns on the first error; remaining items will be redelivered by JetStream.
func (b *StoreBatcher) flush(ctx context.Context, items []batchItem) *problem.Problem {
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

// Close flushes any remaining pending items.
func (b *StoreBatcher) Close(ctx context.Context) *problem.Problem {
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
