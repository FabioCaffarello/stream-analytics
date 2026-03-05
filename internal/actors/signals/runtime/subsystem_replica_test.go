package signalsruntime_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/anthdm/hollywood/actor"
	signalsruntime "github.com/market-raccoon/internal/actors/signals/runtime"
	evidencedomain "github.com/market-raccoon/internal/core/evidence/domain"
	signalsapp "github.com/market-raccoon/internal/core/signals/app"
	signalsdomain "github.com/market-raccoon/internal/core/signals/domain"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/problem"
)

type replicaPublisher struct {
	ch chan envelope.Envelope
}

func (p *replicaPublisher) Publish(_ context.Context, env envelope.Envelope) *problem.Problem {
	p.ch <- env
	return nil
}

func TestSignalsSubsystem_ReplicaCount2_NoDoubleEmit_WithDuplicateInput(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	inA := make(chan envelope.Envelope, 8)
	inB := make(chan envelope.Envelope, 8)
	pubA := &replicaPublisher{ch: make(chan envelope.Envelope, 8)}
	pubB := &replicaPublisher{ch: make(chan envelope.Envelope, 8)}

	policy := signalsapp.DefaultRateLimitPolicy()
	policy.GlobalRateLimitMin = 10_000

	pidA := e.Spawn(signalsruntime.NewSubsystemActor(signalsruntime.SubsystemConfig{
		EnvelopeCh:   inA,
		Composer:     signalsapp.NewSignalComposer(signalsapp.DefaultComposePolicy()),
		Limiter:      signalsapp.NewSignalRateLimiter(policy),
		Publisher:    pubA,
		ReplicaID:    0,
		ReplicaCount: 2,
	}), "strategist-replica-a")
	pidB := e.Spawn(signalsruntime.NewSubsystemActor(signalsruntime.SubsystemConfig{
		EnvelopeCh:   inB,
		Composer:     signalsapp.NewSignalComposer(signalsapp.DefaultComposePolicy()),
		Limiter:      signalsapp.NewSignalRateLimiter(policy),
		Publisher:    pubB,
		ReplicaID:    1,
		ReplicaCount: 2,
	}), "strategist-replica-b")
	defer func() {
		close(inA)
		close(inB)
		<-e.Poison(pidA).Done()
		<-e.Poison(pidB).Done()
	}()

	regime := makeStrategistRegimeEnvelope(t, 10, 1_700_000_060_000)
	micro := makeStrategistMicroEnvelope(t, 11, 1_700_000_060_000)

	inA <- regime
	inB <- regime
	inA <- micro
	inB <- micro
	// Replay duplicate input on both replicas.
	inA <- micro
	inB <- micro

	total := 0
	a := 0
	b := 0
	deadline := time.After(2 * time.Second)
collect:
	for {
		select {
		case <-pubA.ch:
			total++
			a++
		case <-pubB.ch:
			total++
			b++
		case <-deadline:
			break collect
		}
	}
	if total != 1 {
		t.Fatalf("total emissions=%d want=1", total)
	}
	if a > 0 && b > 0 {
		t.Fatalf("double emit across replicas: a=%d b=%d", a, b)
	}
}

