package app

import (
	"testing"

	executiondomain "github.com/market-raccoon/internal/core/execution/domain"
	strategydomain "github.com/market-raccoon/internal/core/strategy/domain"
)

func TestBootstrapExecutor_EmitsAcceptedAndFilled(t *testing.T) {
	executor := NewBootstrapExecutor(DefaultBootstrapConfig())
	intent := strategydomain.StrategyIntentV1{
		IntentID: "intent-1",
		Strategy: strategydomain.StrategyRef{StrategyID: "s", StrategyVersion: "v", StrategyInstanceID: "i"},
		Scope:    strategydomain.IntentScope{Venue: "binance", Symbol: "BTCUSDT", AccountID: "paper"},
		Side:     strategydomain.IntentSideBuy,
		Sizing: strategydomain.SizingIntent{
			Mode:           strategydomain.SizingModeBaseQuantity,
			Value:          2,
			MaxNotionalUSD: 400,
		},
		Constraints: strategydomain.ExecutionConstraints{
			OrderType:      strategydomain.OrderTypeMarket,
			TimeInForce:    strategydomain.TimeInForceIOC,
			MaxSlippageBps: 25,
		},
		CreatedAtMs: 1_700_000_001_000,
		ExpiresAtMs: 1_700_000_031_000,
		Provenance: strategydomain.IntentProvenance{
			Reason:          "fixture",
			CorrelationID:   "corr-1",
			PolicyHash:      "hash-1",
			ParentSignalIDs: []string{"sig-1"},
		},
	}

	events := executor.Execute(intent)
	if len(events) != 2 {
		t.Fatalf("event count=%d want=2", len(events))
	}
	if events[0].Status != executiondomain.ExecutionStatusAccepted {
		t.Fatalf("first status=%q want=accepted", events[0].Status)
	}
	if events[1].Status != executiondomain.ExecutionStatusFilled {
		t.Fatalf("second status=%q want=filled", events[1].Status)
	}
	if events[0].ExecutionSeq != 1 || events[1].ExecutionSeq != 2 {
		t.Fatalf("seqs=(%d,%d) want=(1,2)", events[0].ExecutionSeq, events[1].ExecutionSeq)
	}
	if events[1].LastFillPrice != 200 {
		t.Fatalf("last_fill_price=%v want=200", events[1].LastFillPrice)
	}
	if events[0].Reason != executionReasonAccepted {
		t.Fatalf("accepted reason=%q want=%q", events[0].Reason, executionReasonAccepted)
	}
	if events[1].Reason != executionReasonFilled {
		t.Fatalf("filled reason=%q want=%q", events[1].Reason, executionReasonFilled)
	}
}

func TestBootstrapExecutor_SellIntentUsesNegativeSignedQty(t *testing.T) {
	executor := NewBootstrapExecutor(DefaultBootstrapConfig())
	intent := strategydomain.StrategyIntentV1{
		IntentID: "intent-2",
		Strategy: strategydomain.StrategyRef{StrategyID: "s", StrategyVersion: "v", StrategyInstanceID: "i"},
		Scope:    strategydomain.IntentScope{Venue: "binance", Symbol: "ETHUSDT", AccountID: "paper"},
		Side:     strategydomain.IntentSideSell,
		Sizing: strategydomain.SizingIntent{
			Mode:           strategydomain.SizingModeBaseQuantity,
			Value:          3,
			MaxNotionalUSD: 600,
		},
		Constraints: strategydomain.ExecutionConstraints{
			OrderType:      strategydomain.OrderTypeMarket,
			TimeInForce:    strategydomain.TimeInForceIOC,
			MaxSlippageBps: 25,
		},
		CreatedAtMs: 1_700_000_001_000,
		ExpiresAtMs: 1_700_000_031_000,
		Provenance: strategydomain.IntentProvenance{
			Reason:          "fixture",
			CorrelationID:   "corr-2",
			PolicyHash:      "hash-2",
			ParentSignalIDs: []string{"sig-2"},
		},
	}

	events := executor.Execute(intent)
	if len(events) == 0 {
		t.Fatal("expected at least one event")
	}
	if events[0].RequestedQty >= 0 {
		t.Fatalf("requested_qty=%v want negative for sell", events[0].RequestedQty)
	}
}

