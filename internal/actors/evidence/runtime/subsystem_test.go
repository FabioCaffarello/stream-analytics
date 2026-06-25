package evidenceruntime_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/anthdm/hollywood/actor"
	evidenceruntime "github.com/market-raccoon/internal/actors/evidence/runtime"
	"github.com/market-raccoon/internal/contracts"
	evidenceapp "github.com/market-raccoon/internal/core/evidence/app"
	"github.com/market-raccoon/internal/core/evidence/domain"
	marketmodel "github.com/market-raccoon/internal/core/marketmodel"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/envelope"
	sharedhash "github.com/market-raccoon/internal/shared/hash"
	"github.com/market-raccoon/internal/shared/naming"
	"github.com/market-raccoon/internal/shared/problem"
)

func init() {
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		panic(fmt.Sprintf("BootstrapPayloadCodecRegistry: %v", p))
	}
}

type spyPublisher struct {
	ch chan envelope.Envelope
}

func (s *spyPublisher) Publish(_ context.Context, env envelope.Envelope) *problem.Problem {
	s.ch <- env
	return nil
}

type bookRule struct{}

func (bookRule) Name() string { return "book_rule" }
func (bookRule) StreamCount() int {
	return 0
}
func (bookRule) Reset()               {}
func (bookRule) EvictStream(_ string) {}
func (bookRule) OnEvent(event domain.RuleEvent) []domain.EvidenceEvent {
	if event.Kind != domain.EventKindBook {
		return nil
	}
	return []domain.EvidenceEvent{{
		Type:       domain.Sweep,
		TsServer:   event.TsServer,
		Venue:      event.Venue,
		Symbol:     event.Symbol,
		StreamID:   event.StreamID,
		Seq:        event.Seq,
		Severity:   domain.SeverityMedium,
		Confidence: 0.9,
		Features: []domain.EvidenceFeature{
			{Key: "x", Value: 1},
		},
		Explanation: "book test evidence",
		RuleVersion: domain.RuleVersionV0,
		InputWatermark: domain.InputWatermark{
			SeqStart: event.Seq,
			SeqEnd:   event.Seq,
		},
	}}
}

type fixedRegimeDetector struct{}

func (fixedRegimeDetector) Name() string { return "fixed_regime" }
func (fixedRegimeDetector) Detect(key domain.RegimeStoreKey, candles []domain.RegimeCandleSample) (domain.RegimeSignal, bool) {
	if len(candles) == 0 {
		return domain.RegimeSignal{}, false
	}
	last := candles[len(candles)-1]
	return domain.RegimeSignal{
		Venue:       key.Venue,
		Instrument:  key.Instrument,
		Timeframe:   key.Timeframe,
		Kind:        domain.RegimeTrending,
		Strength:    0.8,
		Confidence:  0.9,
		WindowStart: last.WindowStart,
		WindowEnd:   last.WindowEnd,
		Features: []domain.FeaturePair{
			{Name: "slope_ratio", Value: 0.003},
		},
	}, true
}

