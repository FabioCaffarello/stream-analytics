package deliveryruntime

import (
	"testing"
	"time"

	"github.com/anthdm/hollywood/actor"
	"github.com/market-raccoon/internal/core/delivery/domain"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/ids"
)

type captureActor struct{ ch chan any }

func (c *captureActor) Receive(ctx *actor.Context) {
	switch m := ctx.Message().(type) {
	case actor.Initialized, actor.Started, actor.Stopped:
	default:
		select {
		case c.ch <- m:
		default:
		}
	}
}

func mustParseSubject(t *testing.T, raw string) domain.Subject {
	t.Helper()
	s, p := domain.ParseSubject(raw)
	if p != nil {
		t.Fatalf("ParseSubject(%q): %v", raw, p)
	}
	return s
}

func waitForMessage[T any](t *testing.T, ch <-chan any, timeout time.Duration) T {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case raw := <-ch:
			if msg, ok := raw.(T); ok {
				return msg
			}
		case <-deadline:
			var zero T
			t.Fatalf("timeout waiting for %T", zero)
		}
	}
}

func TestRouter_subscribeUnsubscribeAndBroadcast(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	envCh := make(chan envelope.Envelope, 16)
	routerPID := e.Spawn(NewRouterActor(RouterConfig{EnvelopeCh: envCh, Timeframe: "raw"}), "router")
	defer e.Poison(routerPID)

	ch1 := make(chan any, 16)
	ch2 := make(chan any, 16)
	s1 := e.Spawn(func() actor.Receiver { return &captureActor{ch: ch1} }, "session-capture")
	s2 := e.Spawn(func() actor.Receiver { return &captureActor{ch: ch2} }, "session-capture")
	defer e.Poison(s1)
	defer e.Poison(s2)

	id1 := ids.NewSessionID()
	id2 := ids.NewSessionID()
	subject := mustParseSubject(t, "marketdata.trade/binance/BTC-USDT/raw")

	e.Send(routerPID, RegisterSession{SessionID: id1, PID: s1})
	e.Send(routerPID, RegisterSession{SessionID: id2, PID: s2})
	e.Send(routerPID, SubscribeSession{SessionID: id1, Subject: subject})
	e.Send(routerPID, SubscribeSession{SessionID: id2, Subject: subject})

	envCh <- envelope.Envelope{Type: "marketdata.trade", Version: 1, Venue: "binance", Instrument: "BTC-USDT", Seq: 10, TsIngest: time.Now().UnixMilli(), Payload: []byte("x")}

	msg1 := waitForMessage[DeliveryEvent](t, ch1, time.Second)
	msg2 := waitForMessage[DeliveryEvent](t, ch2, time.Second)
	if msg1.Subject != subject || msg2.Subject != subject {
		t.Fatalf("unexpected subject: msg1=%s msg2=%s", msg1.Subject.String(), msg2.Subject.String())
	}

	e.Send(routerPID, UnsubscribeSession{SessionID: id2, Subject: subject})
	envCh <- envelope.Envelope{Type: "marketdata.trade", Version: 1, Venue: "binance", Instrument: "BTC-USDT", Seq: 11, TsIngest: time.Now().UnixMilli(), Payload: []byte("y")}

	_ = waitForMessage[DeliveryEvent](t, ch1, time.Second)
	select {
	case raw := <-ch2:
		if _, ok := raw.(DeliveryEvent); ok {
			t.Fatal("session 2 should not receive after unsubscribe")
		}
	case <-time.After(150 * time.Millisecond):
	}

	e.Send(routerPID, UnregisterSession{SessionID: id1})
	envCh <- envelope.Envelope{Type: "marketdata.trade", Version: 1, Venue: "binance", Instrument: "BTC-USDT", Seq: 12, TsIngest: time.Now().UnixMilli(), Payload: []byte("z")}
	select {
	case raw := <-ch1:
		if _, ok := raw.(DeliveryEvent); ok {
			t.Fatal("session 1 should not receive after unregister")
		}
	case <-time.After(150 * time.Millisecond):
	}
}

func TestRouter_routesAggregationSnapshot(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	envCh := make(chan envelope.Envelope, 16)
	routerPID := e.Spawn(NewRouterActor(RouterConfig{EnvelopeCh: envCh, Timeframe: "raw"}), "router")
	defer e.Poison(routerPID)

	ch := make(chan any, 16)
	s := e.Spawn(func() actor.Receiver { return &captureActor{ch: ch} }, "session-capture")
	defer e.Poison(s)

	id := ids.NewSessionID()
	subject := mustParseSubject(t, "aggregation.snapshot/binance/BTC-USDT/raw")

	e.Send(routerPID, RegisterSession{SessionID: id, PID: s})
	e.Send(routerPID, SubscribeSession{SessionID: id, Subject: subject})

	envCh <- envelope.Envelope{
		Type:       "aggregation.snapshot",
		Version:    1,
		Venue:      "binance",
		Instrument: "BTC-USDT",
		Seq:        100,
		TsIngest:   time.Now().UnixMilli(),
		Payload:    []byte(`{"ok":true}`),
	}

	msg := waitForMessage[DeliveryEvent](t, ch, time.Second)
	if got, want := msg.Subject.String(), "aggregation.snapshot/binance/BTCUSDT/raw"; got != want {
		t.Fatalf("subject=%q want=%q", got, want)
	}
}

func TestRouter_rejectsUngovernedEnvelopeType(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	envCh := make(chan envelope.Envelope, 16)
	routerPID := e.Spawn(NewRouterActor(RouterConfig{EnvelopeCh: envCh, Timeframe: "raw"}), "router")
	defer e.Poison(routerPID)

	ch := make(chan any, 16)
	s := e.Spawn(func() actor.Receiver { return &captureActor{ch: ch} }, "session-capture")
	defer e.Poison(s)

	id := ids.NewSessionID()
	subject := mustParseSubject(t, "insights.unknown/binance/BTCUSDT/raw")

	e.Send(routerPID, RegisterSession{SessionID: id, PID: s})
	e.Send(routerPID, SubscribeSession{SessionID: id, Subject: subject})
	envCh <- envelope.Envelope{
		Type:       "insights.unknown",
		Version:    1,
		Venue:      "binance",
		Instrument: "BTCUSDT",
		Seq:        1,
		TsIngest:   time.Now().UnixMilli(),
		Payload:    []byte(`{}`),
	}

	select {
	case raw := <-ch:
		if _, ok := raw.(DeliveryEvent); ok {
			t.Fatal("unexpected delivery event for ungoverned envelope type")
		}
	case <-time.After(200 * time.Millisecond):
	}
}
