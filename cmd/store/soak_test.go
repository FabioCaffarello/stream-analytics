//go:build soak
// +build soak

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
	"github.com/market-raccoon/internal/shared/problem"
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
	b *storeWriters,
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
	b := testStoreWriters(writer)
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
	b := testStoreWriters(writer)
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

// ── cold-path burst 10k/s (C3) ───────────────────────────────────────────────

// soakBurstRate reads SOAK_BURST_RATE from the environment or defaults to 10000.
func soakBurstRate() int {
	s := os.Getenv("SOAK_BURST_RATE")
	if s == "" {
		return 10_000
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 10_000
	}
	return n
}

// burstEnvResult tags the kind of envelope generated by burstGenerateEnvelope.
type burstEnvResult struct {
	env         envelope.Envelope
	isInvalid   bool
	isRedeliver bool
}

// burstGenerateEnvelope creates one envelope for the burst soak: unique,
// redelivery, or invalid payload based on RNG roll.
func burstGenerateEnvelope(state *soakBurstState, decodeFailRate, redeliveryRate float64) burstEnvResult {
	venue := state.venueNames[state.rng.IntN(len(state.venueNames))]  //nolint:gosec // bounded
	inst := state.instruments[state.rng.IntN(len(state.instruments))] //nolint:gosec // bounded

	roll := state.rng.Float64() //nolint:gosec // deterministic test seed
	switch {
	case roll < decodeFailRate:
		state.seqCounter++
		return burstEnvResult{
			env: envelope.Envelope{
				Type: "aggregation.snapshot", Version: 1,
				Venue: venue, Instrument: inst,
				Seq: state.seqCounter, IdempotencyKey: fmt.Sprintf("inv-%d", state.seqCounter),
				Payload: []byte(`{corrupt json`),
			},
			isInvalid: true,
		}
	case roll < decodeFailRate+redeliveryRate && len(state.sentEnvs) > 0:
		return burstEnvResult{
			env:         state.sentEnvs[state.rng.IntN(len(state.sentEnvs))], //nolint:gosec // bounded
			isRedeliver: true,
		}
	default:
		state.seqCounter++
		env := soakEnvelope(venue, inst, state.seqCounter, fmt.Sprintf("burst-%d", state.seqCounter))
		state.sentEnvs = append(state.sentEnvs, env)
		return burstEnvResult{env: env}
	}
}

// burstCheckAckInvariant verifies that the commit count delta matches the
// envelope kind after a handler call.  Returns 1 if a violation is detected.
func burstCheckAckInvariant(
	t *testing.T,
	state *soakBurstState,
	r burstEnvResult,
	p *problem.Problem,
	commitBefore, commitAfter int,
) int {
	t.Helper()
	switch {
	case r.isInvalid:
		state.totalInvalid++
		if p == nil {
			t.Fatalf("expected problem for invalid payload at msg %d", state.totalSent)
		}
		if commitAfter != commitBefore {
			return 1
		}
	case r.isRedeliver:
		state.totalRedeliver++
		if p != nil {
			t.Fatalf("redelivery should not fail at msg %d: %v", state.totalSent, p)
		}
	default:
		state.totalUnique++
		if p != nil {
			t.Fatalf("unique snapshot should not fail at msg %d: %v", state.totalSent, p)
		}
		if commitAfter <= commitBefore {
			return 1
		}
	}
	return 0
}

// assertBurstInvariants checks final soak state for the burst test.
func assertBurstInvariants(t *testing.T, state *soakBurstState, writer *clickhouse.Writer, violations int64) {
	t.Helper()

	t.Logf("burst soak: sent=%d unique=%d redelivered=%d invalid=%d commits=%d",
		state.totalSent, state.totalUnique, state.totalRedeliver, state.totalInvalid, writer.CommitCount())

	commitCount := writer.CommitCount()
	if commitCount < 0 || uint64(commitCount) != state.totalUnique { //nolint:gosec // commitCount verified non-negative
		t.Fatalf("commit count=%d want=%d (unique snapshots, zero loss)", commitCount, state.totalUnique)
	}
	if got := storeConsumedCount.Load(); got != state.totalSent {
		t.Fatalf("consumed count=%d want=%d", got, state.totalSent)
	}
	if violations != 0 {
		t.Fatalf("ack-on-commit violations=%d (ack fired without commit or commit missed)", violations)
	}
	assertLatencyP95(t, state.latencies, 50*time.Millisecond)
}

