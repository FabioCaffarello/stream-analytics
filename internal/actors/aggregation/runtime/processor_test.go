package aggruntime_test

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/anthdm/hollywood/actor"
	aggruntime "github.com/market-raccoon/internal/actors/aggregation/runtime"
	actorruntime "github.com/market-raccoon/internal/actors/runtime"
	aggapp "github.com/market-raccoon/internal/core/aggregation/app"
	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	insightsapp "github.com/market-raccoon/internal/core/insights/app"
	insightsdomain "github.com/market-raccoon/internal/core/insights/domain"
	mddomain "github.com/market-raccoon/internal/core/marketdata/domain"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/contracts"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/policykit"
	"github.com/market-raccoon/internal/shared/problem"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func init() {
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		panic(fmt.Sprintf("BootstrapPayloadCodecRegistry: %v", p))
	}
}

// ---------------------------------------------------------------------------
// test doubles
// ---------------------------------------------------------------------------

type spyArtifactPublisher struct {
	mu        sync.Mutex
	snapshots []aggdomain.SnapshotProduced
	candles   []aggdomain.CandleClosed
	stats     []aggdomain.StatsWindowClosed
}

func (s *spyArtifactPublisher) PublishSnapshot(_ context.Context, snap aggdomain.SnapshotProduced) *problem.Problem {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshots = append(s.snapshots, snap)
	return nil
}

func (s *spyArtifactPublisher) PublishInconsistent(_ context.Context, _ aggdomain.OrderBookInconsistentDetected) *problem.Problem {
	return nil
}

func (s *spyArtifactPublisher) PublishCandleClosed(_ context.Context, evt aggdomain.CandleClosed) *problem.Problem {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.candles = append(s.candles, evt)
	return nil
}

func (s *spyArtifactPublisher) PublishStatsClosed(_ context.Context, evt aggdomain.StatsWindowClosed) *problem.Problem {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stats = append(s.stats, evt)
	return nil
}

func (s *spyArtifactPublisher) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.snapshots)
}

func (s *spyArtifactPublisher) candleCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.candles)
}

func (s *spyArtifactPublisher) statsCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.stats)
}

func (s *spyArtifactPublisher) lastStats() aggdomain.StatsWindowClosed {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stats[len(s.stats)-1]
}

type noopStore struct{}

func (n *noopStore) Save(_ context.Context, _ aggdomain.SnapshotProduced) *problem.Problem {
	return nil
}

type spyEnvelopePublisher struct {
	mu   sync.Mutex
	envs []envelope.Envelope
}

func (s *spyEnvelopePublisher) Publish(_ context.Context, env envelope.Envelope) *problem.Problem {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.envs = append(s.envs, env)
	return nil
}

func (s *spyEnvelopePublisher) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.envs)
}

func (s *spyEnvelopePublisher) last() envelope.Envelope {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.envs[len(s.envs)-1]
}

func (s *spyEnvelopePublisher) all() []envelope.Envelope {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]envelope.Envelope, len(s.envs))
	copy(out, s.envs)
	return out
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func newEngine(t *testing.T) *actor.Engine {
	t.Helper()
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	return e
}

func newAggService(pub *spyArtifactPublisher) *aggapp.AggregationService {
	return aggapp.NewAggregationService(aggapp.AggregationServiceConfig{
		Update:    aggapp.UpdateConfig{},
		Candle:    aggapp.BuildCandleConfig{},
		Stats:     aggapp.BuildStatsConfig{},
		Publisher: pub,
		Store:     &noopStore{},
	})
}

func makeBookDeltaEnvelope(venue, instrument string, seq int64, bids, asks []mddomain.PriceLevel) envelope.Envelope {
	delta := mddomain.BookDeltaV1{
		Bids:      bids,
		Asks:      asks,
		Timestamp: time.Now().UnixMilli(),
	}
	payload, p := codec.Marshal(delta)
	if p != nil {
		panic("test: failed to marshal BookDeltaV1: " + p.Message)
	}
	return envelope.Envelope{
		Type:           "marketdata.bookdelta",
		Version:        1,
		Venue:          venue,
		Instrument:     instrument,
		Seq:            seq,
		TsIngest:       time.Now().UnixMilli(),
		IdempotencyKey: "test-idem",
		Payload:        payload,
	}
}

func makeRawEnvelope(venue, instrument string, seq int64) envelope.Envelope {
	return envelope.Envelope{
		Type:           "marketdata.raw",
		Version:        1,
		Venue:          venue,
		Instrument:     instrument,
		Seq:            seq,
		TsIngest:       time.Now().UnixMilli(),
		IdempotencyKey: "test-raw",
		Payload:        []byte(`{"data":"aGVsbG8="}`),
	}
}

