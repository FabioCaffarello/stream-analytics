package domain_test

import (
	"testing"

	"github.com/market-raccoon/internal/core/delivery/domain"
)

func TestShouldDropOnBackpressure_dropNewest(t *testing.T) {
	if got := domain.ShouldDropOnBackpressure(domain.BackpressureDropNewest, 0, 1); got {
		t.Fatal("queue below cap should not drop")
	}
	if got := domain.ShouldDropOnBackpressure(domain.BackpressureDropNewest, 1, 1); !got {
		t.Fatal("queue at cap should drop")
	}
}

func TestShouldDropOnBackpressure_dropOldest(t *testing.T) {
	if got := domain.ShouldDropOnBackpressure(domain.BackpressureDropOldest, 0, 1); got {
		t.Fatal("queue below cap should not drop")
	}
	if got := domain.ShouldDropOnBackpressure(domain.BackpressureDropOldest, 1, 1); got {
		t.Fatal("drop_oldest should not reject new message")
	}
}

func TestShouldDropOnBackpressure_priorityDrop(t *testing.T) {
	if got := domain.ShouldDropOnBackpressure(domain.BackpressurePriorityDrop, 0, 1); got {
		t.Fatal("queue below cap should not drop")
	}
	if got := domain.ShouldDropOnBackpressure(domain.BackpressurePriorityDrop, 1, 1); got {
		t.Fatal("priority_drop should decide eviction in session queue logic")
	}
}

func TestNormalizeBackpressurePolicy_unknownDefaultsToDropNewest(t *testing.T) {
	p := domain.NormalizeBackpressurePolicy(domain.BackpressurePolicy("UNKNOWN_POLICY"))
	if p != domain.BackpressureDropNewest {
		t.Fatalf("policy=%q want=%q", p, domain.BackpressureDropNewest)
	}
}

func TestNormalizeBackpressurePolicy_knownValues(t *testing.T) {
	tests := []struct {
		raw  domain.BackpressurePolicy
		want domain.BackpressurePolicy
	}{
		{raw: "drop_newest", want: domain.BackpressureDropNewest},
		{raw: "DROP_OLDEST", want: domain.BackpressureDropOldest},
		{raw: " priority_drop ", want: domain.BackpressurePriorityDrop},
	}
	for _, tc := range tests {
		if got := domain.NormalizeBackpressurePolicy(tc.raw); got != tc.want {
			t.Fatalf("raw=%q got=%q want=%q", tc.raw, got, tc.want)
		}
	}
}

func TestDefaultBackpressurePriorities_HasCriticalTypes(t *testing.T) {
	priorities := domain.DefaultBackpressurePriorities()
	if priorities["marketdata.trade"] <= priorities["aggregation.candle"] {
		t.Fatal("trade priority must be greater than candle")
	}
	if priorities["aggregation.candle"] <= priorities["insights.crossvenue.spread_signal"] {
		t.Fatal("candle priority must be greater than insights spread signal")
	}
	if priorities["aggregation.oi"] <= 0 || priorities["aggregation.cvd"] <= 0 || priorities["aggregation.delta_volume"] <= 0 || priorities["aggregation.bar_stats"] <= 0 {
		t.Fatal("analytics primitive priorities must be present and positive")
	}
	if priorities["signal.event"] <= priorities["signal.composite"] {
		t.Fatal("canonical signal.event priority must be greater than legacy signal.composite")
	}
	if priorities["strategy.intent"] <= priorities["execution.event"] {
		t.Fatal("strategy.intent priority must be greater than execution.event")
	}
	if priorities["execution.event"] <= priorities["portfolio.state"] {
		t.Fatal("execution.event priority must be greater than portfolio.state")
	}
}
