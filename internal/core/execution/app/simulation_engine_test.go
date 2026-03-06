package app

import (
	"math"
	"testing"

	executiondomain "github.com/market-raccoon/internal/core/execution/domain"
	strategydomain "github.com/market-raccoon/internal/core/strategy/domain"
)

func simIntentFixture() strategydomain.StrategyIntentV1 {
	return strategydomain.StrategyIntentV1{
		IntentID: "sim-intent-1",
		Strategy: strategydomain.StrategyRef{StrategyID: "s", StrategyVersion: "v", StrategyInstanceID: "i"},
		Scope:    strategydomain.IntentScope{Venue: "binance", Symbol: "BTCUSDT", AccountID: "paper"},
		Side:     strategydomain.IntentSideBuy,
		Sizing: strategydomain.SizingIntent{
			Mode:           strategydomain.SizingModeBaseQuantity,
			Value:          2,
			MaxNotionalUSD: 400,
		},
		Constraints: strategydomain.ExecutionConstraints{
			OrderType:      strategydomain.OrderTypeLimit,
			TimeInForce:    strategydomain.TimeInForceGTC,
			LimitPrice:     50_000,
			MaxSlippageBps: 25,
		},
		CreatedAtMs: 1_700_000_001_000,
		ExpiresAtMs: 1_700_000_121_000,
		Provenance: strategydomain.IntentProvenance{
			Reason:          "fixture",
			CorrelationID:   "corr-sim-1",
			PolicyHash:      "hash-sim-1",
			ParentSignalIDs: []string{"sig-sim-1"},
		},
	}
}

func TestSimulationEngine_Deterministic(t *testing.T) {
	cfg := DefaultSimulationConfig()
	eng1 := NewSimulationEngine(cfg)
	eng2 := NewSimulationEngine(cfg)
	intent := simIntentFixture()

	events1 := eng1.ExecuteAt(intent, 1_700_000_001_000)
	events2 := eng2.ExecuteAt(intent, 1_700_000_001_000)

	if len(events1) != len(events2) {
		t.Fatalf("determinism violated: event count %d vs %d", len(events1), len(events2))
	}
	for i := range events1 {
		if events1[i].EventID != events2[i].EventID {
			t.Fatalf("determinism violated at event %d: EventID %q vs %q", i, events1[i].EventID, events2[i].EventID)
		}
		if events1[i].Status != events2[i].Status {
			t.Fatalf("determinism violated at event %d: Status %q vs %q", i, events1[i].Status, events2[i].Status)
		}
		if events1[i].TsEventMs != events2[i].TsEventMs {
			t.Fatalf("determinism violated at event %d: TsEventMs %d vs %d", i, events1[i].TsEventMs, events2[i].TsEventMs)
		}
		if events1[i].LastFillPrice != events2[i].LastFillPrice {
			t.Fatalf("determinism violated at event %d: LastFillPrice %v vs %v", i, events1[i].LastFillPrice, events2[i].LastFillPrice)
		}
	}
}

func TestSimulationEngine_AcceptedAndPlacedAlways(t *testing.T) {
	eng := NewSimulationEngine(DefaultSimulationConfig())
	intent := simIntentFixture()

	events := eng.ExecuteAt(intent, 1_700_000_001_000)
	if len(events) < 3 {
		t.Fatalf("expected at least 3 events (accepted+placed+fill), got %d", len(events))
	}
	if events[0].Status != executiondomain.ExecutionStatusAccepted {
		t.Fatalf("event[0] status=%q want=accepted", events[0].Status)
	}
	if events[1].Status != executiondomain.ExecutionStatusPlaced {
		t.Fatalf("event[1] status=%q want=placed", events[1].Status)
	}
	if events[0].Reason != simReasonAccepted {
		t.Fatalf("accepted reason=%q want=%q", events[0].Reason, simReasonAccepted)
	}
	if events[1].Reason != simReasonPlaced {
		t.Fatalf("placed reason=%q want=%q", events[1].Reason, simReasonPlaced)
	}
}

func TestSimulationEngine_MonotonicSequences(t *testing.T) {
	eng := NewSimulationEngine(DefaultSimulationConfig())
	intent := simIntentFixture()

	events := eng.ExecuteAt(intent, 1_700_000_001_000)
	for i := 1; i < len(events); i++ {
		if events[i].ExecutionSeq <= events[i-1].ExecutionSeq {
			t.Fatalf("non-monotonic seq at index %d: %d <= %d", i, events[i].ExecutionSeq, events[i-1].ExecutionSeq)
		}
	}
}