func makeTradeEnvelope(venue, instrument string, seq, tsIngest int64, price float64, side, tradeID string) envelope.Envelope {
	payload, p := codec.EncodePayload("marketdata.trade", 1, envelope.ContentTypeJSON, mddomain.TradeTickV1{
		Price:     price,
		Size:      1.0,
		Side:      side,
		TradeID:   tradeID,
		Timestamp: tsIngest - 10,
	})
	if p != nil {
		panic("test: failed to encode TradeTickV1: " + p.Message)
	}
	return envelope.Envelope{
		Type:           "marketdata.trade",
		Version:        1,
		Venue:          venue,
		Instrument:     instrument,
		TsExchange:     tsIngest - 10,
		TsIngest:       tsIngest,
		Seq:            seq,
		IdempotencyKey: "trade-idem-" + tradeID,
		ContentType:    envelope.ContentTypeJSON,
		Meta: map[string]string{
			"instrument_market_type": "SPOT",
		},
		Payload: payload,
	}
}

func makeLiquidationEnvelope(venue, instrument string, seq, tsIngest int64, size float64, side string) envelope.Envelope {
	payload, p := codec.EncodePayload("marketdata.liquidation", 1, envelope.ContentTypeJSON, mddomain.LiquidationTickV1{
		Side:      side,
		Price:     100.0,
		Size:      size,
		Timestamp: tsIngest - 10,
	})
	if p != nil {
		panic("test: failed to encode LiquidationTickV1: " + p.Message)
	}
	return envelope.Envelope{
		Type:           "marketdata.liquidation",
		Version:        1,
		Venue:          venue,
		Instrument:     instrument,
		TsExchange:     tsIngest - 10,
		TsIngest:       tsIngest,
		Seq:            seq,
		IdempotencyKey: fmt.Sprintf("liq-idem-%d", seq),
		ContentType:    envelope.ContentTypeJSON,
		Payload:        payload,
	}
}

func makeMarkPriceEnvelope(venue, instrument string, seq, tsIngest int64, markPrice, fundingRate float64) envelope.Envelope {
	payload, p := codec.EncodePayload("marketdata.markprice", 1, envelope.ContentTypeJSON, mddomain.MarkPriceTickV1{
		MarkPrice:   markPrice,
		IndexPrice:  markPrice,
		FundingRate: fundingRate,
		Timestamp:   tsIngest - 10,
	})
	if p != nil {
		panic("test: failed to encode MarkPriceTickV1: " + p.Message)
	}
	return envelope.Envelope{
		Type:           "marketdata.markprice",
		Version:        1,
		Venue:          venue,
		Instrument:     instrument,
		TsExchange:     tsIngest - 10,
		TsIngest:       tsIngest,
		Seq:            seq,
		IdempotencyKey: fmt.Sprintf("mark-idem-%d", seq),
		ContentType:    envelope.ContentTypeJSON,
		Payload:        payload,
	}
}

// waitFor polls fn until it returns true or deadline expires.
func waitFor(t *testing.T, timeout time.Duration, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("waitFor: condition not met within timeout")
}

// ---------------------------------------------------------------------------
// tests
// ---------------------------------------------------------------------------

// TestProcessor_BookDelta_callsUpdateOrderBook verifies end-to-end:
// BookDeltaV1 envelope → UpdateOrderBook → ArtifactPublisher.PublishSnapshot.
func TestProcessor_BookDelta_callsUpdateOrderBook(t *testing.T) {
	pub := &spyArtifactPublisher{}
	aggSvc := newAggService(pub)

	ch := make(chan envelope.Envelope, 8)
	cfg := aggruntime.ProcessorConfig{
		EnvelopeCh: ch,
		Service:    aggSvc,
	}

	e := newEngine(t)
	pid := e.Spawn(aggruntime.NewProcessorSubsystemActor(cfg), "processor", actor.WithID("processor"))

	env := makeBookDeltaEnvelope(
		"BINANCE", "BTC-USDT", 1,
		[]mddomain.PriceLevel{{Price: 42000, Size: 1.5}},
		[]mddomain.PriceLevel{{Price: 42001, Size: 2.0}},
	)
	ch <- env

	waitFor(t, 2*time.Second, func() bool { return pub.count() == 1 })

	<-e.Poison(pid).Done()
}