func TestEvidenceSubsystem_BookDeltaPublishesEvidenceEnvelope(t *testing.T) {
	envCh := make(chan envelope.Envelope, 1)
	publishedCh := make(chan envelope.Envelope, 1)

	engine := evidenceapp.NewEvidenceEngine(evidenceapp.EngineConfig{
		MaxStreamsPerRule: 16,
		MaxStreamsGlobal:  16,
		StreamTTLMillis:   int64((10 * time.Minute) / time.Millisecond),
		BufferCapPerKind:  1000,
		DecayHalfLife:     1 * time.Minute,
	}, bookRule{})

	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	pid := e.Spawn(evidenceruntime.NewSubsystemActor(evidenceruntime.SubsystemConfig{
		EnvelopeCh: envCh,
		Engine:     engine,
		Publisher: &spyPublisher{
			ch: publishedCh,
		},
	}), "evidence", actor.WithID("evidence"))

	payload, p := codec.EncodePayload("marketdata.bookdelta", 1, envelope.ContentTypeJSON, marketmodel.BookDelta{
		Bids: []marketmodel.Level{
			{Price: 100.0, Size: 2.0},
		},
		Asks: []marketmodel.Level{
			{Price: 101.0, Size: 2.0},
		},
		FirstID:   1,
		FinalID:   1,
		PrevFinal: 0,
		Timestamp: 1_000,
	})
	if p != nil {
		t.Fatalf("encode bookdelta payload: %v", p)
	}

	envCh <- envelope.Envelope{
		Type:           "marketdata.bookdelta",
		Version:        1,
		Venue:          "binance",
		Instrument:     "BTCUSDT",
		Seq:            7,
		TsIngest:       1_000,
		ContentType:    envelope.ContentTypeJSON,
		IdempotencyKey: "test-bookdelta-7",
		Payload:        payload,
	}

	select {
	case out := <-publishedCh:
		if out.Type != domain.MicrostructureEvidenceType {
			t.Fatalf("published type=%q want=%q", out.Type, domain.MicrostructureEvidenceType)
		}
		decoded, p := codec.DecodePayload(out.Type, out.Version, out.ContentType, out.Payload)
		if p != nil {
			t.Fatalf("decode evidence payload: %v", p)
		}
		ev, ok := decoded.(domain.EvidenceEvent)
		if !ok {
			t.Fatalf("decoded type=%T want domain.EvidenceEvent", decoded)
		}
		if ev.Type != domain.Sweep {
			t.Fatalf("evidence type=%s want=%s", ev.Type, domain.Sweep)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for evidence envelope publish")
	}

	<-e.Poison(pid).Done()
}

func TestEvidenceSubsystem_CandlePublishesRegimeEnvelope(t *testing.T) {
	envCh := make(chan envelope.Envelope, 1)
	publishedCh := make(chan envelope.Envelope, 1)

	policy, p := domain.NewRegimeStorePolicy(16, 20)
	if p != nil {
		t.Fatalf("regime policy: %v", p)
	}
	store := domain.NewRegimeStore(policy)

	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	pid := e.Spawn(evidenceruntime.NewSubsystemActor(evidenceruntime.SubsystemConfig{
		EnvelopeCh:  envCh,
		Publisher:   &spyPublisher{ch: publishedCh},
		RegimeStore: store,
		RegimeDetectors: []evidenceapp.RegimeDetector{
			fixedRegimeDetector{},
		},
	}), "evidence-regime", actor.WithID("evidence-regime"))

	payload, p := codec.EncodePayload("aggregation.candle", 1, envelope.ContentTypeJSON, contracts.AggregationCandleClosedV1{
		Candle: contracts.AggregationCandleV1{
			Venue:         "binance",
			Instrument:    "BTCUSDT",
			Timeframe:     "1m",
			WindowStartTs: 60_000,
			WindowEndTs:   120_000,
			Open:          100,
			High:          101,
			Low:           99,
			ClosePrice:    100.5,
			Volume:        200,
			IsClosed:      true,
		},
	})
	if p != nil {
		t.Fatalf("encode candle payload: %v", p)
	}

	envCh <- envelope.Envelope{
		Type:        "aggregation.candle",
		Version:     1,
		Venue:       "binance",
		Instrument:  "BTCUSDT",
		Seq:         9,
		TsIngest:    1_000,
		ContentType: envelope.ContentTypeJSON,
		Payload:     payload,
		Meta: map[string]string{
			"timeframe": "1m",
		},
	}

	select {
	case out := <-publishedCh:
		if out.Type != domain.RegimeEvidenceType {
			t.Fatalf("published type=%q want=%q", out.Type, domain.RegimeEvidenceType)
		}
		decoded, p := codec.DecodePayload(out.Type, out.Version, out.ContentType, out.Payload)
		if p != nil {
			t.Fatalf("decode regime payload: %v", p)
		}
		signal, ok := decoded.(domain.RegimeSignal)
		if !ok {
			t.Fatalf("decoded type=%T want domain.RegimeSignal", decoded)
		}
		if signal.Kind != domain.RegimeTrending {
			t.Fatalf("regime kind=%s want=%s", signal.Kind, domain.RegimeTrending)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for regime envelope publish")
	}

	<-e.Poison(pid).Done()
}

type lelSnapshotRule struct{}

func (lelSnapshotRule) Name() string         { return "lel_snapshot_rule" }
func (lelSnapshotRule) StreamCount() int     { return 0 }
func (lelSnapshotRule) Reset()               {}
func (lelSnapshotRule) EvictStream(_ string) {}
func (lelSnapshotRule) OnEvent(ev domain.LELEvent) []domain.LiquidityEvidence {
	if ev.Kind != domain.LELEventKindSnapshot {
		return nil
	}
	return []domain.LiquidityEvidence{{
		EvidenceType: domain.LiquidityEvidenceTypeSweep,
		TsIngestMs:   ev.TsServer,
		Venue:        ev.Venue,
		Symbol:       ev.Symbol,
		WindowMs:     1000,
		Severity:     domain.LiquidityEvidenceSeverityHigh,
		Confidence:   0.9,
		Metrics:      []domain.LiquidityEvidenceMetric{{Key: "x", Value: 1}},
		Explain:      []string{"snapshot evidence"},
		Version:      domain.LiquidityEvidenceVersion,
		StreamID:     ev.StreamID,
		Seq:          ev.Seq,
		Watermark: domain.LiquidityInputWatermark{
			SeqStart: ev.Seq,
			SeqEnd:   ev.Seq,
		},
	}}
}

func TestEvidenceSubsystem_SnapshotPublishesLiquidityEvidenceEnvelope(t *testing.T) {
	envCh := make(chan envelope.Envelope, 1)
	publishedCh := make(chan envelope.Envelope, 1)

	lel := evidenceapp.NewLELEngine(evidenceapp.DefaultLELEngineConfig(), lelSnapshotRule{})

	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	pid := e.Spawn(evidenceruntime.NewSubsystemActor(evidenceruntime.SubsystemConfig{
		EnvelopeCh: envCh,
		LELEngine:  lel,
		Publisher:  &spyPublisher{ch: publishedCh},
	}), "evidence-lel", actor.WithID("evidence-lel"))

	payload, p := codec.EncodePayload("aggregation.snapshot", 1, envelope.ContentTypeJSON, contracts.AggregationSnapshotV2{
		Venue:        "binance",
		Instrument:   "BTCUSDT",
		Seq:          12,
		BestBidPrice: 100.0,
		BestAskPrice: 100.5,
		SpreadBPS:    5.0,
		Bids: []contracts.AggregationOrderBookLevelV1{
			{Price: 100, Quantity: 10},
		},
		Asks: []contracts.AggregationOrderBookLevelV1{
			{Price: 100.5, Quantity: 8},
		},
		TsIngestMs: 12_000,
	})
	if p != nil {
		t.Fatalf("encode snapshot payload: %v", p)
	}
	envCh <- envelope.Envelope{
		Type:        "aggregation.snapshot",
		Version:     1,
		Venue:       "binance",
		Instrument:  "BTCUSDT",
		Seq:         12,
		TsIngest:    12_000,
		ContentType: envelope.ContentTypeJSON,
		Payload:     payload,
	}

	select {
	case out := <-publishedCh:
		if out.Type != domain.LiquidityEvidenceEventType {
			t.Fatalf("published type=%q want=%q", out.Type, domain.LiquidityEvidenceEventType)
		}
		decoded, p := codec.DecodePayload(out.Type, out.Version, out.ContentType, out.Payload)
		if p != nil {
			t.Fatalf("decode payload: %v", p)
		}
		ev, ok := decoded.(contracts.LiquidityEvidenceV1)
		if !ok {
			t.Fatalf("decoded type=%T want contracts.LiquidityEvidenceV1", decoded)
		}
		if ev.EvidenceType != "SWEEP" {
			t.Fatalf("evidence_type=%q want SWEEP", ev.EvidenceType)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for liquidity evidence envelope publish")
	}

	<-e.Poison(pid).Done()
}

func TestEvidenceSubsystem_ReplicaPartitioningDisjoint(t *testing.T) {
	if err := os.Setenv("PROCESSOR_REPLICAS", "2"); err != nil {
		t.Fatalf("set PROCESSOR_REPLICAS: %v", err)
	}
	t.Cleanup(func() { _ = os.Unsetenv("PROCESSOR_REPLICAS") })

	ownerSymbol := func(owner uint64) string {
		for i := 1; i <= 1000; i++ {
			s := fmt.Sprintf("PAIR-%d", i)
			partition := sharedhash.SumFieldsFast64("binance", strings.ToLower(s)) % uint64(2)
			if partition == owner {
				return s
			}
		}
		t.Fatalf("failed to find symbol for owner %d", owner)
		return ""
	}
	sym0 := ownerSymbol(uint64(0))
	sym1 := ownerSymbol(uint64(1))
	sym0Out := naming.CanonicalInstrument(sym0)
	sym1Out := naming.CanonicalInstrument(sym1)

	makeActor := func(id string, replicaID int) (chan envelope.Envelope, chan envelope.Envelope, *actor.Engine, *actor.PID) {
		envCh := make(chan envelope.Envelope, 8)
		pubCh := make(chan envelope.Envelope, 8)
		e, err := actor.NewEngine(actor.NewEngineConfig())
		if err != nil {
			t.Fatalf("new engine: %v", err)
		}
		pid := e.Spawn(evidenceruntime.NewSubsystemActor(evidenceruntime.SubsystemConfig{
			EnvelopeCh:   envCh,
			LELEngine:    evidenceapp.NewLELEngine(evidenceapp.DefaultLELEngineConfig(), lelSnapshotRule{}),
			Publisher:    &spyPublisher{ch: pubCh},
			ReplicaID:    replicaID,
			ReplicaCount: 2,
		}), id, actor.WithID(id))
		return envCh, pubCh, e, pid
	}

	envCh0, pub0, e0, pid0 := makeActor("evidence-replica-0", 0)
	envCh1, pub1, e1, pid1 := makeActor("evidence-replica-1", 1)
	defer func() {
		<-e0.Poison(pid0).Done()
		<-e1.Poison(pid1).Done()
	}()

	makeEnv := func(symbol string, seq int64) envelope.Envelope {
		payload, p := codec.EncodePayload("aggregation.snapshot", 1, envelope.ContentTypeJSON, contracts.AggregationSnapshotV2{
			Venue:        "binance",
			Instrument:   symbol,
			Seq:          seq,
			BestBidPrice: 100,
			BestAskPrice: 101,
			SpreadBPS:    10,
			Bids:         []contracts.AggregationOrderBookLevelV1{{Price: 100, Quantity: 5}},
			Asks:         []contracts.AggregationOrderBookLevelV1{{Price: 101, Quantity: 5}},
			TsIngestMs:   seq * 1000,
		})
		if p != nil {
			t.Fatalf("encode snapshot payload: %v", p)
		}
		return envelope.Envelope{
			Type:        "aggregation.snapshot",
			Version:     1,
			Venue:       "binance",
			Instrument:  symbol,
			Seq:         seq,
			TsIngest:    seq * 1000,
			ContentType: envelope.ContentTypeJSON,
			Payload:     payload,
		}
	}

	for _, env := range []envelope.Envelope{makeEnv(sym0, 1), makeEnv(sym1, 2)} {
		envCh0 <- env
		envCh1 <- env
	}

	time.Sleep(200 * time.Millisecond)

	drainCounts := func(ch chan envelope.Envelope) map[string]int {
		out := make(map[string]int)
		for {
			select {
			case evt := <-ch:
				out[evt.Instrument]++
			default:
				return out
			}
		}
	}

	count0 := drainCounts(pub0)
	count1 := drainCounts(pub1)

	rep0Sym0 := count0[sym0Out]
	rep1Sym0 := count1[sym0Out]
	if rep0Sym0+rep1Sym0 != 1 {
		t.Fatalf("symbol %s emitted %d times across replicas, want 1", sym0, rep0Sym0+rep1Sym0)
	}
	rep0Sym1 := count0[sym1Out]
	rep1Sym1 := count1[sym1Out]
	if rep0Sym1+rep1Sym1 != 1 {
		t.Fatalf("symbol %s emitted %d times across replicas, want 1", sym1, rep0Sym1+rep1Sym1)
	}
}