func TestSimulationEngine_MonotonicTimestamps(t *testing.T) {
	eng := NewSimulationEngine(DefaultSimulationConfig())
	intent := simIntentFixture()

	events := eng.ExecuteAt(intent, 1_700_000_001_000)
	for i := 1; i < len(events); i++ {
		if events[i].TsEventMs < events[i-1].TsEventMs {
			t.Fatalf("non-monotonic timestamp at index %d: %d < %d", i, events[i].TsEventMs, events[i-1].TsEventMs)
		}
	}
}

func TestSimulationEngine_MarketOrderFullFill(t *testing.T) {
	eng := NewSimulationEngine(DefaultSimulationConfig())
	intent := simIntentFixture()
	intent.Constraints.OrderType = strategydomain.OrderTypeMarket
	intent.Constraints.LimitPrice = 0

	events := eng.ExecuteAt(intent, 1_700_000_001_000)

	// Market GTC: accepted + placed + filled.
	if len(events) != 3 {
		t.Fatalf("market order event count=%d want=3", len(events))
	}
	if events[2].Status != executiondomain.ExecutionStatusFilled {
		t.Fatalf("terminal status=%q want=filled", events[2].Status)
	}
	if events[2].LeavesQty != 0 {
		t.Fatalf("leaves_qty=%v want=0", events[2].LeavesQty)
	}
	if events[2].CumulativeFilledQty != intent.Sizing.Value {
		t.Fatalf("cumulative_filled=%v want=%v", events[2].CumulativeFilledQty, intent.Sizing.Value)
	}
}

func TestSimulationEngine_FOKFill(t *testing.T) {
	// Use a config with 0 cancel probability to ensure FOK fills.
	cfg := DefaultSimulationConfig()
	cfg.CancelProbability = 0
	eng := NewSimulationEngine(cfg)

	intent := simIntentFixture()
	intent.Constraints.TimeInForce = strategydomain.TimeInForceFOK

	events := eng.ExecuteAt(intent, 1_700_000_001_000)
	// accepted + placed + filled
	if len(events) != 3 {
		t.Fatalf("FOK fill event count=%d want=3", len(events))
	}
	if events[2].Status != executiondomain.ExecutionStatusFilled {
		t.Fatalf("FOK terminal=%q want=filled", events[2].Status)
	}
	if events[2].LeavesQty != 0 {
		t.Fatalf("FOK leaves=%v want=0", events[2].LeavesQty)
	}
}

func TestSimulationEngine_FOKCancel(t *testing.T) {
	cfg := DefaultSimulationConfig()
	cfg.CancelProbability = 1.0 // Force cancel.
	eng := NewSimulationEngine(cfg)

	intent := simIntentFixture()
	intent.Constraints.TimeInForce = strategydomain.TimeInForceFOK

	events := eng.ExecuteAt(intent, 1_700_000_001_000)
	// accepted + placed + canceled
	if len(events) != 3 {
		t.Fatalf("FOK cancel event count=%d want=3", len(events))
	}
	if events[2].Status != executiondomain.ExecutionStatusCanceled {
		t.Fatalf("FOK terminal=%q want=canceled", events[2].Status)
	}
	if events[2].Reason != simReasonFOKNoFill {
		t.Fatalf("FOK reason=%q want=%q", events[2].Reason, simReasonFOKNoFill)
	}
}

func TestSimulationEngine_IOCPartialFillAndCancel(t *testing.T) {
	cfg := DefaultSimulationConfig()
	cfg.FillRatio = 0.5 // Force 50% fill ratio as base.
	eng := NewSimulationEngine(cfg)

	intent := simIntentFixture()
	intent.Constraints.TimeInForce = strategydomain.TimeInForceIOC

	events := eng.ExecuteAt(intent, 1_700_000_001_000)

	// Must have accepted + placed + (fill or cancel).
	if len(events) < 3 {
		t.Fatalf("IOC event count=%d want>=3", len(events))
	}

	// Find terminal event.
	terminal := events[len(events)-1]
	if terminal.Status != executiondomain.ExecutionStatusFilled &&
		terminal.Status != executiondomain.ExecutionStatusCanceled {
		t.Fatalf("IOC terminal=%q want=filled|canceled", terminal.Status)
	}
}

