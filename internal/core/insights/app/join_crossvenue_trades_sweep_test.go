package app

import (
	"testing"
	"time"

	"github.com/market-raccoon/internal/shared/clock"
)

func TestJoinCrossVenueTrades_SweepCadence_EveryN(t *testing.T) {
	clk := clock.NewFakeClock(time.UnixMilli(1_710_000_000_000))
	uc := NewJoinCrossVenueTradesWithConfig(JoinCrossVenueTradesConfig{
		MaxInstruments: 128,
		TTL:            time.Hour,
		SweepEveryN:    4,
		SweepEvery:     30 * time.Second,
		Clock:          clk,
	})

	seed := JoinCrossVenueTradesRequest{
		Venue:      "BINANCE",
		Instrument: "BTCUSDT",
		MarketType: "SPOT",
		Price:      1,
		Size:       1,
		Side:       "buy",
		TsIngest:   1,
		Seq:        1,
	}

	for i := 0; i < 3; i++ {
		req := seed
		req.Seq = int64(i + 1)
		req.TsIngest = int64(i + 1)
		_ = uc.Execute(t.Context(), req)
	}
	if got := uc.sweepCallsForTest(); got != 0 {
		t.Fatalf("sweep calls before cadence=%d want=0", got)
	}

	req := seed
	req.Seq = 4
	req.TsIngest = 4
	_ = uc.Execute(t.Context(), req)
	if got := uc.sweepCallsForTest(); got != 1 {
		t.Fatalf("sweep calls at cadence=%d want=1", got)
	}
}

func TestJoinCrossVenueTrades_SweepCadence_EveryDuration(t *testing.T) {
	clk := clock.NewFakeClock(time.UnixMilli(1_710_000_000_000))
	uc := NewJoinCrossVenueTradesWithConfig(JoinCrossVenueTradesConfig{
		MaxInstruments: 128,
		TTL:            time.Hour,
		SweepEveryN:    0,
		SweepEvery:     30 * time.Second,
		Clock:          clk,
	})

	req := JoinCrossVenueTradesRequest{
		Venue:      "BINANCE",
		Instrument: "BTCUSDT",
		MarketType: "SPOT",
		Price:      1,
		Size:       1,
		Side:       "buy",
		TsIngest:   1,
		Seq:        1,
	}

	_ = uc.Execute(t.Context(), req)
	if got := uc.sweepCallsForTest(); got != 0 {
		t.Fatalf("initial sweep calls=%d want=0", got)
	}

	clk.Advance(29 * time.Second)
	req.Seq++
	req.TsIngest++
	_ = uc.Execute(t.Context(), req)
	if got := uc.sweepCallsForTest(); got != 0 {
		t.Fatalf("sweep calls before interval=%d want=0", got)
	}

	clk.Advance(2 * time.Second)
	req.Seq++
	req.TsIngest++
	_ = uc.Execute(t.Context(), req)
	if got := uc.sweepCallsForTest(); got != 1 {
		t.Fatalf("sweep calls after interval=%d want=1", got)
	}
}

func TestJoinCrossVenueTrades_SweepCadence_Disabled(t *testing.T) {
	clk := clock.NewFakeClock(time.UnixMilli(1_710_000_000_000))
	uc := NewJoinCrossVenueTradesWithConfig(JoinCrossVenueTradesConfig{
		MaxInstruments: 128,
		TTL:            time.Hour,
		SweepEveryN:    0,
		SweepEvery:     0,
		Clock:          clk,
	})

	req := JoinCrossVenueTradesRequest{
		Venue:      "BINANCE",
		Instrument: "BTCUSDT",
		Price:      1,
		Size:       1,
		Side:       "buy",
		TsIngest:   1,
		Seq:        1,
	}
	for i := 0; i < 32; i++ {
		req.Seq = int64(i + 1)
		req.TsIngest = int64(i + 1)
		_ = uc.Execute(t.Context(), req)
	}
	if got := uc.sweepCallsForTest(); got != 0 {
		t.Fatalf("disabled sweep calls=%d want=0", got)
	}
}

func TestJoinCrossVenueTrades_TTLRemainsCorrectWithThrottledSweep(t *testing.T) {
	clk := clock.NewFakeClock(time.UnixMilli(1_710_000_000_000))
	uc := NewJoinCrossVenueTradesWithConfig(JoinCrossVenueTradesConfig{
		MaxInstruments: 128,
		TTL:            time.Hour,
		SweepEveryN:    1024, // effectively disabled for this test path
		SweepEvery:     30 * time.Second,
		Clock:          clk,
	})

	a := JoinCrossVenueTradesRequest{
		Venue:      "BINANCE",
		Instrument: "BTCUSDT",
		MarketType: "SPOT",
		Price:      1,
		Size:       1,
		Side:       "buy",
		TsIngest:   1,
		Seq:        1,
	}
	b := a
	b.Venue = "BYBIT"
	b.TsIngest = 2
	b.Seq = 2

	if res := uc.Execute(t.Context(), a); res.IsFail() {
		t.Fatalf("seed a failed: %v", res.Problem())
	}
	if res := uc.Execute(t.Context(), b); res.IsFail() || !res.Value().Emitted {
		t.Fatalf("seed b unexpected result: fail=%v emitted=%v", res.IsFail(), res.Value().Emitted)
	}

	clk.Advance(2 * time.Hour)
	again := a
	again.Seq = 3
	again.TsIngest = 3
	res := uc.Execute(t.Context(), again)
	if res.IsFail() {
		t.Fatalf("post-ttl execute failed: %v", res.Problem())
	}
	if res.Value().Emitted {
		t.Fatal("expired state must be treated as missing (single venue should not emit)")
	}
}
