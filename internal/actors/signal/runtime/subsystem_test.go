package signalruntime_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	actorruntime "github.com/market-raccoon/internal/actors/runtime"
	signalruntime "github.com/market-raccoon/internal/actors/signal/runtime"
	evidencedomain "github.com/market-raccoon/internal/core/evidence/domain"
	marketmodel "github.com/market-raccoon/internal/core/marketmodel"
	signalcore "github.com/market-raccoon/internal/core/signal"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/contracts"
	"github.com/market-raccoon/internal/shared/envelope"
	sharedhash "github.com/market-raccoon/internal/shared/hash"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/ownership"
	"github.com/market-raccoon/internal/shared/problem"
	"github.com/prometheus/client_golang/prometheus/testutil"
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

func TestSignalSubsystem_OwnerRejectIncrementsDropMetric(t *testing.T) {
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		t.Fatalf("bootstrap codec registry: %v", p)
	}

	e, err := actorruntime.NewDefaultEngine()
	if err != nil {
		t.Fatalf("engine: %v", err)
	}

	input := make(chan envelope.Envelope, 2)
	pub := &capturePublisher{ch: make(chan envelope.Envelope, 1)}
	cfg := signalcore.DefaultEngineConfig()
	engine := signalcore.NewSignalEngine(cfg, nil, signalcore.BuildV0Rules(cfg.Rules)...)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	key, p := marketmodel.NewStreamKey("binance", "BTC-USDT", marketmodel.ChannelEvidence)
	if p != nil {
		t.Fatalf("stream key: %v", p)
	}
	ownerID := ownership.OwnerReplica(ownership.SubsystemSignals, ownership.StreamKey{
		Venue:      string(key.Venue),
		Instrument: string(key.Symbol),
		Channel:    string(key.Channel),
	}, 2)
	replicaID := 0
	if ownerID == 0 {
		replicaID = 1
	}
	before := testutil.ToFloat64(metrics.SignalDropTotal.WithLabelValues("owner_reject"))
	pid := e.Spawn(signalruntime.NewSubsystemActor(signalruntime.SubsystemConfig{
		Logger:       logger,
		EnvelopeCh:   input,
		Engine:       engine,
		Publisher:    pub,
		ReplicaID:    replicaID,
		ReplicaCount: 2,
	}), "signal-owner-reject")
	defer func() {
		close(input)
		e.Poison(pid)
	}()

	input <- makeEvidenceEnvelope(t, "spread_explosion", 1000, 1)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		after := testutil.ToFloat64(metrics.SignalDropTotal.WithLabelValues("owner_reject"))
		if after >= before+1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	after := testutil.ToFloat64(metrics.SignalDropTotal.WithLabelValues("owner_reject"))
	if after < before+1 {
		t.Fatalf("signal_drop_total{reason=owner_reject}=%.0f want at least %.0f", after, before+1)
	}
	select {
	case env := <-pub.ch:
		t.Fatalf("unexpected owner-rejected emission: kind=%s seq=%d", env.Meta["kind"], env.Seq)
	default:
	}
}

func TestSignalSubsystem_WatermarkRegressionDropsAsOutOfOrder(t *testing.T) {
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		t.Fatalf("bootstrap codec registry: %v", p)
	}

	e, err := actorruntime.NewDefaultEngine()
	if err != nil {
		t.Fatalf("engine: %v", err)
	}

	input := make(chan envelope.Envelope, 4)
	pub := &capturePublisher{ch: make(chan envelope.Envelope, 4)}
	cfg := signalcore.DefaultEngineConfig()
	engine := signalcore.NewSignalEngine(cfg, nil, signalcore.BuildV0Rules(cfg.Rules)...)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	pid := e.Spawn(signalruntime.NewSubsystemActor(signalruntime.SubsystemConfig{
		Logger:       logger,
		EnvelopeCh:   input,
		Engine:       engine,
		Publisher:    pub,
		ReplicaID:    0,
		ReplicaCount: 1,
	}), "signal-watermark-regression")
	defer func() {
		close(input)
		e.Poison(pid)
	}()

	before := testutil.ToFloat64(metrics.SignalDropTotal.WithLabelValues("out_of_order"))
	input <- makeEvidenceEnvelope(t, "spread_explosion", 1_000, 2)
	input <- makeEvidenceEnvelope(t, "spread_explosion", 900, 1) // seq regressed + watermark regressed

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		after := testutil.ToFloat64(metrics.SignalDropTotal.WithLabelValues("out_of_order"))
		if after >= before+1 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	after := testutil.ToFloat64(metrics.SignalDropTotal.WithLabelValues("out_of_order"))
	t.Fatalf("signal_drop_total{reason=out_of_order}=%.0f want at least %.0f", after, before+1)
}

