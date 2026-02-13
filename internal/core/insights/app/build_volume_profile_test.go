package app

import (
	"context"
	"testing"

	sharedhash "github.com/market-raccoon/internal/shared/hash"
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
