package app

import (
	"testing"

	executiondomain "github.com/market-raccoon/internal/core/execution/domain"
)

func TestBootstrapProjector_AppliesFilledExecution(t *testing.T) {
	projector := NewBootstrapProjector(DefaultProjectorConfig())
	state, ok := projector.Apply(executiondomain.ExecutionEventV1{
		EventID:       "evt-1",
		Status:        executiondomain.ExecutionStatusFilled,
		Correlation:   executiondomain.ExecutionCorrelation{IntentID: "intent-1", OrderID: "order-1", Venue: "binance", Symbol: "BTCUSDT", AccountID: "paper"},
		TsEventMs:     1_700_000_001_000,
		ExecutionSeq:  1,
		Attempt:       1,
		RequestedQty:  2,
		LastFillQty:   2,
		LeavesQty:     0,
		AvgFillPrice:  100,
		LastFillPrice: 100,
		Provenance: executiondomain.ExecutionProvenance{
			CorrelationID: "corr-1",
			Source:        "executor.bootstrap.v1",
		},
	})
	if !ok {
		t.Fatal("projector did not return state")
	}
	if len(state.Positions) != 1 {
		t.Fatalf("positions=%d want=1", len(state.Positions))
	}
	if state.Positions[0].Quantity != 2 {
		t.Fatalf("quantity=%v want=2", state.Positions[0].Quantity)
	}
	if len(state.Balances) < 2 {
		t.Fatalf("balances=%d want>=2", len(state.Balances))
	}
	if state.Provenance.SourceExecutionSeq != 1 {
		t.Fatalf("source_execution_seq=%d want=1", state.Provenance.SourceExecutionSeq)
	}
	if p := state.Validate(); p != nil {
		t.Fatalf("state invalid: %v", p)
	}
}

func TestBootstrapProjector_AcceptedThenRejectedClearsLocks(t *testing.T) {
	projector := NewBootstrapProjector(DefaultProjectorConfig())
	accepted, ok := projector.Apply(executiondomain.ExecutionEventV1{
		EventID:       "evt-accepted",
		Status:        executiondomain.ExecutionStatusAccepted,
		Correlation:   executiondomain.ExecutionCorrelation{IntentID: "intent-2", OrderID: "order-2", Venue: "binance", Symbol: "ETHUSDT", AccountID: "paper"},
		TsEventMs:     1_700_000_001_100,
		ExecutionSeq:  1,
		Attempt:       1,
		RequestedQty:  1,
		LeavesQty:     1,
		AvgFillPrice:  250,
		LastFillPrice: 250,
		Provenance: executiondomain.ExecutionProvenance{
			CorrelationID: "corr-2",
			Source:        "executor.bootstrap.v1",
		},
	})
	if !ok {
		t.Fatal("accepted state not projected")
	}
	if len(accepted.Balances) < 2 || accepted.Balances[1].Locked <= 0 {
		t.Fatalf("quote locked balance=%v want > 0", accepted.Balances)
	}

	rejected, ok := projector.Apply(executiondomain.ExecutionEventV1{
		EventID:      "evt-rejected",
		Status:       executiondomain.ExecutionStatusRejected,
		Correlation:  executiondomain.ExecutionCorrelation{IntentID: "intent-2", OrderID: "order-2", Venue: "binance", Symbol: "ETHUSDT", AccountID: "paper"},
		TsEventMs:    1_700_000_001_120,
		ExecutionSeq: 2,
		Attempt:      1,
		RequestedQty: 1,
		LeavesQty:    1,
		Reason:       "rejected_post_only_not_supported",
		Provenance: executiondomain.ExecutionProvenance{
			CorrelationID: "corr-2",
			Source:        "executor.bootstrap.v1",
		},
	})
	if !ok {
		t.Fatal("rejected state not projected")
	}
	if len(rejected.Balances) < 2 || rejected.Balances[1].Locked != 0 {
		t.Fatalf("quote locked balance after reject=%v want 0", rejected.Balances)
	}
}

