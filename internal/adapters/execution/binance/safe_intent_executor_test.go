package binance

import (
	"context"
	"testing"

	executioncred "github.com/market-raccoon/internal/adapters/execution/credentials"
	executiondomain "github.com/market-raccoon/internal/core/execution/domain"
	strategydomain "github.com/market-raccoon/internal/core/strategy/domain"
)

type fakeGateway struct {
	venueOrderID   string
	err            error
	lastReq        TestOrderRequest
	submitSnapshot OrderSnapshot
	submitErr      error
	querySnapshots []OrderSnapshot
	queryErr       error
	queryCalls     int
}

func (g *fakeGateway) SubmitTestOrder(_ context.Context, req TestOrderRequest) (string, error) {
	g.lastReq = req
	return g.venueOrderID, g.err
}

func (g *fakeGateway) SubmitOrder(_ context.Context, req TestOrderRequest) (OrderSnapshot, error) {
	g.lastReq = req
	if g.submitErr != nil {
		return OrderSnapshot{}, g.submitErr
	}
	if g.submitSnapshot.VenueOrderID == "" {
		g.submitSnapshot.VenueOrderID = g.venueOrderID
	}
	if g.submitSnapshot.ClientOrderID == "" {
		g.submitSnapshot.ClientOrderID = req.ClientOrderID
	}
	return g.submitSnapshot, nil
}

func (g *fakeGateway) QueryOrder(_ context.Context, _, _, _ string, _ int64) (OrderSnapshot, error) {
	if g.queryErr != nil {
		return OrderSnapshot{}, g.queryErr
	}
	if len(g.querySnapshots) == 0 {
		return OrderSnapshot{}, nil
	}
	idx := g.queryCalls
	if idx >= len(g.querySnapshots) {
		idx = len(g.querySnapshots) - 1
	}
	g.queryCalls++
	return g.querySnapshots[idx], nil
}

func TestSafeIntentExecutor_AcceptsAllowedIntent(t *testing.T) {
	gateway := &fakeGateway{venueOrderID: "BN-TEST-1"}
	cfg := DefaultSafeIntentExecutorConfig()
	executor := NewSafeIntentExecutor(cfg, gateway)

	events := executor.ExecuteAt(validIntent(), 1_700_000_002_000)
	if len(events) != 1 {
		t.Fatalf("event count=%d want=1", len(events))
	}
	if events[0].Status != executiondomain.ExecutionStatusAccepted {
		t.Fatalf("status=%q want=accepted", events[0].Status)
	}
	if events[0].Reason != reasonAcceptedRealTestOrder {
		t.Fatalf("reason=%q want=%q", events[0].Reason, reasonAcceptedRealTestOrder)
	}
	if gateway.lastReq.Symbol != "BTCUSDT" {
		t.Fatalf("gateway symbol=%q want=BTCUSDT", gateway.lastReq.Symbol)
	}
	if gateway.lastReq.Side != "BUY" {
		t.Fatalf("gateway side=%q want=BUY", gateway.lastReq.Side)
	}
	info := executor.BoundaryInfo()
	if info.Adapter != "binance.spot" {
		t.Fatalf("boundary adapter=%q want=binance.spot", info.Adapter)
	}
}

func TestSafeIntentExecutor_RejectsWhenCredentialsMissing(t *testing.T) {
	gateway := &fakeGateway{err: executioncred.NewLeaseError(executiondomain.ReasonCredentialsUnavailableMaterialMissing, nil)}
	cfg := DefaultSafeIntentExecutorConfig()
	executor := NewSafeIntentExecutor(cfg, gateway)

	events := executor.ExecuteAt(validIntent(), 1_700_000_002_000)
	if len(events) != 1 {
		t.Fatalf("event count=%d want=1", len(events))
	}
	if events[0].Status != executiondomain.ExecutionStatusRejected {
		t.Fatalf("status=%q want=rejected", events[0].Status)
	}
	if events[0].Reason != executiondomain.ReasonCredentialsUnavailableMaterialMissing {
		t.Fatalf("reason=%q want=%q", events[0].Reason, executiondomain.ReasonCredentialsUnavailableMaterialMissing)
	}
}

