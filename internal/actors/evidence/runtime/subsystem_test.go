package evidenceruntime_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/anthdm/hollywood/actor"
	evidenceruntime "github.com/market-raccoon/internal/actors/evidence/runtime"
	evidenceapp "github.com/market-raccoon/internal/core/evidence/app"
	"github.com/market-raccoon/internal/core/evidence/domain"
	marketmodel "github.com/market-raccoon/internal/core/marketmodel"
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
