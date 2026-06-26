package domain_test

import (
	"fmt"
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/core/evidence/domain"
)

func TestRegimeStore_RegimeHistoryCapDropsOldest(t *testing.T) {
	policy, p := domain.NewRegimeStorePolicy(16, 20)
	if p != nil {
		t.Fatalf("policy validation failed: %v", p)
	}
	store := domain.NewRegimeStore(policy)
	key := domain.RegimeStoreKey{Venue: "binance", Instrument: "BTCUSDT", Timeframe: "1m"}

	for i := 1; i <= 21; i++ {
		signal := domain.RegimeSignal{
			Venue:       "binance",
			Instrument:  "BTCUSDT",
			Timeframe:   "1m",
			Kind:        domain.RegimeTrending,
			Strength:    0.8,
			Confidence:  0.9,
			WindowStart: int64(i) * 60_000,
			WindowEnd:   int64(i+1) * 60_000,
			Features: []domain.FeaturePair{
				{Name: "slope", Value: 0.3},
				{Name: "seq", Value: float64(i)},
			},
		}
		if p := store.PutRegime(key, signal); p != nil {
			t.Fatalf("put regime %d failed: %v", i, p)
		}
	}

	history := store.RegimeHistory(key)
	if got := len(history); got != 20 {
		t.Fatalf("history len=%d want=20", got)
	}
	if got := history[0].WindowStart; got != 2*60_000 {
		t.Fatalf("oldest window_start=%d want=%d", got, 2*60_000)
	}
	if got := history[len(history)-1].WindowStart; got != 21*60_000 {
		t.Fatalf("newest window_start=%d want=%d", got, 21*60_000)
	}
	if got := fmt.Sprintf("%0.0f", history[len(history)-1].Features[1].Value); got != "21" {
		t.Fatalf("newest sequence marker=%s want=21", got)
	}
}
