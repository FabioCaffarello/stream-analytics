package app_test

import (
	"context"
	"reflect"
	"slices"
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

func TestBuildStats_MixedInputs_CloseAllTimeframes_CrossSourceConsistency(t *testing.T) {
	uc, _, _ := newStatsUC(1_000)

	events := []app.BuildStatsRequest{
		{
			Venue:           "binance",
			Instrument:      "BTCUSDT",
			Kind:            app.StatsInputLiquidation,
			Seq:             1,
			TsIngest:        1,
			LiquidationSide: "buy",
			LiquidationQty:  2.5,
		},
		{
			Venue:      "binance",
			Instrument: "BTCUSDT",
			Kind:       app.StatsInputMarkPrice,
			Seq:        2,
			TsIngest:   10_000,
			MarkPrice:  100.0,
		},
		{
			Venue:       "binance",
			Instrument:  "BTCUSDT",
			Kind:        app.StatsInputFundingRate,
			Seq:         3,
			TsIngest:    20_000,
			FundingRate: 0.0002,
		},
		{
			// Rolls all open windows (1m/5m/15m/30m/1h/4h/1d).
			Venue:      "binance",
			Instrument: "BTCUSDT",
			Kind:       app.StatsInputMarkPrice,
			Seq:        4,
			TsIngest:   86_400_001,
			MarkPrice:  101.0,
		},
	}

	resp, p := executeStatsSequence(uc, events)
	if p != nil {
		t.Fatalf("Execute sequence: %v", p)
	}
	if got, want := len(resp.Closed), len(domain.AllowedStatsTimeframes); got != want {
		t.Fatalf("closed windows=%d want=%d", got, want)
	}

	gotTFs := assertStatsCrossSourceClosed(t, resp.Closed)
	slices.Sort(gotTFs)
	wantTFs := append([]string(nil), domain.AllowedStatsTimeframes...)
	slices.Sort(wantTFs)
	if !reflect.DeepEqual(gotTFs, wantTFs) {
		t.Fatalf("timeframes mismatch: got=%v want=%v", gotTFs, wantTFs)
	}
}

func executeStatsSequence(uc *app.BuildStatsFromEvents, events []app.BuildStatsRequest) (app.BuildStatsResponse, *problem.Problem) {
	for i := 0; i < len(events)-1; i++ {
		if _, p := uc.Execute(context.Background(), events[i]); p != nil {
			return app.BuildStatsResponse{}, p
		}
	}
	return uc.Execute(context.Background(), events[len(events)-1])
}

func assertStatsCrossSourceClosed(t *testing.T, closed []domain.StatsWindowClosed) []string {
	t.Helper()
	gotTFs := make([]string, 0, len(closed))
	for _, evt := range closed {
		s := evt.Stats
		gotTFs = append(gotTFs, s.Timeframe)
		assertStatsWindowValues(t, s)
	}
	return gotTFs
}

func assertStatsWindowValues(t *testing.T, s domain.StatsWindowV1) {
	t.Helper()
	if s.LiqCount != 1 {
		t.Fatalf("timeframe=%s liq_count=%d want=1", s.Timeframe, s.LiqCount)
	}
	if s.LiqBuyVolume != 2.5 || s.LiqSellVolume != 0 {
		t.Fatalf("timeframe=%s unexpected liq sides: buy=%f sell=%f", s.Timeframe, s.LiqBuyVolume, s.LiqSellVolume)
	}
	if s.LiqTotalVolume != s.LiqBuyVolume+s.LiqSellVolume {
		t.Fatalf("timeframe=%s liq_total mismatch: total=%f buy+sell=%f", s.Timeframe, s.LiqTotalVolume, s.LiqBuyVolume+s.LiqSellVolume)
	}
	if s.MarkPriceOpen != 100.0 || s.MarkPriceClose != 100.0 {
		t.Fatalf("timeframe=%s unexpected markprice open/close: open=%f close=%f", s.Timeframe, s.MarkPriceOpen, s.MarkPriceClose)
	}
	if s.FundingRateAvg != 0.0002 || s.FundingRateLast != 0.0002 {
		t.Fatalf("timeframe=%s unexpected funding: avg=%f last=%f", s.Timeframe, s.FundingRateAvg, s.FundingRateLast)
	}
	if !s.IsClosed {
		t.Fatalf("timeframe=%s expected closed window", s.Timeframe)
	}
}
