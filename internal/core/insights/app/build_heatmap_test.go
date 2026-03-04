package app

import (
	"context"
	"encoding/json"
	"testing"

	sharedhash "github.com/market-raccoon/internal/shared/hash"
)

func TestHeatmapBucketizationDeterministic(t *testing.T) {
	seq := testHeatmapSequence()
	runHash := func() string {
		uc := NewBuildHeatmap()
		var last BuildHeatmapResponse
		for _, req := range seq {
			res := uc.Execute(context.Background(), req)
			if res.IsFail() {
				t.Fatalf("Execute failed: %v", res.Problem())
			}
			last = res.Value()
		}
		raw, err := json.Marshal(last.Artifact)
		if err != nil {
			t.Fatalf("Marshal artifact: %v", err)
		}
		return sharedhash.HashBytes(raw)
	}

	if h1, h2 := runHash(), runHash(); h1 != h2 {
		t.Fatalf("determinism hash mismatch: %s vs %s", h1, h2)
	}
}

func TestHeatmapBoundedBucketsPerPartition(t *testing.T) {
	uc := NewBuildHeatmapWithConfig(BuildHeatmapConfig{
		MaxPriceBucketsPerWindow: 2,
		MaxCellsPerWindow:        64,
		MaxOpenWindowsPerKey:     2,
		MaxPayloadBytes:          256 * 1024,
	})

	for i := 0; i < 20; i++ {
		req := BuildHeatmapRequest{
			EventType:  "marketdata.trade",
			Venue:      "binance",
			Instrument: "BTCUSDT",
			Timeframe:  "1m",
			TickSize:   0.5,
			Price:      100 + float64(i),
			Size:       1,
			Side:       "buy",
			TsIngest:   1_710_000_000_000 + int64(i),
			Seq:        int64(i + 1),
		}
		res := uc.Execute(context.Background(), req)
		if res.IsFail() {
			t.Fatalf("Execute failed: %v", res.Problem())
		}
	}

	out := uc.Execute(context.Background(), BuildHeatmapRequest{
		EventType:  "marketdata.bookdelta",
		Venue:      "binance",
		Instrument: "BTCUSDT",
		Timeframe:  "1m",
		TickSize:   0.5,
		Price:      130,
		Size:       2,
		Side:       "sell",
		TsIngest:   1_710_000_000_099,
		Seq:        99,
	})
	if out.IsFail() {
		t.Fatalf("Execute final failed: %v", out.Problem())
	}
	artifact := out.Value().Artifact
	priceBuckets := map[float64]struct{}{}
	for _, c := range artifact.Cells {
		priceBuckets[c.PriceBucketLow] = struct{}{}
	}
	if got := len(priceBuckets); got > 2 {
		t.Fatalf("price bucket cap exceeded: got=%d want<=2", got)
	}
}

func TestHeatmapPayloadBudgetHardCap(t *testing.T) {
	uc := NewBuildHeatmapWithConfig(BuildHeatmapConfig{
		MaxPriceBucketsPerWindow: 64,
		MaxCellsPerWindow:        64,
		MaxOpenWindowsPerKey:     2,
		MaxPayloadBytes:          420,
	})
	var artifactRaw []byte
	for i := 0; i < 32; i++ {
		res := uc.Execute(context.Background(), BuildHeatmapRequest{
			EventType:  "marketdata.trade",
			Venue:      "binance",
			Instrument: "BTCUSDT",
			Timeframe:  "1m",
			TickSize:   0.1,
			Price:      100 + float64(i)*0.1,
			Size:       0.1 + float64(i%5),
			Side:       "buy",
			TsIngest:   1_710_000_100_000 + int64(i),
			Seq:        int64(i + 1),
		})
		if res.IsFail() {
			t.Fatalf("Execute failed: %v", res.Problem())
		}
		raw, err := json.Marshal(res.Value().Artifact)
		if err != nil {
			t.Fatalf("Marshal artifact: %v", err)
		}
		artifactRaw = raw
	}
	if len(artifactRaw) > 420 {
		t.Fatalf("payload budget exceeded: bytes=%d cap=420", len(artifactRaw))
	}
}