func TestBootstrapProjector_AcceptedThenFilledConsumesLockAndUpdatesPosition(t *testing.T) {
	projector := NewBootstrapProjector(DefaultProjectorConfig())
	_, ok := projector.Apply(executiondomain.ExecutionEventV1{
		EventID:       "evt-accepted-3",
		Status:        executiondomain.ExecutionStatusAccepted,
		Correlation:   executiondomain.ExecutionCorrelation{IntentID: "intent-3", OrderID: "order-3", Venue: "binance", Symbol: "BTCUSDT", AccountID: "paper"},
		TsEventMs:     1_700_000_001_200,
		ExecutionSeq:  1,
		Attempt:       1,
		RequestedQty:  2,
		LeavesQty:     2,
		AvgFillPrice:  100,
		LastFillPrice: 100,
		Provenance: executiondomain.ExecutionProvenance{
			CorrelationID: "corr-3",
			Source:        "executor.bootstrap.v1",
		},
	})
	if !ok {
		t.Fatal("accepted state not projected")
	}

	filled, ok := projector.Apply(executiondomain.ExecutionEventV1{
		EventID:             "evt-filled-3",
		Status:              executiondomain.ExecutionStatusFilled,
		Correlation:         executiondomain.ExecutionCorrelation{IntentID: "intent-3", OrderID: "order-3", Venue: "binance", Symbol: "BTCUSDT", AccountID: "paper"},
		TsEventMs:           1_700_000_001_201,
		ExecutionSeq:        2,
		Attempt:             1,
		RequestedQty:        2,
		CumulativeFilledQty: 2,
		LastFillQty:         2,
		LeavesQty:           0,
		AvgFillPrice:        100,
		LastFillPrice:       100,
		Provenance: executiondomain.ExecutionProvenance{
			CorrelationID: "corr-3",
			Source:        "executor.bootstrap.v1",
		},
	})
	if !ok {
		t.Fatal("filled state not projected")
	}
	if filled.Positions[0].Quantity != 2 {
		t.Fatalf("quantity=%v want=2", filled.Positions[0].Quantity)
	}
	if len(filled.Balances) < 2 || filled.Balances[1].Locked != 0 {
		t.Fatalf("quote locked balance after fill=%v want 0", filled.Balances)
	}
	if filled.UnrealizedPnlUSD != 0 {
		t.Fatalf("unrealized_pnl=%v want=0 with mark=avg", filled.UnrealizedPnlUSD)
	}
}

func TestBootstrapProjector_DeterministicAcrossRuns(t *testing.T) {
	mkEvent := func() executiondomain.ExecutionEventV1 {
		return executiondomain.ExecutionEventV1{
			EventID:       "evt-2",
			Status:        executiondomain.ExecutionStatusFilled,
			Correlation:   executiondomain.ExecutionCorrelation{IntentID: "intent-2", OrderID: "order-2", Venue: "binance", Symbol: "ETHUSDT", AccountID: "paper"},
			TsEventMs:     1_700_000_001_500,
			ExecutionSeq:  2,
			Attempt:       1,
			RequestedQty:  -1,
			LastFillQty:   -1,
			LeavesQty:     0,
			AvgFillPrice:  250,
			LastFillPrice: 250,
			Provenance: executiondomain.ExecutionProvenance{
				CorrelationID: "corr-2",
				Source:        "executor.bootstrap.v1",
			},
		}
	}

	p1 := NewBootstrapProjector(DefaultProjectorConfig())
	s1, ok := p1.Apply(mkEvent())
	if !ok {
		t.Fatal("first projector failed")
	}

	p2 := NewBootstrapProjector(DefaultProjectorConfig())
	s2, ok := p2.Apply(mkEvent())
	if !ok {
		t.Fatal("second projector failed")
	}

	if s1.StateID != s2.StateID {
		t.Fatalf("state_id mismatch first=%q second=%q", s1.StateID, s2.StateID)
	}
	if s1.EquityUSD != s2.EquityUSD {
		t.Fatalf("equity mismatch first=%v second=%v", s1.EquityUSD, s2.EquityUSD)
	}
}

