package mdruntime_test

import (
	"context"
	"sync"
	"testing"
	"time"

	mdruntime "github.com/FabioCaffarello/stream-analytics/internal/actors/marketdata/runtime"
	ws "github.com/FabioCaffarello/stream-analytics/internal/actors/marketdata/ws"
	runtime "github.com/FabioCaffarello/stream-analytics/internal/actors/runtime"
	mdapp "github.com/FabioCaffarello/stream-analytics/internal/core/marketdata/app"
	"github.com/FabioCaffarello/stream-analytics/internal/core/marketdata/domain"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/codec"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/envelope"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
	"github.com/anthdm/hollywood/actor"
)

// ---------------------------------------------------------------------------
// test doubles
// ---------------------------------------------------------------------------

// spyPublisher records every envelope published via the EventPublisher port.
type spyPublisher struct {
	mu        sync.Mutex
	published []envelope.Envelope
}

func (s *spyPublisher) Publish(_ context.Context, env envelope.Envelope) *problem.Problem {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.published = append(s.published, env)
	return nil
}

func (s *spyPublisher) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.published)
}

func (s *spyPublisher) snapshot() []envelope.Envelope {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]envelope.Envelope, len(s.published))
	copy(out, s.published)
	return out
}

// fakeSequencer provides monotonically increasing sequence numbers.
type fakeSequencer struct {
	mu  sync.Mutex
	seq map[string]int64
}

func newFakeSequencer() *fakeSequencer {
	return &fakeSequencer{seq: make(map[string]int64)}
}

func (f *fakeSequencer) Next(venue, instrument string) (int64, *problem.Problem) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := venue + ":" + instrument
	f.seq[key]++
	return f.seq[key], nil
}

// parentActor spawns the subsystem as a child and captures messages it receives.
type parentActor struct {
	cfg    mdruntime.SubsystemConfig
	ch     chan any
	subPID *actor.PID
}