func TestSafeIntentExecutor_RejectsUnsupportedSizingMode(t *testing.T) {
	cfg := DefaultSafeIntentExecutorConfig()
	executor := NewSafeIntentExecutor(cfg, &fakeGateway{venueOrderID: "BN-TEST-1"})
	intent := validIntent()
	intent.Sizing.Mode = strategydomain.SizingModeQuoteNotionalUSD

	events := executor.ExecuteAt(intent, 1_700_000_002_000)
	if len(events) != 1 {
		t.Fatalf("event count=%d want=1", len(events))
	}
	if events[0].Reason != reasonRejectedSizingModeUnsupported {
		t.Fatalf("reason=%q want=%q", events[0].Reason, reasonRejectedSizingModeUnsupported)
	}
}

func TestSafeIntentExecutor_ReconcilesLifecycleDeterministically(t *testing.T) {
	gateway := &fakeGateway{
		submitSnapshot: OrderSnapshot{
			VenueOrderID:        "10001",
			ClientOrderID:       "cid-10001",
			Status:              "NEW",
			RequestedQty:        1.5,
			CumulativeFilledQty: 0,
			LeavesQty:           1.5,
			LimitPrice:          100,
			AvgFillPrice:        0,
			LastFillPrice:       0,
			TsExchangeMs:        1_700_000_002_100,
		},
		querySnapshots: []OrderSnapshot{
			{
				VenueOrderID:        "10001",
				ClientOrderID:       "cid-10001",
				Status:              "NEW",
				RequestedQty:        1.5,
				CumulativeFilledQty: 0,
				LeavesQty:           1.5,
				LimitPrice:          100,
				TsExchangeMs:        1_700_000_002_101,
			},
			{
				VenueOrderID:        "10001",
				ClientOrderID:       "cid-10001",
				Status:              "PARTIALLY_FILLED",
				RequestedQty:        1.5,
				CumulativeFilledQty: 1.0,
				LeavesQty:           0.5,
				LimitPrice:          100,
				AvgFillPrice:        100,
				LastFillPrice:       100,
				TsExchangeMs:        1_700_000_002_120,
			},
			{
				VenueOrderID:        "10001",
				ClientOrderID:       "cid-10001",
				Status:              "PARTIALLY_FILLED",
				RequestedQty:        1.5,
				CumulativeFilledQty: 1.0,
				LeavesQty:           0.5,
				LimitPrice:          100,
				AvgFillPrice:        100,
				LastFillPrice:       100,
				TsExchangeMs:        1_700_000_002_121,
			},
			{
				VenueOrderID:        "10001",
				ClientOrderID:       "cid-10001",
				Status:              "FILLED",
				RequestedQty:        1.5,
				CumulativeFilledQty: 1.5,
				LeavesQty:           0,
				LimitPrice:          100,
				AvgFillPrice:        101,
				LastFillPrice:       101,
				TsExchangeMs:        1_700_000_002_140,
			},
		},
	}
	cfg := DefaultSafeIntentExecutorConfig()
	cfg.EndpointMode = endpointModeSafeLifecycle
	cfg.ReconcileEnabled = true
	cfg.ReconcilePollEvery = 0
	cfg.ReconcileMaxPolls = 6
	executor := NewSafeIntentExecutor(cfg, gateway)

	events := executor.ExecuteAt(validIntent(), 1_700_000_002_000)
	if len(events) != 4 {
		t.Fatalf("event count=%d want=4", len(events))
	}
	wantStatuses := []executiondomain.ExecutionStatus{
		executiondomain.ExecutionStatusAccepted,
		executiondomain.ExecutionStatusPlaced,
		executiondomain.ExecutionStatusPartiallyFilled,
		executiondomain.ExecutionStatusFilled,
	}
	wantReasons := []string{
		reasonAcceptedRealLifecycle,
		reasonPlacedObserved,
		reasonPartiallyFilledObserved,
		reasonFilledObserved,
	}
	for i := range wantStatuses {
		if events[i].Status != wantStatuses[i] {
			t.Fatalf("event[%d].status=%q want=%q", i, events[i].Status, wantStatuses[i])
		}
		if events[i].Reason != wantReasons[i] {
			t.Fatalf("event[%d].reason=%q want=%q", i, events[i].Reason, wantReasons[i])
		}
		if events[i].ExecutionSeq != int64(i+1) {
			t.Fatalf("event[%d].execution_seq=%d want=%d", i, events[i].ExecutionSeq, i+1)
		}
	}
	if events[2].LastFillQty != 1 {
		t.Fatalf("partial last_fill_qty=%v want=1", events[2].LastFillQty)
	}
	if events[3].LastFillQty != 0.5 {
		t.Fatalf("filled last_fill_qty=%v want=0.5", events[3].LastFillQty)
	}
}

