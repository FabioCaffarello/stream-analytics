package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand/v2" //nolint:gosec // deterministic seed for reproducible soak tests
	"os"
	"slices"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/market-raccoon/internal/adapters/storage/clickhouse"
	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	"github.com/market-raccoon/internal/shared/envelope"
)

// soakDuration reads SOAK_DURATION_SECONDS from the environment or defaults to 60.
func soakDuration() time.Duration {
	s := os.Getenv("SOAK_DURATION_SECONDS")
	if s == "" {
		return 60 * time.Second
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 60 * time.Second
	}
	return time.Duration(n) * time.Second
}

// soakEnvelope creates a deterministic snapshot envelope for soak testing.
func soakEnvelope(venue, instrument string, seq int64, idempotencyKey string) envelope.Envelope {
	snap := aggdomain.SnapshotProduced{
		BookID: aggdomain.BookID{
			Venue:      venue,
			Instrument: instrument,
		},
		Seq: seq,
		Bids: []aggdomain.Level{{
			Price:    aggdomain.Price(100.5),
			Quantity: 1,
		}},
		Asks: []aggdomain.Level{{
			Price:    aggdomain.Price(101.0),
			Quantity: 1,
		}},
	}
	payload, _ := json.Marshal(snap)
	return envelope.Envelope{
		Type:           "aggregation.snapshot",
		Version:        1,
		Venue:          venue,
		Instrument:     instrument,
		Seq:            seq,
		IdempotencyKey: idempotencyKey,
		Payload:        payload,
	}
}

// soakBurstState holds mutable state for the burst soak generator.
type soakBurstState struct {
	rng        *rand.Rand
	sentEnvs   []envelope.Envelope
	seqCounter int64

	totalSent      uint64
	totalUnique    uint64
	totalRedeliver uint64
	totalInvalid   uint64
	latencies      []time.Duration

	venueNames  []string
	instruments []string
}

// generateAndProcess generates one envelope (unique, redelivery, or invalid)
// and processes it through the handler, updating counters.
func (s *soakBurstState) generateAndProcess(
	t *testing.T,
	b *StoreBatcher,
	logger *slog.Logger,
	redeliveryRate, decodeFailRate float64,
) {
	t.Helper()

	venue := s.venueNames[s.rng.IntN(len(s.venueNames))]  //nolint:gosec // bounded by len
	inst := s.instruments[s.rng.IntN(len(s.instruments))] //nolint:gosec // bounded by len

	var env envelope.Envelope
	isRedeliver := false
	isInvalid := false

	roll := s.rng.Float64() //nolint:gosec // deterministic test seed
	switch {
	case roll < decodeFailRate:
		isInvalid = true
		s.seqCounter++
		key := fmt.Sprintf("inv-%d", s.seqCounter)
		env = envelope.Envelope{
			Type:           "aggregation.snapshot",
			Version:        1,
			Venue:          venue,
			Instrument:     inst,
			Seq:            s.seqCounter,
			IdempotencyKey: key,
			Payload:        []byte(`{corrupt json`),
		}
	case roll < decodeFailRate+redeliveryRate && len(s.sentEnvs) > 0:
		isRedeliver = true
		env = s.sentEnvs[s.rng.IntN(len(s.sentEnvs))] //nolint:gosec // bounded by len
	default:
		s.seqCounter++
		key := fmt.Sprintf("snap-%d", s.seqCounter)
		env = soakEnvelope(venue, inst, s.seqCounter, key)
		s.sentEnvs = append(s.sentEnvs, env)
	}

	started := time.Now()
	p := handleStoreEnvelope(context.Background(), env, b, logger)
	s.latencies = append(s.latencies, time.Since(started))

	s.totalSent++
	switch {
	case isInvalid:
		s.totalInvalid++
		if p == nil {
			t.Fatalf("expected problem for invalid payload at msg %d", s.totalSent)
		}
	case isRedeliver:
		s.totalRedeliver++
		if p != nil {
			t.Fatalf("redelivery should not fail at msg %d: %v", s.totalSent, p)
		}
	default:
		s.totalUnique++
		if p != nil {
			t.Fatalf("unique snapshot should not fail at msg %d: %v", s.totalSent, p)
		}
	}
}

