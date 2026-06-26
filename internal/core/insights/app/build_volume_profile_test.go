package app

import (
	"context"
	"slices"
	"testing"

	sharedhash "github.com/FabioCaffarello/stream-analytics/internal/shared/hash"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/metrics"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestBuildVolumeProfile_TradesOnlyInput(t *testing.T) {
	uc := NewBuildVolumeProfile()
	res := uc.Execute(context.Background(), BuildVolumeProfileRequest{
		EventType:  "marketdata.bookdelta",
		Venue:      "binance",
		Instrument: "BTCUSDT",
		Timeframe:  "1m",
		TickSize:   0.5,
		Price:      100,
		Size:       1,
		Side:       "buy",
		TsIngest:   1710000000000,
		Seq:        1,
	})
	if !res.IsFail() {
		t.Fatal("expected fail for non-trade event")
	}
}

func TestBuildVolumeProfile_DeterministicSnapshotAndKey(t *testing.T) {
	sequence := []BuildVolumeProfileRequest{
		{
			EventType:  "marketdata.trade",
			Venue:      "binance",
			Instrument: "BTCUSDT",
			Timeframe:  "1m",
			TickSize:   0.5,
			Price:      100.2,
			Size:       1.5,
			Side:       "buy",
			TsIngest:   1710000000000,
			Seq:        1,
		},
		{
			EventType:  "marketdata.trade",
			Venue:      "binance",
			Instrument: "BTC-USDT",
			Timeframe:  "1m",
			TickSize:   0.5,
			Price:      100.7,
			Size:       2.0,
			Side:       "sell",
			TsIngest:   1710000001000,
			Seq:        2,
		},
	}

	run := func() (string, string) {
		uc := NewBuildVolumeProfile()
		var out BuildVolumeProfileResponse
		for _, req := range sequence {
			res := uc.Execute(context.Background(), req)
			if res.IsFail() {
				t.Fatalf("execute failed: %v", res.Problem())
			}
			out = res.Value()
		}
		raw, err := MarshalVPVRSnapshotStableBytes(out.Snapshot)
		if err != nil {
			t.Fatalf("marshal snapshot: %v", err)
		}
		return sharedhash.HashBytes(raw), out.IdempotencyKey
	}

	hash1, key1 := run()
	hash2, key2 := run()
	if hash1 != hash2 {
		t.Fatalf("snapshot hash mismatch: %s vs %s", hash1, hash2)
	}
	if key1 != key2 {
		t.Fatalf("idempotency key mismatch: %s vs %s", key1, key2)
	}
}

func TestBuildVolumeProfile_CardinalityCap(t *testing.T) {
	uc := NewBuildVolumeProfileWithConfig(BuildVolumeProfileConfig{
		MaxBucketsPerWindow:  2,
		MaxLevelsPerPayload:  2,
		MaxOpenWindowsPerKey: 4,
	})
	requests := []BuildVolumeProfileRequest{
		{
			EventType:  "marketdata.trade",
			Venue:      "binance",
			Instrument: "BTCUSDT",
			Timeframe:  "1m",
			TickSize:   1,
			Price:      100,
			Size:       1,
			Side:       "buy",
			TsIngest:   1710000000000,
			Seq:        1,
		},
		{
			EventType:  "marketdata.trade",
			Venue:      "binance",
			Instrument: "BTCUSDT",
			Timeframe:  "1m",
			TickSize:   1,
			Price:      101,
			Size:       1,
			Side:       "buy",
			TsIngest:   1710000001000,
			Seq:        2,
		},
		{
			EventType:  "marketdata.trade",
			Venue:      "binance",
			Instrument: "BTCUSDT",
			Timeframe:  "1m",
			TickSize:   1,
			Price:      102,
			Size:       1,
			Side:       "buy",
			TsIngest:   1710000002000,
			Seq:        3,
		},
	}
	var final BuildVolumeProfileResponse
	for _, req := range requests {
		res := uc.Execute(context.Background(), req)
		if res.IsFail() {
			t.Fatalf("execute failed: %v", res.Problem())
		}
		final = res.Value()
	}
	if final.Emitted {
		t.Fatal("expected cap overflow request not to emit")
	}
	if final.DropReason != "bucket_cap" {
		t.Fatalf("unexpected drop reason: %s", final.DropReason)
	}
}

