package app_test

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	insightsapp "github.com/FabioCaffarello/stream-analytics/internal/core/insights/app"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/clock"
)

func TestJoinCrossVenueTrades_EmitsAfterSecondVenue_Sorted(t *testing.T) {
	clk := clock.NewFakeClock(time.UnixMilli(1_710_000_000_000))
	uc := insightsapp.NewJoinCrossVenueTradesWithConfig(insightsapp.JoinCrossVenueTradesConfig{
		MaxInstruments: 10_000,
		TTL:            time.Hour,
		Clock:          clk,
	})

	first := insightsapp.JoinCrossVenueTradesRequest{
		Venue:          "binance",
		Instrument:     "BTCUSDT",
		MarketType:     "SPOT",
		Price:          100.1,
		Size:           1.2,
		Side:           "buy",
		TradeID:        "b-1",
		TsExchange:     1_710_000_000_010,
		TsIngest:       1_710_000_000_110,
		Seq:            10,
		IdempotencyKey: "idem-b-1",
	}
	second := insightsapp.JoinCrossVenueTradesRequest{
		Venue:          "bybit",
		Instrument:     "BTCUSDT",
		MarketType:     "SPOT",
		Price:          100.2,
		Size:           0.8,
		Side:           "sell",
		TradeID:        "y-1",
		TsExchange:     1_710_000_000_020,
		TsIngest:       1_710_000_000_120,
		Seq:            12,
		IdempotencyKey: "idem-y-1",
	}

	r1 := uc.Execute(t.Context(), first)
	if r1.IsFail() {
		t.Fatalf("first execute failed: %v", r1.Problem())
	}
	if r1.Value().Emitted {
		t.Fatal("first venue must not emit snapshot")
	}

	r2 := uc.Execute(t.Context(), second)
	if r2.IsFail() {
		t.Fatalf("second execute failed: %v", r2.Problem())
	}
	if !r2.Value().Emitted {
		t.Fatal("second venue should emit snapshot")
	}

	snap := r2.Value().Snapshot
	if snap.Instrument != "BTCUSDT" {
		t.Fatalf("snapshot instrument=%q want BTCUSDT", snap.Instrument)
	}
	if snap.MarketType != "SPOT" {
		t.Fatalf("snapshot market_type=%q want SPOT", snap.MarketType)
	}
	if snap.WatermarkTsIngest != second.TsIngest {
		t.Fatalf("watermark=%d want %d", snap.WatermarkTsIngest, second.TsIngest)
	}
	if got := []string{snap.Venues[0].Venue, snap.Venues[1].Venue}; !reflect.DeepEqual(got, []string{"BINANCE", "BYBIT"}) {
		t.Fatalf("snapshot venues=%v want [BINANCE BYBIT]", got)
	}
}

func TestJoinCrossVenueTrades_MarketTypePartitionsState(t *testing.T) {
	uc := insightsapp.NewJoinCrossVenueTrades()

	spot := insightsapp.JoinCrossVenueTradesRequest{
		Venue:      "BINANCE",
		Instrument: "BTCUSDT",
		MarketType: "SPOT",
		Price:      1,
		Size:       1,
		Side:       "buy",
		TsIngest:   10,
		Seq:        1,
	}
	usdm := insightsapp.JoinCrossVenueTradesRequest{
		Venue:      "BYBIT",
		Instrument: "BTCUSDT",
		MarketType: "USD_M_FUTURES",
		Price:      1,
		Size:       1,
		Side:       "sell",
		TsIngest:   20,
		Seq:        1,
	}

	if res := uc.Execute(t.Context(), spot); res.IsFail() || res.Value().Emitted {
		t.Fatalf("spot first venue unexpected result: fail=%v emitted=%v", res.IsFail(), res.Value().Emitted)
	}
	if res := uc.Execute(t.Context(), usdm); res.IsFail() || res.Value().Emitted {
		t.Fatalf("usdm first venue unexpected result: fail=%v emitted=%v", res.IsFail(), res.Value().Emitted)
	}

	spot2 := spot
	spot2.Venue = "BYBIT"
	spot2.TsIngest = 30
	spot2.Seq = 2
	res := uc.Execute(t.Context(), spot2)
	if res.IsFail() {
		t.Fatalf("spot second venue failed: %v", res.Problem())
	}
	if !res.Value().Emitted {
		t.Fatal("spot partition should emit after second venue")
	}
	if res.Value().Snapshot.MarketType != "SPOT" {
		t.Fatalf("snapshot market_type=%q want SPOT", res.Value().Snapshot.MarketType)
	}
}