func TestSubsystem_LiquidityEvidenceEnvelope_EmitsSignalEvent(t *testing.T) {
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		t.Fatalf("bootstrap codec registry: %v", p)
	}
	e, err := actorruntime.NewDefaultEngine()
	if err != nil {
		t.Fatalf("engine: %v", err)
	}

	input := make(chan envelope.Envelope, 8)
	pub := &capturePublisher{ch: make(chan envelope.Envelope, 8)}
	cfg := signalcore.DefaultEngineConfig()
	engine := signalcore.NewSignalEngine(cfg, nil, signalcore.BuildV0Rules(cfg.Rules)...)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	pid := e.Spawn(signalruntime.NewSubsystemActor(signalruntime.SubsystemConfig{
		Logger:       logger,
		EnvelopeCh:   input,
		Engine:       engine,
		Publisher:    pub,
		ReplicaID:    0,
		ReplicaCount: 1,
	}), "signal-lel-emit")
	defer func() {
		close(input)
		e.Poison(pid)
	}()

	input <- makeLiquidityEvidenceEnvelope(t, "SWEEP", 1000, 1)
	input <- makeLiquidityEvidenceEnvelope(t, "THINNING", 1200, 2)
	input <- makeLiquidityEvidenceEnvelope(t, "SPREAD_REGIME", 1300, 3)

	signalEnv := waitForSignalEnvelope(t, pub.ch, "regime_change", 2*time.Second)
	if signalEnv.Type != signalcore.EventType {
		t.Fatalf("envelope type=%q want=%q", signalEnv.Type, signalcore.EventType)
	}
	if signalEnv.Version != signalcore.EventVersion {
		t.Fatalf("envelope version=%d want=%d", signalEnv.Version, signalcore.EventVersion)
	}
	wantIdem := sharedhash.IdempotencyKeyFast(signalEnv.Venue, signalEnv.Instrument, signalcore.EventType, signalEnv.Seq)
	if signalEnv.IdempotencyKey != wantIdem {
		t.Fatalf("idempotency_key=%q want=%q", signalEnv.IdempotencyKey, wantIdem)
	}
	if got := signalEnv.Meta["intent_id"]; got == "" {
		t.Fatal("intent_id must not be empty")
	}
	decoded, p := codec.DecodePayload(signalcore.EventType, signalcore.EventVersion, signalEnv.ContentType, signalEnv.Payload)
	if p != nil {
		t.Fatalf("decode signal payload: %v", p)
	}
	ev, ok := decoded.(marketmodel.SignalEvent)
	if !ok {
		t.Fatalf("decoded type=%T want marketmodel.SignalEvent", decoded)
	}
	if ev.Type != "regime_change" {
		t.Fatalf("signal type=%q want=regime_change", ev.Type)
	}
	if ev.RuleVersion != "v1" {
		t.Fatalf("signal rule_version=%q want=v1", ev.RuleVersion)
	}
}

