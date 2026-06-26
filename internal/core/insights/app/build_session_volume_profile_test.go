package app

import (
	"context"
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/core/insights/domain"
	sharedhash "github.com/FabioCaffarello/stream-analytics/internal/shared/hash"
)

func TestBuildSessionVolumeProfile_BasicAccumulation(t *testing.T) {
	uc := NewBuildSessionVolumeProfileWithConfig(BuildSessionVolumeProfileConfig{
		EmitCadence: 1, // emit every candle for testing
	})
	anchor := domain.SessionPresets["UTC_DAILY"]
	res := uc.Execute(context.Background(), BuildSessionVolumeProfileRequest{
		Venue:      "binance",
		Instrument: "BTCUSDT",
		Anchor:     anchor,
		TickSize:   0.5,
		Open:       100,
		High:       102,
		Low:        99,
		Close:      101,
		BuyVolume:  10,
		SellVolume: 5,
		TradeCount: 15,
		TsIngest:   1772920800000,
		SeqFirst:   1,
		SeqLast:    15,
	})
	if res.IsFail() {
		t.Fatalf("execute failed: %v", res.Problem())
	}
	v := res.Value()
	if !v.Emitted {
		t.Fatal("expected emission")
	}
	if v.Snapshot.TotalVolume != 15 {
		t.Errorf("total volume: got %f, want 15", v.Snapshot.TotalVolume)
	}
	if len(v.Snapshot.Buckets) == 0 {
		t.Fatal("expected buckets")
	}
	if v.IdempotencyKey == "" {
		t.Fatal("expected idempotency key")
	}
}

func TestBuildSessionVolumeProfile_Deterministic(t *testing.T) {
	anchor := domain.SessionPresets["UTC_DAILY"]
	candles := []BuildSessionVolumeProfileRequest{
		{Venue: "binance", Instrument: "BTCUSDT", Anchor: anchor, TickSize: 0.5, Open: 100, High: 102, Low: 99, Close: 101, BuyVolume: 10, SellVolume: 5, TradeCount: 15, TsIngest: 1772920800000, SeqFirst: 1, SeqLast: 15},
		{Venue: "binance", Instrument: "BTCUSDT", Anchor: anchor, TickSize: 0.5, Open: 101, High: 103, Low: 100, Close: 102, BuyVolume: 8, SellVolume: 12, TradeCount: 20, TsIngest: 1772920860000, SeqFirst: 16, SeqLast: 35},
	}

	run := func() string {
		uc := NewBuildSessionVolumeProfileWithConfig(BuildSessionVolumeProfileConfig{EmitCadence: 1})
		var snap domain.SessionVolumeProfileV1
		for _, c := range candles {
			res := uc.Execute(context.Background(), c)
			if res.IsFail() {
				t.Fatalf("execute failed: %v", res.Problem())
			}
			if res.Value().Emitted {
				snap = res.Value().Snapshot
			}
		}
		raw, _ := MarshalVPVRSnapshotStableBytes(domain.VolumeProfileSnapshotV1{
			Venue: snap.Venue, Instrument: snap.Instrument,
			WindowStartTs: snap.WindowStartTs, WindowEndTs: snap.WindowEndTs,
			Buckets: snap.Buckets, POCPrice: snap.POCPrice,
			ValueAreaLow: snap.ValueAreaLow, ValueAreaHigh: snap.ValueAreaHigh,
		})
		return sharedhash.HashBytes(raw)
	}

	h1 := run()
	h2 := run()
	if h1 != h2 {
		t.Fatalf("non-deterministic: %s vs %s", h1, h2)
	}
}

