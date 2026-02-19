package app_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/market-raccoon/internal/core/aggregation/app"
	"github.com/market-raccoon/internal/core/aggregation/domain"
	"github.com/market-raccoon/internal/shared/clock"
	"github.com/market-raccoon/internal/shared/problem"
)

type fakeStatsStore struct {
	events  []domain.StatsWindowClosed
	saveErr *problem.Problem
}

func (f *fakeStatsStore) SaveStats(_ context.Context, evt domain.StatsWindowClosed) *problem.Problem {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.events = append(f.events, evt)
	return nil
}

func newStatsUC(maxWindows int) (*app.BuildStatsFromEvents, *fakePublisher, *fakeStatsStore) {
	pub := &fakePublisher{}
	store := &fakeStatsStore{}
	uc := app.NewBuildStatsFromEvents(pub, store, app.BuildStatsConfig{
		MaxWindows: maxWindows,
		WindowTTL:  time.Hour,
		Clock:      clock.NewFakeClock(time.Unix(0, 0)),
	})
	return uc, pub, store
}

func TestBuildStats_WindowClose_EmitsStatsClosed(t *testing.T) {
	uc, pub, store := newStatsUC(1_000)
	if _, p := uc.Execute(context.Background(), app.BuildStatsRequest{
		Venue: "binance", Instrument: "BTCUSDT", Kind: app.StatsInputMarkPrice, MarkPrice: 100, Seq: 1, TsIngest: 1,
	}); p != nil {
		t.Fatalf("Execute #1: %v", p)
	}
	resp, p := uc.Execute(context.Background(), app.BuildStatsRequest{
		Venue: "binance", Instrument: "BTCUSDT", Kind: app.StatsInputMarkPrice, MarkPrice: 101, Seq: 2, TsIngest: 60_001,
	})
	if p != nil {
		t.Fatalf("Execute #2: %v", p)
	}
	if len(resp.Closed) == 0 {
		t.Fatal("expected at least one closed stats window")
	}
	if resp.Closed[0].Stats.Timeframe != "1m" {
		t.Fatalf("closed timeframe=%s want=1m", resp.Closed[0].Stats.Timeframe)
	}
	if len(pub.stats) == 0 || len(store.events) == 0 {
		t.Fatalf("expected publish+store side effects, got pub=%d store=%d", len(pub.stats), len(store.events))
	}
}

func TestBuildStats_PartialInputsProducePartialStats(t *testing.T) {
	uc, _, _ := newStatsUC(1_000)
	if _, p := uc.Execute(context.Background(), app.BuildStatsRequest{
		Venue: "binance", Instrument: "BTCUSDT", Kind: app.StatsInputMarkPrice, MarkPrice: 100, Seq: 1, TsIngest: 1,
	}); p != nil {
		t.Fatalf("Execute #1: %v", p)
	}
	resp, p := uc.Execute(context.Background(), app.BuildStatsRequest{
		Venue: "binance", Instrument: "BTCUSDT", Kind: app.StatsInputMarkPrice, MarkPrice: 101, Seq: 2, TsIngest: 60_001,
	})
	if p != nil {
		t.Fatalf("Execute #2: %v", p)
	}
	if len(resp.Closed) == 0 {
		t.Fatal("expected closed stats window")
	}
	closed := resp.Closed[0].Stats
	if closed.FundingRateAvg != 0 || closed.FundingRateLast != 0 {
		t.Fatalf("expected zero funding fields on partial window, got avg=%v last=%v", closed.FundingRateAvg, closed.FundingRateLast)
	}
}

func TestBuildStats_Deterministic_SameInputSameOutput(t *testing.T) {
	uc1, _, _ := newStatsUC(1_000)
	uc2, _, _ := newStatsUC(1_000)
	sequence := []app.BuildStatsRequest{
		{Venue: "binance", Instrument: "BTCUSDT", Kind: app.StatsInputLiquidation, LiquidationSide: "buy", LiquidationQty: 1, Seq: 1, TsIngest: 1},
		{Venue: "binance", Instrument: "BTCUSDT", Kind: app.StatsInputMarkPrice, MarkPrice: 100.5, Seq: 2, TsIngest: 10_000},
		{Venue: "binance", Instrument: "BTCUSDT", Kind: app.StatsInputFundingRate, FundingRate: 0.0001, Seq: 3, TsIngest: 20_000},
		{Venue: "binance", Instrument: "BTCUSDT", Kind: app.StatsInputMarkPrice, MarkPrice: 101.0, Seq: 4, TsIngest: 60_001},
	}

	var left []domain.StatsWindowClosed
	var right []domain.StatsWindowClosed
	for i := range sequence {
		resp1, p := uc1.Execute(context.Background(), sequence[i])
		if p != nil {
			t.Fatalf("uc1 Execute[%d]: %v", i, p)
		}
		resp2, p := uc2.Execute(context.Background(), sequence[i])
		if p != nil {
			t.Fatalf("uc2 Execute[%d]: %v", i, p)
		}
		left = append(left, resp1.Closed...)
		right = append(right, resp2.Closed...)
	}
	if !reflect.DeepEqual(left, right) {
		t.Fatalf("non-deterministic closed stats output:\nleft=%+v\nright=%+v", left, right)
	}
}

func TestBuildStats_BoundedMap_EvictsOldest(t *testing.T) {
	uc, _, _ := newStatsUC(1)
	if _, p := uc.Execute(context.Background(), app.BuildStatsRequest{
		Venue: "binance", Instrument: "BTCUSDT", Kind: app.StatsInputMarkPrice, MarkPrice: 100, Seq: 1, TsIngest: 1,
	}); p != nil {
		t.Fatalf("Execute BTC: %v", p)
	}
	if _, p := uc.Execute(context.Background(), app.BuildStatsRequest{
		Venue: "binance", Instrument: "ETHUSDT", Kind: app.StatsInputMarkPrice, MarkPrice: 200, Seq: 1, TsIngest: 1,
	}); p != nil {
		t.Fatalf("Execute ETH: %v", p)
	}
	if got := uc.ActiveWindows(); got != 1 {
		t.Fatalf("active windows=%d want=1", got)
	}
}