func TestSubsystem_LiquidityEvidenceEnvelope_ReplayDeterministicNoDoubleEmit(t *testing.T) {
	first := runLELRegimeScenario(t)
	second := runLELRegimeScenario(t)

	if first.Seq != second.Seq {
		t.Fatalf("seq mismatch first=%d second=%d", first.Seq, second.Seq)
	}
	if first.IdempotencyKey != second.IdempotencyKey {
		t.Fatalf("idempotency mismatch first=%q second=%q", first.IdempotencyKey, second.IdempotencyKey)
	}
	if first.Meta["intent_id"] == "" {
		t.Fatal("first intent_id must not be empty")
	}
	if first.Meta["intent_id"] != second.Meta["intent_id"] {
		t.Fatalf("intent_id mismatch first=%q second=%q", first.Meta["intent_id"], second.Meta["intent_id"])
	}
	if !bytes.Equal(first.Payload, second.Payload) {
		t.Fatal("payload bytes differ between identical runs")
	}
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

func makeLiquidityEvidenceEnvelope(t *testing.T, evidenceType string, ts, seq int64) envelope.Envelope {
	t.Helper()

	ev := contracts.LiquidityEvidenceV1{
		EvidenceType: evidenceType,
		TsIngestMs:   ts,
		Venue:        "binance",
		Symbol:       "BTC-USDT",
		WindowMs:     3000,
		Severity:     "high",
		Confidence:   0.9,
		Metrics: []contracts.LiquidityEvidenceMetric{
			{Key: "pressure", Value: 1.2},
			{Key: "spread_bps", Value: 0.8},
		},
		Explain:  []string{"fixture"},
		Version:  1,
		StreamID: "BINANCE|BTCUSDT",
		Seq:      seq,
		Watermark: contracts.LiquidityInputWatermark{
			SeqStart: seq,
			SeqEnd:   seq,
		},
	}
	payload, p := codec.EncodePayload(evidencedomain.LiquidityEvidenceEventType, int(evidencedomain.LiquidityEvidenceVersion), envelope.ContentTypeJSON, ev)
	if p != nil {
		t.Fatalf("encode liquidity evidence: %v", p)
	}
	return envelope.Envelope{
		Type:           evidencedomain.LiquidityEvidenceEventType,
		Version:        int(evidencedomain.LiquidityEvidenceVersion),
		Venue:          "binance",
		Instrument:     "BTC-USDT",
		TsIngest:       ts,
		Seq:            seq,
		IdempotencyKey: "lel-k",
		ContentType:    envelope.ContentTypeJSON,
		Payload:        payload,
	}
}

func waitForSignalEnvelope(
	t *testing.T,
	ch <-chan envelope.Envelope,
	wantKind string,
	timeout time.Duration,
) envelope.Envelope {
	t.Helper()

	deadline := time.After(timeout)
	for {
		select {
		case env := <-ch:
			if env.Meta["kind"] == wantKind {
				return env
			}
		case <-deadline:
			t.Fatalf("timeout waiting signal envelope kind=%s", wantKind)
		}
	}
}

func runLELRegimeScenario(t *testing.T) envelope.Envelope {
	t.Helper()
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		t.Fatalf("bootstrap codec registry: %v", p)
	}
	e, err := actorruntime.NewDefaultEngine()
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	input := make(chan envelope.Envelope, 8)
	pub := &capturePublisher{ch: make(chan envelope.Envelope, 8)}
	cfg := signalcore.DefaultEngineConfig()
	engine := signalcore.NewSignalEngine(cfg, nil, signalcore.BuildV0Rules(cfg.Rules)...)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	pid := e.Spawn(signalruntime.NewSubsystemActor(signalruntime.SubsystemConfig{
		Logger:       logger,
		EnvelopeCh:   input,
		Engine:       engine,
		Publisher:    pub,
		ReplicaID:    0,
		ReplicaCount: 1,
	}), "signal-lel-replay")
	defer func() {
		close(input)
		e.Poison(pid)
	}()

	input <- makeLiquidityEvidenceEnvelope(t, "THINNING", 1200, 2)
	input <- makeLiquidityEvidenceEnvelope(t, "SPREAD_REGIME", 1300, 3)
	out := waitForSignalEnvelope(t, pub.ch, "liquidity_collapse", 2*time.Second)

	// Replay duplicate with same sequence must not produce another emission.
	input <- makeLiquidityEvidenceEnvelope(t, "SPREAD_REGIME", 1300, 3)
	select {
	case env := <-pub.ch:
		t.Fatalf("unexpected extra emission after replay duplicate: kind=%s seq=%d", env.Meta["kind"], env.Seq)
	case <-time.After(250 * time.Millisecond):
	}
	return out
}
