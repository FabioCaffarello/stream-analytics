package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/market-raccoon/internal/core/aggregation/app"
	"github.com/market-raccoon/internal/shared/clock"
	"github.com/market-raccoon/internal/shared/problem"
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
