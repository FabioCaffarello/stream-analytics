package app_test

import (
	"context"
	"math"
	"sort"
	"testing"
	"time"

	"github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/app"
	"github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/domain"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/clock"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

type fakeTapeStore struct {
	events  []domain.TapeClosed
	saveErr *problem.Problem
}

func (f *fakeTapeStore) SaveTape(_ context.Context, evt domain.TapeClosed) *problem.Problem {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.events = append(f.events, evt)
	return nil
}

func newTapeUC(maxWindows int) (*app.BuildTapeFromTrades, *fakePublisher, *fakeTapeStore) {
	pub := &fakePublisher{}
	store := &fakeTapeStore{}
	uc := app.NewBuildTapeFromTrades(pub, store, nil, nil, nil, app.BuildTapeConfig{
		MaxWindows: maxWindows,
		WindowCap:  96,
		WindowTTL:  time.Hour,
		Clock:      clock.NewFakeClock(time.Unix(0, 0)),
	})
	return uc, pub, store
}

func TestBuildTapeFromTrades_Execute_BasicFlow(t *testing.T) {
	uc, pub, store := newTapeUC(1_000)

	for i := int64(1); i <= 10; i++ {
		if _, p := uc.Execute(context.Background(), app.BuildTapeRequest{
			Venue:      "binance",
			Instrument: "BTCUSDT",
			Price:      100 + float64(i),
			Quantity:   1,
			IsBuy:      i%2 == 0,
			Seq:        i,
			TsIngest:   1_000,
		}); p != nil {
			t.Fatalf("Execute #%d: %v", i, p)
		}
	}
	resp, p := uc.Execute(context.Background(), app.BuildTapeRequest{
		Venue:      "binance",
		Instrument: "BTCUSDT",
		Price:      120,
		Quantity:   1,
		IsBuy:      true,
		Seq:        11,
		TsIngest:   6_000,
	})
	if p != nil {
		t.Fatalf("Execute flush: %v", p)
	}
	if len(resp.ClosedWindows) == 0 {
		t.Fatal("expected closed tape windows")
	}
	if len(pub.tapes) == 0 || len(store.events) == 0 {
		t.Fatalf("expected publisher/store writes, got publisher=%d store=%d", len(pub.tapes), len(store.events))
	}

	seen := map[string]bool{}
	for _, evt := range pub.tapes {
		seen[evt.Window.Timeframe] = true
	}
	for _, tf := range []string{"250ms", "1s", "5s"} {
		if !seen[tf] {
			t.Fatalf("expected closed timeframe %s in publisher output", tf)
		}
	}
}

func TestBuildTapeFromTrades_PublishesDerivedAnalyticsPerClosedWindow(t *testing.T) {
	uc, pub, _ := newTapeUC(1_000)
	for i := int64(1); i <= 12; i++ {
		if _, p := uc.Execute(context.Background(), app.BuildTapeRequest{
			Venue:      "binance",
			Instrument: "BTCUSDT",
			Price:      100 + float64(i),
			Quantity:   1,
			IsBuy:      i%2 == 0,
			Seq:        i,
			TsIngest:   1_000,
		}); p != nil {
			t.Fatalf("Execute #%d: %v", i, p)
		}
	}
	if _, p := uc.Execute(context.Background(), app.BuildTapeRequest{
		Venue:      "binance",
		Instrument: "BTCUSDT",
		Price:      115,
		Quantity:   1,
		IsBuy:      true,
		Seq:        13,
		TsIngest:   6_000,
	}); p != nil {
		t.Fatalf("Execute flush: %v", p)
	}

	if len(pub.tapes) == 0 {
		t.Fatal("expected tape windows")
	}
	if len(pub.deltaVolume) != len(pub.tapes) {
		t.Fatalf("delta windows=%d want=%d", len(pub.deltaVolume), len(pub.tapes))
	}
	if len(pub.cvd) != len(pub.tapes) {
		t.Fatalf("cvd windows=%d want=%d", len(pub.cvd), len(pub.tapes))
	}
	if len(pub.barStats) != len(pub.tapes) {
		t.Fatalf("bar_stats windows=%d want=%d", len(pub.barStats), len(pub.tapes))
	}
}

func TestBuildTapeFromTrades_WindowBoundary(t *testing.T) {
	uc, _, _ := newTapeUC(1_000)
	if _, p := uc.Execute(context.Background(), app.BuildTapeRequest{
		Venue: "binance", Instrument: "BTCUSDT", Price: 100, Quantity: 1, IsBuy: true, Seq: 1, TsIngest: 999,
	}); p != nil {
		t.Fatalf("Execute #1: %v", p)
	}
	resp, p := uc.Execute(context.Background(), app.BuildTapeRequest{
		Venue: "binance", Instrument: "BTCUSDT", Price: 101, Quantity: 1, IsBuy: true, Seq: 2, TsIngest: 1_000,
	})
	if p != nil {
		t.Fatalf("Execute #2: %v", p)
	}
	if len(resp.ClosedWindows) == 0 {
		t.Fatal("expected boundary close for 250ms")
	}
	found := false
	for _, evt := range resp.ClosedWindows {
		if evt.Window.Timeframe == "250ms" && evt.Window.WindowStartTs == 750 {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected close event for 250ms window_start=750")
	}
}

func TestBuildTapeFromTrades_BurstFlag(t *testing.T) {
	uc, _, _ := newTapeUC(1_000)
	for i := int64(1); i <= 26; i++ {
		if _, p := uc.Execute(context.Background(), app.BuildTapeRequest{
			Venue: "binance", Instrument: "BTCUSDT", Price: 100, Quantity: 1, IsBuy: true, Seq: i, TsIngest: 1_000,
		}); p != nil {
			t.Fatalf("Execute #%d: %v", i, p)
		}
	}
	resp, p := uc.Execute(context.Background(), app.BuildTapeRequest{
		Venue: "binance", Instrument: "BTCUSDT", Price: 100, Quantity: 1, IsBuy: true, Seq: 27, TsIngest: 1_500,
	})
	if p != nil {
		t.Fatalf("Execute close: %v", p)
	}
	burst := false
	for _, evt := range resp.ClosedWindows {
		if evt.Window.Timeframe == "250ms" && evt.IsBurst {
			burst = true
			break
		}
	}
	if !burst {
		t.Fatal("expected 250ms burst window")
	}
}

func TestBuildTapeFromTrades_DeterministicReplay(t *testing.T) {
	type tr struct {
		price float64
		size  float64
		buy   bool
		seq   int64
	}
	trades := []tr{
		{100, 1, true, 1},
		{101, 0.5, false, 2},
		{99.5, 2, true, 3},
		{100.2, 1.2, false, 4},
	}

	left, _, _ := newTapeUC(1_000)
	for _, tr := range trades {
		if _, p := left.Execute(context.Background(), app.BuildTapeRequest{
			Venue: "binance", Instrument: "BTCUSDT", Price: tr.price, Quantity: tr.size, IsBuy: tr.buy, Seq: tr.seq, TsIngest: 1_000,
		}); p != nil {
			t.Fatalf("left Execute trade: %v", p)
		}
	}
	lResp, p := left.Execute(context.Background(), app.BuildTapeRequest{
		Venue: "binance", Instrument: "BTCUSDT", Price: 100, Quantity: 1, IsBuy: true, Seq: 10, TsIngest: 2_000,
	})
	if p != nil {
		t.Fatalf("left Execute flush: %v", p)
	}

	right, _, _ := newTapeUC(1_000)
	for i := len(trades) - 1; i >= 0; i-- {
		tr := trades[i]
		if _, p := right.Execute(context.Background(), app.BuildTapeRequest{
			Venue: "binance", Instrument: "BTCUSDT", Price: tr.price, Quantity: tr.size, IsBuy: tr.buy, Seq: tr.seq, TsIngest: 1_000,
		}); p != nil {
			t.Fatalf("right Execute trade: %v", p)
		}
	}
	rResp, p := right.Execute(context.Background(), app.BuildTapeRequest{
		Venue: "binance", Instrument: "BTCUSDT", Price: 100, Quantity: 1, IsBuy: true, Seq: 10, TsIngest: 2_000,
	})
	if p != nil {
		t.Fatalf("right Execute flush: %v", p)
	}

	assertTapeCloseEventsEqual(t, lResp.ClosedWindows, rResp.ClosedWindows)
}

func assertTapeCloseEventsEqual(t *testing.T, left, right []app.TapeCloseEvent) {
	t.Helper()
	if len(left) != len(right) {
		t.Fatalf("len mismatch left=%d right=%d", len(left), len(right))
	}
	sort.Slice(left, func(i, j int) bool {
		if left[i].Window.Timeframe != left[j].Window.Timeframe {
			return left[i].Window.Timeframe < left[j].Window.Timeframe
		}
		return left[i].Window.WindowStartTs < left[j].Window.WindowStartTs
	})
	sort.Slice(right, func(i, j int) bool {
		if right[i].Window.Timeframe != right[j].Window.Timeframe {
			return right[i].Window.Timeframe < right[j].Window.Timeframe
		}
		return right[i].Window.WindowStartTs < right[j].Window.WindowStartTs
	})
	for i := range left {
		l := left[i]
		r := right[i]
		if l.Window.Timeframe != r.Window.Timeframe || l.Window.WindowStartTs != r.Window.WindowStartTs {
			t.Fatalf("window key mismatch l=%+v r=%+v", l.Window, r.Window)
		}
		if l.IsBurst != r.IsBurst {
			t.Fatalf("burst mismatch l=%v r=%v", l.IsBurst, r.IsBurst)
		}
		if l.Window.TradeCount != r.Window.TradeCount || l.Window.BuyCount != r.Window.BuyCount || l.Window.SellCount != r.Window.SellCount {
			t.Fatalf("count mismatch l=%+v r=%+v", l.Window, r.Window)
		}
		for _, v := range []struct {
			name string
			lv   float64
			rv   float64
		}{
			{"vwap", l.Window.VwapPrice, r.Window.VwapPrice},
			{"rate", l.Window.Rate(), r.Window.Rate()},
			{"imbalance", l.Window.Imbalance(), r.Window.Imbalance()},
			{"volume", l.Window.TotalVolume, r.Window.TotalVolume},
		} {
			if math.Abs(v.lv-v.rv) > 1e-9 {
				t.Fatalf("%s mismatch l=%f r=%f", v.name, v.lv, v.rv)
			}
		}
	}
}