func TestSimulationEngine_GTCPartialFills(t *testing.T) {
	cfg := DefaultSimulationConfig()
	cfg.MaxPartialFills = 3
	cfg.FillRatio = 0.4
	cfg.CancelProbability = 0 // No cancel, forces full fill.
	eng := NewSimulationEngine(cfg)

	intent := simIntentFixture()

	events := eng.ExecuteAt(intent, 1_700_000_001_000)

	// accepted + placed + N fills
	if len(events) < 4 {
		t.Fatalf("GTC event count=%d want>=4 (accepted+placed+fills)", len(events))
	}

	// Verify terminal event is filled.
	terminal := events[len(events)-1]
	if terminal.Status != executiondomain.ExecutionStatusFilled {
		t.Fatalf("GTC terminal=%q want=filled", terminal.Status)
	}
	if terminal.LeavesQty != 0 {
		t.Fatalf("GTC leaves=%v want=0", terminal.LeavesQty)
	}

	// Verify partial fills have correct status.
	for _, e := range events[2 : len(events)-1] {
		if e.Status != executiondomain.ExecutionStatusPartiallyFilled {
			t.Fatalf("intermediate status=%q want=partially_filled", e.Status)
		}
	}
}

func TestSimulationEngine_GTCWithCancel(t *testing.T) {
	cfg := DefaultSimulationConfig()
	cfg.CancelProbability = 1.0 // Force cancel.
	cfg.MaxPartialFills = 3
	cfg.FillRatio = 0.3
	eng := NewSimulationEngine(cfg)

	intent := simIntentFixture()

	events := eng.ExecuteAt(intent, 1_700_000_001_000)

	terminal := events[len(events)-1]
	if terminal.Status != executiondomain.ExecutionStatusCanceled {
		t.Fatalf("GTC cancel terminal=%q want=canceled", terminal.Status)
	}
	if terminal.Reason != simReasonCanceledPartial {
		t.Fatalf("GTC cancel reason=%q want=%q", terminal.Reason, simReasonCanceledPartial)
	}
	// Must have some cumulative fill.
	if math.Abs(terminal.CumulativeFilledQty) < 1e-9 {
		t.Fatal("GTC cancel: expected some cumulative fill before cancel")
	}
}

func TestSimulationEngine_PriceImpact(t *testing.T) {
	cfg := DefaultSimulationConfig()
	cfg.PriceImpactBps = 10 // 10 bps per step.
	cfg.CancelProbability = 0
	cfg.MaxPartialFills = 3
	cfg.FillRatio = 0.3
	eng := NewSimulationEngine(cfg)

	intent := simIntentFixture()
	events := eng.ExecuteAt(intent, 1_700_000_001_000)

	// Collect fill prices.
	var fillPrices []float64
	for _, e := range events {
		if e.Status == executiondomain.ExecutionStatusPartiallyFilled ||
			e.Status == executiondomain.ExecutionStatusFilled {
			fillPrices = append(fillPrices, e.LastFillPrice)
		}
	}

	if len(fillPrices) < 2 {
		t.Skipf("only %d fills, need >=2 for price impact check", len(fillPrices))
	}

	// Buy order: prices should be non-decreasing (impact pushes up).
	for i := 1; i < len(fillPrices); i++ {
		if fillPrices[i] < fillPrices[i-1] {
			t.Fatalf("buy price impact violated at step %d: %v < %v", i, fillPrices[i], fillPrices[i-1])
		}
	}
}

func TestSimulationEngine_SellPriceImpact(t *testing.T) {
	cfg := DefaultSimulationConfig()
	cfg.PriceImpactBps = 10
	cfg.CancelProbability = 0
	cfg.MaxPartialFills = 3
	cfg.FillRatio = 0.3
	eng := NewSimulationEngine(cfg)

	intent := simIntentFixture()
	intent.Side = strategydomain.IntentSideSell

	events := eng.ExecuteAt(intent, 1_700_000_001_000)

	var fillPrices []float64
	for _, e := range events {
		if e.Status == executiondomain.ExecutionStatusPartiallyFilled ||
			e.Status == executiondomain.ExecutionStatusFilled {
			fillPrices = append(fillPrices, e.LastFillPrice)
		}
	}

	if len(fillPrices) < 2 {
		t.Skipf("only %d fills, need >=2 for price impact check", len(fillPrices))
	}

	// Sell order: prices should be non-increasing (impact pushes down).
	for i := 1; i < len(fillPrices); i++ {
		if fillPrices[i] > fillPrices[i-1] {
			t.Fatalf("sell price impact violated at step %d: %v > %v", i, fillPrices[i], fillPrices[i-1])
		}
	}
}