func TestSignalsSubsystem_WatermarkOutOfOrderDropped(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	input := make(chan envelope.Envelope, 8)
	pub := &replicaPublisher{ch: make(chan envelope.Envelope, 8)}
	policy := signalsapp.DefaultRateLimitPolicy()
	policy.GlobalRateLimitMin = 10_000
	pid := e.Spawn(signalsruntime.NewSubsystemActor(signalsruntime.SubsystemConfig{
		EnvelopeCh:   input,
		Composer:     signalsapp.NewSignalComposer(signalsapp.DefaultComposePolicy()),
		Limiter:      signalsapp.NewSignalRateLimiter(policy),
		Publisher:    pub,
		ReplicaID:    0,
		ReplicaCount: 1,
	}), "strategist-watermark")
	defer func() {
		close(input)
		<-e.Poison(pid).Done()
	}()

	input <- makeStrategistRegimeEnvelope(t, 10, 1_700_000_060_000)
	input <- makeStrategistMicroEnvelope(t, 11, 1_700_000_060_000)
	input <- makeStrategistMicroEnvelope(t, 10, 1_700_000_059_000) // seq + watermark regressed

	first := waitStrategistSignal(t, pub.ch, 2*time.Second)
	if first.Type != signalsdomain.CompositeSignalType {
		t.Fatalf("first signal type=%q", first.Type)
	}
	select {
	case got := <-pub.ch:
		t.Fatalf("unexpected second publish on watermark regression seq=%d", got.Seq)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestSignalsSubsystem_ReplayDeterministic(t *testing.T) {
	first := runStrategistReplayScenario(t)
	second := runStrategistReplayScenario(t)

	if first.Seq != second.Seq {
		t.Fatalf("seq mismatch first=%d second=%d", first.Seq, second.Seq)
	}
	if !bytes.Equal(first.Payload, second.Payload) {
		t.Fatal("payload mismatch between deterministic replays")
	}
	if first.Meta["kind"] != second.Meta["kind"] {
		t.Fatalf("kind mismatch first=%q second=%q", first.Meta["kind"], second.Meta["kind"])
	}
}

func runStrategistReplayScenario(t *testing.T) envelope.Envelope {
	t.Helper()
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	input := make(chan envelope.Envelope, 4)
	pub := &replicaPublisher{ch: make(chan envelope.Envelope, 2)}
	policy := signalsapp.DefaultRateLimitPolicy()
	policy.GlobalRateLimitMin = 10_000
	pid := e.Spawn(signalsruntime.NewSubsystemActor(signalsruntime.SubsystemConfig{
		EnvelopeCh:   input,
		Composer:     signalsapp.NewSignalComposer(signalsapp.DefaultComposePolicy()),
		Limiter:      signalsapp.NewSignalRateLimiter(policy),
		Publisher:    pub,
		ReplicaID:    0,
		ReplicaCount: 1,
	}), "strategist-replay")
	defer func() {
		close(input)
		<-e.Poison(pid).Done()
	}()

	input <- makeStrategistRegimeEnvelope(t, 10, 1_700_000_060_000)
	input <- makeStrategistMicroEnvelope(t, 11, 1_700_000_060_000)
	return waitStrategistSignal(t, pub.ch, 2*time.Second)
}

func waitStrategistSignal(t *testing.T, ch <-chan envelope.Envelope, timeout time.Duration) envelope.Envelope {
	t.Helper()
	select {
	case env := <-ch:
		return env
	case <-time.After(timeout):
		t.Fatal("timeout waiting strategist signal")
		return envelope.Envelope{}
	}
}

func makeStrategistRegimeEnvelope(t *testing.T, seq, windowEnd int64) envelope.Envelope {
	t.Helper()
	payload, p := codec.EncodePayload(evidencedomain.RegimeEvidenceType, evidencedomain.RegimeEvidenceVersion, envelope.ContentTypeJSON, evidencedomain.RegimeSignal{
		Venue:       "binance",
		Instrument:  "BTCUSDT",
		Timeframe:   "1m",
		Kind:        evidencedomain.RegimeTrending,
		Strength:    0.8,
		Confidence:  0.9,
		WindowStart: windowEnd - 60_000,
		WindowEnd:   windowEnd,
		Features: []evidencedomain.FeaturePair{{
			Name:  "slope_ratio",
			Value: 0.003,
		}},
	})
	if p != nil {
		t.Fatalf("encode regime payload: %v", p)
	}
	return envelope.Envelope{
		Type:        evidencedomain.RegimeEvidenceType,
		Version:     evidencedomain.RegimeEvidenceVersion,
		Venue:       "binance",
		Instrument:  "BTCUSDT",
		TsIngest:    windowEnd,
		Seq:         seq,
		ContentType: envelope.ContentTypeJSON,
		Payload:     payload,
		Meta: map[string]string{
			"timeframe": "1m",
		},
	}
}

func makeStrategistMicroEnvelope(t *testing.T, seq, ts int64) envelope.Envelope {
	t.Helper()
	payload, p := codec.EncodePayload(evidencedomain.MicrostructureEvidenceType, evidencedomain.MicrostructureEvidenceVersion, envelope.ContentTypeJSON, evidencedomain.EvidenceEvent{
		Type:       evidencedomain.Absorption,
		TsServer:   ts,
		Venue:      "binance",
		Symbol:     "BTCUSDT",
		StreamID:   "binance/BTCUSDT/trade",
		Seq:        seq,
		Severity:   evidencedomain.SeverityMedium,
		Confidence: 0.6,
		Features: []evidencedomain.EvidenceFeature{{
			Key:   "volume_ratio",
			Value: 2.1,
		}},
		Explanation: "absorption detected",
		RuleVersion: evidencedomain.RuleVersionV0,
		InputWatermark: evidencedomain.InputWatermark{
			SeqStart: seq,
			SeqEnd:   seq,
		},
	})
	if p != nil {
		t.Fatalf("encode micro payload: %v", p)
	}
	return envelope.Envelope{
		Type:        evidencedomain.MicrostructureEvidenceType,
		Version:     evidencedomain.MicrostructureEvidenceVersion,
		Venue:       "binance",
		Instrument:  "BTCUSDT",
		TsIngest:    ts,
		Seq:         seq,
		ContentType: envelope.ContentTypeJSON,
		Payload:     payload,
		Meta: map[string]string{
			"timeframe": "1m",
		},
	}
}