// TestProcessor_BookDelta_ProtoDecoded verifies that a protobuf-encoded BookDeltaV1
// envelope is decoded and processed (no DECODE_FAILED).
func TestProcessor_BookDelta_ProtoDecoded(t *testing.T) {
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		t.Fatalf("BootstrapPayloadCodecRegistry: %v", p)
	}

	pub := &spyArtifactPublisher{}
	aggSvc := newAggService(pub)

	ch := make(chan envelope.Envelope, 8)
	resultCh := make(chan aggruntime.EnvelopeProcessResult, 1)
	cfg := aggruntime.ProcessorConfig{
		EnvelopeCh: ch,
		Service:    aggSvc,
		OnEnvelopeProcessed: func(res aggruntime.EnvelopeProcessResult) {
			select {
			case resultCh <- res:
			default:
			}
		},
	}

	e := newEngine(t)
	pid := e.Spawn(aggruntime.NewProcessorSubsystemActor(cfg), "processor", actor.WithID("processor"))

	delta := mddomain.BookDeltaV1{
		Bids:      []mddomain.PriceLevel{{Price: 42000, Size: 1.5}},
		Asks:      []mddomain.PriceLevel{{Price: 42001, Size: 2.0}},
		Timestamp: time.Now().UnixMilli(),
	}
	payload, p := codec.EncodePayload("marketdata.bookdelta", 1, envelope.ContentTypeProto, delta)
	if p != nil {
		t.Fatalf("failed to encode proto bookdelta: %v", p)
	}
	env := envelope.Envelope{
		Type:           "marketdata.bookdelta",
		Version:        1,
		Venue:          "BINANCE",
		Instrument:     "BTC-USDT",
		Seq:            1,
		TsIngest:       time.Now().UnixMilli(),
		IdempotencyKey: "proto-bookdelta-1",
		ContentType:    envelope.ContentTypeProto,
		Payload:        payload,
	}

	ch <- env

	// Ensure update produced a snapshot
	waitFor(t, 2*time.Second, func() bool { return pub.count() == 1 })

	// Ensure OnEnvelopeProcessed did not report a DECODE_FAILED
	select {
	case res := <-resultCh:
		if res.Problem != nil {
			if got, ok := res.Problem.Details["reason_code"].(string); ok && got == "DECODE_FAILED" {
				t.Fatalf("unexpected DECODE_FAILED for proto bookdelta")
			}
			// other problem is unexpected
			if res.Problem != nil {
				t.Fatalf("unexpected processing problem: %v", res.Problem)
			}
		}
	case <-time.After(2 * time.Second):
		// it's acceptable if callback wasn't invoked (success path may not populate); ensure snapshot produced above
	}

	<-e.Poison(pid).Done()
}

// TestProcessor_MultipleDeltas_allAggregated verifies that N envelopes for N
// different instruments each produce one snapshot (independent order books).
func TestProcessor_MultipleDeltas_allAggregated(t *testing.T) {
	pub := &spyArtifactPublisher{}
	aggSvc := newAggService(pub)

	ch := make(chan envelope.Envelope, 16)
	cfg := aggruntime.ProcessorConfig{
		EnvelopeCh: ch,
		Service:    aggSvc,
	}

	e := newEngine(t)
	pid := e.Spawn(aggruntime.NewProcessorSubsystemActor(cfg), "processor", actor.WithID("processor"))

	// Each envelope targets a unique instrument so the order books are
	// independent and there's no risk of a crossed-book violation.
	instruments := []string{"BTC-USDT", "ETH-USDT", "SOL-USDT", "BNB-USDT", "XRP-USDT"}
	for i, sym := range instruments {
		env := makeBookDeltaEnvelope(
			"BINANCE", sym, int64(i+1),
			[]mddomain.PriceLevel{{Price: 100.0, Size: 1.0}}, // bid
			[]mddomain.PriceLevel{{Price: 101.0, Size: 1.0}}, // ask
		)
		ch <- env
	}

	waitFor(t, 2*time.Second, func() bool { return pub.count() == len(instruments) })

	<-e.Poison(pid).Done()
}

// TestProcessor_RawEnvelope_skipped verifies that raw envelopes are silently
// skipped and do not call the aggregation use case.
func TestProcessor_RawEnvelope_skipped(t *testing.T) {
	pub := &spyArtifactPublisher{}
	aggSvc := newAggService(pub)

	ch := make(chan envelope.Envelope, 8)
	cfg := aggruntime.ProcessorConfig{
		EnvelopeCh: ch,
		Service:    aggSvc,
	}

	e := newEngine(t)
	pid := e.Spawn(aggruntime.NewProcessorSubsystemActor(cfg), "processor", actor.WithID("processor"))

	ch <- makeRawEnvelope("BINANCE", "BTC-USDT", 1)
	time.Sleep(50 * time.Millisecond)

	if pub.count() != 0 {
		t.Fatalf("expected 0 snapshots for raw envelope, got %d", pub.count())
	}

	<-e.Poison(pid).Done()
}