func TestSimulationEngine_Rejection(t *testing.T) {
	eng := NewSimulationEngine(DefaultSimulationConfig())
	intent := simIntentFixture()
	intent.Sizing.Value = 0

	events := eng.ExecuteAt(intent, 1_700_000_001_000)
	if len(events) != 1 {
		t.Fatalf("rejection event count=%d want=1", len(events))
	}
	if events[0].Status != executiondomain.ExecutionStatusRejected {
		t.Fatalf("rejection status=%q want=rejected", events[0].Status)
	}
}

func TestSimulationEngine_AllEventsValidate(t *testing.T) {
	eng := NewSimulationEngine(DefaultSimulationConfig())
	intent := simIntentFixture()

	events := eng.ExecuteAt(intent, 1_700_000_001_000)
	for i, e := range events {
		if p := e.Validate(); p != nil {
			t.Fatalf("event %d (%s) failed validation: %s", i, e.Status, p.Message)
		}
	}
}

func TestSimulationEngine_BoundaryInfo(t *testing.T) {
	eng := NewSimulationEngine(DefaultSimulationConfig())
	info := eng.BoundaryInfo()
	if info.Boundary != "execution.adapter" {
		t.Fatalf("boundary=%q want=execution.adapter", info.Boundary)
	}
	if info.Adapter != "simulation.deterministic" {
		t.Fatalf("adapter=%q want=simulation.deterministic", info.Adapter)
	}
	if info.Mode != "bootstrap_simulated" {
		t.Fatalf("mode=%q want=bootstrap_simulated", info.Mode)
	}
}

func TestSimulationEngine_CorrelationConsistency(t *testing.T) {
	eng := NewSimulationEngine(DefaultSimulationConfig())
	intent := simIntentFixture()

	events := eng.ExecuteAt(intent, 1_700_000_001_000)
	if len(events) == 0 {
		t.Fatal("no events")
	}

	orderID := events[0].Correlation.OrderID
	for i, e := range events {
		if e.Correlation.OrderID != orderID {
			t.Fatalf("event %d order_id=%q != %q", i, e.Correlation.OrderID, orderID)
		}
		if e.Correlation.IntentID != intent.IntentID {
			t.Fatalf("event %d intent_id=%q != %q", i, e.Correlation.IntentID, intent.IntentID)
		}
		if e.Correlation.Venue != "binance" {
			t.Fatalf("event %d venue=%q", i, e.Correlation.Venue)
		}
		if e.Correlation.Symbol != "BTCUSDT" {
			t.Fatalf("event %d symbol=%q", i, e.Correlation.Symbol)
		}
	}
}

func TestSimulationEngine_ExpiryBeforeFill(t *testing.T) {
	cfg := DefaultSimulationConfig()
	cfg.FillBaseDelayMs = 100
	cfg.FillStepDelayMs = 100
	cfg.CancelProbability = 0
	cfg.MaxPartialFills = 5
	eng := NewSimulationEngine(cfg)

	intent := simIntentFixture()
	// Set very tight TTL that expires during fill sequence.
	intent.ExpiresAtMs = intent.CreatedAtMs + 120 // Only 120ms window.

	events := eng.ExecuteAt(intent, intent.CreatedAtMs)

	terminal := events[len(events)-1]
	if terminal.Status != executiondomain.ExecutionStatusExpired {
		t.Fatalf("expected expired, got %q", terminal.Status)
	}
	if terminal.Reason != simReasonExpired {
		t.Fatalf("reason=%q want=%q", terminal.Reason, simReasonExpired)
	}
}

func TestSimulationEngine_QuantityConservation(t *testing.T) {
	cfg := DefaultSimulationConfig()
	cfg.CancelProbability = 0
	eng := NewSimulationEngine(cfg)
	intent := simIntentFixture()

	events := eng.ExecuteAt(intent, 1_700_000_001_000)
	terminal := events[len(events)-1]

	if terminal.Status == executiondomain.ExecutionStatusFilled {
		if math.Abs(terminal.CumulativeFilledQty-terminal.RequestedQty) > 1e-6 {
			t.Fatalf("fill conservation: cumulative=%v requested=%v",
				terminal.CumulativeFilledQty, terminal.RequestedQty)
		}
		if terminal.LeavesQty > 1e-9 {
			t.Fatalf("filled but leaves=%v", terminal.LeavesQty)
		}
	}
}

