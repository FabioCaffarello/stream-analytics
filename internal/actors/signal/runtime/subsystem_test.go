package signalruntime_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	actorruntime "github.com/market-raccoon/internal/actors/runtime"
	signalruntime "github.com/market-raccoon/internal/actors/signal/runtime"
	evidencedomain "github.com/market-raccoon/internal/core/evidence/domain"
	signalcore "github.com/market-raccoon/internal/core/signal"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/contracts"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/problem"
)

type capturePublisher struct {
	ch chan envelope.Envelope
}

func (p *capturePublisher) Publish(_ context.Context, env envelope.Envelope) *problem.Problem {
	p.ch <- env
	return nil
}

func TestSignalSubsystem_OwnerOnlyEmitsAcrossReplicas_WithReplayDuplicates(t *testing.T) {
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		t.Fatalf("bootstrap codec registry: %v", p)
	}

	e, err := actorruntime.NewDefaultEngine()
	if err != nil {
		t.Fatalf("engine: %v", err)
	}

	inputA := make(chan envelope.Envelope, 8)
	inputB := make(chan envelope.Envelope, 8)
	pubA := &capturePublisher{ch: make(chan envelope.Envelope, 8)}
	pubB := &capturePublisher{ch: make(chan envelope.Envelope, 8)}
	cfg := signalcore.DefaultEngineConfig()
	engineA := signalcore.NewSignalEngine(cfg, nil, signalcore.BuildV0Rules(cfg.Rules)...)
	engineB := signalcore.NewSignalEngine(cfg, nil, signalcore.BuildV0Rules(cfg.Rules)...)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	pidA := e.Spawn(signalruntime.NewSubsystemActor(signalruntime.SubsystemConfig{
		Logger:       logger,
		EnvelopeCh:   inputA,
		Engine:       engineA,
		Publisher:    pubA,
		ReplicaID:    0,
		ReplicaCount: 2,
	}), "signal-owner-0")
	pidB := e.Spawn(signalruntime.NewSubsystemActor(signalruntime.SubsystemConfig{
		Logger:       logger,
		EnvelopeCh:   inputB,
		Engine:       engineB,
		Publisher:    pubB,
		ReplicaID:    1,
		ReplicaCount: 2,
	}), "signal-owner-1")

	evidenceA := makeEvidenceEnvelope(t, "spread_explosion", 1000, 1)
	evidenceB := makeEvidenceEnvelope(t, "liquidity_thinning", 1200, 2)
	inputA <- evidenceA
	inputB <- evidenceA
	inputA <- evidenceB
	inputB <- evidenceB
	// Simulate reconnect/resync replay: both replicas receive identical evidence again.
	inputA <- evidenceA
	inputB <- evidenceA
	inputA <- evidenceB
	inputB <- evidenceB

	total := 0
	pubACount := 0
	pubBCount := 0
	deadline := time.After(1 * time.Second)
collect:
	for {
		select {
		case <-pubA.ch:
			total++
			pubACount++
		case <-pubB.ch:
			total++
			pubBCount++
		case <-deadline:
			break collect
		}
	}
	if total != 1 {
		t.Fatalf("total emissions=%d want=1", total)
	}
	if pubACount > 0 && pubBCount > 0 {
		t.Fatalf("owner-only violated: both replicas emitted (A=%d B=%d)", pubACount, pubBCount)
	}

	close(inputA)
	close(inputB)
	e.Poison(pidA)
	e.Poison(pidB)
}

func makeEvidenceEnvelope(t *testing.T, kind string, ts, seq int64) envelope.Envelope {
	t.Helper()
	ev := evidencedomain.EvidenceEvent{
		Type:        evidencedomain.EvidenceType(kind),
		TsServer:    ts,
		Venue:       "binance",
		Symbol:      "BTC-USDT",
		StreamID:    "binance/BTC-USDT/evidence",
		Seq:         seq,
		Severity:    evidencedomain.SeverityHigh,
		Confidence:  0.9,
		Features:    []evidencedomain.EvidenceFeature{{Key: "f", Value: 1}},
		Explanation: "fixture",
		RuleVersion: "v0",
		InputWatermark: evidencedomain.InputWatermark{
			SeqStart: seq,
			SeqEnd:   seq,
		},
	}
	payload, p := codec.EncodePayload(evidencedomain.MicrostructureEvidenceType, 1, envelope.ContentTypeJSON, ev)
	if p != nil {
		t.Fatalf("encode evidence: %v", p)
	}
	return envelope.Envelope{
		Type:           evidencedomain.MicrostructureEvidenceType,
		Version:        1,
		Venue:          "binance",
		Instrument:     "BTC-USDT",
		TsIngest:       ts,
		Seq:            seq,
		IdempotencyKey: "k",
		ContentType:    envelope.ContentTypeJSON,
		Payload:        payload,
	}
}