func TestBootstrapProjector_PartialThenFilledFromCumulativeProgress(t *testing.T) {
	projector := NewBootstrapProjector(DefaultProjectorConfig())
	_, ok := projector.Apply(executiondomain.ExecutionEventV1{
		EventID:      "evt-accepted-4",
		Status:       executiondomain.ExecutionStatusPlaced,
		Correlation:  executiondomain.ExecutionCorrelation{IntentID: "intent-4", OrderID: "order-4", Venue: "binance", Symbol: "BTCUSDT", AccountID: "paper"},
		TsEventMs:    1_700_000_001_300,
		ExecutionSeq: 1,
		Attempt:      1,
		RequestedQty: 1,
		LeavesQty:    1,
		LimitPrice:   100,
		Provenance:   executiondomain.ExecutionProvenance{CorrelationID: "corr-4", Source: "executor.real_adapter.safe.v1"},
	})
	if !ok {
		t.Fatal("placed state not projected")
	}

	partial, ok := projector.Apply(executiondomain.ExecutionEventV1{
		EventID:             "evt-partial-4",
		Status:              executiondomain.ExecutionStatusPartiallyFilled,
		Correlation:         executiondomain.ExecutionCorrelation{IntentID: "intent-4", OrderID: "order-4", Venue: "binance", Symbol: "BTCUSDT", AccountID: "paper"},
		TsEventMs:           1_700_000_001_320,
		ExecutionSeq:        2,
		Attempt:             1,
		RequestedQty:        1,
		CumulativeFilledQty: 0.4,
		LeavesQty:           0.6,
		AvgFillPrice:        100,
		LastFillPrice:       100,
		Provenance:          executiondomain.ExecutionProvenance{CorrelationID: "corr-4", Source: "executor.real_adapter.safe.v1"},
	})
	if !ok {
		t.Fatal("partial state not projected")
	}
	if partial.Positions[0].Quantity != 0.4 {
		t.Fatalf("partial quantity=%v want=0.4", partial.Positions[0].Quantity)
	}
	if len(partial.Balances) < 2 || partial.Balances[1].Locked <= 0 {
		t.Fatalf("partial locked quote=%v want>0", partial.Balances)
	}

	duplicatePartial, ok := projector.Apply(executiondomain.ExecutionEventV1{
		EventID:             "evt-partial-4-dup",
		Status:              executiondomain.ExecutionStatusPartiallyFilled,
		Correlation:         executiondomain.ExecutionCorrelation{IntentID: "intent-4", OrderID: "order-4", Venue: "binance", Symbol: "BTCUSDT", AccountID: "paper"},
		TsEventMs:           1_700_000_001_321,
		ExecutionSeq:        3,
		Attempt:             1,
		RequestedQty:        1,
		CumulativeFilledQty: 0.4,
		LeavesQty:           0.6,
		AvgFillPrice:        100,
		LastFillPrice:       100,
		Provenance:          executiondomain.ExecutionProvenance{CorrelationID: "corr-4", Source: "executor.real_adapter.safe.v1"},
	})
	if !ok {
		t.Fatal("duplicate partial state not projected")
	}
	if duplicatePartial.Positions[0].Quantity != 0.4 {
		t.Fatalf("duplicate partial quantity=%v want=0.4", duplicatePartial.Positions[0].Quantity)
	}

	filled, ok := projector.Apply(executiondomain.ExecutionEventV1{
		EventID:             "evt-filled-4",
		Status:              executiondomain.ExecutionStatusFilled,
		Correlation:         executiondomain.ExecutionCorrelation{IntentID: "intent-4", OrderID: "order-4", Venue: "binance", Symbol: "BTCUSDT", AccountID: "paper"},
		TsEventMs:           1_700_000_001_340,
		ExecutionSeq:        4,
		Attempt:             1,
		RequestedQty:        1,
		CumulativeFilledQty: 1,
		LeavesQty:           0,
		AvgFillPrice:        101,
		LastFillPrice:       101,
		Provenance:          executiondomain.ExecutionProvenance{CorrelationID: "corr-4", Source: "executor.real_adapter.safe.v1"},
	})
	if !ok {
		t.Fatal("filled state not projected")
	}
	if filled.Positions[0].Quantity != 1 {
		t.Fatalf("filled quantity=%v want=1", filled.Positions[0].Quantity)
	}
	if len(filled.Balances) < 2 || filled.Balances[1].Locked != 0 {
		t.Fatalf("filled locked quote=%v want=0", filled.Balances)
	}
}
