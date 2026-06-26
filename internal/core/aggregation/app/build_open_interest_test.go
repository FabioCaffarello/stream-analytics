package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/app"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/clock"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

func newOpenInterestUC(maxStreams int) (*app.BuildOpenInterestFromEvents, *fakePublisher) {
	pub := &fakePublisher{}
	uc := app.NewBuildOpenInterestFromEvents(pub, nil, app.BuildOpenInterestConfig{
		MaxStreams: maxStreams,
		StreamTTL:  time.Hour,
		Clock:      clock.NewFakeClock(time.Unix(0, 0)),
	})
	return uc, pub
}

func newOpenInterestUCWithClock(maxStreams int, clk *clock.FakeClock) (*app.BuildOpenInterestFromEvents, *fakePublisher) {
	pub := &fakePublisher{}
	uc := app.NewBuildOpenInterestFromEvents(pub, nil, app.BuildOpenInterestConfig{
		MaxStreams: maxStreams,
		StreamTTL:  time.Hour,
		Clock:      clk,
	})
	return uc, pub
}

func TestBuildOpenInterestFromEvents_ExecuteAndDelta(t *testing.T) {
	uc, pub := newOpenInterestUC(16)

	left, p := uc.Execute(context.Background(), app.BuildOpenInterestRequest{
		Venue:        "binance",
		Instrument:   "BTCUSDT",
		OpenInterest: 100,
		Seq:          1,
		TsIngest:     1_000,
		Timestamp:    1_000,
	})
	if p != nil {
		t.Fatalf("first Execute: %v", p)
	}
	right, p := uc.Execute(context.Background(), app.BuildOpenInterestRequest{
		Venue:        "binance",
		Instrument:   "BTCUSDT",
		OpenInterest: 120,
		Seq:          2,
		TsIngest:     2_100,
		Timestamp:    2_000,
	})
	if p != nil {
		t.Fatalf("second Execute: %v", p)
	}

	if !left.HasEmission || !right.HasEmission {
		t.Fatal("expected emissions on both updates")
	}
	if got := right.Emitted.Window.Delta; got != 20 {
		t.Fatalf("delta=%f want=20", got)
	}
	if got := right.Emitted.Window.DeltaPct; got != 0.2 {
		t.Fatalf("delta_pct=%f want=0.2", got)
	}
	if got := uc.ActiveStreams(); got != 1 {
		t.Fatalf("active streams=%d want=1", got)
	}
	if len(pub.openInterest) != 2 {
		t.Fatalf("published oi=%d want=2", len(pub.openInterest))
	}
}

func TestBuildOpenInterestFromEvents_RejectsOutOfOrder(t *testing.T) {
	uc, _ := newOpenInterestUC(16)
	if _, p := uc.Execute(context.Background(), app.BuildOpenInterestRequest{
		Venue:        "binance",
		Instrument:   "BTCUSDT",
		OpenInterest: 100,
		Seq:          2,
		TsIngest:     2_000,
		Timestamp:    2_000,
	}); p != nil {
		t.Fatalf("seed Execute: %v", p)
	}
	_, p := uc.Execute(context.Background(), app.BuildOpenInterestRequest{
		Venue:        "binance",
		Instrument:   "BTCUSDT",
		OpenInterest: 90,
		Seq:          1,
		TsIngest:     2_100,
		Timestamp:    2_100,
	})
	if p == nil || p.Code != problem.OutOfOrder {
		t.Fatalf("expected out_of_order, got=%v", p)
	}
}

func TestBuildOpenInterestFromEvents_BoundednessByStreamCap(t *testing.T) {
	uc, _ := newOpenInterestUC(2)
	reqs := []app.BuildOpenInterestRequest{
		{Venue: "binance", Instrument: "BTCUSDT", OpenInterest: 100, Seq: 1, TsIngest: 1_000, Timestamp: 1_000},
		{Venue: "binance", Instrument: "ETHUSDT", OpenInterest: 50, Seq: 1, TsIngest: 1_100, Timestamp: 1_100},
		{Venue: "binance", Instrument: "SOLUSDT", OpenInterest: 20, Seq: 1, TsIngest: 1_200, Timestamp: 1_200},
	}
	for i, req := range reqs {
		if _, p := uc.Execute(context.Background(), req); p != nil {
			t.Fatalf("Execute #%d: %v", i+1, p)
		}
		if got := uc.ActiveStreams(); got > 2 {
			t.Fatalf("active streams=%d exceed cap", got)
		}
	}
}

func TestBuildOpenInterestFromEvents_DeterministicReplay(t *testing.T) {
	left, _ := newOpenInterestUC(16)
	right, _ := newOpenInterestUC(16)
	events := []app.BuildOpenInterestRequest{
		{Venue: "binance", Instrument: "BTCUSDT", OpenInterest: 100, Seq: 1, TsIngest: 1_000, Timestamp: 1_000},
		{Venue: "binance", Instrument: "ETHUSDT", OpenInterest: 80, Seq: 1, TsIngest: 1_050, Timestamp: 1_050},
		{Venue: "binance", Instrument: "BTCUSDT", OpenInterest: 110, Seq: 2, TsIngest: 2_000, Timestamp: 2_000},
		{Venue: "binance", Instrument: "ETHUSDT", OpenInterest: 76, Seq: 2, TsIngest: 2_050, Timestamp: 2_050},
	}

	collect := func(uc *app.BuildOpenInterestFromEvents) []app.BuildOpenInterestResponse {
		out := make([]app.BuildOpenInterestResponse, 0, len(events))
		for i, req := range events {
			resp, p := uc.Execute(context.Background(), req)
			if p != nil {
				t.Fatalf("Execute #%d: %v", i+1, p)
			}
			out = append(out, resp)
		}
		return out
	}

	lhs := collect(left)
	rhs := collect(right)
	if len(lhs) != len(rhs) {
		t.Fatalf("response len mismatch left=%d right=%d", len(lhs), len(rhs))
	}
	for i := range lhs {
		l := lhs[i].Emitted.Window
		r := rhs[i].Emitted.Window
		if l != r {
			t.Fatalf("non-deterministic emission at idx=%d left=%+v right=%+v", i, l, r)
		}
	}
}

