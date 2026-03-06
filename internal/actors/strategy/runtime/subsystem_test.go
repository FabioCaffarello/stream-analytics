package strategyruntime_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/anthdm/hollywood/actor"
	strategyruntime "github.com/market-raccoon/internal/actors/strategy/runtime"
	marketmodel "github.com/market-raccoon/internal/core/marketmodel"
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

type captureIntentPublisher struct {
	ch chan envelope.Envelope
}

func (p *captureIntentPublisher) Publish(_ context.Context, env envelope.Envelope) *problem.Problem {
	p.ch <- env
	return nil
}

func TestStrategySubsystem_ConsumesSignalEventAndPublishesIntent(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	input := make(chan envelope.Envelope, 2)
	pub := &captureIntentPublisher{ch: make(chan envelope.Envelope, 2)}
	pid := e.Spawn(strategyruntime.NewSubsystemActor(strategyruntime.SubsystemConfig{
		EnvelopeCh:   input,
		Publisher:    pub,
		ReplicaID:    0,
		ReplicaCount: 1,
	}), "strategy-subsystem")
	defer func() {
		close(input)
		<-e.Poison(pid).Done()
	}()

	signalPayload, p := codec.EncodePayload(signalcore.EventType, signalcore.EventVersion, envelope.ContentTypeProto, marketmodel.SignalEvent{
		Type:       "liquidity_thinning",
		TsServer:   1_700_000_001_000,
		Scope:      marketmodel.SignalScopeStream,
		Venue:      "binance",
		Symbol:     "BTCUSDT",
		Severity:   "high",
		Confidence: 0.9,
		Features: []marketmodel.SignalFeature{
			{Key: "imbalance", Value: 0.8},
		},
		Explanation:    "thin book",
		SignalID:       "sig-1",
		RuleID:         "rule-1",
		RuleVersion:    "v1",
		InputWatermark: []marketmodel.SignalInputSeqRange{{Venue: "binance", Symbol: "BTCUSDT", SeqStart: 10, SeqEnd: 10}},
		CorrelationID:  "corr-1",
	})
	if p != nil {
		t.Fatalf("encode signal payload: %v", p)
	}

	input <- envelope.Envelope{
		Type:        signalcore.EventType,
		Version:     signalcore.EventVersion,
		Venue:       "binance",
		Instrument:  "BTCUSDT",
		TsIngest:    1_700_000_001_000,
		Seq:         10,
		ContentType: envelope.ContentTypeProto,
		Payload:     signalPayload,
	}

	select {
	case out := <-pub.ch:
		if out.Type != strategydomain.IntentEventType {
			t.Fatalf("type=%q want=%q", out.Type, strategydomain.IntentEventType)
		}
		decoded, p := codec.DecodePayload(out.Type, out.Version, out.ContentType, out.Payload)
		if p != nil {
			t.Fatalf("decode intent payload: %v", p)
		}
		intent, ok := decoded.(strategydomain.StrategyIntentV1)
		if !ok {
			t.Fatalf("decoded type=%T want StrategyIntentV1", decoded)
		}
		if intent.Scope.Venue != "binance" || intent.Scope.Symbol != "BTCUSDT" {
			t.Fatalf("scope=(%s,%s) want=(binance,BTCUSDT)", intent.Scope.Venue, intent.Scope.Symbol)
		}
		if intent.Side != strategydomain.IntentSideSell {
			t.Fatalf("side=%q want=%q", intent.Side, strategydomain.IntentSideSell)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting strategy.intent")
	}
}

func TestStrategySubsystem_DeprecatedCompositeInputIsIgnored(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	input := make(chan envelope.Envelope, 2)
	pub := &captureIntentPublisher{ch: make(chan envelope.Envelope, 1)}
	pid := e.Spawn(strategyruntime.NewSubsystemActor(strategyruntime.SubsystemConfig{
		EnvelopeCh:   input,
		Publisher:    pub,
		ReplicaID:    0,
		ReplicaCount: 1,
	}), "strategy-subsystem-ignore-composite")
	defer func() {
		close(input)
		<-e.Poison(pid).Done()
	}()

	input <- envelope.Envelope{
		Type:        "signal.composite",
		Version:     1,
		Venue:       "binance",
		Instrument:  "ETHUSDT",
		TsIngest:    1_700_000_001_200,
		Seq:         99,
		ContentType: envelope.ContentTypeJSON,
		Payload:     []byte(`{"kind":"absorption"}`),
	}

	select {
	case out := <-pub.ch:
		t.Fatalf("unexpected publish for deprecated composite input: %+v", out.Meta)
	case <-time.After(300 * time.Millisecond):
	}
}