// TestProcessor_UnknownType_doesNotCrash verifies that unknown event types are
// handled gracefully without panicking.
func TestProcessor_UnknownType_doesNotCrash(t *testing.T) {
	pub := &spyArtifactPublisher{}
	aggSvc := newAggService(pub)

	ch := make(chan envelope.Envelope, 8)
	cfg := aggruntime.ProcessorConfig{
		EnvelopeCh: ch,
		Service:    aggSvc,
	}

	e := newEngine(t)
	pid := e.Spawn(aggruntime.NewProcessorSubsystemActor(cfg), "processor", actor.WithID("processor"))

	ch <- envelope.Envelope{
		Type:    "unknown.type",
		Version: 1,
		Venue:   "BINANCE",
		Payload: []byte(`{}`),
	}
	time.Sleep(50 * time.Millisecond)

	if pub.count() != 0 {
		t.Fatalf("expected 0 snapshots for unknown type, got %d", pub.count())
	}

	<-e.Poison(pid).Done()
}

func TestProcessor_UnknownVersion_ProducesValidationProblem(t *testing.T) {
	pub := &spyArtifactPublisher{}
	aggSvc := newAggService(pub)

	ch := make(chan envelope.Envelope, 8)
	resultCh := make(chan aggruntime.EnvelopeProcessResult, 1)
	cfg := aggruntime.ProcessorConfig{
		EnvelopeCh: ch,
		Service:    aggSvc,
		OnEnvelopeProcessed: func(res aggruntime.EnvelopeProcessResult) {
			resultCh <- res
		},
	}

	e := newEngine(t)
	pid := e.Spawn(aggruntime.NewProcessorSubsystemActor(cfg), "processor", actor.WithID("processor"))

	ch <- envelope.Envelope{
		Type:           "marketdata.bookdelta",
		Version:        2,
		Venue:          "BINANCE",
		Instrument:     "BTCUSDT",
		TsIngest:       time.Now().UnixMilli(),
		IdempotencyKey: "unknown-version-1",
		Payload:        []byte(`{"bids":[],"asks":[]}`),
	}

	select {
	case res := <-resultCh:
		if res.Problem == nil {
			t.Fatal("expected problem for unknown event version")
		}
		if res.Problem.Code != problem.ValidationFailed {
			t.Fatalf("problem code=%s want=%s", res.Problem.Code, problem.ValidationFailed)
		}
		if got, ok := res.Problem.Details["reason_code"].(string); !ok || got != "UNKNOWN_EVENT_VERSION" {
			t.Fatalf("reason_code=%v want=%q", res.Problem.Details["reason_code"], "UNKNOWN_EVENT_VERSION")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for callback result")
	}

	<-e.Poison(pid).Done()
}

func TestProcessor_OnEnvelopeProcessed_callbackReceivesProblem(t *testing.T) {
	pub := &spyArtifactPublisher{}
	aggSvc := newAggService(pub)

	ch := make(chan envelope.Envelope, 8)
	resultCh := make(chan aggruntime.EnvelopeProcessResult, 1)
	cfg := aggruntime.ProcessorConfig{
		EnvelopeCh: ch,
		Service:    aggSvc,
		OnEnvelopeProcessed: func(res aggruntime.EnvelopeProcessResult) {
			resultCh <- res
		},
	}

	e := newEngine(t)
	pid := e.Spawn(aggruntime.NewProcessorSubsystemActor(cfg), "processor", actor.WithID("processor"))

	ch <- envelope.Envelope{
		Type:           "unknown.type",
		Version:        1,
		Venue:          "BINANCE",
		Instrument:     "BTC-USDT",
		TsIngest:       time.Now().UnixMilli(),
		IdempotencyKey: "unknown-type-1",
		Payload:        []byte(`{}`),
	}

	select {
	case res := <-resultCh:
		if res.Problem == nil {
			t.Fatal("expected problem for unknown event type")
		}
		if res.Problem.Code != problem.ValidationFailed {
			t.Fatalf("problem code=%s want=%s", res.Problem.Code, problem.ValidationFailed)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for callback result")
	}

	<-e.Poison(pid).Done()
}

// TestProcessor_BusClosed_sendsChildFailed verifies that closing the envelope
// channel causes the actor to send runtime.ChildFailed to its parent.
func TestProcessor_BusClosed_sendsChildFailed(t *testing.T) {
	pub := &spyArtifactPublisher{}
	aggSvc := newAggService(pub)

	ch := make(chan envelope.Envelope, 8)

	parentCh := make(chan any, 16)
	pa := &struct {
		subPID *actor.PID
		ch     chan any
	}{ch: parentCh}

	e := newEngine(t)
	parentPID := e.Spawn(func() actor.Receiver {
		return &inlineParent{
			ch: parentCh,
			spawnChild: func(ctx *actor.Context) *actor.PID {
				cfg := aggruntime.ProcessorConfig{
					EnvelopeCh: ch,
					Service:    aggSvc,
				}
				return ctx.SpawnChild(
					aggruntime.NewProcessorSubsystemActor(cfg),
					"processor",
					actor.WithID("processor"),
				)
			},
			subPID: &pa.subPID,
		}
	}, "parent", actor.WithID("parent"))

	time.Sleep(50 * time.Millisecond)

	// Close the channel to trigger busClosedMsg.
	close(ch)

	// Wait for parent to receive ChildFailed.
	var got actorruntime.ChildFailed
	select {
	case raw := <-parentCh:
		var ok bool
		got, ok = raw.(actorruntime.ChildFailed)
		if !ok {
			t.Fatalf("expected ChildFailed, got %T", raw)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for ChildFailed")
	}

	if got.Subsystem != actorruntime.SubsystemAggregation {
		t.Errorf("expected subsystem=%s, got %s", actorruntime.SubsystemAggregation, got.Subsystem)
	}
	if got.Kind != "bus_closed" {
		t.Errorf("expected kind=bus_closed, got %s", got.Kind)
	}

	<-e.Poison(parentPID).Done()
	_ = pa
}

// TestProcessor_NilChannel_idle verifies no panic when EnvelopeCh is nil.
func TestProcessor_NilChannel_idle(t *testing.T) {
	pub := &spyArtifactPublisher{}
	aggSvc := newAggService(pub)

	cfg := aggruntime.ProcessorConfig{
		EnvelopeCh: nil,
		Service:    aggSvc,
	}

	e := newEngine(t)
	pid := e.Spawn(aggruntime.NewProcessorSubsystemActor(cfg), "processor", actor.WithID("processor"))
	time.Sleep(30 * time.Millisecond)
	<-e.Poison(pid).Done()
}

func TestProcessor_TradeEnvelopeWithoutJoin_ProcessesCandle(t *testing.T) {
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		t.Fatalf("BootstrapPayloadCodecRegistry: %v", p)
	}

	pub := &spyArtifactPublisher{}
	aggSvc := newAggService(pub)

	ch := make(chan envelope.Envelope, 8)
	resultCh := make(chan aggruntime.EnvelopeProcessResult, 8)
	cfg := aggruntime.ProcessorConfig{
		EnvelopeCh: ch,
		Service:    aggSvc,
		OnEnvelopeProcessed: func(res aggruntime.EnvelopeProcessResult) {
			select {
			case resultCh <- res:
			default:
			}
		},
	}

	e := newEngine(t)
	pid := e.Spawn(aggruntime.NewProcessorSubsystemActor(cfg), "processor", actor.WithID("processor"))

	ch <- makeTradeEnvelope("BINANCE", "BTCUSDT", 1, 1, 100.5, "buy", "trade-1")
	ch <- makeTradeEnvelope("BINANCE", "BTCUSDT", 2, 60_001, 101.5, "sell", "trade-2")
	waitFor(t, 2*time.Second, func() bool { return pub.candleCount() == 1 })

	for i := 0; i < 2; i++ {
		select {
		case res := <-resultCh:
			if res.Problem != nil {
				t.Fatalf("unexpected trade processing problem with join disabled: %v", res.Problem)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for callback result")
		}
	}

	<-e.Poison(pid).Done()
}

func TestProcessor_LiquidationRoute_EmitsStatsClosed(t *testing.T) {
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		t.Fatalf("BootstrapPayloadCodecRegistry: %v", p)
	}

	pub := &spyArtifactPublisher{}
	aggSvc := newAggService(pub)

	ch := make(chan envelope.Envelope, 8)
	cfg := aggruntime.ProcessorConfig{
		EnvelopeCh: ch,
		Service:    aggSvc,
	}

	e := newEngine(t)
	pid := e.Spawn(aggruntime.NewProcessorSubsystemActor(cfg), "processor", actor.WithID("processor"))

	ch <- makeLiquidationEnvelope("BINANCE", "BTCUSDT", 1, 1, 2.0, "buy")
	ch <- makeLiquidationEnvelope("BINANCE", "BTCUSDT", 2, 60_001, 1.0, "sell")
	waitFor(t, 2*time.Second, func() bool { return pub.statsCount() > 0 })

	<-e.Poison(pid).Done()
}

func TestProcessor_MarkPriceRoute_WithFunding_EmitsStatsClosed(t *testing.T) {
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		t.Fatalf("BootstrapPayloadCodecRegistry: %v", p)
	}

	pub := &spyArtifactPublisher{}
	aggSvc := newAggService(pub)

	ch := make(chan envelope.Envelope, 8)
	cfg := aggruntime.ProcessorConfig{
		EnvelopeCh: ch,
		Service:    aggSvc,
	}

	e := newEngine(t)
	pid := e.Spawn(aggruntime.NewProcessorSubsystemActor(cfg), "processor", actor.WithID("processor"))

	ch <- makeMarkPriceEnvelope("BINANCE", "BTCUSDT", 1, 1, 100.0, 0.0002)
	ch <- makeMarkPriceEnvelope("BINANCE", "BTCUSDT", 2, 60_001, 101.0, 0.0003)
	waitFor(t, 2*time.Second, func() bool { return pub.statsCount() > 0 })

	closed := pub.lastStats().Stats
	if closed.MarkPriceOpen == 0 || closed.MarkPriceClose == 0 {
		t.Fatalf("expected markprice fields to be populated, got open=%f close=%f", closed.MarkPriceOpen, closed.MarkPriceClose)
	}
	if closed.FundingRateLast == 0 {
		t.Fatalf("expected non-zero funding_rate_last from dual routing, got %f", closed.FundingRateLast)
	}

	<-e.Poison(pid).Done()
}

func TestProcessor_TradeJoinEnabled_PublishesCrossVenueSnapshot(t *testing.T) {
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		t.Fatalf("BootstrapPayloadCodecRegistry: %v", p)
	}

	pub := &spyArtifactPublisher{}
	aggSvc := newAggService(pub)
	outPublisher := &spyEnvelopePublisher{}
	joinUC := insightsapp.NewJoinCrossVenueTrades()

	ch := make(chan envelope.Envelope, 8)
	cfg := aggruntime.ProcessorConfig{
		EnvelopeCh:            ch,
		Service:               aggSvc,
		JoinTrades:            joinUC,
		PublishEnvelope:       outPublisher,
		SnapshotSubjectPrefix: "",
	}

	e := newEngine(t)
	pid := e.Spawn(aggruntime.NewProcessorSubsystemActor(cfg), "processor", actor.WithID("processor"))

	ch <- makeTradeEnvelope("BINANCE", "BTCUSDT", 1, 1000, 100.5, "buy", "trade-1")
	ch <- makeTradeEnvelope("BYBIT", "BTCUSDT", 1, 1010, 100.7, "sell", "trade-2")

	waitFor(t, 2*time.Second, func() bool { return outPublisher.count() == 1 })

	out := outPublisher.last()
	if out.Type != insightsdomain.CrossVenueTradeSnapshotType {
		t.Fatalf("snapshot type=%q want=%q", out.Type, insightsdomain.CrossVenueTradeSnapshotType)
	}
	if out.Venue != insightsdomain.CrossVenueSnapshotVenue {
		t.Fatalf("snapshot venue=%q want=%q", out.Venue, insightsdomain.CrossVenueSnapshotVenue)
	}
	if out.ContentType != envelope.ContentTypeJSON {
		t.Fatalf("snapshot content_type=%q want=%q", out.ContentType, envelope.ContentTypeJSON)
	}

	decoded, p := codec.DecodePayload(out.Type, out.Version, out.ContentType, out.Payload)
	if p != nil {
		t.Fatalf("decode snapshot payload: %v", p)
	}
	snap, ok := decoded.(insightsdomain.CrossVenueTradeSnapshotV1)
	if !ok {
		t.Fatalf("snapshot payload type=%T want %T", decoded, insightsdomain.CrossVenueTradeSnapshotV1{})
	}
	if len(snap.Venues) != 2 {
		t.Fatalf("snapshot venues=%d want=2", len(snap.Venues))
	}
	if snap.Venues[0].Venue != "BINANCE" || snap.Venues[1].Venue != "BYBIT" {
		t.Fatalf("snapshot venues order=%q,%q want BINANCE,BYBIT", snap.Venues[0].Venue, snap.Venues[1].Venue)
	}

	<-e.Poison(pid).Done()
}

