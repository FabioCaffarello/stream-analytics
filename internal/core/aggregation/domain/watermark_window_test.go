package domain_test

import (
	"testing"

	"github.com/market-raccoon/internal/core/aggregation/domain"
	"github.com/market-raccoon/internal/shared/problem"
)

func newWindowManager(t *testing.T, cap int) *domain.WatermarkWindowManager {
	t.Helper()
	mgr, p := domain.NewWatermarkWindowManager(domain.WatermarkWindowConfig{
		MaxOpenWindows:  cap,
		LateToleranceMs: 30_000,
	})
	if p != nil {
		t.Fatalf("NewWatermarkWindowManager: %v", p)
	}
	return mgr
}

func TestWatermarkWindow_AdvanceClosesCurrent(t *testing.T) {
	mgr := newWindowManager(t, 96)
	key := domain.WindowKey{Venue: "binance", Instrument: "BTCUSDT", Timeframe: "1m"}

	first, p := mgr.Observe(key, 60_000, 60_000)
	if p != nil {
		t.Fatalf("Observe first: %v", p)
	}
	if first.ShouldCloseCurrent {
		t.Fatal("first window should not close current")
	}

	next, p := mgr.Observe(key, 120_000, 60_000)
	if p != nil {
		t.Fatalf("Observe second: %v", p)
	}
	if !next.ShouldCloseCurrent {
		t.Fatal("expected close on window advance")
	}
	if next.PreviousWindowStart != 60_000 {
		t.Fatalf("previous window start=%d want=60000", next.PreviousWindowStart)
	}
}

func TestWatermarkWindow_LateArrival(t *testing.T) {
	mgr := newWindowManager(t, 96)
	key := domain.WindowKey{Venue: "binance", Instrument: "BTCUSDT", Timeframe: "1m"}

	if _, p := mgr.Observe(key, 120_000, 60_000); p != nil {
		t.Fatalf("Observe first: %v", p)
	}
	late, p := mgr.Observe(key, 60_000, 60_000)
	if p != nil {
		t.Fatalf("Observe late: %v", p)
	}
	if !late.IsLate {
		t.Fatal("expected late arrival flag")
	}
	if late.ShouldCloseCurrent {
		t.Fatal("late arrival must not close current window")
	}
}

func TestWatermarkWindow_ForcedClose(t *testing.T) {
	mgr := newWindowManager(t, 96)
	for i := 0; i < 97; i++ {
		key := domain.WindowKey{
			Venue:      "binance",
			Instrument: "BTCUSDT-" + string(rune('A'+(i%26))) + string(rune('A'+((i/26)%26))),
			Timeframe:  "1m",
		}
		decision, p := mgr.Observe(key, int64(60_000+i), 60_000)
		if p != nil {
			t.Fatalf("Observe[%d]: %v", i, p)
		}
		if i < 96 && decision.ForcedClose != nil {
			t.Fatalf("unexpected forced close at index %d", i)
		}
		if i == 96 && decision.ForcedClose == nil {
			t.Fatal("expected forced close on 97th window")
		}
	}
	if got := mgr.ActiveWindows(); got != 96 {
		t.Fatalf("active windows=%d want=96", got)
	}
}

func TestWatermarkWindow_InvalidInput(t *testing.T) {
	mgr := newWindowManager(t, 96)
	_, p := mgr.Observe(domain.WindowKey{}, -1, 0)
	if p == nil {
		t.Fatal("expected validation error")
	}
	if p.Code != problem.ValidationFailed {
		t.Fatalf("code=%s want=%s", p.Code, problem.ValidationFailed)
	}
}
