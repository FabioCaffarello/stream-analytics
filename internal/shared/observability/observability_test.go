package observability

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// helpers: global state reset between test groups
// ---------------------------------------------------------------------------

// resetOverloadGlobal replaces the global overload store with a fresh instance.
func resetOverloadGlobal() {
	globalPolicyKitOverloadStore = newPolicyKitOverloadStore()
}

// resetShardGlobal zeroes every field of the global shard state store.
func resetShardGlobal() {
	atomic.StoreInt32(&globalShardStateStore.shardIndex, 0)
	atomic.StoreInt32(&globalShardStateStore.shardCount, 0)
	atomic.StoreInt64(&globalShardStateStore.lag, 0)
	atomic.StoreUint64(&globalShardStateStore.eventsTotal, 0)
	atomic.StoreUint64(&globalShardStateStore.skipTotal, 0)
	atomic.StoreInt64(&globalShardStateStore.budget, 0)
	atomic.StoreUint32(&globalShardStateStore.configured, 0)
}

// resetStorageGlobal replaces the global storage state store with a fresh instance.
func resetStorageGlobal() {
	globalStorageStateStore = newStorageStateStore()
}

// resetWSGlobal zeroes every field of the global WS state store.
func resetWSGlobal() {
	atomic.StoreInt64(&globalWSStateStore.sessionsActive, 0)
	atomic.StoreInt64(&globalWSStateStore.preferProtoSessions, 0)
	atomic.StoreUint64(&globalWSStateStore.deliveriesProtoTotal, 0)
	atomic.StoreUint64(&globalWSStateStore.deliveriesJSONTotal, 0)
	atomic.StoreUint64(&globalWSStateStore.reconnectsTotal, 0)
	atomic.StoreUint32(&globalWSStateStore.sessionsActiveKnown, 0)
	atomic.StoreUint32(&globalWSStateStore.preferProtoSessionsKnown, 0)
	atomic.StoreUint32(&globalWSStateStore.deliveriesProtoTotalKnown, 0)
	atomic.StoreUint32(&globalWSStateStore.deliveriesJSONTotalKnown, 0)
	atomic.StoreUint32(&globalWSStateStore.reconnectsTotalKnown, 0)
}

// ===========================================================================
// 1. bus.go -- NopBusObserver
// ===========================================================================

func TestNopBusObserver_ReturnsNonNil(t *testing.T) {
	t.Parallel()
	obs := NopBusObserver()
	if obs == nil {
		t.Fatal("NopBusObserver() returned nil")
	}
}

func TestNopBusObserver_ImplementsBusObserverInterface(t *testing.T) {
	t.Parallel()
	var _ = (BusObserver)(NopBusObserver())
}

func TestNopBusObserver_AllMethodsCallableWithoutPanic(t *testing.T) {
	t.Parallel()
	obs := NopBusObserver()

	// Each call must complete without panic. The test implicitly passes
	// if no panic is raised.
	obs.IncPublished("trade", "binance")
	obs.IncPublished("", "")
	obs.IncDropped(0)
	obs.IncDropped(-1)
	obs.IncDropped(999)
	obs.IncPublishError("timeout")
	obs.IncPublishError("")
	obs.ObservePublishLatency("nats", 0)
	obs.ObservePublishLatency("inmem", time.Second)
	obs.ObservePublishLatency("", -time.Millisecond)
	obs.IncConsumed("nats", "ok")
	obs.IncConsumed("", "")
	obs.IncRedelivered("nats")
	obs.IncRedelivered("")
	obs.ObserveAckLatency("nats", time.Millisecond)
	obs.ObserveAckLatency("", 0)
	obs.SetConsumerLag("nats", 100)
	obs.SetConsumerLag("", 0)
	obs.SetConsumerLag("nats", -1)
}

func TestNopBusObserver_MultipleCallsReturnSameType(t *testing.T) {
	t.Parallel()
	a := NopBusObserver()
	b := NopBusObserver()
	if fmt.Sprintf("%T", a) != fmt.Sprintf("%T", b) {
		t.Fatalf("expected same type, got %T and %T", a, b)
	}
}

// ===========================================================================
// 2. overload_state.go -- PolicyKitOverload store
// ===========================================================================

func TestOverload_EmptySnapshot(t *testing.T) {
	resetOverloadGlobal()
	snap := SnapshotPolicyKitOverload()
	if len(snap) != 0 {
		t.Fatalf("expected empty snapshot, got %d entries", len(snap))
	}
}

func TestOverload_SingleEntry(t *testing.T) {
	resetOverloadGlobal()
	UpdatePolicyKitOverload(PolicyKitOverloadEntry{
		Stream:        "bookdelta",
		Venue:         "binance",
		OverloadLevel: 2,
		Stride:        4,
	})
	snap := SnapshotPolicyKitOverload()
	if len(snap) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(snap))
	}
	if snap[0].Stream != "bookdelta" {
		t.Errorf("expected stream bookdelta, got %q", snap[0].Stream)
	}
	if snap[0].Venue != "binance" {
		t.Errorf("expected venue binance, got %q", snap[0].Venue)
	}
	if snap[0].OverloadLevel != 2 {
		t.Errorf("expected overload level 2, got %d", snap[0].OverloadLevel)
	}
	if snap[0].Stride != 4 {
		t.Errorf("expected stride 4, got %d", snap[0].Stride)
	}
}

