package runtime_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/anthdm/hollywood/actor"
	executionruntime "github.com/market-raccoon/internal/actors/execution/runtime"
	portfolioruntime "github.com/market-raccoon/internal/actors/portfolio/runtime"
	strategyruntime "github.com/market-raccoon/internal/actors/strategy/runtime"
	executionadapterbinance "github.com/market-raccoon/internal/adapters/execution/binance"
	executioncred "github.com/market-raccoon/internal/adapters/execution/credentials"
	executionapp "github.com/market-raccoon/internal/core/execution/app"
	executiongovernance "github.com/market-raccoon/internal/core/execution/governance"
	executionports "github.com/market-raccoon/internal/core/execution/ports"
	marketmodel "github.com/market-raccoon/internal/core/marketmodel"
	portfoliodomain "github.com/market-raccoon/internal/core/portfolio/domain"
	signalcore "github.com/market-raccoon/internal/core/signal"
	strategydomain "github.com/market-raccoon/internal/core/strategy/domain"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/contracts"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/problem"
)

func init() {
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		panic(fmt.Sprintf("BootstrapPayloadCodecRegistry: %v", p))
	}
}

type forwardPublisher struct {
	to chan envelope.Envelope
}

func (p *forwardPublisher) Publish(_ context.Context, env envelope.Envelope) *problem.Problem {
	p.to <- env
	return nil
}

type capturePublisher struct {
	ch chan envelope.Envelope
}

func (p *capturePublisher) Publish(_ context.Context, env envelope.Envelope) *problem.Problem {
	p.ch <- env
	return nil
}

type fakeRealAdapterGateway struct {
	venueOrderID   string
	err            error
	submitSnapshot executionadapterbinance.OrderSnapshot
	querySnapshots []executionadapterbinance.OrderSnapshot
	queryErr       error
	queryCalls     int
}

func (g *fakeRealAdapterGateway) SubmitTestOrder(_ context.Context, _ executionadapterbinance.TestOrderRequest) (string, error) {
	return g.venueOrderID, g.err
}

func (g *fakeRealAdapterGateway) SubmitOrder(_ context.Context, req executionadapterbinance.TestOrderRequest) (executionadapterbinance.OrderSnapshot, error) {
	if g.err != nil {
		return executionadapterbinance.OrderSnapshot{}, g.err
	}
	if g.submitSnapshot.VenueOrderID == "" {
		g.submitSnapshot.VenueOrderID = g.venueOrderID
	}
	if g.submitSnapshot.ClientOrderID == "" {
		g.submitSnapshot.ClientOrderID = req.ClientOrderID
	}
	return g.submitSnapshot, nil
}

func (g *fakeRealAdapterGateway) QueryOrder(_ context.Context, _, _, _ string, _ int64) (executionadapterbinance.OrderSnapshot, error) {
	if g.queryErr != nil {
		return executionadapterbinance.OrderSnapshot{}, g.queryErr
	}
	if len(g.querySnapshots) == 0 {
		return executionadapterbinance.OrderSnapshot{}, nil
	}
	idx := g.queryCalls
	if idx >= len(g.querySnapshots) {
		idx = len(g.querySnapshots) - 1
	}
	g.queryCalls++
	return g.querySnapshots[idx], nil
}

type staticTradeCredentialProvider struct{}

func (staticTradeCredentialProvider) ResolveTradeCredentialMaterial() (executioncred.ProviderMaterial, error) {
	return executioncred.ProviderMaterial{
		Credentials: executioncred.TradeCredentials{
			APIKey:    "test-api-key",
			APISecret: "test-api-secret",
		},
		Scope:           executioncred.ScopeTradeOnly,
		TradeOnly:       true,
		ProviderID:      executioncred.ProviderIDEnvStaticV1,
		SourceType:      executioncred.SourceTypeEnv,
		SourceRef:       "test.runtime.fixture",
		RevocationReady: true,
	}, nil
}