func TestProcessor_TradeJoinEnabledWithSpreadSignal_PublishesSignalEnvelope(t *testing.T) {
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		t.Fatalf("BootstrapPayloadCodecRegistry: %v", p)
	}

	pub := &spyArtifactPublisher{}
	aggSvc := newAggService(pub)
	outPublisher := &spyEnvelopePublisher{}
	joinUC := insightsapp.NewJoinCrossVenueTradesWithConfig(insightsapp.JoinCrossVenueTradesConfig{
		EnableSpreadSignal: true,
		MinVenues:          2,
		MinSpreadBPS:       5,
		RoundingMode:       "half_even",
	})

	ch := make(chan envelope.Envelope, 8)
	cfg := aggruntime.ProcessorConfig{
		EnvelopeCh:      ch,
		Service:         aggSvc,
		JoinTrades:      joinUC,
		PublishEnvelope: outPublisher,
	}

	e := newEngine(t)
	pid := e.Spawn(aggruntime.NewProcessorSubsystemActor(cfg), "processor", actor.WithID("processor"))

	ch <- makeTradeEnvelope("BINANCE", "BTCUSDT", 1, 1000, 100.0, "buy", "trade-1")
	ch <- makeTradeEnvelope("BYBIT", "BTCUSDT", 1, 1010, 100.2, "sell", "trade-2")

	waitFor(t, 2*time.Second, func() bool { return outPublisher.count() == 2 })

	published := outPublisher.all()
	if published[0].Type != insightsdomain.CrossVenueTradeSnapshotType {
		t.Fatalf("first published type=%q want=%q", published[0].Type, insightsdomain.CrossVenueTradeSnapshotType)
	}
	if published[1].Type != insightsdomain.CrossVenueSpreadSignalType {
		t.Fatalf("second published type=%q want=%q", published[1].Type, insightsdomain.CrossVenueSpreadSignalType)
	}

	decoded, p := codec.DecodePayload(published[1].Type, published[1].Version, published[1].ContentType, published[1].Payload)
	if p != nil {
		t.Fatalf("decode spread signal payload: %v", p)
	}
	signal, ok := decoded.(insightsdomain.CrossVenueSpreadSignalV1)
	if !ok {
		t.Fatalf("spread signal payload type=%T want %T", decoded, insightsdomain.CrossVenueSpreadSignalV1{})
	}
	if signal.Instrument != "BTCUSDT" {
		t.Fatalf("signal instrument=%q want BTCUSDT", signal.Instrument)
	}
	if signal.SpreadBps < 5 {
		t.Fatalf("signal spread_bps=%f want >= 5", signal.SpreadBps)
	}

	<-e.Poison(pid).Done()
}