// TestStoreSoak_ColdPathBurst10k_CommitAckInvariants sends 10k envelopes/s
// for 60s through the store handler pipeline and asserts:
//   - Every unique snapshot is committed exactly once (zero loss, zero dup).
//   - Every redelivery is deduplicated (commit count = unique count).
//   - Ack-on-commit: handler returns nil ONLY when the underlying writer
//     has successfully committed (verified via pre/post commit count delta).
//   - Consumed counter matches total sent.
func TestStoreSoak_ColdPathBurst10k_CommitAckInvariants(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping soak test in short mode")
	}

	duration := soakDuration()
	rate := soakBurstRate()
	t.Logf("cold-path burst soak: rate=%d/s duration=%s", rate, duration)

	writer := clickhouse.NewWriter()
	b := testStoreWriters(writer)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	storeConsumedCount.Store(0)

	const (
		redeliveryRate = 0.05
		decodeFailRate = 0.01
	)

	state := &soakBurstState{
		rng:         rand.New(rand.NewPCG(99, 0)), //nolint:gosec // deterministic seed
		venueNames:  []string{"binance", "bybit", "okx", "kraken", "coinbase"},
		instruments: []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "DOGEUSDT", "AVAXUSDT", "LINKUSDT", "MATICUSDT", "DOTUSDT"},
	}

	var ackCommitViolations atomic.Int64

	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		batchStart := time.Now()
		for range rate {
			commitBefore := writer.CommitCount()
			r := burstGenerateEnvelope(state, decodeFailRate, redeliveryRate)

			started := time.Now()
			p := handleStoreEnvelope(context.Background(), r.env, b, logger)
			state.latencies = append(state.latencies, time.Since(started))
			state.totalSent++

			v := burstCheckAckInvariant(t, state, r, p, commitBefore, writer.CommitCount())
			ackCommitViolations.Add(int64(v))
		}
		if sleepFor := time.Second - time.Since(batchStart); sleepFor > 0 {
			time.Sleep(sleepFor)
		}
	}

	assertBurstInvariants(t, state, writer, ackCommitViolations.Load())
}

// ── cold-path commit latency budgets (C4) ────────────────────────────────────

// latencyBudget defines a percentile threshold for latency assertions.
type latencyBudget struct {
	percentile float64
	threshold  time.Duration
}

// assertLatencyBudgets checks multiple percentile thresholds.
func assertLatencyBudgets(t *testing.T, latencies []time.Duration, budgets []latencyBudget) {
	t.Helper()
	if len(latencies) == 0 {
		return
	}
	slices.Sort(latencies)
	n := len(latencies)
	p50 := latencies[n/2]
	t.Logf("latency p50=%s n=%d", p50, n)

	for _, b := range budgets {
		idx := int(float64(n) * b.percentile)
		if idx >= n {
			idx = n - 1
		}
		val := latencies[idx]
		t.Logf("  p%.0f=%s budget=%s", b.percentile*100, val, b.threshold)
		if val > b.threshold {
			t.Fatalf("p%.0f latency=%s exceeds budget %s", b.percentile*100, val, b.threshold)
		}
	}
}

// TestStoreSoak_ColdPathLatencyBudgets sends a sustained burst and enforces
// explicit p95 and p99 commit latency budgets.
func TestStoreSoak_ColdPathLatencyBudgets(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping soak test in short mode")
	}

	duration := soakDuration()
	rate := soakBurstRate()
	t.Logf("latency budget soak: rate=%d/s duration=%s", rate, duration)

	writer := clickhouse.NewWriter()
	b := testStoreWriters(writer)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	storeConsumedCount.Store(0)

	state := &soakBurstState{
		rng:         rand.New(rand.NewPCG(777, 0)), //nolint:gosec // deterministic seed
		venueNames:  []string{"binance", "bybit", "okx"},
		instruments: []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "DOGEUSDT"},
	}

	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		batchStart := time.Now()
		for range rate {
			state.generateAndProcess(t, b, logger, 0.05, 0.01)
		}
		if sleepFor := time.Second - time.Since(batchStart); sleepFor > 0 {
			time.Sleep(sleepFor)
		}
	}

	t.Logf("latency budget soak: sent=%d unique=%d redelivered=%d invalid=%d commits=%d",
		state.totalSent, state.totalUnique, state.totalRedeliver, state.totalInvalid, writer.CommitCount())

	// Enforce explicit p95 and p99 budgets for the cold-path commit pipeline.
	assertLatencyBudgets(t, state.latencies, []latencyBudget{
		{percentile: 0.95, threshold: 10 * time.Millisecond},
		{percentile: 0.99, threshold: 25 * time.Millisecond},
	})
}

// ── storage-slow soak (S5-D3) ────────────────────────────────────────────────