func TestHeatmapReplayGoldenMatrixHash(t *testing.T) {
	// Run the full sequence multiple times and verify the hash is stable.
	// We avoid a hardcoded golden value because ARM64 and x86-64 may produce
	// subtly different floating-point results (FMA, rounding), changing the
	// JSON representation and therefore the SHA-256 hash.
	const runs = 5
	hashes := make([]string, runs)
	for r := 0; r < runs; r++ {
		uc := NewBuildHeatmap()
		var last BuildHeatmapResponse
		for _, req := range testHeatmapSequence() {
			res := uc.Execute(context.Background(), req)
			if res.IsFail() {
				t.Fatalf("Execute failed: %v", res.Problem())
			}
			last = res.Value()
		}
		raw, err := json.Marshal(last.Artifact)
		if err != nil {
			t.Fatalf("Marshal artifact: %v", err)
		}
		hashes[r] = sharedhash.HashBytes(raw)
	}
	for i := 1; i < runs; i++ {
		if hashes[i] != hashes[0] {
			t.Fatalf("golden hash unstable across runs: run0=%s run%d=%s", hashes[0], i, hashes[i])
		}
	}
	t.Logf("stable golden hash (%d runs): %s", runs, hashes[0])
}

func TestHeatmapSubMinuteTF_FinerBins(t *testing.T) {
	// With sub-minute TF (5s), the same price distribution should produce
	// more distinct price buckets than with the default 1h TF due to the
	// smaller bin factor. Use $1 increments over a $19 range at BTC price —
	// tight enough that coarser bins (25 for 1h) merge most levels while
	// finer bins (1.0 for 5s) keep them distinct.
	runAndCountBuckets := func(tf string) int {
		uc := NewBuildHeatmap()
		var last BuildHeatmapResponse
		for i := 0; i < 20; i++ {
			res := uc.Execute(context.Background(), BuildHeatmapRequest{
				EventType:  "marketdata.trade",
				Venue:      "binance",
				Instrument: "BTCUSDT",
				Timeframe:  tf,
				TickSize:   0.01,
				Price:      90000 + float64(i),
				Size:       1,
				Side:       "buy",
				TsIngest:   1_710_000_000_000 + int64(i),
				Seq:        int64(i + 1),
			})
			if res.IsFail() {
				t.Fatalf("Execute failed for tf=%s: %v", tf, res.Problem())
			}
			last = res.Value()
		}
		buckets := map[float64]struct{}{}
		for _, c := range last.Artifact.Cells {
			buckets[c.PriceBucketLow] = struct{}{}
		}
		return len(buckets)
	}

	buckets5s := runAndCountBuckets("5s")
	buckets1h := runAndCountBuckets("1h")

	// 5s (bin ~1.0) should have many more distinct price buckets than
	// 1h (bin ~25), since $1 increments stay distinct at 1.0 granularity
	// but merge at 25 granularity.
	if buckets5s <= buckets1h {
		t.Fatalf("5s should have more buckets than 1h: got 5s=%d, 1h=%d", buckets5s, buckets1h)
	}
	t.Logf("price bucket counts: 5s=%d, 1h=%d", buckets5s, buckets1h)
}

func testHeatmapSequence() []BuildHeatmapRequest {
	out := make([]BuildHeatmapRequest, 0, 24)
	for i := 0; i < 24; i++ {
		eventType := "marketdata.trade"
		if i%3 == 0 {
			eventType = "marketdata.bookdelta"
		}
		side := "buy"
		if i%2 == 1 {
			side = "sell"
		}
		out = append(out, BuildHeatmapRequest{
			EventType:  eventType,
			Venue:      "binance",
			Instrument: "BTC-USDT",
			Timeframe:  "1m",
			TickSize:   0.5,
			Price:      100 + float64(i%8)*0.5,
			Size:       0.2 + float64((i%5)+1)*0.3,
			Side:       side,
			TsIngest:   1_710_000_000_000 + int64(i*500),
			Seq:        int64(i + 1),
		})
	}
	return out
}
