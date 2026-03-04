package app

import (
	"testing"

	"github.com/market-raccoon/internal/core/evidence/domain"
)

func TestSweepRule_DetectsFiveLevelSweep(t *testing.T) {
	rule := NewSweepRule(DefaultRuleConfig())
	stream := domain.RuleEvent{
		Kind:     domain.EventKindBook,
		Venue:    "binance",
		Symbol:   "BTCUSDT",
		StreamID: "binance/BTCUSDT/book_delta",
	}

	// Baseline
	rule.OnEvent(withBook(stream, 1, 10_000, 20, 20, 500, 500))

	// Bid side loses 6 levels and 50% depth.
	events := rule.OnEvent(withBook(stream, 2, 16_000, 14, 20, 250, 500))
	if len(events) != 1 {
		t.Fatalf("events=%d want=1", len(events))
	}
	ev := events[0]
	if ev.Type != domain.Sweep {
		t.Fatalf("kind=%s want=%s", ev.Type, domain.Sweep)
	}
	if featureValue(ev.Features, "level_drop") < 5 {
		t.Fatalf("level_drop=%f want>=5", featureValue(ev.Features, "level_drop"))
	}
}

func TestSweepRule_NoEmissionBelowThreshold(t *testing.T) {
	rule := NewSweepRule(DefaultRuleConfig())
	stream := domain.RuleEvent{
		Kind:     domain.EventKindBook,
		Venue:    "binance",
		Symbol:   "ETHUSDT",
		StreamID: "binance/ETHUSDT/book_delta",
	}
	rule.OnEvent(withBook(stream, 1, 10_000, 20, 20, 500, 500))

	// Only 4 levels dropped, should not emit.
	events := rule.OnEvent(withBook(stream, 2, 11_000, 16, 20, 250, 500))
	if len(events) != 0 {
		t.Fatalf("events=%d want=0", len(events))
	}
}

func withBook(base domain.RuleEvent, seq, ts int64, bidLevels, askLevels int, bidDepth, askDepth float64) domain.RuleEvent {
	base.Seq = seq
	base.TsServer = ts
	base.BidLevels = bidLevels
	base.AskLevels = askLevels
	base.BidDepth = bidDepth
	base.AskDepth = askDepth
	return base
}

func featureValue(features []domain.EvidenceFeature, key string) float64 {
	for i := range features {
		if features[i].Key == key {
			return features[i].Value
		}
	}
	return 0
}