func TestJoinCrossVenueTrades_StaleVenueTradeIgnored(t *testing.T) {
	uc := insightsapp.NewJoinCrossVenueTrades()

	base := insightsapp.JoinCrossVenueTradesRequest{
		Venue:      "BINANCE",
		Instrument: "ETHUSDT",
		MarketType: "SPOT",
		Price:      2000,
		Size:       1,
		Side:       "buy",
		TsIngest:   100,
		Seq:        10,
	}
	other := base
	other.Venue = "BYBIT"
	other.TsIngest = 110
	other.Seq = 12
	if res := uc.Execute(t.Context(), base); res.IsFail() || res.Value().Emitted {
		t.Fatalf("base unexpected result: fail=%v emitted=%v", res.IsFail(), res.Value().Emitted)
	}
	if res := uc.Execute(t.Context(), other); res.IsFail() || !res.Value().Emitted {
		t.Fatalf("other unexpected result: fail=%v emitted=%v", res.IsFail(), res.Value().Emitted)
	}

	stale := base
	stale.Price = 1999
	stale.TsIngest = 90
	stale.Seq = 9
	res := uc.Execute(t.Context(), stale)
	if res.IsFail() {
		t.Fatalf("stale execute failed: %v", res.Problem())
	}
	if res.Value().Emitted {
		t.Fatal("stale venue update must not emit snapshot")
	}
}

func TestJoinCrossVenueTrades_TieBreak_PreferHigherSeqThenTimestamps(t *testing.T) {
	uc := insightsapp.NewJoinCrossVenueTrades()

	// Seed venue state.
	base := insightsapp.JoinCrossVenueTradesRequest{
		Venue:      "BINANCE",
		Instrument: "BTCUSDT",
		MarketType: "SPOT",
		Price:      100.0,
		Size:       1.0,
		Side:       "buy",
		TradeID:    "t-base",
		TsExchange: 1000,
		TsIngest:   2000,
		Seq:        10,
	}
	if res := uc.Execute(t.Context(), base); res.IsFail() {
		t.Fatalf("seed failed: %v", res.Problem())
	}

	// Higher seq must win even with lower ts_ingest.
	higherSeqLowerIngest := base
	higherSeqLowerIngest.Price = 101.0
	higherSeqLowerIngest.TradeID = "t-seq"
	higherSeqLowerIngest.Seq = 11
	higherSeqLowerIngest.TsIngest = 1500
	if res := uc.Execute(t.Context(), higherSeqLowerIngest); res.IsFail() {
		t.Fatalf("higher seq update failed: %v", res.Problem())
	}

	// Same seq, higher ts_ingest must win.
	sameSeqHigherIngest := higherSeqLowerIngest
	sameSeqHigherIngest.Price = 102.0
	sameSeqHigherIngest.TradeID = "t-ingest"
	sameSeqHigherIngest.TsIngest = 2500
	if res := uc.Execute(t.Context(), sameSeqHigherIngest); res.IsFail() {
		t.Fatalf("same seq higher ingest update failed: %v", res.Problem())
	}

	// Same seq + same ts_ingest, higher ts_exchange must win.
	sameSeqSameIngestHigherExchange := sameSeqHigherIngest
	sameSeqSameIngestHigherExchange.Price = 103.0
	sameSeqSameIngestHigherExchange.TradeID = "t-exchange"
	sameSeqSameIngestHigherExchange.TsExchange = 2000
	if res := uc.Execute(t.Context(), sameSeqSameIngestHigherExchange); res.IsFail() {
		t.Fatalf("same seq same ingest higher exchange update failed: %v", res.Problem())
	}

	// Add second venue to force snapshot and inspect winning BINANCE row.
	other := base
	other.Venue = "BYBIT"
	other.Price = 200.0
	other.TradeID = "other-1"
	other.TsIngest = 3000
	other.Seq = 1
	res := uc.Execute(t.Context(), other)
	if res.IsFail() {
		t.Fatalf("second venue failed: %v", res.Problem())
	}
	if !res.Value().Emitted {
		t.Fatal("expected snapshot emission")
	}

	snap := res.Value().Snapshot
	if len(snap.Venues) != 2 {
		t.Fatalf("venues=%d want=2", len(snap.Venues))
	}
	if snap.Venues[0].Venue != "BINANCE" {
		t.Fatalf("first venue=%q want BINANCE", snap.Venues[0].Venue)
	}
	if snap.Venues[0].TradeID != "t-exchange" {
		t.Fatalf("winning trade_id=%q want t-exchange", snap.Venues[0].TradeID)
	}
}

