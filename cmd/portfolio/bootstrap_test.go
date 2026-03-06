package main

import "testing"

func TestEffectivePortfolioFilters_NarrowsToExecutionEvent(t *testing.T) {
	filters := effectivePortfolioFilters([]string{
		"execution.>",
		"execution.event.>",
		"execution.event.v1.binance.BTCUSDT",
	})
	if len(filters) != 2 {
		t.Fatalf("filters=%v want 2 canonical entries", filters)
	}
	for _, filter := range filters {
		if filter != "execution.event.>" && filter != "execution.event.v1.binance.BTCUSDT" {
			t.Fatalf("unexpected filter retained: %q", filter)
		}
	}

	filters = effectivePortfolioFilters(nil)
	if len(filters) != 1 || filters[0] != "execution.event.>" {
		t.Fatalf("filters=%v want=[execution.event.>]", filters)
	}
}