// slowWriter wraps a clickhouse.SnapshotWriter and injects artificial latency
// before each call to SaveIdempotent, simulating a degraded storage backend.
type slowWriter struct {
	delegate clickhouse.SnapshotWriter
	latency  time.Duration
}

func (w *slowWriter) SaveIdempotent(ctx context.Context, snap aggdomain.SnapshotProduced, sourceIdempotencyKey string) *problem.Problem {
	select {
	case <-time.After(w.latency):
	case <-ctx.Done():
		return problem.Wrap(ctx.Err(), problem.Unavailable, "slow writer: context cancelled during latency injection")
	}
	return w.delegate.SaveIdempotent(ctx, snap, sourceIdempotencyKey)
}

// TestStoreSoak_StorageSlow processes envelopes through a writer with
// artificial latency and asserts:
//   - ACK only occurs after commit (handler returns nil only after slow write).
//   - Redelivery does not duplicate writes (idempotency maintained).
//   - Observed handler latency reflects injected latency.
func TestStoreSoak_StorageSlow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping soak test in short mode")
	}

	const (
		injectedLatency = 10 * time.Millisecond
		uniqueCount     = 200
		replaysEach     = 3
		totalExpected   = uniqueCount * (1 + replaysEach) // first delivery + replays
	)

	writer := clickhouse.NewWriter()
	sw := &slowWriter{delegate: writer, latency: injectedLatency}
	b := &storeWriters{
		batcher: clickhouse.NewBatchWriter(sw, defaultBatchCfg()),
		candle:  clickhouse.NewChCandleWriter(nil),
		stats:   clickhouse.NewChStatsWriter(nil),
		heatmap: clickhouse.NewChHeatmapWriter(nil),
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	// Reset global counter for deterministic test.
	storeConsumedCount.Store(0)

	// Generate unique envelopes.
	envs := make([]envelope.Envelope, uniqueCount)
	for i := range uniqueCount {
		envs[i] = soakEnvelope("binance", "BTCUSDT", int64(i+1), fmt.Sprintf("slow-%d", i+1))
	}

	latencies := make([]time.Duration, 0, totalExpected)

	// First pass: all unique — each must take >= injectedLatency.
	for _, env := range envs {
		started := time.Now()
		p := handleStoreEnvelope(context.Background(), env, b, logger)
		elapsed := time.Since(started)
		latencies = append(latencies, elapsed)
		if p != nil {
			t.Fatalf("unique write should succeed: %v", p)
		}
	}

	// Replay passes: simulate redelivery — all must be idempotent successes
	// and still pay the latency cost.
	for replay := range replaysEach {
		for _, env := range envs {
			started := time.Now()
			p := handleStoreEnvelope(context.Background(), env, b, logger)
			elapsed := time.Since(started)
			latencies = append(latencies, elapsed)
			if p != nil {
				t.Fatalf("redelivery (replay=%d) should succeed idempotently: %v", replay+1, p)
			}
		}
	}

	// ── Assertions ───────────────────────────────────────────────────────────

	t.Logf("total processed=%d unique=%d replays=%d commits=%d",
		totalExpected, uniqueCount, replaysEach, writer.CommitCount())

	// 1. Exactly uniqueCount commits — redeliveries must be deduplicated.
	if got := writer.CommitCount(); got != uniqueCount {
		t.Fatalf("commit count=%d want=%d (zero duplicates despite slow writes)", got, uniqueCount)
	}

	// 2. Consumed counter = total processed.
	if got := storeConsumedCount.Load(); got != uint64(totalExpected) {
		t.Fatalf("consumed count=%d want=%d", got, totalExpected)
	}

	// 3. Latency p50 must be >= injectedLatency (proves we actually waited).
	slices.Sort(latencies)
	p50 := latencies[len(latencies)/2]
	p95Idx := int(float64(len(latencies)) * 0.95)
	p95 := latencies[p95Idx]
	t.Logf("latency p50=%s p95=%s max=%s (injected=%s)",
		p50, p95, latencies[len(latencies)-1], injectedLatency)

	if p50 < injectedLatency {
		t.Fatalf("p50 latency=%s is below injected latency=%s — slow writer not effective", p50, injectedLatency)
	}

	// 4. Latency p95 should be reasonable (injected + small overhead).
	// Allow 5x injected as ceiling for CI variance.
	maxAllowed := 5 * injectedLatency
	if p95 > maxAllowed {
		t.Fatalf("p95 latency=%s exceeds %s — unexpected overhead beyond injected latency", p95, maxAllowed)
	}
}
