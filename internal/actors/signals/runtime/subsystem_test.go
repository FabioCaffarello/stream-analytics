package signalsruntime_test

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/anthdm/hollywood/actor"
	signalsruntime "github.com/market-raccoon/internal/actors/signals/runtime"
	"github.com/market-raccoon/internal/contracts"
	evidencedomain "github.com/market-raccoon/internal/core/evidence/domain"
	signalsapp "github.com/market-raccoon/internal/core/signals/app"
	signalsdomain "github.com/market-raccoon/internal/core/signals/domain"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/problem"
)

func init() {
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		panic(fmt.Sprintf("BootstrapPayloadCodecRegistry: %v", p))
	}
}

type spySignalPublisher struct {
	ch chan envelope.Envelope
}

func (s *spySignalPublisher) Publish(_ context.Context, env envelope.Envelope) *problem.Problem {
	s.ch <- env
	return nil
}

func TestSignalsSubsystem_ComposesAndPublishesSignal(t *testing.T) {
	envCh := make(chan envelope.Envelope, 2)
	publishedCh := make(chan envelope.Envelope, 1)

	composer := signalsapp.NewSignalComposer(signalsapp.DefaultComposePolicy())
	policy := signalsapp.DefaultRateLimitPolicy()
	policy.GlobalRateLimitMin = 1000
	limiter := signalsapp.NewSignalRateLimiter(policy)

	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	pid := e.Spawn(signalsruntime.NewSubsystemActor(signalsruntime.SubsystemConfig{
		EnvelopeCh: envCh,
		Composer:   composer,
		Limiter:    limiter,
		Publisher: &spySignalPublisher{
			ch: publishedCh,
		},
	}), "signals", actor.WithID("signals"))

	regimePayload, p := codec.EncodePayload(evidencedomain.RegimeEvidenceType, evidencedomain.RegimeEvidenceVersion, envelope.ContentTypeJSON, evidencedomain.RegimeSignal{
		Venue:       "binance",
		Instrument:  "BTCUSDT",
		Timeframe:   "1m",
		Kind:        evidencedomain.RegimeTrending,
		Strength:    0.8,
		Confidence:  0.9,
		WindowStart: 1700000000000,
		WindowEnd:   1700000060000,
		Features: []evidencedomain.FeaturePair{{
			Name:  "slope_ratio",
			Value: 0.003,
		}},
	})
	if p != nil {
		t.Fatalf("encode regime payload: %v", p)
	}

	microPayload, p := codec.EncodePayload(evidencedomain.MicrostructureEvidenceType, evidencedomain.MicrostructureEvidenceVersion, envelope.ContentTypeJSON, evidencedomain.EvidenceEvent{
		Type:       evidencedomain.Absorption,
		TsServer:   1700000060000,
		Venue:      "binance",
		Symbol:     "BTCUSDT",
		StreamID:   "binance/BTCUSDT/trade",
		Seq:        77,
		Severity:   evidencedomain.SeverityMedium,
		Confidence: 0.6,
		Features: []evidencedomain.EvidenceFeature{
			{Key: "volume_ratio", Value: 2.1},
		},
		Explanation: "absorption detected",
		RuleVersion: evidencedomain.RuleVersionV0,
		InputWatermark: evidencedomain.InputWatermark{
			SeqStart: 77,
			SeqEnd:   77,
		},
	})
	if p != nil {
		t.Fatalf("encode micro payload: %v", p)
	}

	envCh <- envelope.Envelope{
		Type:        evidencedomain.RegimeEvidenceType,
		Version:     evidencedomain.RegimeEvidenceVersion,
		Venue:       "binance",
		Instrument:  "BTCUSDT",
		TsIngest:    1700000060000,
		Seq:         10,
		ContentType: envelope.ContentTypeJSON,
		Payload:     regimePayload,
	}
	envCh <- envelope.Envelope{
		Type:        evidencedomain.MicrostructureEvidenceType,
		Version:     evidencedomain.MicrostructureEvidenceVersion,
		Venue:       "binance",
		Instrument:  "BTCUSDT",
		TsIngest:    1700000060000,
		Seq:         11,
		ContentType: envelope.ContentTypeJSON,
		Payload:     microPayload,
	}

	select {
	case out := <-publishedCh:
		if out.Type != signalsdomain.CompositeSignalType {
			t.Fatalf("type=%q want=%q", out.Type, signalsdomain.CompositeSignalType)
		}
		decoded, p := codec.DecodePayload(out.Type, out.Version, out.ContentType, out.Payload)
		if p != nil {
			t.Fatalf("decode signal payload: %v", p)
		}
		signal, ok := decoded.(signalsdomain.CompositeSignalV1)
		if !ok {
			t.Fatalf("decoded type=%T want CompositeSignalV1", decoded)
		}
		if math.Abs(signal.Confidence-0.696) > 1e-12 {
			t.Fatalf("confidence=%0.12f want=0.696000000000", signal.Confidence)
		}
		if signal.RegimeKind != "trending" {
			t.Fatalf("regime_kind=%q want=trending", signal.RegimeKind)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for composite signal publish")
	}

	<-e.Poison(pid).Done()
}