func newGovernedRealExecutor(symbol string, cfg executionadapterbinance.SafeIntentExecutorConfig, gateway *fakeRealAdapterGateway) executionports.IntentExecutor {
	symbol = executiongovernanceSymbol(symbol)
	grant := executiongovernance.ExecutionGrant{
		GrantID:   "execution.real_adapter_safe.runtime_fixture",
		Boundary:  "execution.adapter",
		AdapterID: "binance.spot",
		Mode:      "real_adapter_safe",
		SafeMode:  true,
		TradeOnly: true,
		Scope: executiongovernance.ExecutionScope{
			AllowedVenues:   map[string]struct{}{"binance": {}},
			AllowedSymbols:  map[string]struct{}{symbol: {}},
			AllowAnyAccount: true,
		},
		Provenance: executiongovernance.GrantProvenance{
			Source:   "runtime.fixture",
			PolicyID: "stage9b.runtime_fixture",
		},
	}
	credentialRequirement := executiongovernance.CredentialRequirement{
		Required:            true,
		Boundary:            grant.Boundary,
		AdapterID:           grant.AdapterID,
		Mode:                grant.Mode,
		Scope:               executioncred.ScopeTradeOnly,
		TradeOnly:           true,
		AcceptedResolverIDs: []string{executioncred.ResolverIDTradeBrokerV1},
		AcceptedProviderIDs: []string{executioncred.ProviderIDEnvStaticV1},
	}
	credentialBroker := executioncred.NewBroker(executioncred.BrokerConfig{
		Boundary:   grant.Boundary,
		AdapterID:  grant.AdapterID,
		Mode:       grant.Mode,
		ResolverID: executioncred.ResolverIDTradeBrokerV1,
		ProviderID: executioncred.ProviderIDEnvStaticV1,
		SourceType: executioncred.SourceTypeEnv,
		SourceRef:  "test.runtime.fixture",
	}, staticTradeCredentialProvider{})
	realExecutor := executionadapterbinance.NewSafeIntentExecutor(cfg, gateway)
	return executionapp.NewGovernedExecutor(executionapp.GovernedExecutorConfig{
		Governance: executionapp.NewStaticExecutionGovernance(executionapp.StaticExecutionGovernanceConfig{
			Authorizer: executionapp.StaticCapabilityAuthorizer{Grant: &grant},
			Selector: executionapp.NewStaticAdapterSelector(executionapp.AdapterRoute{
				Boundary:              grant.Boundary,
				AdapterID:             grant.AdapterID,
				Mode:                  grant.Mode,
				CredentialRequirement: credentialRequirement,
			}),
			CredentialResolver: credentialBroker,
			BoundaryInfo: executionports.BoundaryInfo{
				Boundary: grant.Boundary,
				Adapter:  grant.AdapterID,
				Mode:     grant.Mode,
			},
		}),
		Adapters: map[string]executionports.IntentExecutor{
			grant.AdapterID: realExecutor,
		},
	})
}

func executiongovernanceSymbol(symbol string) string {
	if symbol == "" {
		return "BTCUSDT"
	}
	return symbol
}