// TestStoreSoak_BatchDedupBurst sends a high volume of envelopes through the
// store handler pipeline, including deliberate redeliveries and invalid
// payloads, then asserts dedup, commit counts, and latency invariants.
func TestStoreSoak_BatchDedupBurst(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping soak test in short mode")
	}

	duration := soakDuration()
	t.Logf("soak duration: %s", duration)

	writer := clickhouse.NewWriter()
	b := testBatcher(writer)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	// Reset global counter for deterministic test.
	storeConsumedCount.Store(0)

	const (
		envsPerSec     = 100
		redeliveryRate = 0.10 // 10% redelivery
		decodeFailRate = 0.01 // 1% invalid payload
	)

	state := &soakBurstState{
		rng:         rand.New(rand.NewPCG(42, 0)), //nolint:gosec // deterministic seed
		venueNames:  []string{"binance", "bybit", "okx"},
		instruments: []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "DOGEUSDT"},
	}

	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		batchStart := time.Now()
		for range envsPerSec {
			state.generateAndProcess(t, b, logger, redeliveryRate, decodeFailRate)
		}
		if sleepFor := time.Second - time.Since(batchStart); sleepFor > 0 {
			time.Sleep(sleepFor)
		}
	}

	// ── Assertions ───────────────────────────────────────────────────────────

	t.Logf("total sent=%d unique=%d redelivered=%d invalid=%d commits=%d",
		state.totalSent, state.totalUnique, state.totalRedeliver, state.totalInvalid, writer.CommitCount())

	// All unique snapshots committed, redeliveries deduplicated.
	commitCount := writer.CommitCount()
	if commitCount < 0 || uint64(commitCount) != state.totalUnique { //nolint:gosec // commitCount verified non-negative
		t.Fatalf("commit count=%d want=%d (unique snapshots)", commitCount, state.totalUnique)
	}

	// Counter matches total sent (including invalid and redelivered).
	if got := storeConsumedCount.Load(); got != state.totalSent {
		t.Fatalf("consumed count=%d want=%d", got, state.totalSent)
	}

	// Latency p95 < 50ms (generous for in-memory).
	assertLatencyP95(t, state.latencies, 50*time.Millisecond)
}

// assertLatencyP95 sorts latencies and checks p95 against threshold.
func assertLatencyP95(t *testing.T, latencies []time.Duration, threshold time.Duration) {
	t.Helper()
	if len(latencies) == 0 {
		return
	}
	slices.Sort(latencies)
	p95Idx := int(float64(len(latencies)) * 0.95)
	p99Idx := int(float64(len(latencies)) * 0.99)
	p95 := latencies[p95Idx]
	t.Logf("latency p50=%s p95=%s p99=%s max=%s",
		latencies[len(latencies)/2],
		p95,
		latencies[p99Idx],
		latencies[len(latencies)-1],
	)
	if p95 > threshold {
		t.Fatalf("p95 latency=%s exceeds %s threshold", p95, threshold)
	}
}

// TestStoreSoak_RedeliveryStorm replays every envelope 5x and asserts zero
// duplicates in the writer.
func TestStoreSoak_RedeliveryStorm(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping soak test in short mode")
	}

	writer := clickhouse.NewWriter()
	b := testBatcher(writer)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	// Reset global counter.
	storeConsumedCount.Store(0)

	const (
		uniqueCount   = 500
		replaysEach   = 5
		totalExpected = uniqueCount * replaysEach
	)

	// Generate unique envelopes.
	envs := make([]envelope.Envelope, uniqueCount)
	for i := range uniqueCount {
		envs[i] = soakEnvelope("binance", "BTCUSDT", int64(i+1), fmt.Sprintf("storm-%d", i+1))
	}

	var handlerErrors atomic.Int64

	// Replay each envelope 5 times.
	for range replaysEach {
		for _, env := range envs {
			p := handleStoreEnvelope(context.Background(), env, b, logger)
			if p != nil {
				handlerErrors.Add(1)
			}
		}
	}

	// ── Assertions ───────────────────────────────────────────────────────────

	t.Logf("total processed=%d unique_envs=%d replays=%d commits=%d errors=%d",
		totalExpected, uniqueCount, replaysEach, writer.CommitCount(), handlerErrors.Load())

	// Zero handler errors — all calls should be idempotent successes.
	if got := handlerErrors.Load(); got != 0 {
		t.Fatalf("handler errors=%d want=0", got)
	}

	// Exactly uniqueCount commits (no duplicates).
	if got := writer.CommitCount(); got != uniqueCount {
		t.Fatalf("commit count=%d want=%d (zero duplicates)", got, uniqueCount)
	}

	// Consumed counter = total replayed.
	if got := storeConsumedCount.Load(); got != uint64(totalExpected) {
		t.Fatalf("consumed count=%d want=%d", got, totalExpected)
	}
}