func TestBuildVolumeProfile_EmitsSnapshotAndDelta(t *testing.T) {
	uc := NewBuildVolumeProfile()
	res := uc.Execute(context.Background(), BuildVolumeProfileRequest{
		EventType:  "marketdata.trade",
		Venue:      "binance",
		Instrument: "BTCUSDT",
		Timeframe:  "1m",
		TickSize:   0.5,
		Price:      100.4,
		Size:       3,
		Side:       "sell",
		TsIngest:   1710000000000,
		Seq:        1,
	})
	if res.IsFail() {
		t.Fatalf("execute failed: %v", res.Problem())
	}
	out := res.Value()
	if !out.Emitted {
		t.Fatal("expected emitted response")
	}
	if len(out.Snapshot.Buckets) != 1 {
		t.Fatalf("expected snapshot with one bucket, got %d", len(out.Snapshot.Buckets))
	}
	if len(out.Delta.Buckets) != 1 {
		t.Fatalf("expected delta with one bucket, got %d", len(out.Delta.Buckets))
	}
}

func TestBuildVolumeProfile_Deterministic50Runs(t *testing.T) {
	sequence := []BuildVolumeProfileRequest{
		{EventType: "marketdata.trade", Venue: "binance", Instrument: "BTC-USDT", Timeframe: "1m", TickSize: 0.5, Price: 100.2, Size: 1.5, Side: "buy", TsIngest: 1710000000000, Seq: 1},
		{EventType: "marketdata.trade", Venue: "binance", Instrument: "BTCUSDT", Timeframe: "1m", TickSize: 0.5, Price: 100.7, Size: 2.0, Side: "sell", TsIngest: 1710000001000, Seq: 2},
		{EventType: "marketdata.trade", Venue: "binance", Instrument: "BTCUSDT", Timeframe: "1m", TickSize: 0.5, Price: 101.3, Size: 0.9, Side: "buy", TsIngest: 1710000002000, Seq: 3},
		{EventType: "marketdata.trade", Venue: "binance", Instrument: "BTCUSDT", Timeframe: "1m", TickSize: 0.5, Price: 100.1, Size: 1.2, Side: "sell", TsIngest: 1710000003000, Seq: 4},
	}

	var baselineHash string
	var baselineKey string
	for run := 0; run < 50; run++ {
		uc := NewBuildVolumeProfile()
		var out BuildVolumeProfileResponse
		for _, req := range sequence {
			res := uc.Execute(context.Background(), req)
			if res.IsFail() {
				t.Fatalf("run=%d execute failed: %v", run, res.Problem())
			}
			out = res.Value()
		}
		raw, err := MarshalVPVRSnapshotStableBytes(out.Snapshot)
		if err != nil {
			t.Fatalf("run=%d marshal snapshot: %v", run, err)
		}
		hash := sharedhash.HashBytes(raw)
		if run == 0 {
			baselineHash = hash
			baselineKey = out.IdempotencyKey
			continue
		}
		if hash != baselineHash {
			t.Fatalf("run=%d snapshot hash mismatch: got=%s want=%s", run, hash, baselineHash)
		}
		if out.IdempotencyKey != baselineKey {
			t.Fatalf("run=%d idempotency key mismatch: got=%s want=%s", run, out.IdempotencyKey, baselineKey)
		}
	}
}