func TestJoinCrossVenueTrades_BoundedMapEvictionByTTLAndSize(t *testing.T) {
	clk := clock.NewFakeClock(time.UnixMilli(1_710_000_000_000))
	uc := insightsapp.NewJoinCrossVenueTradesWithConfig(insightsapp.JoinCrossVenueTradesConfig{
		MaxInstruments: 1,
		TTL:            time.Hour,
		Clock:          clk,
	})

	seed := func(instrument string, ts int64) {
		a := insightsapp.JoinCrossVenueTradesRequest{
			Venue:      "BINANCE",
			Instrument: instrument,
			Price:      1,
			Size:       1,
			Side:       "buy",
			TsIngest:   ts,
			Seq:        1,
		}
		b := a
		b.Venue = "BYBIT"
		b.TsIngest = ts + 1
		b.Seq = 2
		_ = uc.Execute(t.Context(), a)
		_ = uc.Execute(t.Context(), b)
	}

	seed("BTCUSDT", 10)
	if got := uc.ActiveInstruments(); got != 1 {
		t.Fatalf("active instruments=%d want 1", got)
	}
	seed("ETHUSDT", 20)
	if got := uc.ActiveInstruments(); got != 1 {
		t.Fatalf("after size eviction active instruments=%d want 1", got)
	}

	clk.Advance(2 * time.Hour)
	res := uc.Execute(t.Context(), insightsapp.JoinCrossVenueTradesRequest{
		Venue:      "BINANCE",
		Instrument: "ETHUSDT",
		Price:      2,
		Size:       2,
		Side:       "buy",
		TsIngest:   30,
		Seq:        3,
	})
	if res.IsFail() {
		t.Fatalf("post-ttl execute failed: %v", res.Problem())
	}
	if res.Value().Emitted {
		t.Fatal("expired state should not emit with a single venue")
	}
}

func TestJoinCrossVenueTrades_DeterministicGivenSameInputSequence(t *testing.T) {
	sequence := []insightsapp.JoinCrossVenueTradesRequest{
		{Venue: "BINANCE", Instrument: "BTCUSDT", MarketType: "SPOT", Price: 100, Size: 1, Side: "buy", TradeID: "b1", TsExchange: 10, TsIngest: 10, Seq: 1, IdempotencyKey: "i1"},
		{Venue: "BYBIT", Instrument: "BTCUSDT", MarketType: "SPOT", Price: 101, Size: 1, Side: "sell", TradeID: "y1", TsExchange: 11, TsIngest: 11, Seq: 1, IdempotencyKey: "i2"},
		{Venue: "BINANCE", Instrument: "BTCUSDT", MarketType: "SPOT", Price: 102, Size: 1, Side: "buy", TradeID: "b2", TsExchange: 12, TsIngest: 12, Seq: 2, IdempotencyKey: "i3"},
		{Venue: "BYBIT", Instrument: "ETHUSDT", MarketType: "SPOT", Price: 200, Size: 2, Side: "buy", TradeID: "y2", TsExchange: 13, TsIngest: 13, Seq: 1, IdempotencyKey: "i4"},
		{Venue: "BINANCE", Instrument: "ETHUSDT", MarketType: "SPOT", Price: 201, Size: 2, Side: "sell", TradeID: "b3", TsExchange: 14, TsIngest: 14, Seq: 1, IdempotencyKey: "i5"},
	}

	collect := func() [][]byte {
		uc := insightsapp.NewJoinCrossVenueTrades()
		out := make([][]byte, 0, len(sequence))
		for _, req := range sequence {
			res := uc.Execute(t.Context(), req)
			if res.IsFail() {
				t.Fatalf("execute failed: %v", res.Problem())
			}
			if !res.Value().Emitted {
				continue
			}
			b, err := json.Marshal(res.Value().Snapshot)
			if err != nil {
				t.Fatalf("json marshal snapshot: %v", err)
			}
			out = append(out, b)
		}
		return out
	}

	first := collect()
	second := collect()
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("deterministic replay mismatch\nfirst=%q\nsecond=%q", first, second)
	}
}