func TestBootstrapFlow_SignalToStrategyToExecutionToPortfolio(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	signalIn := make(chan envelope.Envelope, 2)
	intentCh := make(chan envelope.Envelope, 4)
	execCh := make(chan envelope.Envelope, 8)
	stateCh := make(chan envelope.Envelope, 2)

	strategyPID := e.Spawn(strategyruntime.NewSubsystemActor(strategyruntime.SubsystemConfig{
		EnvelopeCh:   signalIn,
		Publisher:    &forwardPublisher{to: intentCh},
		ReplicaID:    0,
		ReplicaCount: 1,
	}), "strategy-e2e")
	executionPID := e.Spawn(executionruntime.NewSubsystemActor(executionruntime.SubsystemConfig{
		EnvelopeCh:   intentCh,
		Publisher:    &forwardPublisher{to: execCh},
		ReplicaID:    0,
		ReplicaCount: 1,
	}), "execution-e2e")
	portfolioPID := e.Spawn(portfolioruntime.NewSubsystemActor(portfolioruntime.SubsystemConfig{
		EnvelopeCh:   execCh,
		Publisher:    &capturePublisher{ch: stateCh},
		ReplicaID:    0,
		ReplicaCount: 1,
	}), "portfolio-e2e")
	defer func() {
		close(signalIn)
		close(intentCh)
		close(execCh)
		<-e.Poison(strategyPID).Done()
		<-e.Poison(executionPID).Done()
		<-e.Poison(portfolioPID).Done()
	}()

	signalPayload, p := codec.EncodePayload(signalcore.EventType, signalcore.EventVersion, envelope.ContentTypeProto, marketmodel.SignalEvent{
		Type:       "liquidity_thinning",
		TsServer:   1_700_000_001_000,
		Scope:      marketmodel.SignalScopeStream,
		Venue:      "binance",
		Symbol:     "BTCUSDT",
		Severity:   "high",
		Confidence: 0.8,
		Features: []marketmodel.SignalFeature{
			{Key: "imbalance", Value: 0.7},
		},
		Explanation:    "thin book",
		SignalID:       "sig-e2e",
		RuleID:         "rule-e2e",
		RuleVersion:    "v1",
		InputWatermark: []marketmodel.SignalInputSeqRange{{Venue: "binance", Symbol: "BTCUSDT", SeqStart: 55, SeqEnd: 55}},
		CorrelationID:  "corr-e2e",
	})
	if p != nil {
		t.Fatalf("encode signal payload: %v", p)
	}

	signalIn <- envelope.Envelope{
		Type:        signalcore.EventType,
		Version:     signalcore.EventVersion,
		Venue:       "binance",
		Instrument:  "BTCUSDT",
		TsIngest:    1_700_000_001_000,
		Seq:         55,
		ContentType: envelope.ContentTypeProto,
		Payload:     signalPayload,
	}

	deadline := time.After(3 * time.Second)
	for {
		select {
		case out := <-stateCh:
			if out.Meta["source_execution_status"] != "filled" {
				continue
			}
			if out.Type != portfoliodomain.StateEventType {
				t.Fatalf("type=%q want=%q", out.Type, portfoliodomain.StateEventType)
			}
			decoded, p := codec.DecodePayload(out.Type, out.Version, out.ContentType, out.Payload)
			if p != nil {
				t.Fatalf("decode portfolio payload: %v", p)
			}
			state, ok := decoded.(portfoliodomain.PortfolioStateV1)
			if !ok {
				t.Fatalf("decoded type=%T want PortfolioStateV1", decoded)
			}
			if len(state.Positions) != 1 {
				t.Fatalf("positions=%d want=1", len(state.Positions))
			}
			if state.Positions[0].Symbol != "BTCUSDT" {
				t.Fatalf("symbol=%q want=BTCUSDT", state.Positions[0].Symbol)
			}
			if state.Positions[0].Quantity == 0 {
				t.Fatalf("quantity=%v want non-zero after filled", state.Positions[0].Quantity)
			}
			return
		case <-deadline:
			t.Fatal("timeout waiting filled-derived portfolio state")
		}
	}
}