func TestOverload_SanitizeEmptyStreamAndVenue(t *testing.T) {
	resetOverloadGlobal()
	UpdatePolicyKitOverload(PolicyKitOverloadEntry{
		Stream: "",
		Venue:  "",
	})
	snap := SnapshotPolicyKitOverload()
	if len(snap) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(snap))
	}
	if snap[0].Stream != "unknown" {
		t.Errorf("expected sanitized stream 'unknown', got %q", snap[0].Stream)
	}
	if snap[0].Venue != "unknown" {
		t.Errorf("expected sanitized venue 'unknown', got %q", snap[0].Venue)
	}
}

func TestOverload_UpdateExistingKey(t *testing.T) {
	resetOverloadGlobal()
	UpdatePolicyKitOverload(PolicyKitOverloadEntry{
		Stream:        "trade",
		Venue:         "bybit",
		OverloadLevel: 1,
		Stride:        2,
	})
	UpdatePolicyKitOverload(PolicyKitOverloadEntry{
		Stream:        "trade",
		Venue:         "bybit",
		OverloadLevel: 3,
		Stride:        8,
	})
	snap := SnapshotPolicyKitOverload()
	if len(snap) != 1 {
		t.Fatalf("expected 1 entry after update-in-place, got %d", len(snap))
	}
	if snap[0].OverloadLevel != 3 {
		t.Errorf("expected updated overload level 3, got %d", snap[0].OverloadLevel)
	}
	if snap[0].Stride != 8 {
		t.Errorf("expected updated stride 8, got %d", snap[0].Stride)
	}
}

func TestOverload_SnapshotPreservesInsertionOrder(t *testing.T) {
	resetOverloadGlobal()
	venues := []string{"binance", "bybit", "coinbase", "hyperliquid"}
	for _, v := range venues {
		UpdatePolicyKitOverload(PolicyKitOverloadEntry{
			Stream: "trade",
			Venue:  v,
		})
	}
	snap := SnapshotPolicyKitOverload()
	if len(snap) != len(venues) {
		t.Fatalf("expected %d entries, got %d", len(venues), len(snap))
	}
	for i, v := range venues {
		if snap[i].Venue != v {
			t.Errorf("entry[%d]: expected venue %q, got %q", i, v, snap[i].Venue)
		}
	}
}

func TestOverload_FIFOEvictionAt64Entries(t *testing.T) {
	resetOverloadGlobal()
	const cap = 64

	// Fill to capacity.
	for i := 0; i < cap; i++ {
		UpdatePolicyKitOverload(PolicyKitOverloadEntry{
			Stream: fmt.Sprintf("s%d", i),
			Venue:  "v",
		})
	}
	snap := SnapshotPolicyKitOverload()
	if len(snap) != cap {
		t.Fatalf("expected %d entries at capacity, got %d", cap, len(snap))
	}
	// Oldest entry should still be present.
	if snap[0].Stream != "s0" {
		t.Errorf("expected first entry s0, got %q", snap[0].Stream)
	}

	// Insert one more -- oldest (s0) should be evicted.
	UpdatePolicyKitOverload(PolicyKitOverloadEntry{
		Stream: "s64",
		Venue:  "v",
	})
	snap = SnapshotPolicyKitOverload()
	if len(snap) != cap {
		t.Fatalf("expected %d entries after eviction, got %d", cap, len(snap))
	}
	if snap[0].Stream != "s1" {
		t.Errorf("expected oldest surviving entry s1, got %q", snap[0].Stream)
	}
	if snap[cap-1].Stream != "s64" {
		t.Errorf("expected newest entry s64, got %q", snap[cap-1].Stream)
	}
}

func TestOverload_FIFOEvictionMultiple(t *testing.T) {
	resetOverloadGlobal()
	const cap = 64

	// Fill to capacity.
	for i := 0; i < cap; i++ {
		UpdatePolicyKitOverload(PolicyKitOverloadEntry{
			Stream: fmt.Sprintf("s%d", i),
			Venue:  "v",
		})
	}

	// Insert 5 more -- the 5 oldest should be evicted.
	for i := cap; i < cap+5; i++ {
		UpdatePolicyKitOverload(PolicyKitOverloadEntry{
			Stream: fmt.Sprintf("s%d", i),
			Venue:  "v",
		})
	}
	snap := SnapshotPolicyKitOverload()
	if len(snap) != cap {
		t.Fatalf("expected %d entries, got %d", cap, len(snap))
	}
	if snap[0].Stream != "s5" {
		t.Errorf("expected oldest surviving entry s5, got %q", snap[0].Stream)
	}
	if snap[cap-1].Stream != "s68" {
		t.Errorf("expected newest entry s68, got %q", snap[cap-1].Stream)
	}
}