func TestJoinCrossVenueTrades_DeterministicFinalSnapshotAcrossCrossVenueOrders(t *testing.T) {
	base := []insightsapp.JoinCrossVenueTradesRequest{
		{Venue: "BINANCE", Instrument: "BTCUSDT", MarketType: "SPOT", Price: 100, Size: 1, Side: "buy", TradeID: "b1", TsExchange: 10, TsIngest: 10, Seq: 1},
		{Venue: "BYBIT", Instrument: "BTCUSDT", MarketType: "SPOT", Price: 101, Size: 1, Side: "sell", TradeID: "y1", TsExchange: 11, TsIngest: 11, Seq: 1},
		{Venue: "BINANCE", Instrument: "BTCUSDT", MarketType: "SPOT", Price: 102, Size: 1, Side: "buy", TradeID: "b2", TsExchange: 12, TsIngest: 12, Seq: 2},
		{Venue: "BYBIT", Instrument: "BTCUSDT", MarketType: "SPOT", Price: 103, Size: 1, Side: "sell", TradeID: "y2", TsExchange: 13, TsIngest: 13, Seq: 2},
	}
	altOrder := []insightsapp.JoinCrossVenueTradesRequest{
		base[1],
		base[0],
		base[2],
		base[3],
	}

	finalSnapshot := func(seq []insightsapp.JoinCrossVenueTradesRequest) []byte {
		uc := insightsapp.NewJoinCrossVenueTrades()
		var last []byte
		for _, req := range seq {
			res := uc.Execute(t.Context(), req)
			if res.IsFail() {
				t.Fatalf("execute failed: %v", res.Problem())
			}
			if !res.Value().Emitted {
				continue
			}
			b, err := json.Marshal(res.Value().Snapshot)
			if err != nil {
				t.Fatalf("json marshal snapshot: %v", err)
			}
			last = b
		}
		if len(last) == 0 {
			t.Fatal("expected at least one emitted snapshot")
		}
		return last
	}

	first := finalSnapshot(base)
	second := finalSnapshot(altOrder)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("final snapshot differs across cross-venue orders\nfirst=%s\nsecond=%s", first, second)
	}
}