func TestBootstrapFlow_ExpiredIntentProjectsRejectedPortfolioState(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	intentIn := make(chan envelope.Envelope, 2)
	execCh := make(chan envelope.Envelope, 4)
	stateCh := make(chan envelope.Envelope, 2)

	executionPID := e.Spawn(executionruntime.NewSubsystemActor(executionruntime.SubsystemConfig{
		EnvelopeCh:   intentIn,
		Publisher:    &forwardPublisher{to: execCh},
		ReplicaID:    0,
		ReplicaCount: 1,
	}), "execution-expired-e2e")
	portfolioPID := e.Spawn(portfolioruntime.NewSubsystemActor(portfolioruntime.SubsystemConfig{
		EnvelopeCh:   execCh,
		Publisher:    &capturePublisher{ch: stateCh},
		ReplicaID:    0,
		ReplicaCount: 1,
	}), "portfolio-expired-e2e")
	defer func() {
		close(intentIn)
		close(execCh)
		<-e.Poison(executionPID).Done()
		<-e.Poison(portfolioPID).Done()
	}()

	intentPayload, p := codec.EncodePayload(strategydomain.IntentEventType, strategydomain.IntentEventVersion, envelope.ContentTypeProto, strategydomain.StrategyIntentV1{
		IntentID: "intent-expired-e2e",
		Strategy: strategydomain.StrategyRef{StrategyID: "s", StrategyVersion: "v", StrategyInstanceID: "i"},
		Scope:    strategydomain.IntentScope{Venue: "binance", Symbol: "ETHUSDT", AccountID: "paper"},
		Side:     strategydomain.IntentSideBuy,
		Sizing: strategydomain.SizingIntent{
			Mode:           strategydomain.SizingModeBaseQuantity,
			Value:          1,
			MaxNotionalUSD: 200,
		},
		Constraints: strategydomain.ExecutionConstraints{
			OrderType:      strategydomain.OrderTypeMarket,
			TimeInForce:    strategydomain.TimeInForceIOC,
			MaxSlippageBps: 25,
		},
		CreatedAtMs: 1_700_000_001_000,
		ExpiresAtMs: 1_700_000_001_050,
		Provenance: strategydomain.IntentProvenance{
			Reason:          "expired fixture",
			CorrelationID:   "corr-expired-e2e",
			ParentSignalIDs: []string{"sig-expired-e2e"},
			PolicyHash:      "policy-expired-e2e",
		},
	})
	if p != nil {
		t.Fatalf("encode strategy intent payload: %v", p)
	}

	intentIn <- envelope.Envelope{
		Type:        strategydomain.IntentEventType,
		Version:     strategydomain.IntentEventVersion,
		Venue:       "binance",
		Instrument:  "ETHUSDT",
		TsIngest:    1_700_000_001_200,
		Seq:         1,
		ContentType: envelope.ContentTypeProto,
		Payload:     intentPayload,
	}

	select {
	case out := <-stateCh:
		if out.Meta["source_execution_status"] != "rejected" {
			t.Fatalf("source_execution_status=%q want=rejected", out.Meta["source_execution_status"])
		}
		decoded, p := codec.DecodePayload(out.Type, out.Version, out.ContentType, out.Payload)
		if p != nil {
			t.Fatalf("decode portfolio payload: %v", p)
		}
		state, ok := decoded.(portfoliodomain.PortfolioStateV1)
		if !ok {
			t.Fatalf("decoded type=%T want PortfolioStateV1", decoded)
		}
		if state.Positions[0].Quantity != 0 {
			t.Fatalf("quantity=%v want=0 for rejected-only flow", state.Positions[0].Quantity)
		}
		if len(state.Balances) < 2 || state.Balances[1].Locked != 0 {
			t.Fatalf("quote locked=%v want=0", state.Balances)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting rejected-derived portfolio state")
	}
}

func TestBootstrapFlow_RealAdapterSafeAcceptedProjectsPendingPortfolioState(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	intentIn := make(chan envelope.Envelope, 2)
	execCh := make(chan envelope.Envelope, 4)
	stateCh := make(chan envelope.Envelope, 2)

	realCfg := executionadapterbinance.DefaultSafeIntentExecutorConfig()
	realExecutor := newGovernedRealExecutor("BTCUSDT", realCfg, &fakeRealAdapterGateway{venueOrderID: "BN-TEST-E2E"})

	executionPID := e.Spawn(executionruntime.NewSubsystemActor(executionruntime.SubsystemConfig{
		EnvelopeCh:   intentIn,
		Executor:     realExecutor,
		Publisher:    &forwardPublisher{to: execCh},
		ReplicaID:    0,
		ReplicaCount: 1,
	}), "execution-real-safe-e2e")
	portfolioPID := e.Spawn(portfolioruntime.NewSubsystemActor(portfolioruntime.SubsystemConfig{
		EnvelopeCh:   execCh,
		Publisher:    &capturePublisher{ch: stateCh},
		ReplicaID:    0,
		ReplicaCount: 1,
	}), "portfolio-real-safe-e2e")
	defer func() {
		close(intentIn)
		close(execCh)
		<-e.Poison(executionPID).Done()
		<-e.Poison(portfolioPID).Done()
	}()

	intentPayload, p := codec.EncodePayload(strategydomain.IntentEventType, strategydomain.IntentEventVersion, envelope.ContentTypeProto, strategydomain.StrategyIntentV1{
		IntentID: "intent-real-safe-e2e",
		Strategy: strategydomain.StrategyRef{StrategyID: "s", StrategyVersion: "v", StrategyInstanceID: "i"},
		Scope:    strategydomain.IntentScope{Venue: "binance", Symbol: "BTCUSDT", AccountID: "paper"},
		Side:     strategydomain.IntentSideBuy,
		Sizing: strategydomain.SizingIntent{
			Mode:           strategydomain.SizingModeBaseQuantity,
			Value:          1,
			MaxNotionalUSD: 200,
		},
		Constraints: strategydomain.ExecutionConstraints{
			OrderType:      strategydomain.OrderTypeMarket,
			TimeInForce:    strategydomain.TimeInForceIOC,
			MaxSlippageBps: 25,
		},
		CreatedAtMs: 1_700_000_001_000,
		ExpiresAtMs: 1_700_000_001_900,
		Provenance: strategydomain.IntentProvenance{
			Reason:          "real-safe fixture",
			CorrelationID:   "corr-real-safe-e2e",
			ParentSignalIDs: []string{"sig-real-safe-e2e"},
			PolicyHash:      "policy-real-safe-e2e",
		},
	})
	if p != nil {
		t.Fatalf("encode strategy intent payload: %v", p)
	}

	intentIn <- envelope.Envelope{
		Type:        strategydomain.IntentEventType,
		Version:     strategydomain.IntentEventVersion,
		Venue:       "binance",
		Instrument:  "BTCUSDT",
		TsIngest:    1_700_000_001_200,
		Seq:         1,
		ContentType: envelope.ContentTypeProto,
		Payload:     intentPayload,
	}

	select {
	case out := <-stateCh:
		if out.Meta["source_execution_status"] != "accepted" {
			t.Fatalf("source_execution_status=%q want=accepted", out.Meta["source_execution_status"])
		}
		decoded, p := codec.DecodePayload(out.Type, out.Version, out.ContentType, out.Payload)
		if p != nil {
			t.Fatalf("decode portfolio payload: %v", p)
		}
		state, ok := decoded.(portfoliodomain.PortfolioStateV1)
		if !ok {
			t.Fatalf("decoded type=%T want PortfolioStateV1", decoded)
		}
		if len(state.Balances) < 2 {
			t.Fatalf("balances=%d want>=2", len(state.Balances))
		}
		if state.Balances[1].Locked <= 0 {
			t.Fatalf("quote locked=%v want>0 for accepted pending order", state.Balances[1].Locked)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting accepted-derived portfolio state")
	}
}

func TestBootstrapFlow_RealAdapterSafeLifecycleReconcilesToFilledPortfolioState(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	intentIn := make(chan envelope.Envelope, 2)
	execCh := make(chan envelope.Envelope, 8)
	stateCh := make(chan envelope.Envelope, 8)

	realCfg := executionadapterbinance.DefaultSafeIntentExecutorConfig()
	realCfg.EndpointMode = "safe_order_lifecycle"
	realCfg.ReconcileEnabled = true
	realCfg.ReconcilePollEvery = 0
	realCfg.ReconcileMaxPolls = 6
	realExecutor := newGovernedRealExecutor("BTCUSDT", realCfg, &fakeRealAdapterGateway{
		venueOrderID: "7001",
		submitSnapshot: executionadapterbinance.OrderSnapshot{
			VenueOrderID:        "7001",
			ClientOrderID:       "cid-7001",
			Status:              "NEW",
			RequestedQty:        1,
			CumulativeFilledQty: 0,
			LeavesQty:           1,
			LimitPrice:          100,
			TsExchangeMs:        1_700_000_001_210,
		},
		querySnapshots: []executionadapterbinance.OrderSnapshot{
			{
				VenueOrderID:        "7001",
				ClientOrderID:       "cid-7001",
				Status:              "PARTIALLY_FILLED",
				RequestedQty:        1,
				CumulativeFilledQty: 0.4,
				LeavesQty:           0.6,
				LimitPrice:          100,
				AvgFillPrice:        100,
				LastFillPrice:       100,
				TsExchangeMs:        1_700_000_001_250,
			},
			{
				VenueOrderID:        "7001",
				ClientOrderID:       "cid-7001",
				Status:              "FILLED",
				RequestedQty:        1,
				CumulativeFilledQty: 1,
				LeavesQty:           0,
				LimitPrice:          100,
				AvgFillPrice:        101,
				LastFillPrice:       101,
				TsExchangeMs:        1_700_000_001_280,
			},
		},
	})

	executionPID := e.Spawn(executionruntime.NewSubsystemActor(executionruntime.SubsystemConfig{
		EnvelopeCh:   intentIn,
		Executor:     realExecutor,
		Publisher:    &forwardPublisher{to: execCh},
		ReplicaID:    0,
		ReplicaCount: 1,
	}), "execution-real-lifecycle-e2e")
	portfolioPID := e.Spawn(portfolioruntime.NewSubsystemActor(portfolioruntime.SubsystemConfig{
		EnvelopeCh:   execCh,
		Publisher:    &capturePublisher{ch: stateCh},
		ReplicaID:    0,
		ReplicaCount: 1,
	}), "portfolio-real-lifecycle-e2e")
	defer func() {
		close(intentIn)
		close(execCh)
		<-e.Poison(executionPID).Done()
		<-e.Poison(portfolioPID).Done()
	}()

	intentPayload, p := codec.EncodePayload(strategydomain.IntentEventType, strategydomain.IntentEventVersion, envelope.ContentTypeProto, strategydomain.StrategyIntentV1{
		IntentID: "intent-real-lifecycle-e2e",
		Strategy: strategydomain.StrategyRef{StrategyID: "s", StrategyVersion: "v", StrategyInstanceID: "i"},
		Scope:    strategydomain.IntentScope{Venue: "binance", Symbol: "BTCUSDT", AccountID: "paper"},
		Side:     strategydomain.IntentSideBuy,
		Sizing: strategydomain.SizingIntent{
			Mode:           strategydomain.SizingModeBaseQuantity,
			Value:          1,
			MaxNotionalUSD: 200,
		},
		Constraints: strategydomain.ExecutionConstraints{
			OrderType:      strategydomain.OrderTypeLimit,
			TimeInForce:    strategydomain.TimeInForceGTC,
			LimitPrice:     100,
			MaxSlippageBps: 25,
		},
		CreatedAtMs: 1_700_000_001_000,
		ExpiresAtMs: 1_700_000_001_900,
		Provenance: strategydomain.IntentProvenance{
			Reason:          "real-safe lifecycle fixture",
			CorrelationID:   "corr-real-lifecycle-e2e",
			ParentSignalIDs: []string{"sig-real-lifecycle-e2e"},
			PolicyHash:      "policy-real-lifecycle-e2e",
		},
	})
	if p != nil {
		t.Fatalf("encode strategy intent payload: %v", p)
	}

	intentIn <- envelope.Envelope{
		Type:        strategydomain.IntentEventType,
		Version:     strategydomain.IntentEventVersion,
		Venue:       "binance",
		Instrument:  "BTCUSDT",
		TsIngest:    1_700_000_001_200,
		Seq:         1,
		ContentType: envelope.ContentTypeProto,
		Payload:     intentPayload,
	}

	deadline := time.After(3 * time.Second)
	for {
		select {
		case out := <-stateCh:
			if out.Meta["source_execution_status"] != "filled" {
				continue
			}
			decoded, p := codec.DecodePayload(out.Type, out.Version, out.ContentType, out.Payload)
			if p != nil {
				t.Fatalf("decode portfolio payload: %v", p)
			}
			state, ok := decoded.(portfoliodomain.PortfolioStateV1)
			if !ok {
				t.Fatalf("decoded type=%T want PortfolioStateV1", decoded)
			}
			if state.Positions[0].Quantity != 1 {
				t.Fatalf("quantity=%v want=1", state.Positions[0].Quantity)
			}
			if len(state.Balances) < 2 || state.Balances[1].Locked != 0 {
				t.Fatalf("quote locked=%v want=0 after filled", state.Balances)
			}
			return
		case <-deadline:
			t.Fatal("timeout waiting filled lifecycle-derived portfolio state")
		}
	}
}