func TestOverload_UpdateExistingDoesNotEvict(t *testing.T) {
	resetOverloadGlobal()
	const cap = 64

	// Fill to capacity.
	for i := 0; i < cap; i++ {
		UpdatePolicyKitOverload(PolicyKitOverloadEntry{
			Stream: fmt.Sprintf("s%d", i),
			Venue:  "v",
		})
	}

	// Update the first entry -- should NOT trigger eviction.
	UpdatePolicyKitOverload(PolicyKitOverloadEntry{
		Stream:        "s0",
		Venue:         "v",
		OverloadLevel: 99,
	})
	snap := SnapshotPolicyKitOverload()
	if len(snap) != cap {
		t.Fatalf("expected %d entries, got %d", cap, len(snap))
	}
	// s0 should still be at position 0 (order unchanged).
	if snap[0].Stream != "s0" || snap[0].OverloadLevel != 99 {
		t.Errorf("expected updated s0 at index 0 with level 99, got stream=%q level=%d",
			snap[0].Stream, snap[0].OverloadLevel)
	}
}

func TestOverload_ThresholdsPreserved(t *testing.T) {
	resetOverloadGlobal()
	entry := PolicyKitOverloadEntry{
		Stream: "bookdelta",
		Venue:  "binance",
		Thresholds: PolicyKitThresholdPair{
			Enter: PolicyKitThreshold{
				QueueRatio:   0.8,
				BacklogRatio: 0.9,
				MapRatio:     0.7,
				LatencyMs:    50.0,
			},
			Recover: PolicyKitThreshold{
				QueueRatio:   0.3,
				BacklogRatio: 0.4,
				MapRatio:     0.2,
				LatencyMs:    20.0,
			},
		},
	}
	UpdatePolicyKitOverload(entry)
	snap := SnapshotPolicyKitOverload()
	if len(snap) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(snap))
	}
	if snap[0].Thresholds.Enter.QueueRatio != 0.8 {
		t.Errorf("expected enter queue ratio 0.8, got %f", snap[0].Thresholds.Enter.QueueRatio)
	}
	if snap[0].Thresholds.Recover.LatencyMs != 20.0 {
		t.Errorf("expected recover latency 20.0, got %f", snap[0].Thresholds.Recover.LatencyMs)
	}
}

func TestOverload_ConcurrentUpdates(t *testing.T) {
	resetOverloadGlobal()
	const goroutines = 20
	const updatesPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(gID int) {
			defer wg.Done()
			for i := 0; i < updatesPerGoroutine; i++ {
				UpdatePolicyKitOverload(PolicyKitOverloadEntry{
					Stream:        fmt.Sprintf("g%d_s%d", gID, i),
					Venue:         "v",
					OverloadLevel: i,
				})
			}
		}(g)
	}
	wg.Wait()

	snap := SnapshotPolicyKitOverload()
	// Total unique keys = goroutines * updatesPerGoroutine = 1000 > 64 cap.
	// After eviction, exactly 64 should remain.
	if len(snap) != 64 {
		t.Fatalf("expected 64 entries after concurrent flood, got %d", len(snap))
	}
}

func TestOverload_SnapshotIsACopy(t *testing.T) {
	resetOverloadGlobal()
	UpdatePolicyKitOverload(PolicyKitOverloadEntry{
		Stream: "trade",
		Venue:  "binance",
	})
	snap1 := SnapshotPolicyKitOverload()

	// Mutate the returned slice.
	snap1[0].OverloadLevel = 999

	// A new snapshot must be unaffected.
	snap2 := SnapshotPolicyKitOverload()
	if snap2[0].OverloadLevel == 999 {
		t.Error("snapshot mutation leaked into store -- snapshot is not a copy")
	}
}

// ===========================================================================
// 3. shard_state.go -- Shard topology + counters
// ===========================================================================

func TestShard_InitialState(t *testing.T) {
	resetShardGlobal()
	if ShardConfigured() {
		t.Error("expected ShardConfigured() == false before SetShardTopology")
	}
	snap := SnapshotShardState()
	if snap.ShardIndex != 0 || snap.ShardCount != 0 {
		t.Errorf("expected zero index/count, got %d/%d", snap.ShardIndex, snap.ShardCount)
	}
	if snap.EventsTotal != 0 || snap.SkipTotal != 0 {
		t.Errorf("expected zero counters, got events=%d skip=%d", snap.EventsTotal, snap.SkipTotal)
	}
	// budget==0, so BudgetOK should be true (budget==0 means no budget constraint).
	if !snap.BudgetOK {
		t.Error("expected BudgetOK == true when budget is 0")
	}
}

func TestShard_SetShardTopology(t *testing.T) {
	resetShardGlobal()
	SetShardTopology(3, 8, 1000)
	if !ShardConfigured() {
		t.Error("expected ShardConfigured() == true after SetShardTopology")
	}
	snap := SnapshotShardState()
	if snap.ShardIndex != 3 {
		t.Errorf("expected shard index 3, got %d", snap.ShardIndex)
	}
	if snap.ShardCount != 8 {
		t.Errorf("expected shard count 8, got %d", snap.ShardCount)
	}
	if snap.Budget != 1000 {
		t.Errorf("expected budget 1000, got %d", snap.Budget)
	}
}

