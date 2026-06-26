package app

import (
	"context"
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/core/insights/domain"
)

func TestBuildTPOProfile_BasicAccumulation(t *testing.T) {
	uc := NewBuildTPOProfileWithConfig(BuildTPOProfileConfig{EmitCadence: 1})
	anchor := domain.SessionPresets["UTC_DAILY"]

	res := uc.Execute(context.Background(), BuildTPOProfileRequest{
		Venue:      "binance",
		Instrument: "BTCUSDT",
		Anchor:     anchor,
		TickSize:   0.5,
		High:       102,
		Low:        99,
		TsIngest:   1772920800000,
		SeqLast:    10,
	})
	if res.IsFail() {
		t.Fatalf("execute failed: %v", res.Problem())
	}
	v := res.Value()
	if !v.Emitted {
		t.Fatal("expected emission")
	}
	if len(v.Snapshot.Periods) == 0 {
		t.Fatal("expected at least one period")
	}
	if len(v.Snapshot.Levels) == 0 {
		t.Fatal("expected at least one level")
	}
	if v.Snapshot.POCPrice <= 0 {
		t.Fatal("expected positive POC price")
	}
}

func TestBuildTPOProfile_MultiPeriod(t *testing.T) {
	uc := NewBuildTPOProfileWithConfig(BuildTPOProfileConfig{EmitCadence: 1})
	anchor := domain.SessionPresets["UTC_DAILY"]
	thirtyMin := int64(30 * 60 * 1000)
	// Use midnight UTC as session start so periods align to A, B.
	sessionStart := int64(1772841600000)

	// Period A candle (at midnight).
	uc.Execute(context.Background(), BuildTPOProfileRequest{
		Venue: "binance", Instrument: "BTCUSDT", Anchor: anchor,
		TickSize: 0.5, High: 102, Low: 99,
		TsIngest: sessionStart, SeqLast: 10,
	})

	// Period B candle (30min later).
	res := uc.Execute(context.Background(), BuildTPOProfileRequest{
		Venue: "binance", Instrument: "BTCUSDT", Anchor: anchor,
		TickSize: 0.5, High: 104, Low: 100,
		TsIngest: sessionStart + thirtyMin, SeqLast: 20,
	})
	if res.IsFail() {
		t.Fatalf("execute failed: %v", res.Problem())
	}

	snap := res.Value().Snapshot
	if len(snap.Periods) != 2 {
		t.Errorf("expected 2 periods, got %d", len(snap.Periods))
	}
	if snap.Periods[0].Letter != 'A' || snap.Periods[1].Letter != 'B' {
		t.Errorf("expected periods A and B, got %c and %c", snap.Periods[0].Letter, snap.Periods[1].Letter)
	}
	if snap.RangeHigh != 104 {
		t.Errorf("range high: got %f, want 104", snap.RangeHigh)
	}
}

func TestBuildTPOProfile_InitialBalance(t *testing.T) {
	uc := NewBuildTPOProfileWithConfig(BuildTPOProfileConfig{EmitCadence: 1})
	anchor := domain.SessionPresets["UTC_DAILY"]
	thirtyMin := int64(30 * 60 * 1000)
	sessionStart := int64(1772841600000)

	// Period A.
	uc.Execute(context.Background(), BuildTPOProfileRequest{
		Venue: "binance", Instrument: "BTCUSDT", Anchor: anchor,
		TickSize: 0.5, High: 102, Low: 99,
		TsIngest: sessionStart, SeqLast: 10,
	})

	// Period B.
	res := uc.Execute(context.Background(), BuildTPOProfileRequest{
		Venue: "binance", Instrument: "BTCUSDT", Anchor: anchor,
		TickSize: 0.5, High: 105, Low: 98,
		TsIngest: sessionStart + thirtyMin, SeqLast: 20,
	})
	snap := res.Value().Snapshot
	if snap.IBHigh != 105 {
		t.Errorf("IB high: got %f, want 105", snap.IBHigh)
	}
	if snap.IBLow != 98 {
		t.Errorf("IB low: got %f, want 98", snap.IBLow)
	}
}

func TestBuildTPOProfile_SessionRollover(t *testing.T) {
	uc := NewBuildTPOProfileWithConfig(BuildTPOProfileConfig{EmitCadence: 1})
	anchor := domain.SessionPresets["CRYPTO_4H"]
	fourH := int64(4 * 3600 * 1000)

	// Session A.
	res1 := uc.Execute(context.Background(), BuildTPOProfileRequest{
		Venue: "binance", Instrument: "BTCUSDT", Anchor: anchor,
		TickSize: 0.5, High: 102, Low: 99,
		TsIngest: 1772920800000, SeqLast: 10,
	})
	// Session B.
	res2 := uc.Execute(context.Background(), BuildTPOProfileRequest{
		Venue: "binance", Instrument: "BTCUSDT", Anchor: anchor,
		TickSize: 0.5, High: 110, Low: 108,
		TsIngest: 1772920800000 + fourH, SeqLast: 20,
	})
	s1 := res1.Value().Snapshot
	s2 := res2.Value().Snapshot
	if s1.WindowStartTs == s2.WindowStartTs {
		t.Fatal("expected different sessions")
	}
}

func TestBuildTPOProfile_ValidationFailure(t *testing.T) {
	uc := NewBuildTPOProfile()
	res := uc.Execute(context.Background(), BuildTPOProfileRequest{})
	if !res.IsFail() {
		t.Fatal("expected validation failure")
	}
}