func TestSimulationEngine_AllEventsPassDomainValidation(t *testing.T) {
	configs := []struct {
		name string
		cfg  SimulationConfig
		tif  strategydomain.TimeInForce
	}{
		{"GTC_no_cancel", func() SimulationConfig { c := DefaultSimulationConfig(); c.CancelProbability = 0; return c }(), strategydomain.TimeInForceGTC},
		{"GTC_cancel", func() SimulationConfig { c := DefaultSimulationConfig(); c.CancelProbability = 1; return c }(), strategydomain.TimeInForceGTC},
		{"IOC", DefaultSimulationConfig(), strategydomain.TimeInForceIOC},
		{"FOK_fill", func() SimulationConfig { c := DefaultSimulationConfig(); c.CancelProbability = 0; return c }(), strategydomain.TimeInForceFOK},
		{"FOK_cancel", func() SimulationConfig { c := DefaultSimulationConfig(); c.CancelProbability = 1; return c }(), strategydomain.TimeInForceFOK},
	}

	for _, tc := range configs {
		t.Run(tc.name, func(t *testing.T) {
			eng := NewSimulationEngine(tc.cfg)
			intent := simIntentFixture()
			intent.Constraints.TimeInForce = tc.tif

			events := eng.ExecuteAt(intent, 1_700_000_001_000)
			for i, e := range events {
				if p := e.Validate(); p != nil {
					t.Fatalf("event %d (%s) validation failed: %s", i, e.Status, p.Message)
				}
			}
		})
	}
}

func TestSimulationEngine_TerminalStateReached(t *testing.T) {
	configs := []struct {
		name string
		cfg  SimulationConfig
		tif  strategydomain.TimeInForce
	}{
		{"GTC_no_cancel", func() SimulationConfig { c := DefaultSimulationConfig(); c.CancelProbability = 0; return c }(), strategydomain.TimeInForceGTC},
		{"GTC_cancel", func() SimulationConfig { c := DefaultSimulationConfig(); c.CancelProbability = 1; return c }(), strategydomain.TimeInForceGTC},
		{"IOC", DefaultSimulationConfig(), strategydomain.TimeInForceIOC},
		{"FOK", DefaultSimulationConfig(), strategydomain.TimeInForceFOK},
	}

	terminalStatuses := map[executiondomain.ExecutionStatus]bool{
		executiondomain.ExecutionStatusFilled:   true,
		executiondomain.ExecutionStatusCanceled: true,
		executiondomain.ExecutionStatusExpired:  true,
		executiondomain.ExecutionStatusFailed:   true,
		executiondomain.ExecutionStatusRejected: true,
	}

	for _, tc := range configs {
		t.Run(tc.name, func(t *testing.T) {
			eng := NewSimulationEngine(tc.cfg)
			intent := simIntentFixture()
			intent.Constraints.TimeInForce = tc.tif

			events := eng.ExecuteAt(intent, 1_700_000_001_000)
			terminal := events[len(events)-1]
			if !terminalStatuses[terminal.Status] {
				t.Fatalf("last event status=%q is not terminal", terminal.Status)
			}
		})
	}
}

func TestSimulationEngine_MultipleIntentsSequencing(t *testing.T) {
	eng := NewSimulationEngine(DefaultSimulationConfig())

	intent1 := simIntentFixture()
	intent1.IntentID = "intent-multi-1"

	intent2 := simIntentFixture()
	intent2.IntentID = "intent-multi-2"

	events1 := eng.ExecuteAt(intent1, 1_700_000_001_000)
	events2 := eng.ExecuteAt(intent2, 1_700_000_002_000)

	// Sequences should continue monotonically across intents (same stream).
	lastSeq1 := events1[len(events1)-1].ExecutionSeq
	firstSeq2 := events2[0].ExecutionSeq

	if firstSeq2 <= lastSeq1 {
		t.Fatalf("cross-intent seq not monotonic: %d <= %d", firstSeq2, lastSeq1)
	}
}

func TestDeterministicHash_Stable(t *testing.T) {
	h1 := deterministicHash("test-intent", "cancel", "fok")
	h2 := deterministicHash("test-intent", "cancel", "fok")
	if h1 != h2 {
		t.Fatalf("hash not stable: %v vs %v", h1, h2)
	}
	if h1 < 0 || h1 >= 1 {
		t.Fatalf("hash out of range: %v", h1)
	}
}

func TestDeterministicHash_DifferentInputs(t *testing.T) {
	h1 := deterministicHash("intent-a", "cancel", "fok")
	h2 := deterministicHash("intent-b", "cancel", "fok")
	if h1 == h2 {
		t.Fatal("different inputs produced same hash")
	}
}