func TestShard_SetShardLag(t *testing.T) {
	resetShardGlobal()
	SetShardTopology(0, 1, 500)
	SetShardLag(250)
	snap := SnapshotShardState()
	if snap.Lag != 250 {
		t.Errorf("expected lag 250, got %d", snap.Lag)
	}
}

func TestShard_IncEventsTotal(t *testing.T) {
	resetShardGlobal()
	for i := 0; i < 100; i++ {
		IncShardEventsTotal()
	}
	snap := SnapshotShardState()
	if snap.EventsTotal != 100 {
		t.Errorf("expected events total 100, got %d", snap.EventsTotal)
	}
}

func TestShard_IncSkipTotal(t *testing.T) {
	resetShardGlobal()
	for i := 0; i < 50; i++ {
		IncShardSkipTotal()
	}
	snap := SnapshotShardState()
	if snap.SkipTotal != 50 {
		t.Errorf("expected skip total 50, got %d", snap.SkipTotal)
	}
}

func TestShard_BudgetOK_WhenLagWithinBudget(t *testing.T) {
	resetShardGlobal()
	SetShardTopology(0, 1, 100)
	SetShardLag(50)
	snap := SnapshotShardState()
	if !snap.BudgetOK {
		t.Error("expected BudgetOK == true when lag (50) <= budget (100)")
	}
}

func TestShard_BudgetOK_WhenLagEqualsBudget(t *testing.T) {
	resetShardGlobal()
	SetShardTopology(0, 1, 100)
	SetShardLag(100)
	snap := SnapshotShardState()
	if !snap.BudgetOK {
		t.Error("expected BudgetOK == true when lag (100) == budget (100)")
	}
}

func TestShard_BudgetNotOK_WhenLagExceedsBudget(t *testing.T) {
	resetShardGlobal()
	SetShardTopology(0, 1, 100)
	SetShardLag(101)
	snap := SnapshotShardState()
	if snap.BudgetOK {
		t.Error("expected BudgetOK == false when lag (101) > budget (100)")
	}
}

func TestShard_BudgetOK_WhenBudgetZero(t *testing.T) {
	resetShardGlobal()
	SetShardTopology(0, 1, 0) // budget==0 means unconstrained
	SetShardLag(999999)
	snap := SnapshotShardState()
	if !snap.BudgetOK {
		t.Error("expected BudgetOK == true when budget is 0 (unconstrained)")
	}
}

func TestShard_ConcurrentIncrements(t *testing.T) {
	resetShardGlobal()
	const goroutines = 10
	const incPerGoroutine = 1000

	var wg sync.WaitGroup
	wg.Add(goroutines * 2)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < incPerGoroutine; i++ {
				IncShardEventsTotal()
			}
		}()
		go func() {
			defer wg.Done()
			for i := 0; i < incPerGoroutine; i++ {
				IncShardSkipTotal()
			}
		}()
	}
	wg.Wait()

	snap := SnapshotShardState()
	expectedEvents := uint64(goroutines * incPerGoroutine)
	expectedSkip := uint64(goroutines * incPerGoroutine)
	if snap.EventsTotal != expectedEvents {
		t.Errorf("expected events total %d, got %d", expectedEvents, snap.EventsTotal)
	}
	if snap.SkipTotal != expectedSkip {
		t.Errorf("expected skip total %d, got %d", expectedSkip, snap.SkipTotal)
	}
}

func TestShard_TopologyOverwrite(t *testing.T) {
	resetShardGlobal()
	SetShardTopology(0, 4, 500)
	SetShardTopology(2, 8, 1000) // overwrite
	snap := SnapshotShardState()
	if snap.ShardIndex != 2 {
		t.Errorf("expected shard index 2 after overwrite, got %d", snap.ShardIndex)
	}
	if snap.ShardCount != 8 {
		t.Errorf("expected shard count 8 after overwrite, got %d", snap.ShardCount)
	}
	if snap.Budget != 1000 {
		t.Errorf("expected budget 1000 after overwrite, got %d", snap.Budget)
	}
}

// ===========================================================================
// 4. storage_state.go -- Storage path state tracking
// ===========================================================================

func TestStorage_InitialState(t *testing.T) {
	resetStorageGlobal()
	snap := SnapshotStorageState()
	if snap.Hot.LastOKKnown || snap.Cold.LastOKKnown || snap.Committer.LastOKKnown {
		t.Error("expected no Known flags before any calls")
	}
	if snap.Hot.FailsTotalKnown || snap.Cold.FailsTotalKnown {
		t.Error("expected FailsTotalKnown == false before any errors")
	}
	if snap.Hot.FailsTotal != 0 || snap.Cold.FailsTotal != 0 {
		t.Errorf("expected zero fails, got hot=%d cold=%d", snap.Hot.FailsTotal, snap.Cold.FailsTotal)
	}
}