func TestJoinCrossVenueTrades_SnapshotDerivedSpreadFields(t *testing.T) {
	uc := insightsapp.NewJoinCrossVenueTradesWithConfig(insightsapp.JoinCrossVenueTradesConfig{
		RoundingMode: "half_even",
	})

	first := insightsapp.JoinCrossVenueTradesRequest{
		Venue:      "BINANCE",
		Instrument: "BTCUSDT",
		MarketType: "SPOT",
		Price:      100.0,
		Size:       1.0,
		Side:       "buy",
		TsExchange: 100,
		TsIngest:   110,
		Seq:        1,
	}
	second := insightsapp.JoinCrossVenueTradesRequest{
		Venue:      "BYBIT",
		Instrument: "BTCUSDT",
		MarketType: "SPOT",
		Price:      101.0,
		Size:       1.0,
		Side:       "sell",
		TsExchange: 101,
		TsIngest:   111,
		Seq:        1,
	}

	if res := uc.Execute(t.Context(), first); res.IsFail() || res.Value().Emitted {
		t.Fatalf("first execute unexpected result: fail=%v emitted=%v", res.IsFail(), res.Value().Emitted)
	}
	res := uc.Execute(t.Context(), second)
	if res.IsFail() {
		t.Fatalf("second execute failed: %v", res.Problem())
	}
	if !res.Value().Emitted {
		t.Fatal("expected snapshot emission")
	}

	snap := res.Value().Snapshot
	if snap.MinPrice != 100.0 || snap.MinPriceVenue != "BINANCE" {
		t.Fatalf("min derived mismatch: price=%f venue=%s", snap.MinPrice, snap.MinPriceVenue)
	}
	if snap.MaxPrice != 101.0 || snap.MaxPriceVenue != "BYBIT" {
		t.Fatalf("max derived mismatch: price=%f venue=%s", snap.MaxPrice, snap.MaxPriceVenue)
	}
	if snap.SpreadAbs != 1.0 {
		t.Fatalf("spread_abs=%f want=1.0", snap.SpreadAbs)
	}
	if snap.MidPrice != 100.5 {
		t.Fatalf("mid_price=%f want=100.5", snap.MidPrice)
	}
	if snap.SpreadBps != 99.5025 {
		t.Fatalf("spread_bps=%f want=99.5025", snap.SpreadBps)
	}
}

func TestJoinCrossVenueTrades_SnapshotDerivedSpreadFields_FloorRounding(t *testing.T) {
	uc := insightsapp.NewJoinCrossVenueTradesWithConfig(insightsapp.JoinCrossVenueTradesConfig{
		RoundingMode: "floor",
	})

	first := insightsapp.JoinCrossVenueTradesRequest{
		Venue:      "BINANCE",
		Instrument: "BTCUSDT",
		Price:      100.0,
		Size:       1.0,
		Side:       "buy",
		TsIngest:   100,
		Seq:        1,
	}
	second := first
	second.Venue = "BYBIT"
	second.Price = 101.0
	second.TsIngest = 101

	_ = uc.Execute(t.Context(), first)
	res := uc.Execute(t.Context(), second)
	if res.IsFail() {
		t.Fatalf("execute failed: %v", res.Problem())
	}
	if !res.Value().Emitted {
		t.Fatal("expected snapshot emission")
	}
	if res.Value().Snapshot.SpreadBps != 99.5024 {
		t.Fatalf("spread_bps=%f want=99.5024", res.Value().Snapshot.SpreadBps)
	}
}

func TestJoinCrossVenueTrades_SpreadSignalOptInThreshold(t *testing.T) {
	uc := insightsapp.NewJoinCrossVenueTradesWithConfig(insightsapp.JoinCrossVenueTradesConfig{
		EnableSpreadSignal: true,
		MinVenues:          2,
		MinSpreadBPS:       50,
		RoundingMode:       "half_even",
	})

	first := insightsapp.JoinCrossVenueTradesRequest{
		Venue:      "BINANCE",
		Instrument: "BTCUSDT",
		Price:      100.0,
		Size:       1.0,
		Side:       "buy",
		TsIngest:   100,
		Seq:        1,
	}
	second := first
	second.Venue = "BYBIT"
	second.Price = 101.0
	second.TsIngest = 101

	_ = uc.Execute(t.Context(), first)
	res := uc.Execute(t.Context(), second)
	if res.IsFail() {
		t.Fatalf("execute failed: %v", res.Problem())
	}
	if !res.Value().Emitted {
		t.Fatal("expected snapshot emission")
	}
	if !res.Value().SignalEmitted {
		t.Fatal("expected spread signal emission")
	}
	if res.Value().SpreadSignal.SpreadBps < 50 {
		t.Fatalf("spread signal bps=%f want>=50", res.Value().SpreadSignal.SpreadBps)
	}
}

