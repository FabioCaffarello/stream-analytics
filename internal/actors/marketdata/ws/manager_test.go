package ws

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/anthdm/hollywood/actor"
)

func TestPlan_FillStrategyFirst(t *testing.T) {
	sink := &actor.PID{}
	cfg := ManagerConfig{
		SendTo:                 sink,
		Exchange:               "binance",
		Tickers:                []string{"a", "b", "c", "d", "e"},
		StreamsPerTicker:       2,
		MaxStreamsPerWebsocket: 4,
		MaxWebsockets:          4,
		FillStrategy:           FillStrategyFirst,
		EndpointBuilder:        func(t []string) string { return fmt.Sprintf("wss://x/%d", len(t)) },
		MaxWebsocketLifetime:   time.Minute,
	}

	plan, err := Plan(cfg)
	if err != nil {
		t.Fatalf("unexpected plan error: %v", err)
	}

	if got, want := len(plan.Buckets), 3; got != want {
		t.Fatalf("buckets len = %d, want %d", got, want)
	}
	if got, want := len(plan.Buckets[0]), 2; got != want {
		t.Fatalf("bucket[0] len = %d, want %d", got, want)
	}
	if got, want := len(plan.Buckets[1]), 2; got != want {
		t.Fatalf("bucket[1] len = %d, want %d", got, want)
	}
	if got, want := len(plan.Buckets[2]), 1; got != want {
		t.Fatalf("bucket[2] len = %d, want %d", got, want)
	}
}

func TestPlan_FillStrategyEvenly(t *testing.T) {
	sink := &actor.PID{}
	cfg := ManagerConfig{
		SendTo:                 sink,
		Exchange:               "binance",
		Tickers:                []string{"a", "b", "c", "d", "e", "f"},
		StreamsPerTicker:       1,
		MaxStreamsPerWebsocket: 3,
		MaxWebsockets:          3,
		FillStrategy:           FillStrategyEvenly,
		EndpointBuilder:        func(t []string) string { return fmt.Sprintf("wss://x/%d", len(t)) },
		MaxWebsocketLifetime:   time.Minute,
	}

	plan, err := Plan(cfg)
	if err != nil {
		t.Fatalf("unexpected plan error: %v", err)
	}

	if got, want := len(plan.Buckets), 3; got != want {
		t.Fatalf("buckets len = %d, want %d", got, want)
	}
	for i, b := range plan.Buckets {
		if len(b) != 2 {
			t.Fatalf("bucket[%d] len = %d, want 2", i, len(b))
		}
	}
}

func TestPlan_FillStrategyAuto(t *testing.T) {
	sink := &actor.PID{}
	tickers := make([]string, 51)
	for i := range tickers {
		tickers[i] = fmt.Sprintf("T-%d", i)
	}

	cfg := ManagerConfig{
		SendTo:                 sink,
		Exchange:               "binance",
		Tickers:                tickers,
		StreamsPerTicker:       1,
		MaxStreamsPerWebsocket: 100,
		MaxWebsockets:          5,
		FillStrategy:           FillStrategyAuto,
		EndpointBuilder:        func(t []string) string { return fmt.Sprintf("wss://x/%d", len(t)) },
		MaxWebsocketLifetime:   time.Minute,
	}

	plan, err := Plan(cfg)
	if err != nil {
		t.Fatalf("unexpected plan error: %v", err)
	}
	if plan.ResolvedFillStrategy != FillStrategyEvenly {
		t.Fatalf("resolved strategy = %s, want %s", plan.ResolvedFillStrategy, FillStrategyEvenly)
	}
}