func TestStorage_SetHotOk(t *testing.T) {
	resetStorageGlobal()
	SetHotOk()
	snap := SnapshotStorageState()
	if !snap.Hot.LastOKKnown {
		t.Error("expected Hot.LastOKKnown == true after SetHotOk")
	}
	if !snap.Hot.LastOK {
		t.Error("expected Hot.LastOK == true after SetHotOk")
	}
	if snap.Hot.LastError != "" {
		t.Errorf("expected empty LastError after SetHotOk, got %q", snap.Hot.LastError)
	}
}

func TestStorage_SetHotErr(t *testing.T) {
	resetStorageGlobal()
	SetHotErr(errors.New("connection refused"))
	snap := SnapshotStorageState()
	if !snap.Hot.LastOKKnown {
		t.Error("expected Hot.LastOKKnown == true after SetHotErr")
	}
	if snap.Hot.LastOK {
		t.Error("expected Hot.LastOK == false after SetHotErr")
	}
	if snap.Hot.LastError != "connection refused" {
		t.Errorf("expected error message 'connection refused', got %q", snap.Hot.LastError)
	}
	if !snap.Hot.FailsTotalKnown {
		t.Error("expected Hot.FailsTotalKnown == true after SetHotErr")
	}
	if snap.Hot.FailsTotal != 1 {
		t.Errorf("expected Hot.FailsTotal == 1, got %d", snap.Hot.FailsTotal)
	}
}

func TestStorage_HotErr_AccumulatesFailsTotal(t *testing.T) {
	resetStorageGlobal()
	for i := 0; i < 5; i++ {
		SetHotErr(fmt.Errorf("error %d", i))
	}
	snap := SnapshotStorageState()
	if snap.Hot.FailsTotal != 5 {
		t.Errorf("expected Hot.FailsTotal == 5, got %d", snap.Hot.FailsTotal)
	}
	if snap.Hot.LastError != "error 4" {
		t.Errorf("expected last error 'error 4', got %q", snap.Hot.LastError)
	}
}

func TestStorage_HotErrThenOk_ResetsLastError(t *testing.T) {
	resetStorageGlobal()
	SetHotErr(errors.New("bad"))
	SetHotOk()
	snap := SnapshotStorageState()
	if !snap.Hot.LastOK {
		t.Error("expected Hot.LastOK == true after SetHotOk following error")
	}
	if snap.Hot.LastError != "" {
		t.Errorf("expected empty LastError after SetHotOk, got %q", snap.Hot.LastError)
	}
	// FailsTotal should NOT be reset by SetHotOk -- it is cumulative.
	if snap.Hot.FailsTotal != 1 {
		t.Errorf("expected Hot.FailsTotal == 1 (cumulative), got %d", snap.Hot.FailsTotal)
	}
}

func TestStorage_HotErr_NilError(t *testing.T) {
	resetStorageGlobal()
	SetHotErr(nil)
	snap := SnapshotStorageState()
	if snap.Hot.LastError != "unknown" {
		t.Errorf("expected 'unknown' for nil error, got %q", snap.Hot.LastError)
	}
}

func TestStorage_SetColdOk(t *testing.T) {
	resetStorageGlobal()
	SetColdOk()
	snap := SnapshotStorageState()
	if !snap.Cold.LastOKKnown {
		t.Error("expected Cold.LastOKKnown == true after SetColdOk")
	}
	if !snap.Cold.LastOK {
		t.Error("expected Cold.LastOK == true after SetColdOk")
	}
}

func TestStorage_SetColdErr(t *testing.T) {
	resetStorageGlobal()
	SetColdErr(errors.New("disk full"))
	snap := SnapshotStorageState()
	if snap.Cold.LastOK {
		t.Error("expected Cold.LastOK == false after SetColdErr")
	}
	if snap.Cold.LastError != "disk full" {
		t.Errorf("expected 'disk full', got %q", snap.Cold.LastError)
	}
	if snap.Cold.FailsTotal != 1 {
		t.Errorf("expected Cold.FailsTotal == 1, got %d", snap.Cold.FailsTotal)
	}
}

func TestStorage_ColdErr_AccumulatesFailsTotal(t *testing.T) {
	resetStorageGlobal()
	for i := 0; i < 3; i++ {
		SetColdErr(errors.New("fail"))
	}
	snap := SnapshotStorageState()
	if snap.Cold.FailsTotal != 3 {
		t.Errorf("expected Cold.FailsTotal == 3, got %d", snap.Cold.FailsTotal)
	}
}

func TestStorage_ColdErrThenOk_ResetsLastError(t *testing.T) {
	resetStorageGlobal()
	SetColdErr(errors.New("oops"))
	SetColdOk()
	snap := SnapshotStorageState()
	if !snap.Cold.LastOK {
		t.Error("expected Cold.LastOK == true after recovery")
	}
	if snap.Cold.LastError != "" {
		t.Errorf("expected empty LastError after SetColdOk, got %q", snap.Cold.LastError)
	}
	if snap.Cold.FailsTotal != 1 {
		t.Errorf("expected Cold.FailsTotal == 1 (cumulative), got %d", snap.Cold.FailsTotal)
	}
}