func TestBuildVolumeProfile_WindowEviction_FIFODeterministic(t *testing.T) {
	cfg := BuildVolumeProfileConfig{
		MaxBucketsPerWindow:  64,
		MaxLevelsPerPayload:  64,
		MaxOpenWindowsPerKey: 2,
	}
	sequence := []BuildVolumeProfileRequest{
		{EventType: "marketdata.trade", Venue: "binance", Instrument: "BTCUSDT", Timeframe: "1m", TickSize: 1, Price: 100, Size: 1, Side: "buy", TsIngest: 1710000000000, Seq: 1}, // w0
		{EventType: "marketdata.trade", Venue: "binance", Instrument: "BTCUSDT", Timeframe: "1m", TickSize: 1, Price: 101, Size: 1, Side: "buy", TsIngest: 1710000060000, Seq: 2}, // w1
		{EventType: "marketdata.trade", Venue: "binance", Instrument: "BTCUSDT", Timeframe: "1m", TickSize: 1, Price: 102, Size: 1, Side: "buy", TsIngest: 1710000120000, Seq: 3}, // w2 (evict w0)
		{EventType: "marketdata.trade", Venue: "binance", Instrument: "BTCUSDT", Timeframe: "1m", TickSize: 1, Price: 103, Size: 1, Side: "buy", TsIngest: 1710000005000, Seq: 4}, // w0 re-created (evict w1)
	}

	run := func() (order []int64, finalHash string) {
		uc := NewBuildVolumeProfileWithConfig(cfg)
		var out BuildVolumeProfileResponse
		for _, req := range sequence {
			res := uc.Execute(context.Background(), req)
			if res.IsFail() {
				t.Fatalf("execute failed: %v", res.Problem())
			}
			out = res.Value()
		}
		key := "BINANCE|BTCUSDT|1m"
		ps, ok := uc.states[key]
		if !ok {
			t.Fatalf("partition state missing for %s", key)
		}
		order = append([]int64(nil), ps.order...)
		if got, want := len(order), 2; got != want {
			t.Fatalf("open windows=%d want=%d", got, want)
		}
		raw, err := MarshalVPVRSnapshotStableBytes(out.Snapshot)
		if err != nil {
			t.Fatalf("marshal final snapshot: %v", err)
		}
		return order, sharedhash.HashBytes(raw)
	}

	order1, hash1 := run()
	order2, hash2 := run()
	if !slices.Equal(order1, order2) {
		t.Fatalf("window order mismatch: run1=%v run2=%v", order1, order2)
	}
	if hash1 != hash2 {
		t.Fatalf("final hash mismatch: run1=%s run2=%s", hash1, hash2)
	}
}

func TestBuildVolumeProfile_ObservabilityCountersAndGauges(t *testing.T) {
	uc := NewBuildVolumeProfileWithConfig(BuildVolumeProfileConfig{
		MaxBucketsPerWindow:  1,
		MaxLevelsPerPayload:  8,
		MaxOpenWindowsPerKey: 1,
	})

	overloadBefore := testutil.ToFloat64(metrics.VPVRBuilderOverloadActionsTotal.WithLabelValues("window_evict"))
	dropBefore := testutil.ToFloat64(metrics.VPVRBuilderDropTotal.WithLabelValues("bucket_cap"))
	replayBefore := testutil.ToFloat64(metrics.VPVRBuilderReplayMismatchTotal)

	req1 := BuildVolumeProfileRequest{
		EventType:  "marketdata.trade",
		Venue:      "binance",
		Instrument: "BTC",
		Timeframe:  "1m",
		TickSize:   1,
		Price:      100,
		Size:       1,
		Side:       "buy",
		TsIngest:   1710000000000,
		Seq:        10,
	}
	req2 := req1
	req2.TsIngest = 1710000060000
	req2.Seq = 11
	req3 := req2
	req3.Price = 101
	req3.Seq = 12
	req4 := req2
	req4.Seq = 10

	for _, req := range []BuildVolumeProfileRequest{req1, req2, req3, req4} {
		res := uc.Execute(context.Background(), req)
		if res.IsFail() {
			t.Fatalf("execute failed: %v", res.Problem())
		}
	}

	if got := testutil.ToFloat64(metrics.VPVRBuilderOverloadActionsTotal.WithLabelValues("window_evict")); got < overloadBefore+1 {
		t.Fatalf("expected overload increment, got=%f before=%f", got, overloadBefore)
	}
	if got := testutil.ToFloat64(metrics.VPVRBuilderDropTotal.WithLabelValues("bucket_cap")); got < dropBefore+1 {
		t.Fatalf("expected drop increment, got=%f before=%f", got, dropBefore)
	}
	if got := testutil.ToFloat64(metrics.VPVRBuilderReplayMismatchTotal); got < replayBefore+1 {
		t.Fatalf("expected replay mismatch increment, got=%f before=%f", got, replayBefore)
	}
	if got := testutil.ToFloat64(metrics.VPVRBuilderWindowsOpen.WithLabelValues("binance", "btc", "1m")); got != 1 {
		t.Fatalf("expected windows_open=1, got=%f", got)
	}
	if got := testutil.ToFloat64(metrics.VPVRBuilderBucketCount.WithLabelValues("binance", "btc", "1m")); got != 1 {
		t.Fatalf("expected bucket_count=1, got=%f", got)
	}
}