func TestBuildSessionVolumeProfile_SessionRollover(t *testing.T) {
	uc := NewBuildSessionVolumeProfileWithConfig(BuildSessionVolumeProfileConfig{EmitCadence: 1})
	anchor := domain.SessionPresets["CRYPTO_4H"]

	// First candle in session A.
	res1 := uc.Execute(context.Background(), BuildSessionVolumeProfileRequest{
		Venue: "binance", Instrument: "BTCUSDT", Anchor: anchor,
		TickSize: 0.5, Open: 100, High: 102, Low: 99, Close: 101,
		BuyVolume: 10, SellVolume: 5, TradeCount: 15,
		TsIngest: 1772920800000, SeqFirst: 1, SeqLast: 15,
	})
	if res1.IsFail() {
		t.Fatalf("candle 1 failed: %v", res1.Problem())
	}
	snap1 := res1.Value().Snapshot

	// Second candle in session B (4h later).
	fourH := int64(4 * 3600 * 1000)
	res2 := uc.Execute(context.Background(), BuildSessionVolumeProfileRequest{
		Venue: "binance", Instrument: "BTCUSDT", Anchor: anchor,
		TickSize: 0.5, Open: 105, High: 107, Low: 104, Close: 106,
		BuyVolume: 20, SellVolume: 10, TradeCount: 30,
		TsIngest: 1772920800000 + fourH, SeqFirst: 16, SeqLast: 45,
	})
	if res2.IsFail() {
		t.Fatalf("candle 2 failed: %v", res2.Problem())
	}
	snap2 := res2.Value().Snapshot

	// Snapshots should be from different sessions.
	if snap1.WindowStartTs == snap2.WindowStartTs {
		t.Fatal("expected different sessions after rollover")
	}
	// New session should have only the second candle's volume.
	if snap2.TotalVolume != 30 {
		t.Errorf("new session total: got %f, want 30", snap2.TotalVolume)
	}
}

func TestBuildSessionVolumeProfile_EmitCadence(t *testing.T) {
	uc := NewBuildSessionVolumeProfileWithConfig(BuildSessionVolumeProfileConfig{EmitCadence: 3})
	anchor := domain.SessionPresets["UTC_DAILY"]

	for i := 1; i <= 5; i++ {
		res := uc.Execute(context.Background(), BuildSessionVolumeProfileRequest{
			Venue: "binance", Instrument: "BTCUSDT", Anchor: anchor,
			TickSize: 0.5, Open: 100, High: 102, Low: 99, Close: 101,
			BuyVolume: 1, SellVolume: 1, TradeCount: 1,
			TsIngest: 1772920800000, SeqFirst: int64(i), SeqLast: int64(i),
		})
		if res.IsFail() {
			t.Fatalf("candle %d failed: %v", i, res.Problem())
		}
		emitted := res.Value().Emitted
		shouldEmit := i%3 == 0
		if emitted != shouldEmit {
			t.Errorf("candle %d: emitted=%v, want %v", i, emitted, shouldEmit)
		}
	}
}

func TestBuildSessionVolumeProfile_ValidationFailure(t *testing.T) {
	uc := NewBuildSessionVolumeProfile()
	res := uc.Execute(context.Background(), BuildSessionVolumeProfileRequest{})
	if !res.IsFail() {
		t.Fatal("expected validation failure for empty request")
	}
}

func TestBuildSessionVolumeProfile_Snapshot(t *testing.T) {
	uc := NewBuildSessionVolumeProfileWithConfig(BuildSessionVolumeProfileConfig{EmitCadence: 1})
	anchor := domain.SessionPresets["UTC_DAILY"]
	uc.Execute(context.Background(), BuildSessionVolumeProfileRequest{
		Venue: "binance", Instrument: "BTCUSDT", Anchor: anchor,
		TickSize: 0.5, Open: 100, High: 102, Low: 99, Close: 101,
		BuyVolume: 10, SellVolume: 5, TradeCount: 15,
		TsIngest: 1772920800000, SeqFirst: 1, SeqLast: 15,
	})

	snap, p := uc.Snapshot("binance", "BTCUSDT", "UTC_DAILY")
	if p != nil {
		t.Fatalf("snapshot query failed: %v", p)
	}
	if snap.TotalVolume != 15 {
		t.Errorf("snapshot total: got %f, want 15", snap.TotalVolume)
	}
}
