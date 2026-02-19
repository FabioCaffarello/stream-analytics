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
	const want = "ec6b2894a0d61a0b2264c132420fcdcf87a8b12bc6903929d0ec0a72820c7a75"
	got := sharedhash.HashBytes(raw)
	if got != want {
		t.Fatalf("golden hash mismatch: got=%s want=%s", got, want)
	}
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