func TestStorage_ColdErr_NilError(t *testing.T) {
	resetStorageGlobal()
	SetColdErr(nil)
	snap := SnapshotStorageState()
	if snap.Cold.LastError != "unknown" {
		t.Errorf("expected 'unknown' for nil error, got %q", snap.Cold.LastError)
	}
}

func TestStorage_SetCommitterOk(t *testing.T) {
	resetStorageGlobal()
	SetCommitterOk()
	snap := SnapshotStorageState()
	if !snap.Committer.LastOKKnown {
		t.Error("expected Committer.LastOKKnown == true after SetCommitterOk")
	}
	if !snap.Committer.LastOK {
		t.Error("expected Committer.LastOK == true after SetCommitterOk")
	}
}

func TestStorage_SetCommitterErr(t *testing.T) {
	resetStorageGlobal()
	SetCommitterErr(errors.New("commit failed"))
	snap := SnapshotStorageState()
	if snap.Committer.LastOK {
		t.Error("expected Committer.LastOK == false after SetCommitterErr")
	}
	if snap.Committer.LastError != "commit failed" {
		t.Errorf("expected 'commit failed', got %q", snap.Committer.LastError)
	}
}

func TestStorage_CommitterErrThenOk(t *testing.T) {
	resetStorageGlobal()
	SetCommitterErr(errors.New("fail"))
	SetCommitterOk()
	snap := SnapshotStorageState()
	if !snap.Committer.LastOK {
		t.Error("expected Committer.LastOK == true after recovery")
	}
	if snap.Committer.LastError != "" {
		t.Errorf("expected empty LastError, got %q", snap.Committer.LastError)
	}
}

func TestStorage_CommitterErr_NilError(t *testing.T) {
	resetStorageGlobal()
	SetCommitterErr(nil)
	snap := SnapshotStorageState()
	if snap.Committer.LastError != "unknown" {
		t.Errorf("expected 'unknown' for nil error, got %q", snap.Committer.LastError)
	}
}

func TestStorage_PathsAreIndependent(t *testing.T) {
	resetStorageGlobal()
	SetHotOk()
	SetColdErr(errors.New("cold broke"))
	SetCommitterErr(errors.New("commit broke"))

	snap := SnapshotStorageState()
	if !snap.Hot.LastOK {
		t.Error("hot should be OK")
	}
	if snap.Cold.LastOK {
		t.Error("cold should NOT be OK")
	}
	if snap.Committer.LastOK {
		t.Error("committer should NOT be OK")
	}
}

func TestStorage_ConcurrentAccess(t *testing.T) {
	resetStorageGlobal()
	const goroutines = 20
	const opsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(gID int) {
			defer wg.Done()
			for i := 0; i < opsPerGoroutine; i++ {
				switch gID % 3 {
				case 0:
					if i%2 == 0 {
						SetHotOk()
					} else {
						SetHotErr(fmt.Errorf("hot err %d", i))
					}
				case 1:
					if i%2 == 0 {
						SetColdOk()
					} else {
						SetColdErr(fmt.Errorf("cold err %d", i))
					}
				case 2:
					if i%2 == 0 {
						SetCommitterOk()
					} else {
						SetCommitterErr(fmt.Errorf("commit err %d", i))
					}
				}
				// Concurrent snapshots must not panic.
				_ = SnapshotStorageState()
			}
		}(g)
	}
	wg.Wait()

	// No assertions on final values (non-deterministic), but the fact
	// that we reached here without panic or data race is the test.
	snap := SnapshotStorageState()
	if !snap.Hot.LastOKKnown {
		t.Error("Hot.LastOKKnown should be true after operations")
	}
}

func TestStorage_SnapshotIsACopy(t *testing.T) {
	resetStorageGlobal()
	SetHotErr(errors.New("original error"))
	snap1 := SnapshotStorageState()

	// Mutate the returned struct.
	snap1.Hot.LastError = "mutated"
	snap1.Hot.FailsTotal = 9999

	// A new snapshot must be unaffected.
	snap2 := SnapshotStorageState()
	if snap2.Hot.LastError == "mutated" {
		t.Error("snapshot mutation leaked into store")
	}
	if snap2.Hot.FailsTotal == 9999 {
		t.Error("snapshot mutation leaked into store (FailsTotal)")
	}
}

// ===========================================================================
// 5. ws_state.go -- WebSocket session state
// ===========================================================================