func TestOpenInterestCadenceComputation(t *testing.T) {
	// Arrivals every 2 seconds => cadence ~2000ms => HIGH confidence.
	clk := clock.NewFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	uc, _ := newOpenInterestUCWithClock(16, clk)

	for i := 1; i <= 5; i++ {
		clk.Advance(2 * time.Second)
		resp, p := uc.Execute(context.Background(), app.BuildOpenInterestRequest{
			Venue:        "binance",
			Instrument:   "BTCUSDT",
			OpenInterest: float64(100 + i),
			Seq:          int64(i),
			TsIngest:     int64(i * 1000),
			Timestamp:    int64(i * 1000),
		})
		if p != nil {
			t.Fatalf("Execute #%d: %v", i, p)
		}
		if i >= 2 {
			w := resp.Emitted.Window
			if w.CadenceHintMs != 2000 {
				t.Fatalf("Execute #%d: cadence=%d want=2000", i, w.CadenceHintMs)
			}
			if w.Confidence != "high" {
				t.Fatalf("Execute #%d: confidence=%q want=high", i, w.Confidence)
			}
		}
	}
}

func TestOpenInterestConfidenceDerivation(t *testing.T) {
	tests := []struct {
		name       string
		interval   time.Duration
		wantConf   string
		minCadence int64
		maxCadence int64
	}{
		{"high_1s", 1 * time.Second, "high", 1000, 1000},
		{"high_4s", 4 * time.Second, "high", 4000, 4000},
		{"medium_10s", 10 * time.Second, "medium", 10000, 10000},
		{"medium_25s", 25 * time.Second, "medium", 25000, 25000},
		{"low_60s", 60 * time.Second, "low", 60000, 60000},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clk := clock.NewFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
			uc, _ := newOpenInterestUCWithClock(16, clk)

			var lastResp app.BuildOpenInterestResponse
			for i := 1; i <= 4; i++ {
				clk.Advance(tc.interval)
				resp, p := uc.Execute(context.Background(), app.BuildOpenInterestRequest{
					Venue:        "binance",
					Instrument:   "BTCUSDT",
					OpenInterest: float64(100 + i),
					Seq:          int64(i),
					TsIngest:     int64(i * 1000),
					Timestamp:    int64(i * 1000),
				})
				if p != nil {
					t.Fatalf("Execute #%d: %v", i, p)
				}
				lastResp = resp
			}

			w := lastResp.Emitted.Window
			if w.Confidence != tc.wantConf {
				t.Fatalf("confidence=%q want=%q (cadence=%d)", w.Confidence, tc.wantConf, w.CadenceHintMs)
			}
			if w.CadenceHintMs < tc.minCadence || w.CadenceHintMs > tc.maxCadence {
				t.Fatalf("cadence=%d outside [%d, %d]", w.CadenceHintMs, tc.minCadence, tc.maxCadence)
			}
		})
	}
}

func TestOpenInterestCadenceWithSparseUpdates(t *testing.T) {
	// Arrivals every 45 seconds => cadence 45000ms => LOW confidence.
	clk := clock.NewFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	uc, _ := newOpenInterestUCWithClock(16, clk)

	for i := 1; i <= 5; i++ {
		clk.Advance(45 * time.Second)
		resp, p := uc.Execute(context.Background(), app.BuildOpenInterestRequest{
			Venue:        "bybit",
			Instrument:   "ETHUSDT",
			OpenInterest: float64(500 + i*10),
			Seq:          int64(i),
			TsIngest:     int64(i * 45000),
			Timestamp:    int64(i * 45000),
		})
		if p != nil {
			t.Fatalf("Execute #%d: %v", i, p)
		}
		if i >= 2 {
			w := resp.Emitted.Window
			if w.CadenceHintMs != 45000 {
				t.Fatalf("Execute #%d: cadence=%d want=45000", i, w.CadenceHintMs)
			}
			if w.Confidence != "low" {
				t.Fatalf("Execute #%d: confidence=%q want=low", i, w.Confidence)
			}
		}
	}

	// First event should have no cadence (only 1 arrival).
	clk2 := clock.NewFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	uc2, _ := newOpenInterestUCWithClock(16, clk2)
	clk2.Advance(time.Second)
	resp, p := uc2.Execute(context.Background(), app.BuildOpenInterestRequest{
		Venue:        "bybit",
		Instrument:   "ETHUSDT",
		OpenInterest: 500,
		Seq:          1,
		TsIngest:     1000,
		Timestamp:    1000,
	})
	if p != nil {
		t.Fatalf("single Execute: %v", p)
	}
	if resp.Emitted.Window.CadenceHintMs != 0 {
		t.Fatalf("single arrival cadence=%d want=0", resp.Emitted.Window.CadenceHintMs)
	}
	if resp.Emitted.Window.Confidence != "" {
		t.Fatalf("single arrival confidence=%q want empty", resp.Emitted.Window.Confidence)
	}
}