func TestProcessor_PolicyKit_BookDeltaDeterministicStride(t *testing.T) {
	type runResult struct {
		processed []int64
		snaps     int
	}
	run := func() runResult {
		pub := &spyArtifactPublisher{}
		aggSvc := newAggService(pub)

		ch := make(chan envelope.Envelope, 16)
		var mu sync.Mutex
		processed := make([]int64, 0, 8)
		cfg := aggruntime.ProcessorConfig{
			EnvelopeCh:               ch,
			Service:                  aggSvc,
			PolicyKitEngine:          staticEngine{decision: policykit.Decision{Actions: []policykit.Action{{Type: policykit.ActionDegradeStride, Stride: 2}}}},
			PolicyKitBacklogCapacity: 16,
			OnEnvelopeProcessed: func(res aggruntime.EnvelopeProcessResult) {
				if res.Envelope.Type == "marketdata.bookdelta" {
					mu.Lock()
					processed = append(processed, res.Envelope.Seq)
					mu.Unlock()
				}
			},
		}

		e := newEngine(t)
		pid := e.Spawn(aggruntime.NewProcessorSubsystemActor(cfg), "processor", actor.WithID("processor"))
		for i := 1; i <= 6; i++ {
			ch <- makeBookDeltaEnvelope(
				"BINANCE", "BTC-USDT", int64(i),
				[]mddomain.PriceLevel{{Price: 42000, Size: 1.5}},
				[]mddomain.PriceLevel{{Price: 42001, Size: 2.0}},
			)
		}

		waitFor(t, 2*time.Second, func() bool {
			mu.Lock()
			defer mu.Unlock()
			return len(processed) >= 6
		})
		<-e.Poison(pid).Done()
		mu.Lock()
		defer mu.Unlock()
		out := make([]int64, len(processed))
		copy(out, processed)
		return runResult{processed: out, snaps: pub.count()}
	}

	first := run()
	second := run()
	want := []int64{1, 2, 3, 4, 5, 6}
	if !slices.Equal(first.processed, want) {
		t.Fatalf("first run seq=%v want=%v", first.processed, want)
	}
	if !slices.Equal(second.processed, want) {
		t.Fatalf("second run seq=%v want=%v", second.processed, want)
	}
	if first.snaps != 3 || second.snaps != 3 {
		t.Fatalf("snapshot count mismatch first=%d second=%d want=3", first.snaps, second.snaps)
	}
}