func TestWS_InitialState(t *testing.T) {
	resetWSGlobal()
	snap := SnapshotWSState()
	if snap.SessionsActiveKnown {
		t.Error("expected SessionsActiveKnown == false initially")
	}
	if snap.PreferProtoSessionsKnown {
		t.Error("expected PreferProtoSessionsKnown == false initially")
	}
	if snap.DeliveriesProtoTotalKnown {
		t.Error("expected DeliveriesProtoTotalKnown == false initially")
	}
	if snap.DeliveriesJSONTotalKnown {
		t.Error("expected DeliveriesJSONTotalKnown == false initially")
	}
	if snap.ReconnectsTotalKnown {
		t.Error("expected ReconnectsTotalKnown == false initially")
	}
	if snap.SessionsActive != 0 || snap.PreferProtoSessions != 0 {
		t.Error("expected zero sessions initially")
	}
	if snap.DeliveriesProtoTotal != 0 || snap.DeliveriesJSONTotal != 0 || snap.ReconnectsTotal != 0 {
		t.Error("expected zero delivery/reconnect counters initially")
	}
}

func TestWS_IncDecSessionsActive(t *testing.T) {
	resetWSGlobal()
	IncSessionsActive()
	IncSessionsActive()
	IncSessionsActive()
	snap := SnapshotWSState()
	if snap.SessionsActive != 3 {
		t.Errorf("expected 3 active sessions, got %d", snap.SessionsActive)
	}
	if !snap.SessionsActiveKnown {
		t.Error("expected SessionsActiveKnown == true after Inc")
	}

	DecSessionsActive()
	snap = SnapshotWSState()
	if snap.SessionsActive != 2 {
		t.Errorf("expected 2 active sessions after Dec, got %d", snap.SessionsActive)
	}
}

func TestWS_DecSessionsActive_FloorAtZero(t *testing.T) {
	resetWSGlobal()
	// Dec with no prior Inc -- should floor at 0, not go negative.
	DecSessionsActive()
	snap := SnapshotWSState()
	if snap.SessionsActive != 0 {
		t.Errorf("expected 0 after Dec on empty, got %d", snap.SessionsActive)
	}
	if !snap.SessionsActiveKnown {
		t.Error("expected SessionsActiveKnown == true after Dec (it sets Known flag)")
	}
}

func TestWS_DecSessionsActive_FloorAtZero_AfterDecrementToZero(t *testing.T) {
	resetWSGlobal()
	IncSessionsActive()
	DecSessionsActive()
	// Now at zero. Another Dec should stay at 0.
	DecSessionsActive()
	snap := SnapshotWSState()
	if snap.SessionsActive != 0 {
		t.Errorf("expected 0, got %d", snap.SessionsActive)
	}
}

func TestWS_IncDecPreferProtoSessions(t *testing.T) {
	resetWSGlobal()
	IncPreferProtoSessions()
	IncPreferProtoSessions()
	snap := SnapshotWSState()
	if snap.PreferProtoSessions != 2 {
		t.Errorf("expected 2 prefer-proto sessions, got %d", snap.PreferProtoSessions)
	}
	if !snap.PreferProtoSessionsKnown {
		t.Error("expected PreferProtoSessionsKnown == true after Inc")
	}

	DecPreferProtoSessions()
	snap = SnapshotWSState()
	if snap.PreferProtoSessions != 1 {
		t.Errorf("expected 1 after Dec, got %d", snap.PreferProtoSessions)
	}
}

func TestWS_DecPreferProtoSessions_FloorAtZero(t *testing.T) {
	resetWSGlobal()
	DecPreferProtoSessions()
	snap := SnapshotWSState()
	if snap.PreferProtoSessions != 0 {
		t.Errorf("expected 0 after Dec on empty, got %d", snap.PreferProtoSessions)
	}
}

func TestWS_IncDeliveryProto(t *testing.T) {
	resetWSGlobal()
	for i := 0; i < 10; i++ {
		IncDeliveryProto()
	}
	snap := SnapshotWSState()
	if snap.DeliveriesProtoTotal != 10 {
		t.Errorf("expected 10 proto deliveries, got %d", snap.DeliveriesProtoTotal)
	}
	if !snap.DeliveriesProtoTotalKnown {
		t.Error("expected DeliveriesProtoTotalKnown == true")
	}
}

func TestWS_IncDeliveryJSON(t *testing.T) {
	resetWSGlobal()
	for i := 0; i < 7; i++ {
		IncDeliveryJSON()
	}
	snap := SnapshotWSState()
	if snap.DeliveriesJSONTotal != 7 {
		t.Errorf("expected 7 JSON deliveries, got %d", snap.DeliveriesJSONTotal)
	}
	if !snap.DeliveriesJSONTotalKnown {
		t.Error("expected DeliveriesJSONTotalKnown == true")
	}
}

func TestWS_IncReconnects(t *testing.T) {
	resetWSGlobal()
	IncReconnects()
	IncReconnects()
	IncReconnects()
	snap := SnapshotWSState()
	if snap.ReconnectsTotal != 3 {
		t.Errorf("expected 3 reconnects, got %d", snap.ReconnectsTotal)
	}
	if !snap.ReconnectsTotalKnown {
		t.Error("expected ReconnectsTotalKnown == true")
	}
}