func TestJoinCrossVenueTrades_SpreadSignalDisabledByDefault(t *testing.T) {
	uc := insightsapp.NewJoinCrossVenueTrades()

	first := insightsapp.JoinCrossVenueTradesRequest{
		Venue:      "BINANCE",
		Instrument: "BTCUSDT",
		Price:      100.0,
		Size:       1.0,
		Side:       "buy",
		TsIngest:   100,
		Seq:        1,
	}
	second := first
	second.Venue = "BYBIT"
	second.Price = 101.0
	second.TsIngest = 101

	_ = uc.Execute(t.Context(), first)
	res := uc.Execute(t.Context(), second)
	if res.IsFail() {
		t.Fatalf("execute failed: %v", res.Problem())
	}
	if !res.Value().Emitted {
		t.Fatal("expected snapshot emission")
	}
	if res.Value().SignalEmitted {
		t.Fatal("spread signal must be disabled by default")
	}
}

func TestJoinCrossVenueTrades_GoldenDeterministicSnapshotAndSignalBytes_50Runs(t *testing.T) {
	sequence := []insightsapp.JoinCrossVenueTradesRequest{
		{Venue: "BINANCE", Instrument: "BTCUSDT", MarketType: "SPOT", Price: 100.25, Size: 1, Side: "buy", TradeID: "b1", TsExchange: 10, TsIngest: 10, Seq: 1},
		{Venue: "BYBIT", Instrument: "BTCUSDT", MarketType: "SPOT", Price: 100.55, Size: 1, Side: "sell", TradeID: "y1", TsExchange: 11, TsIngest: 11, Seq: 1},
		{Venue: "BINANCE", Instrument: "BTCUSDT", MarketType: "SPOT", Price: 100.35, Size: 1, Side: "buy", TradeID: "b2", TsExchange: 12, TsIngest: 12, Seq: 2},
		{Venue: "BYBIT", Instrument: "BTCUSDT", MarketType: "SPOT", Price: 100.65, Size: 1, Side: "sell", TradeID: "y2", TsExchange: 13, TsIngest: 13, Seq: 2},
	}

	collect := func() ([]byte, []byte) {
		uc := insightsapp.NewJoinCrossVenueTradesWithConfig(insightsapp.JoinCrossVenueTradesConfig{
			EnableSpreadSignal: true,
			MinVenues:          2,
			MinSpreadBPS:       0,
			RoundingMode:       "half_even",
		})
		var lastSnapshot []byte
		var lastSignal []byte
		for _, req := range sequence {
			res := uc.Execute(t.Context(), req)
			if res.IsFail() {
				t.Fatalf("execute failed: %v", res.Problem())
			}
			if !res.Value().Emitted {
				continue
			}
			snapshotBytes, err := json.Marshal(res.Value().Snapshot)
			if err != nil {
				t.Fatalf("marshal snapshot: %v", err)
			}
			lastSnapshot = snapshotBytes

			if !res.Value().SignalEmitted {
				t.Fatal("expected spread signal emission")
			}
			signalBytes, err := json.Marshal(res.Value().SpreadSignal)
			if err != nil {
				t.Fatalf("marshal spread signal: %v", err)
			}
			lastSignal = signalBytes
		}
		if len(lastSnapshot) == 0 {
			t.Fatal("expected at least one emitted snapshot")
		}
		if len(lastSignal) == 0 {
			t.Fatal("expected at least one emitted signal")
		}
		return lastSnapshot, lastSignal
	}

	baseSnapshot, baseSignal := collect()
	for i := 0; i < 50; i++ {
		nextSnapshot, nextSignal := collect()
		if !bytes.Equal(baseSnapshot, nextSnapshot) {
			t.Fatalf("snapshot bytes changed at run %d\nbase=%s\nnext=%s", i, string(baseSnapshot), string(nextSnapshot))
		}
		if !bytes.Equal(baseSignal, nextSignal) {
			t.Fatalf("signal bytes changed at run %d\nbase=%s\nnext=%s", i, string(baseSignal), string(nextSignal))
		}
	}
}