func (p *parentActor) Receive(ctx *actor.Context) {
	switch m := ctx.Message().(type) {
	case actor.Initialized:
		// no-op; lifecycle preamble.
	case actor.Started:
		p.subPID = ctx.SpawnChild(mdruntime.NewSubsystemActor(p.cfg), "md-subsystem",
			actor.WithID("md-subsystem"))
	case actor.Stopped:
	default:
		select {
		case p.ch <- m:
		default:
		}
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func newTestService(pub *spyPublisher) *mdapp.MarketDataService {
	return &mdapp.MarketDataService{
		Ingest: mdapp.NewIngestMarketData(
			fakeClock{},
			newFakeSequencer(),
			pub,
		),
	}
}

type fakeClock struct{}

func (fakeClock) Now() time.Time      { return time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC) }
func (fakeClock) NowUnixMilli() int64 { return time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli() }

func makeWsMessage(exchange, endpoint string, data []byte) *ws.WsMessage {
	return &ws.WsMessage{
		Exchange:   exchange,
		BucketID:   1,
		ConsumerID: "c-1",
		Endpoint:   endpoint,
		Data:       data,
		RecvAt:     time.Now(),
	}
}

func makeWsError(exchange, kind string, err error) *ws.WsError {
	return &ws.WsError{
		Exchange:   exchange,
		BucketID:   1,
		ConsumerID: "c-1",
		Endpoint:   "wss://fake",
		Kind:       kind,
		Err:        err,
		At:         time.Now(),
	}
}

func makeWsState(exchange, status string) *ws.WsState {
	return &ws.WsState{
		Exchange:   exchange,
		BucketID:   1,
		ConsumerID: "c-1",
		Endpoint:   "wss://fake",
		Status:     status,
		At:         time.Now(),
	}
}

func newEngine(t *testing.T) *actor.Engine {
	t.Helper()
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	return e
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

// TestSubsystem_WsMessage_callsIngest verifies that a WsMessage with a valid
// ParseFunc results in a published envelope via the EventPublisher port.
func TestSubsystem_WsMessage_callsIngest(t *testing.T) {
	pub := &spyPublisher{}
	svc := newTestService(pub)
	parse := mdruntime.MakeRawParseFunc("binance", "BTC-USDT")

	cfg := mdruntime.SubsystemConfig{
		Service:      svc,
		ParseMessage: parse,
	}

	e := newEngine(t)
	pid := e.Spawn(mdruntime.NewSubsystemActor(cfg), "subsystem", actor.WithID("subsystem"))

	msg := makeWsMessage("binance", "wss://fake", []byte(`{"price":42000}`))
	e.Send(pid, msg)

	waitFor(t, 2*time.Second, func() bool { return pub.count() == 1 })

	<-e.Poison(pid).Done()
}

// TestSubsystem_WsMessage_nilParseFn_dropsMessage verifies that when
// ParseMessage is nil, no ingest call is made and nothing panics.
func TestSubsystem_WsMessage_nilParseFn_dropsMessage(t *testing.T) {
	pub := &spyPublisher{}
	svc := newTestService(pub)

	cfg := mdruntime.SubsystemConfig{
		Service:      svc,
		ParseMessage: nil, // intentionally nil
	}

	e := newEngine(t)
	pid := e.Spawn(mdruntime.NewSubsystemActor(cfg), "subsystem", actor.WithID("subsystem"))

	msg := makeWsMessage("binance", "wss://fake", []byte(`{"price":42000}`))
	e.Send(pid, msg)
	// Let the actor process.
	time.Sleep(50 * time.Millisecond)

	if pub.count() != 0 {
		t.Fatalf("expected 0 published events with nil parser, got %d", pub.count())
	}

	<-e.Poison(pid).Done()
}

// TestSubsystem_ParseSkip_doesNotIngest verifies that when ParseFunc returns
// skip=true the ingest use case is not called.
func TestSubsystem_ParseSkip_doesNotIngest(t *testing.T) {
	pub := &spyPublisher{}
	svc := newTestService(pub)

	skipAll := mdruntime.ParseFunc(func(msg *ws.WsMessage) (mdapp.IngestRequest, bool) {
		return mdapp.IngestRequest{}, true // skip
	})

	cfg := mdruntime.SubsystemConfig{
		Service:      svc,
		ParseMessage: skipAll,
	}

	e := newEngine(t)
	pid := e.Spawn(mdruntime.NewSubsystemActor(cfg), "subsystem", actor.WithID("subsystem"))

	msg := makeWsMessage("binance", "wss://fake", []byte(`pong`))
	e.Send(pid, msg)
	time.Sleep(50 * time.Millisecond)

	if pub.count() != 0 {
		t.Fatalf("expected 0 published events (skip=true), got %d", pub.count())
	}

	<-e.Poison(pid).Done()
}

// TestSubsystem_WsError_TransientDoesNotEscalate verifies transient websocket
// failures do not trigger parent-level ChildFailed restarts.
func TestSubsystem_WsError_TransientDoesNotEscalate(t *testing.T) {
	pub := &spyPublisher{}
	svc := newTestService(pub)

	cfg := mdruntime.SubsystemConfig{
		Service:      svc,
		ParseMessage: mdruntime.MakeRawParseFunc("binance", "BTC-USDT"),
	}

	parentCh := make(chan any, 16)
	pa := &parentActor{cfg: cfg, ch: parentCh}

	e := newEngine(t)
	parentPID := e.Spawn(func() actor.Receiver { return pa }, "parent", actor.WithID("parent"))

	// Wait for the parent and child to start.
	time.Sleep(50 * time.Millisecond)

	// Inject a WsError directly into the subsystem child.
	subPID := pa.subPID
	if subPID == nil {
		t.Fatal("subsystem PID not set; parent did not spawn child")
	}

	wsErr := makeWsError("binance", "dial", errFakeRead)
	e.Send(subPID, wsErr)

	select {
	case raw := <-parentCh:
		t.Fatalf("expected no ChildFailed for transient ws error, got %T", raw)
	case <-time.After(250 * time.Millisecond):
	}

	<-e.Poison(parentPID).Done()
}

// TestSubsystem_WsError_UnknownEscalates verifies non-transient websocket
// failures are forwarded to parent actor as runtime.ChildFailed.
func TestSubsystem_WsError_UnknownEscalates(t *testing.T) {
	pub := &spyPublisher{}
	svc := newTestService(pub)

	cfg := mdruntime.SubsystemConfig{
		Service:      svc,
		ParseMessage: mdruntime.MakeRawParseFunc("binance", "BTC-USDT"),
	}

	parentCh := make(chan any, 16)
	pa := &parentActor{cfg: cfg, ch: parentCh}

	e := newEngine(t)
	parentPID := e.Spawn(func() actor.Receiver { return pa }, "parent", actor.WithID("parent"))
	time.Sleep(50 * time.Millisecond)

	subPID := pa.subPID
	if subPID == nil {
		t.Fatal("subsystem PID not set; parent did not spawn child")
	}

	wsErr := makeWsError("binance", "unknown", errFakeRead)
	e.Send(subPID, wsErr)

	var got runtime.ChildFailed
	select {
	case raw := <-parentCh:
		var ok bool
		got, ok = raw.(runtime.ChildFailed)
		if !ok {
			t.Fatalf("expected runtime.ChildFailed, got %T", raw)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for ChildFailed message")
	}

	if got.Subsystem != runtime.SubsystemMarketData {
		t.Errorf("expected subsystem=%s, got %s", runtime.SubsystemMarketData, got.Subsystem)
	}
	if got.Kind != "unknown" {
		t.Errorf("expected kind=unknown, got %s", got.Kind)
	}

	<-e.Poison(parentPID).Done()
}

// TestSubsystem_WsState_doesNotPanic verifies that all WsState status values
// are handled without panic.
func TestSubsystem_WsState_doesNotPanic(t *testing.T) {
	pub := &spyPublisher{}
	svc := newTestService(pub)

	cfg := mdruntime.SubsystemConfig{
		Service:      svc,
		ParseMessage: mdruntime.MakeRawParseFunc("binance", "BTC-USDT"),
	}

	e := newEngine(t)
	pid := e.Spawn(mdruntime.NewSubsystemActor(cfg), "subsystem", actor.WithID("subsystem"))
	time.Sleep(20 * time.Millisecond)

	statuses := []string{"starting", "dialing", "connected", "subscribed", "error", "closed"}
	for _, status := range statuses {
		e.Send(pid, makeWsState("binance", status))
	}
	time.Sleep(50 * time.Millisecond)

	<-e.Poison(pid).Done()
}

// TestSubsystem_MultipleMessages_allIngested verifies sequential message
// delivery and ingest under a real engine.
func TestSubsystem_MultipleMessages_allIngested(t *testing.T) {
	pub := &spyPublisher{}
	svc := newTestService(pub)
	parse := mdruntime.MakeRawParseFunc("binance", "ETH-USDT")

	cfg := mdruntime.SubsystemConfig{
		Service:      svc,
		ParseMessage: parse,
	}

	e := newEngine(t)
	pid := e.Spawn(mdruntime.NewSubsystemActor(cfg), "subsystem", actor.WithID("subsystem"))

	const n = 10
	for i := 0; i < n; i++ {
		e.Send(pid, makeWsMessage("binance", "wss://fake", []byte(`{"price":1000}`)))
	}

	waitFor(t, 2*time.Second, func() bool { return pub.count() == n })

	<-e.Poison(pid).Done()
}

// TestSubsystem_NoManagerSpawned_whenConfigIsNil verifies that no manager is
// spawned when ManagerConfig is nil (test-only / processor mode).
func TestSubsystem_NoManagerSpawned_whenConfigIsNil(t *testing.T) {
	pub := &spyPublisher{}
	svc := newTestService(pub)

	cfg := mdruntime.SubsystemConfig{
		Service:       svc,
		ParseMessage:  mdruntime.MakeRawParseFunc("binance", "BTC-USDT"),
		ManagerConfig: nil, // explicit
	}

	e := newEngine(t)
	pid := e.Spawn(mdruntime.NewSubsystemActor(cfg), "subsystem", actor.WithID("subsystem"))
	time.Sleep(30 * time.Millisecond)
	// No panic or error expected; actor starts and waits for messages.
	<-e.Poison(pid).Done()
}

func TestSubsystem_MarkPriceNormalization_setsCanonicalAndIdempotency(t *testing.T) {
	pub := &spyPublisher{}
	svc := newTestService(pub)
	parse := mdruntime.ParseFunc(func(_ *ws.WsMessage) (mdapp.IngestRequest, bool) {
		return mdapp.IngestRequest{
			Venue:      " binance ",
			Instrument: " btc/usdt ",
			EventType:  "MARKETDATA.MARKPRICE",
			Version:    1,
			TsExchange: 1710000001000,
			Payload: domain.MarkPriceTickV1{
				MarkPrice:   50000,
				IndexPrice:  49990,
				FundingRate: 0.0001,
				Timestamp:   1710000001000,
			},
		}, false
	})

	cfg := mdruntime.SubsystemConfig{
		Service:      svc,
		ParseMessage: parse,
	}

	e := newEngine(t)
	pid := e.Spawn(mdruntime.NewSubsystemActor(cfg), "subsystem", actor.WithID("subsystem"))

	msg := makeWsMessage("binance", "wss://fake", []byte(`{"e":"markPriceUpdate"}`))
	msg.RecvAt = time.UnixMilli(1710000001000)
	e.Send(pid, msg)

	waitFor(t, 2*time.Second, func() bool { return pub.count() == 1 })
	env := pub.snapshot()[0]
	if env.Venue != "BINANCE" {
		t.Fatalf("env venue=%q want BINANCE", env.Venue)
	}
	if env.Instrument != "BTCUSDT" {
		t.Fatalf("env instrument=%q want BTCUSDT", env.Instrument)
	}
	if env.Type != "marketdata.markprice" {
		t.Fatalf("env type=%q want marketdata.markprice", env.Type)
	}
	if env.IdempotencyKey == "" {
		t.Fatal("idempotency key must not be empty after normalization")
	}
	decoded, p := codec.DecodePayload(env.Type, env.Version, env.ContentType, env.Payload)
	if p != nil {
		t.Fatalf("decode payload failed: %v", p)
	}
	if _, ok := decoded.(domain.MarkPriceTickV1); !ok {
		t.Fatalf("decoded type=%T want=%T", decoded, domain.MarkPriceTickV1{})
	}

	<-e.Poison(pid).Done()
}

func TestSubsystem_LiquidationDuplicateSkippedByNormalizer(t *testing.T) {
	pub := &spyPublisher{}
	svc := newTestService(pub)
	parse := mdruntime.ParseFunc(func(_ *ws.WsMessage) (mdapp.IngestRequest, bool) {
		return mdapp.IngestRequest{
			Venue:      "BYBIT",
			Instrument: "ETH-USDT",
			EventType:  "marketdata.liquidation",
			Version:    1,
			TsExchange: 1710000002000,
			Payload: domain.LiquidationTickV1{
				Side:      "SELL",
				Price:     3200.5,
				Size:      10,
				Timestamp: 1710000002000,
			},
		}, false
	})

	cfg := mdruntime.SubsystemConfig{
		Service:      svc,
		ParseMessage: parse,
	}

	e := newEngine(t)
	pid := e.Spawn(mdruntime.NewSubsystemActor(cfg), "subsystem", actor.WithID("subsystem"))

	msg := makeWsMessage("bybit", "wss://fake", []byte(`{"e":"forceOrder"}`))
	msg.RecvAt = time.UnixMilli(1710000002000)
	e.Send(pid, msg)
	e.Send(pid, msg)

	waitFor(t, 2*time.Second, func() bool { return pub.count() == 1 })
	time.Sleep(50 * time.Millisecond)
	if pub.count() != 1 {
		t.Fatalf("expected duplicate liquidation to be skipped; published=%d", pub.count())
	}

	<-e.Poison(pid).Done()
}

// errFakeRead is a sentinel error used in WsError tests.
var errFakeRead = fakeError("fake read error")

type fakeError string

func (e fakeError) Error() string { return string(e) }