func TestSafeIntentExecutor_ReconciliationTimeoutEmitsFailed(t *testing.T) {
	gateway := &fakeGateway{
		submitSnapshot: OrderSnapshot{
			VenueOrderID:        "10002",
			ClientOrderID:       "cid-10002",
			Status:              "NEW",
			RequestedQty:        1.5,
			CumulativeFilledQty: 0,
			LeavesQty:           1.5,
			LimitPrice:          100,
			TsExchangeMs:        1_700_000_002_100,
		},
		querySnapshots: []OrderSnapshot{
			{
				VenueOrderID:        "10002",
				ClientOrderID:       "cid-10002",
				Status:              "NEW",
				RequestedQty:        1.5,
				CumulativeFilledQty: 0,
				LeavesQty:           1.5,
				LimitPrice:          100,
				TsExchangeMs:        1_700_000_002_120,
			},
		},
	}
	cfg := DefaultSafeIntentExecutorConfig()
	cfg.EndpointMode = endpointModeSafeLifecycle
	cfg.ReconcileEnabled = true
	cfg.ReconcilePollEvery = 0
	cfg.ReconcileMaxPolls = 1
	executor := NewSafeIntentExecutor(cfg, gateway)

	events := executor.ExecuteAt(validIntent(), 1_700_000_002_000)
	if len(events) != 3 {
		t.Fatalf("event count=%d want=3", len(events))
	}
	if events[2].Status != executiondomain.ExecutionStatusFailed {
		t.Fatalf("status=%q want=failed", events[2].Status)
	}
	if events[2].Reason != reasonFailedReconciliationTimeout {
		t.Fatalf("reason=%q want=%q", events[2].Reason, reasonFailedReconciliationTimeout)
	}
}

func validIntent() strategydomain.StrategyIntentV1 {
	return strategydomain.StrategyIntentV1{
		IntentID: "intent-stage7",
		Strategy: strategydomain.StrategyRef{StrategyID: "s", StrategyVersion: "v", StrategyInstanceID: "i"},
		Scope:    strategydomain.IntentScope{Venue: "binance", Symbol: "BTCUSDT", AccountID: "paper"},
		Side:     strategydomain.IntentSideBuy,
		Sizing: strategydomain.SizingIntent{
			Mode:           strategydomain.SizingModeBaseQuantity,
			Value:          1.5,
			MaxNotionalUSD: 500,
		},
		Constraints: strategydomain.ExecutionConstraints{
			OrderType:      strategydomain.OrderTypeMarket,
			TimeInForce:    strategydomain.TimeInForceIOC,
			MaxSlippageBps: 20,
		},
		CreatedAtMs: 1_700_000_001_000,
		ExpiresAtMs: 1_700_000_031_000,
		Provenance: strategydomain.IntentProvenance{
			Reason:          "fixture",
			CorrelationID:   "corr-stage7",
			PolicyHash:      "policy-stage7",
			ParentSignalIDs: []string{"sig-stage7"},
		},
	}
}
