package aggruntime_test

import (
	"context"
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
	"github.com/market-raccoon/internal/shared/problem"
)

// ---------------------------------------------------------------------------
// test doubles
// ---------------------------------------------------------------------------

type spyArtifactPublisher struct {
	mu        sync.Mutex
	snapshots []aggdomain.SnapshotProduced
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

func (s *spyArtifactPublisher) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.snapshots)
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

func newUpdateBook(pub *spyArtifactPublisher) *aggapp.UpdateOrderBookFromEvents {
	return aggapp.NewUpdateOrderBookFromEvents(pub, &noopStore{})
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
	updateBook := newUpdateBook(pub)

	ch := make(chan envelope.Envelope, 8)
	cfg := aggruntime.ProcessorConfig{
		EnvelopeCh: ch,
		UpdateBook: updateBook,
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

// TestProcessor_MultipleDeltas_allAggregated verifies that N envelopes for N
// different instruments each produce one snapshot (independent order books).
func TestProcessor_MultipleDeltas_allAggregated(t *testing.T) {
	pub := &spyArtifactPublisher{}
	updateBook := newUpdateBook(pub)

	ch := make(chan envelope.Envelope, 16)
	cfg := aggruntime.ProcessorConfig{
		EnvelopeCh: ch,
		UpdateBook: updateBook,
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
	updateBook := newUpdateBook(pub)

	ch := make(chan envelope.Envelope, 8)
	cfg := aggruntime.ProcessorConfig{
		EnvelopeCh: ch,
		UpdateBook: updateBook,
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
	updateBook := newUpdateBook(pub)

	ch := make(chan envelope.Envelope, 8)
	cfg := aggruntime.ProcessorConfig{
		EnvelopeCh: ch,
		UpdateBook: updateBook,
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

func TestProcessor_OnEnvelopeProcessed_callbackReceivesProblem(t *testing.T) {
	pub := &spyArtifactPublisher{}
	updateBook := newUpdateBook(pub)

	ch := make(chan envelope.Envelope, 8)
	resultCh := make(chan aggruntime.EnvelopeProcessResult, 1)
	cfg := aggruntime.ProcessorConfig{
		EnvelopeCh: ch,
		UpdateBook: updateBook,
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
	updateBook := newUpdateBook(pub)

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
					UpdateBook: updateBook,
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
	updateBook := newUpdateBook(pub)

	cfg := aggruntime.ProcessorConfig{
		EnvelopeCh: nil,
		UpdateBook: updateBook,
	}

	e := newEngine(t)
	pid := e.Spawn(aggruntime.NewProcessorSubsystemActor(cfg), "processor", actor.WithID("processor"))
	time.Sleep(30 * time.Millisecond)
	<-e.Poison(pid).Done()
}

func TestProcessor_TradeEnvelopeWithoutJoin_ProducesValidationProblem(t *testing.T) {
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		t.Fatalf("BootstrapPayloadCodecRegistry: %v", p)
	}

	pub := &spyArtifactPublisher{}
	updateBook := newUpdateBook(pub)

	ch := make(chan envelope.Envelope, 8)
	resultCh := make(chan aggruntime.EnvelopeProcessResult, 1)
	cfg := aggruntime.ProcessorConfig{
		EnvelopeCh: ch,
		UpdateBook: updateBook,
		OnEnvelopeProcessed: func(res aggruntime.EnvelopeProcessResult) {
			resultCh <- res
		},
	}

	e := newEngine(t)
	pid := e.Spawn(aggruntime.NewProcessorSubsystemActor(cfg), "processor", actor.WithID("processor"))

	ch <- makeTradeEnvelope("BINANCE", "BTCUSDT", 1, 1000, 100.5, "buy", "trade-1")
	select {
	case res := <-resultCh:
		if res.Problem == nil {
			t.Fatal("expected problem for trade type when join is disabled")
		}
		if res.Problem.Code != problem.ValidationFailed {
			t.Fatalf("problem code=%s want=%s", res.Problem.Code, problem.ValidationFailed)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for callback result")
	}

	<-e.Poison(pid).Done()
}

func TestProcessor_TradeJoinEnabled_PublishesCrossVenueSnapshot(t *testing.T) {
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		t.Fatalf("BootstrapPayloadCodecRegistry: %v", p)
	}

	pub := &spyArtifactPublisher{}
	updateBook := newUpdateBook(pub)
	outPublisher := &spyEnvelopePublisher{}
	joinUC := insightsapp.NewJoinCrossVenueTrades()

	ch := make(chan envelope.Envelope, 8)
	cfg := aggruntime.ProcessorConfig{
		EnvelopeCh:            ch,
		UpdateBook:            updateBook,
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
	updateBook := newUpdateBook(pub)
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
		UpdateBook:      updateBook,
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

// ---------------------------------------------------------------------------
// inline parent actor helper
// ---------------------------------------------------------------------------

type inlineParent struct {
	ch         chan any
	spawnChild func(ctx *actor.Context) *actor.PID
	subPID     **actor.PID
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