func TestBootstrapExecutor_RejectsExpiredTTLDeterministically(t *testing.T) {
	executor := NewBootstrapExecutor(DefaultBootstrapConfig())
	intent := validIntentFixture()
	intent.ExpiresAtMs = intent.CreatedAtMs + 5

	events := executor.ExecuteAt(intent, intent.ExpiresAtMs+1)
	if len(events) != 1 {
		t.Fatalf("event count=%d want=1", len(events))
	}
	if events[0].Status != executiondomain.ExecutionStatusRejected {
		t.Fatalf("status=%q want=rejected", events[0].Status)
	}
	if events[0].Reason != executionReasonTTLExpired {
		t.Fatalf("reason=%q want=%q", events[0].Reason, executionReasonTTLExpired)
	}
}

func TestBootstrapExecutor_RejectsNonPositiveSizing(t *testing.T) {
	executor := NewBootstrapExecutor(DefaultBootstrapConfig())
	intent := validIntentFixture()
	intent.Sizing.Value = 0

	events := executor.Execute(intent)
	if len(events) != 1 {
		t.Fatalf("event count=%d want=1", len(events))
	}
	if events[0].Status != executiondomain.ExecutionStatusRejected {
		t.Fatalf("status=%q want=rejected", events[0].Status)
	}
	if events[0].Reason != executionReasonSizingNonPositive {
		t.Fatalf("reason=%q want=%q", events[0].Reason, executionReasonSizingNonPositive)
	}
}

func TestBootstrapExecutor_RejectsUnsupportedConstraint(t *testing.T) {
	executor := NewBootstrapExecutor(DefaultBootstrapConfig())
	intent := validIntentFixture()
	intent.Constraints.PostOnly = true

	events := executor.Execute(intent)
	if len(events) != 1 {
		t.Fatalf("event count=%d want=1", len(events))
	}
	if events[0].Status != executiondomain.ExecutionStatusRejected {
		t.Fatalf("status=%q want=rejected", events[0].Status)
	}
	if events[0].Reason != executionReasonPostOnlyUnsupported {
		t.Fatalf("reason=%q want=%q", events[0].Reason, executionReasonPostOnlyUnsupported)
	}
}

func TestBootstrapExecutor_BoundaryInfoDefaults(t *testing.T) {
	executor := NewBootstrapExecutor(BootstrapConfig{})
	info := executor.BoundaryInfo()
	if info.Boundary != "execution.adapter" {
		t.Fatalf("boundary=%q want=execution.adapter", info.Boundary)
	}
	if info.Adapter != "bootstrap.simulated" {
		t.Fatalf("adapter=%q want=bootstrap.simulated", info.Adapter)
	}
	if info.Mode != "bootstrap_simulated" {
		t.Fatalf("mode=%q want=bootstrap_simulated", info.Mode)
	}
}

func TestBootstrapExecutor_BoundaryInfoCustomValues(t *testing.T) {
	executor := NewBootstrapExecutor(BootstrapConfig{
		Boundary:      "execution.venue_adapter",
		AdapterID:     "stub.dryrun",
		ExecutionMode: "paper_stub",
	})
	info := executor.BoundaryInfo()
	if info.Boundary != "execution.venue_adapter" {
		t.Fatalf("boundary=%q want=execution.venue_adapter", info.Boundary)
	}
	if info.Adapter != "stub.dryrun" {
		t.Fatalf("adapter=%q want=stub.dryrun", info.Adapter)
	}
	if info.Mode != "paper_stub" {
		t.Fatalf("mode=%q want=paper_stub", info.Mode)
	}
}

func validIntentFixture() strategydomain.StrategyIntentV1 {
	return strategydomain.StrategyIntentV1{
		IntentID: "intent-fixture",
		Strategy: strategydomain.StrategyRef{StrategyID: "s", StrategyVersion: "v", StrategyInstanceID: "i"},
		Scope:    strategydomain.IntentScope{Venue: "binance", Symbol: "BTCUSDT", AccountID: "paper"},
		Side:     strategydomain.IntentSideBuy,
		Sizing: strategydomain.SizingIntent{
			Mode:           strategydomain.SizingModeBaseQuantity,
			Value:          1.5,
			MaxNotionalUSD: 400,
		},
		Constraints: strategydomain.ExecutionConstraints{
			OrderType:      strategydomain.OrderTypeMarket,
			TimeInForce:    strategydomain.TimeInForceIOC,
			MaxSlippageBps: 25,
		},
		CreatedAtMs: 1_700_000_001_000,
		ExpiresAtMs: 1_700_000_031_000,
		Provenance: strategydomain.IntentProvenance{
			Reason:          "fixture",
			CorrelationID:   "corr-fixture",
			PolicyHash:      "hash-fixture",
			ParentSignalIDs: []string{"sig-fixture"},
		},
	}
}