func TestWS_KnownFlags_OnlySetAfterFirstCall(t *testing.T) {
	resetWSGlobal()

	// Initially nothing is known.
	snap := SnapshotWSState()
	if snap.SessionsActiveKnown || snap.PreferProtoSessionsKnown ||
		snap.DeliveriesProtoTotalKnown || snap.DeliveriesJSONTotalKnown ||
		snap.ReconnectsTotalKnown {
		t.Fatal("no Known flags should be set initially")
	}

	// Call only IncDeliveryJSON -- only that flag should become known.
	IncDeliveryJSON()
	snap = SnapshotWSState()
	if snap.SessionsActiveKnown {
		t.Error("SessionsActiveKnown should still be false")
	}
	if snap.PreferProtoSessionsKnown {
		t.Error("PreferProtoSessionsKnown should still be false")
	}
	if !snap.DeliveriesJSONTotalKnown {
		t.Error("DeliveriesJSONTotalKnown should be true")
	}
	if snap.DeliveriesProtoTotalKnown {
		t.Error("DeliveriesProtoTotalKnown should still be false")
	}
	if snap.ReconnectsTotalKnown {
		t.Error("ReconnectsTotalKnown should still be false")
	}
}

func TestWS_ConcurrentIncDec(t *testing.T) {
	resetWSGlobal()
	const goroutines = 50
	const opsPerGoroutine = 200

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < opsPerGoroutine; i++ {
				IncSessionsActive()
				DecSessionsActive()
				IncPreferProtoSessions()
				DecPreferProtoSessions()
				IncDeliveryProto()
				IncDeliveryJSON()
				IncReconnects()
			}
		}()
	}
	wg.Wait()

	snap := SnapshotWSState()

	// Sessions should be 0 (equal inc/dec).
	if snap.SessionsActive != 0 {
		t.Errorf("expected SessionsActive == 0 after balanced inc/dec, got %d", snap.SessionsActive)
	}
	if snap.PreferProtoSessions != 0 {
		t.Errorf("expected PreferProtoSessions == 0 after balanced inc/dec, got %d", snap.PreferProtoSessions)
	}

	// Delivery/reconnect counters are monotonic.
	expectedDeliveries := uint64(goroutines * opsPerGoroutine)
	if snap.DeliveriesProtoTotal != expectedDeliveries {
		t.Errorf("expected %d proto deliveries, got %d", expectedDeliveries, snap.DeliveriesProtoTotal)
	}
	if snap.DeliveriesJSONTotal != expectedDeliveries {
		t.Errorf("expected %d JSON deliveries, got %d", expectedDeliveries, snap.DeliveriesJSONTotal)
	}
	if snap.ReconnectsTotal != expectedDeliveries {
		t.Errorf("expected %d reconnects, got %d", expectedDeliveries, snap.ReconnectsTotal)
	}
}

func TestWS_ConcurrentDecFloorStress(t *testing.T) {
	resetWSGlobal()
	// Start with 1 active session, then have many goroutines try to Dec.
	// Only one Dec should succeed; the rest should floor at 0.
	IncSessionsActive()

	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			DecSessionsActive()
		}()
	}
	wg.Wait()

	snap := SnapshotWSState()
	if snap.SessionsActive != 0 {
		t.Errorf("expected SessionsActive == 0 after stress Dec, got %d", snap.SessionsActive)
	}
}

func TestWS_AllKnownFlagsSetAfterFullUsage(t *testing.T) {
	resetWSGlobal()
	IncSessionsActive()
	IncPreferProtoSessions()
	IncDeliveryProto()
	IncDeliveryJSON()
	IncReconnects()

	snap := SnapshotWSState()
	if !snap.SessionsActiveKnown {
		t.Error("SessionsActiveKnown should be true")
	}
	if !snap.PreferProtoSessionsKnown {
		t.Error("PreferProtoSessionsKnown should be true")
	}
	if !snap.DeliveriesProtoTotalKnown {
		t.Error("DeliveriesProtoTotalKnown should be true")
	}
	if !snap.DeliveriesJSONTotalKnown {
		t.Error("DeliveriesJSONTotalKnown should be true")
	}
	if !snap.ReconnectsTotalKnown {
		t.Error("ReconnectsTotalKnown should be true")
	}
}

// ===========================================================================
// Cross-cutting: sanitizeOverloadPart (internal helper, accessible in-package)
// ===========================================================================

func TestSanitizeOverloadPart_Empty(t *testing.T) {
	t.Parallel()
	if got := sanitizeOverloadPart(""); got != "unknown" {
		t.Errorf("expected 'unknown' for empty string, got %q", got)
	}
}

func TestSanitizeOverloadPart_NonEmpty(t *testing.T) {
	t.Parallel()
	if got := sanitizeOverloadPart("binance"); got != "binance" {
		t.Errorf("expected 'binance', got %q", got)
	}
}

func TestStorageErrString_Nil(t *testing.T) {
	t.Parallel()
	if got := storageErrString(nil); got != "unknown" {
		t.Errorf("expected 'unknown' for nil error, got %q", got)
	}
}

func TestStorageErrString_NonNil(t *testing.T) {
	t.Parallel()
	err := errors.New("something broke")
	if got := storageErrString(err); got != "something broke" {
		t.Errorf("expected 'something broke', got %q", got)
	}
}
