package contracts_test

import (
	"testing"

	executiondomain "github.com/market-raccoon/internal/core/execution/domain"
	portfoliodomain "github.com/market-raccoon/internal/core/portfolio/domain"
	strategydomain "github.com/market-raccoon/internal/core/strategy/domain"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/contracts"
)

func TestRegisterStrategyExecutionPortfolioPayloadsV1(t *testing.T) {
	reg := codec.NewRegistry()
	if p := contracts.RegisterStrategyPayloadV1(reg); p != nil {
		t.Fatalf("RegisterStrategyPayloadV1 failed: %s", p.Message)
	}
	if p := contracts.RegisterExecutionPayloadV1(reg); p != nil {
		t.Fatalf("RegisterExecutionPayloadV1 failed: %s", p.Message)
	}
	if p := contracts.RegisterPortfolioPayloadV1(reg); p != nil {
		t.Fatalf("RegisterPortfolioPayloadV1 failed: %s", p.Message)
	}

	mustHaveCodec(t, reg, strategydomain.IntentEventType)
	mustHaveCodec(t, reg, executiondomain.EventType)
	mustHaveCodec(t, reg, portfoliodomain.StateEventType)
}

func TestStrategyExecutionPortfolioCodecRoundTrip(t *testing.T) {
	reg := codec.NewRegistry()
	if p := contracts.RegisterStrategyPayloadV1(reg); p != nil {
		t.Fatalf("RegisterStrategyPayloadV1 failed: %s", p.Message)
	}
	if p := contracts.RegisterExecutionPayloadV1(reg); p != nil {
		t.Fatalf("RegisterExecutionPayloadV1 failed: %s", p.Message)
	}
	if p := contracts.RegisterPortfolioPayloadV1(reg); p != nil {
		t.Fatalf("RegisterPortfolioPayloadV1 failed: %s", p.Message)
	}

	intent := strategydomain.StrategyIntentV1{
		IntentID: "intent-1",
		Strategy: strategydomain.StrategyRef{StrategyID: "s", StrategyVersion: "v", StrategyInstanceID: "i"},
		Scope:    strategydomain.IntentScope{Venue: "binance", Symbol: "BTCUSDT", AccountID: "paper"},
		Side:     strategydomain.IntentSideBuy,
		Sizing: strategydomain.SizingIntent{
			Mode:           strategydomain.SizingModeBaseQuantity,
			Value:          1.5,
			MaxNotionalUSD: 1000,
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
			TraceID:         "trace-1",
			ParentSignalIDs: []string{"sig-1"},
			PolicyHash:      "policy-1",
		},
	}
	mustRoundTrip(t, reg, strategydomain.IntentEventType, intent)

	execEvent := executiondomain.ExecutionEventV1{
		EventID:       "evt-1",
		Status:        executiondomain.ExecutionStatusFilled,
		Correlation:   executiondomain.ExecutionCorrelation{IntentID: "intent-1", OrderID: "order-1", Venue: "binance", Symbol: "BTCUSDT", AccountID: "paper"},
		TsEventMs:     1_700_000_001_500,
		ExecutionSeq:  1,
		Attempt:       1,
		RequestedQty:  1.5,
		LastFillQty:   1.5,
		LeavesQty:     0,
		AvgFillPrice:  100,
		LastFillPrice: 100,
		Provenance:    executiondomain.ExecutionProvenance{CorrelationID: "corr-1", TraceID: "trace-1", Source: "executor.bootstrap.v1"},
	}
	mustRoundTrip(t, reg, executiondomain.EventType, execEvent)

	state := portfoliodomain.PortfolioStateV1{
		StateID:       "state-1",
		Scope:         portfoliodomain.PortfolioScopeVenueAccount,
		AccountID:     "paper",
		Venue:         "binance",
		ProjectedAtMs: 1_700_000_001_500,
		Balances: []portfoliodomain.BalanceV1{
			{Asset: "BTC", Total: 1.5, Available: 1.5, Locked: 0},
			{Asset: "USDT", Total: 8500, Available: 8500, Locked: 0},
		},
		Positions: []portfoliodomain.PositionV1{{Venue: "binance", Symbol: "BTCUSDT", Quantity: 1.5, AvgEntryPrice: 100, NotionalUSD: 150}},
		Exposures: []portfoliodomain.ExposureV1{{Symbol: "BTCUSDT", NetQty: 1.5, GrossNotionalUSD: 150, Leverage: 0.02}},
		EquityUSD: 8500,
		Risk:      portfoliodomain.RiskSnapshotV1{MarginUsedUSD: 15, MarginAvailableUSD: 8485, MaintenanceMarginUSD: 7.5, Var95USD: 3},
		Provenance: portfoliodomain.ProjectionProvenanceV1{
			SourceExecutionEventID: "evt-1",
			SourceExecutionSeq:     1,
			CorrelationID:          "corr-1",
			TraceID:                "trace-1",
			ProjectorVersion:       "portfolio-bootstrap-v1",
		},
	}
	mustRoundTrip(t, reg, portfoliodomain.StateEventType, state)
}

func mustHaveCodec(t *testing.T, reg *codec.Registry, eventType string) {
	t.Helper()
	for _, format := range []codec.Format{codec.FormatJSON, codec.FormatProto} {
		key := codec.SchemaKey{Type: eventType, Version: 1, Format: format}
		if _, ok := reg.Encoder(key); !ok {
			t.Fatalf("missing encoder for %+v", key)
		}
		if _, ok := reg.Decoder(key); !ok {
			t.Fatalf("missing decoder for %+v", key)
		}
	}
}

func mustRoundTrip(t *testing.T, reg *codec.Registry, eventType string, payload any) {
	t.Helper()
	for _, format := range []codec.Format{codec.FormatJSON, codec.FormatProto} {
		key := codec.SchemaKey{Type: eventType, Version: 1, Format: format}
		enc, ok := reg.Encoder(key)
		if !ok {
			t.Fatalf("missing encoder for %+v", key)
		}
		raw, p := enc.Encode(payload)
		if p != nil {
			t.Fatalf("encode failed for %+v: %s", key, p.Message)
		}
		dec, ok := reg.Decoder(key)
		if !ok {
			t.Fatalf("missing decoder for %+v", key)
		}
		decoded, p := dec.Decode(raw)
		if p != nil {
			t.Fatalf("decode failed for %+v: %s", key, p.Message)
		}
		if decoded == nil {
			t.Fatalf("decoded payload is nil for %+v", key)
		}
	}
}