func TestPlan_CapacityExceeded(t *testing.T) {
	sink := &actor.PID{}
	cfg := ManagerConfig{
		SendTo:                 sink,
		Exchange:               "binance",
		Tickers:                []string{"a", "b", "c", "d", "e"},
		StreamsPerTicker:       2,
		MaxStreamsPerWebsocket: 3,
		MaxWebsockets:          2,
		FillStrategy:           FillStrategyFirst,
		EndpointBuilder:        func(t []string) string { return fmt.Sprintf("wss://x/%d", len(t)) },
		MaxWebsocketLifetime:   time.Minute,
	}

	_, err := Plan(cfg)
	if err == nil {
		t.Fatal("expected capacity error, got nil")
	}
	if !strings.Contains(err.Error(), "exceed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPlan_ReservesOverlapCapacity(t *testing.T) {
	sink := &actor.PID{}
	cfg := ManagerConfig{
		SendTo:                 sink,
		Exchange:               "binance",
		Tickers:                []string{"a", "b", "c", "d"},
		StreamsPerTicker:       1,
		MaxStreamsPerWebsocket: 2,
		MaxWebsockets:          2,
		FillStrategy:           FillStrategyFirst,
		RespawnOverlap:         10 * time.Second,
		EndpointBuilder:        func(t []string) string { return fmt.Sprintf("wss://x/%d", len(t)) },
		MaxWebsocketLifetime:   time.Minute,
	}

	_, err := Plan(cfg)
	if err == nil {
		t.Fatal("expected overlap reserve capacity error, got nil")
	}
	if !strings.Contains(err.Error(), "exceed") {
		t.Fatalf("unexpected error: %v", err)
	}

	cfg.RespawnOverlap = 0
	plan, err := Plan(cfg)
	if err != nil {
		t.Fatalf("expected plan without overlap reserve to pass: %v", err)
	}
	if got, want := plan.WebsocketCapacity, int64(2); got != want {
		t.Fatalf("websocket capacity = %d, want %d", got, want)
	}
}

func TestBucketTickers_First(t *testing.T) {
	buckets, err := bucketTickers([]string{"a", "b", "c", "d", "e"}, FillStrategyFirst, 3, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := len(buckets), 3; got != want {
		t.Fatalf("bucket count = %d, want %d", got, want)
	}
	if got, want := len(buckets[2]), 1; got != want {
		t.Fatalf("last bucket size = %d, want %d", got, want)
	}
}

func TestBucketTickers_Evenly(t *testing.T) {
	buckets, err := bucketTickers([]string{"a", "b", "c", "d", "e", "f"}, FillStrategyEvenly, 3, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := len(buckets), 3; got != want {
		t.Fatalf("bucket count = %d, want %d", got, want)
	}
	for i := range buckets {
		if got, want := len(buckets[i]), 2; got != want {
			t.Fatalf("bucket[%d] size = %d, want %d", i, got, want)
		}
	}
}

func TestBucketTickers_InsufficientCapacity(t *testing.T) {
	_, err := bucketTickers([]string{"a", "b", "c", "d", "e"}, FillStrategyFirst, 2, 2)
	if err == nil {
		t.Fatal("expected insufficient capacity error")
	}
}

func TestManager_DoesNotBlockMailboxOnOverlap(t *testing.T) {
	now := time.Now()
	manager := &Manager{
		config: ManagerConfig{
			Exchange:             "binance",
			MaxWebsocketLifetime: 10 * time.Second,
			RespawnOverlap:       300 * time.Millisecond,
		},
		streams: []*stream{{
			uid:      "old-1",
			bid:      0,
			endpoint: "wss://x",
			tickers:  []string{"BTCUSDT"},
			started:  now.Add(-11 * time.Second),
		}},
		scheduledPoison: map[string]cancelSchedule{},
		nowFn:           func() time.Time { return now },
		createReplacementFn: func(c *actor.Context, oldStream *stream, index int) (*stream, error) {
			return &stream{
				uid:      "new-1",
				bid:      oldStream.bid,
				endpoint: oldStream.endpoint,
				tickers:  oldStream.tickers,
				started:  now,
			}, nil
		},
		scheduleFn: func(delay time.Duration, fn func()) cancelSchedule {
			return func() {}
		},
	}

	start := time.Now()
	manager.updateStreams(nil)
	elapsed := time.Since(start)

	if elapsed > 50*time.Millisecond {
		t.Fatalf("updateStreams blocked mailbox path: elapsed=%v", elapsed)
	}
	if got, want := manager.streams[0].uid, "new-1"; got != want {
		t.Fatalf("stream uid = %q, want %q", got, want)
	}
	if _, ok := manager.scheduledPoison["old-1"]; !ok {
		t.Fatal("expected overlap poison to be scheduled")
	}

	start = time.Now()
	manager.updateStreams(nil)
	if time.Since(start) > 50*time.Millisecond {
		t.Fatal("second updateStreams call should stay non-blocking")
	}
}

func TestStopped_StopsRepeater(t *testing.T) {
	var repeaterStopped int
	var scheduleCanceled int
	var poisonCalls int

	manager := &Manager{
		repeaterStopFn: func() { repeaterStopped++ },
		scheduledPoison: map[string]cancelSchedule{
			"old-1": func() { scheduleCanceled++ },
		},
		streams: []*stream{{pid: &actor.PID{}, uid: "x"}},
		poisonFn: func(c *actor.Context, pid *actor.PID) {
			poisonCalls++
		},
	}

	manager.handleStopped(nil)
	manager.handleStopped(nil)

	if repeaterStopped != 1 {
		t.Fatalf("repeater stop calls = %d, want 1", repeaterStopped)
	}
	if scheduleCanceled != 1 {
		t.Fatalf("scheduled cancel calls = %d, want 1", scheduleCanceled)
	}
	if len(manager.scheduledPoison) != 0 {
		t.Fatalf("expected scheduledPoison to be empty after stop, got %d", len(manager.scheduledPoison))
	}
	if poisonCalls != 1 {
		t.Fatalf("poison calls = %d, want 1", poisonCalls)
	}
}
