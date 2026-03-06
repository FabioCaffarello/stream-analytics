package executionruntime_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/anthdm/hollywood/actor"
	executionruntime "github.com/market-raccoon/internal/actors/execution/runtime"
	executiondomain "github.com/market-raccoon/internal/core/execution/domain"
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

type captureExecutionPublisher struct {
	ch chan envelope.Envelope
}

func (p *captureExecutionPublisher) Publish(_ context.Context, env envelope.Envelope) *problem.Problem {
	p.ch <- env
	return nil
}

func TestExecutionSubsystem_ConsumesIntentAndPublishesExecutionEvents(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	input := make(chan envelope.Envelope, 2)
	pub := &captureExecutionPublisher{ch: make(chan envelope.Envelope, 4)}
	pid := e.Spawn(executionruntime.NewSubsystemActor(executionruntime.SubsystemConfig{
		EnvelopeCh:   input,
		Publisher:    pub,
		ReplicaID:    0,
		ReplicaCount: 1,
	}), "execution-subsystem")
	defer func() {
		close(input)
		<-e.Poison(pid).Done()
	}()

	intentPayload, p := codec.EncodePayload(strategydomain.IntentEventType, strategydomain.IntentEventVersion, envelope.ContentTypeProto, strategydomain.StrategyIntentV1{
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
			ParentSignalIDs: []string{"sig-1"},
			PolicyHash:      "policy-1",
		},
	})
	if p != nil {
		t.Fatalf("encode strategy.intent payload: %v", p)
	}

	input <- envelope.Envelope{
		Type:        strategydomain.IntentEventType,
		Version:     strategydomain.IntentEventVersion,
		Venue:       "binance",
		Instrument:  "BTCUSDT",
		TsIngest:    1_700_000_001_000,
		Seq:         10,
		ContentType: envelope.ContentTypeProto,
		Payload:     intentPayload,
	}

	received := make([]envelope.Envelope, 0, 2)
	deadline := time.After(2 * time.Second)
	for len(received) < 2 {
		select {
		case env := <-pub.ch:
			received = append(received, env)
		case <-deadline:
			t.Fatalf("timeout waiting execution events, got=%d", len(received))
		}
	}

	if received[0].Type != executiondomain.EventType || received[1].Type != executiondomain.EventType {
		t.Fatalf("unexpected types=(%q,%q)", received[0].Type, received[1].Type)
	}
	if got := received[0].Meta["execution_boundary"]; got != "execution.adapter" {
		t.Fatalf("execution_boundary=%q want=execution.adapter", got)
	}
	if got := received[0].Meta["execution_adapter"]; got != "bootstrap.simulated" {
		t.Fatalf("execution_adapter=%q want=bootstrap.simulated", got)
	}
	if got := received[0].Meta["execution_mode"]; got != "bootstrap_simulated" {
		t.Fatalf("execution_mode=%q want=bootstrap_simulated", got)
	}
	if got := received[0].Meta["execution_reason_category"]; got != executiondomain.ReasonCategoryAccepted {
		t.Fatalf("execution_reason_category=%q want=%q", got, executiondomain.ReasonCategoryAccepted)
	}
	decoded, p := codec.DecodePayload(received[1].Type, received[1].Version, received[1].ContentType, received[1].Payload)
	if p != nil {
		t.Fatalf("decode execution payload: %v", p)
	}
	ev, ok := decoded.(executiondomain.ExecutionEventV1)
	if !ok {
		t.Fatalf("decoded type=%T want ExecutionEventV1", decoded)
	}
	if ev.Status != executiondomain.ExecutionStatusFilled {
		t.Fatalf("status=%q want=filled", ev.Status)
	}
	if ev.ExecutionSeq != 2 {
		t.Fatalf("execution_seq=%d want=2", ev.ExecutionSeq)
	}
}

func TestExecutionSubsystem_RejectsExpiredIntent(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	input := make(chan envelope.Envelope, 2)
	pub := &captureExecutionPublisher{ch: make(chan envelope.Envelope, 2)}
	pid := e.Spawn(executionruntime.NewSubsystemActor(executionruntime.SubsystemConfig{
		EnvelopeCh:   input,
		Publisher:    pub,
		ReplicaID:    0,
		ReplicaCount: 1,
	}), "execution-subsystem-expired")
	defer func() {
		close(input)
		<-e.Poison(pid).Done()
	}()

	intentPayload, p := codec.EncodePayload(strategydomain.IntentEventType, strategydomain.IntentEventVersion, envelope.ContentTypeProto, strategydomain.StrategyIntentV1{
		IntentID: "intent-expired",
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
			CorrelationID:   "corr-expired",
			ParentSignalIDs: []string{"sig-expired"},
			PolicyHash:      "policy-expired",
		},
	})
	if p != nil {
		t.Fatalf("encode strategy.intent payload: %v", p)
	}

	input <- envelope.Envelope{
		Type:        strategydomain.IntentEventType,
		Version:     strategydomain.IntentEventVersion,
		Venue:       "binance",
		Instrument:  "ETHUSDT",
		TsIngest:    1_700_000_001_100,
		Seq:         20,
		ContentType: envelope.ContentTypeProto,
		Payload:     intentPayload,
	}

	select {
	case env := <-pub.ch:
		decoded, p := codec.DecodePayload(env.Type, env.Version, env.ContentType, env.Payload)
		if p != nil {
			t.Fatalf("decode execution payload: %v", p)
		}
		ev, ok := decoded.(executiondomain.ExecutionEventV1)
		if !ok {
			t.Fatalf("decoded type=%T want ExecutionEventV1", decoded)
		}
		if ev.Status != executiondomain.ExecutionStatusRejected {
			t.Fatalf("status=%q want=rejected", ev.Status)
		}
		if ev.Reason != executiondomain.ReasonGovernanceTTLExpired {
			t.Fatalf("reason=%q want=%q", ev.Reason, executiondomain.ReasonGovernanceTTLExpired)
		}
		if got := env.Meta["execution_reason_category"]; got != executiondomain.ReasonCategoryGovernanceDenied {
			t.Fatalf("execution_reason_category=%q want=%q", got, executiondomain.ReasonCategoryGovernanceDenied)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting rejected execution event")
	}
}
