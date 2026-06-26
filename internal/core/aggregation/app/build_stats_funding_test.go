package app_test

import (
	"context"
	"math"
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/app"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

func TestBuildStats_FundingRate_FlowsToWindow(t *testing.T) {
	uc, _, _ := newStatsUC(1_000)
	resp, p := uc.Execute(context.Background(), app.BuildStatsRequest{
		Venue:       "binance",
		Instrument:  "BTCUSDT",
		Kind:        app.StatsInputFundingRate,
		FundingRate: 0.0002,
		Seq:         1,
		TsIngest:    1,
	})
	if p != nil {
		t.Fatalf("Execute: %v", p)
	}
	if resp.ActiveWindows == 0 {
		t.Fatal("expected at least one active window")
	}
}

func TestBuildStats_FundingRate_WindowClose_EmitsCorrectValues(t *testing.T) {
	uc, _, _ := newStatsUC(1_000)
	// Send funding rate in first 1m window.
	if _, p := uc.Execute(context.Background(), app.BuildStatsRequest{
		Venue:       "binance",
		Instrument:  "BTCUSDT",
		Kind:        app.StatsInputFundingRate,
		FundingRate: 0.0003,
		Seq:         1,
		TsIngest:    1,
	}); p != nil {
		t.Fatalf("Execute #1: %v", p)
	}
	// Send second funding rate in same window.
	if _, p := uc.Execute(context.Background(), app.BuildStatsRequest{
		Venue:       "binance",
		Instrument:  "BTCUSDT",
		Kind:        app.StatsInputFundingRate,
		FundingRate: -0.0001,
		Seq:         2,
		TsIngest:    30_000,
	}); p != nil {
		t.Fatalf("Execute #2: %v", p)
	}
	// Roll the 1m window.
	resp, p := uc.Execute(context.Background(), app.BuildStatsRequest{
		Venue:       "binance",
		Instrument:  "BTCUSDT",
		Kind:        app.StatsInputFundingRate,
		FundingRate: 0.0001,
		Seq:         3,
		TsIngest:    60_001,
	})
	if p != nil {
		t.Fatalf("Execute #3: %v", p)
	}
	if len(resp.Closed) == 0 {
		t.Fatal("expected at least one closed window")
	}
	// Find the 1m closed window.
	for _, evt := range resp.Closed {
		if evt.Stats.Timeframe != "1m" {
			continue
		}
		s := evt.Stats
		// avg of 0.0003 and -0.0001 = 0.0001
		expectedAvg := 0.0001
		tolerance := 1e-9
		if math.Abs(s.FundingRateAvg-expectedAvg) > tolerance {
			t.Fatalf("1m avg=%v want≈%v", s.FundingRateAvg, expectedAvg)
		}
		if math.Abs(s.FundingRateLast-(-0.0001)) > tolerance {
			t.Fatalf("1m last=%v want≈-0.0001", s.FundingRateLast)
		}
		return
	}
	t.Fatal("no 1m closed window found")
}

func TestBuildStats_FundingRate_ValidationRejects_InvalidRate(t *testing.T) {
	uc, _, _ := newStatsUC(1_000)
	_, p := uc.Execute(context.Background(), app.BuildStatsRequest{
		Venue:       "binance",
		Instrument:  "BTCUSDT",
		Kind:        app.StatsInputFundingRate,
		FundingRate: math.NaN(),
		Seq:         1,
		TsIngest:    1,
	})
	if p == nil || p.Code != problem.ValidationFailed {
		t.Fatalf("expected ValidationFailed for NaN funding rate, got=%v", p)
	}
}
