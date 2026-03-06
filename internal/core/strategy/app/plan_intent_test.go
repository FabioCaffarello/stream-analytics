package app

import (
	"testing"

	strategydomain "github.com/market-raccoon/internal/core/strategy/domain"
)

func TestIntentPlanner_PlanFromSignalEvent(t *testing.T) {
	planner := NewIntentPlanner(DefaultPlannerConfig())
	intent, ok := planner.Plan(IntentInput{
		Kind:          "liquidity_thinning",
		Venue:         "binance",
		Instrument:    "BTCUSDT",
		SignalID:      "sig-1",
		CorrelationID: "corr-1",
		TraceID:       "trace-1",
		Reason:        "thin book",
		Confidence:    0.8,
		TsServer:      1_700_000_001_000,
		Seq:           42,
	})
	if !ok {
		t.Fatal("planner did not return intent")
	}
	if intent.Side != strategydomain.IntentSideSell {
		t.Fatalf("side=%q want=%q", intent.Side, strategydomain.IntentSideSell)
	}
	if intent.Scope.Venue != "binance" {
		t.Fatalf("venue=%q want=binance", intent.Scope.Venue)
	}
	if intent.Scope.Symbol != "BTCUSDT" {
		t.Fatalf("symbol=%q want=BTCUSDT", intent.Scope.Symbol)
	}
	if len(intent.Provenance.ParentSignalIDs) != 1 || intent.Provenance.ParentSignalIDs[0] != "sig-1" {
		t.Fatalf("parent_signal_ids=%v want=[sig-1]", intent.Provenance.ParentSignalIDs)
	}
	if intent.ExpiresAtMs <= intent.CreatedAtMs {
		t.Fatalf("expires_at_ms=%d must be > created_at_ms=%d", intent.ExpiresAtMs, intent.CreatedAtMs)
	}
	if p := intent.Validate(); p != nil {
		t.Fatalf("intent invalid: %v", p)
	}
}

func TestIntentPlanner_UsesCanonicalReasonWhenReasonMissing(t *testing.T) {
	planner := NewIntentPlanner(DefaultPlannerConfig())
	intent, ok := planner.Plan(IntentInput{
		Kind:       "absorption",
		Venue:      "binance",
		Instrument: "ETHUSDT",
		SignalID:   "legacy-sig",
		Confidence: 0.6,
		TsServer:   1_700_000_001_500,
		Seq:        7,
	})
	if !ok {
		t.Fatal("planner did not return intent")
	}
	if intent.Side != strategydomain.IntentSideBuy {
		t.Fatalf("side=%q want=%q", intent.Side, strategydomain.IntentSideBuy)
	}
	if got := intent.Provenance.Reason; got != "bootstrap intent from canonical signal" {
		t.Fatalf("reason=%q want canonical fallback", got)
	}
}