func TestProcessor_PolicyKit_NeverDropCloseFinal(t *testing.T) {
	pub := &spyArtifactPublisher{}
	aggSvc := newAggService(pub)

	ch := make(chan envelope.Envelope, 8)
	resultCh := make(chan aggruntime.EnvelopeProcessResult, 1)
	cfg := aggruntime.ProcessorConfig{
		EnvelopeCh:               ch,
		Service:                  aggSvc,
		PolicyKitEngine:          staticEngine{decision: policykit.Decision{Actions: []policykit.Action{{Type: policykit.ActionDropDelta}}}},
		PolicyKitBacklogCapacity: 8,
		OnEnvelopeProcessed: func(res aggruntime.EnvelopeProcessResult) {
			resultCh <- res
		},
	}

	e := newEngine(t)
	pid := e.Spawn(aggruntime.NewProcessorSubsystemActor(cfg), "processor", actor.WithID("processor"))

	ch <- envelope.Envelope{
		Type:       "marketdata.bookdelta_final",
		Version:    1,
		Venue:      "BINANCE",
		Instrument: "BTCUSDT",
		Seq:        1,
		TsIngest:   time.Now().UnixMilli(),
		Payload:    []byte(`{}`),
	}

	select {
	case res := <-resultCh:
		if res.Problem == nil {
			t.Fatal("expected processing problem for unhandled close/final type")
		}
		if res.Envelope.Type != "marketdata.bookdelta_final" {
			t.Fatalf("envelope type=%q want marketdata.bookdelta_final", res.Envelope.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for callback result")
	}

	<-e.Poison(pid).Done()
}

// TestProcessor_TradeDelivered_IncreasesProcessedMetric proves that a trade
// envelope delivered to the processor is actually processed (not silently
// dropped).  This is the integration assertion for the filter_subjects fix
// from "marketdata.bookdelta.>" → "marketdata.>".
func TestProcessor_TradeDelivered_IncreasesProcessedMetric(t *testing.T) {
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		t.Fatalf("BootstrapPayloadCodecRegistry: %v", p)
	}

	pub := &spyArtifactPublisher{}
	aggSvc := newAggService(pub)

	ch := make(chan envelope.Envelope, 8)
	resultCh := make(chan aggruntime.EnvelopeProcessResult, 1)
	cfg := aggruntime.ProcessorConfig{
		EnvelopeCh: ch,
		Service:    aggSvc,
		OnEnvelopeProcessed: func(res aggruntime.EnvelopeProcessResult) {
			select {
			case resultCh <- res:
			default:
			}
		},
	}

	before := testutil.ToFloat64(metrics.ProcessorProcessedTotal.WithLabelValues("marketdata.trade", "ok"))

	e := newEngine(t)
	pid := e.Spawn(aggruntime.NewProcessorSubsystemActor(cfg), "processor", actor.WithID("processor"))

	ch <- makeTradeEnvelope("BINANCE", "BTCUSDT", 1, 1000, 100.5, "buy", "trade-1")

	select {
	case res := <-resultCh:
		// JoinTrades is nil and candles are enabled by service config.
		// The key assertion is that the trade is processed successfully.
		if res.Problem != nil {
			t.Fatalf("unexpected processing problem for trade with join disabled: %v", res.Problem)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for processing result")
	}

	after := testutil.ToFloat64(metrics.ProcessorProcessedTotal.WithLabelValues("marketdata.trade", "ok"))
	if after < before+1 {
		t.Fatalf("processor_processed_total{event_type=marketdata.trade,status=ok} not incremented: before=%.0f after=%.0f", before, after)
	}

	<-e.Poison(pid).Done()
}

// ---------------------------------------------------------------------------
// inline parent actor helper
// ---------------------------------------------------------------------------

type inlineParent struct {
	ch         chan any
	spawnChild func(ctx *actor.Context) *actor.PID
	subPID     **actor.PID
}

type staticEngine struct {
	decision policykit.Decision
}

func (s staticEngine) Decide(_ policykit.Level, _ policykit.Signals) policykit.Decision {
	return s.decision
}

func (p *inlineParent) Receive(ctx *actor.Context) {
	switch m := ctx.Message().(type) {
	case actor.Initialized:
	case actor.Started:
		pid := p.spawnChild(ctx)
		if p.subPID != nil {
			*p.subPID = pid
		}
	case actor.Stopped:
	default:
		select {
		case p.ch <- m:
		default:
		}
	}
}
