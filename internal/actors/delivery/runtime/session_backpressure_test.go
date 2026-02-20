package deliveryruntime

import (
	"testing"
	"time"

	"github.com/anthdm/hollywood/actor"
	"github.com/market-raccoon/internal/core/delivery/domain"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestWSBackpressureSlowClientDropPolicy(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-capture-backpressure")
	defer e.Poison(routerPID)

	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{
		RouterPID:         routerPID,
		Conn:              conn,
		OutboundQueueSize: 1,
	}), "ws-session-backpressure")
	defer e.Poison(sessionPID)

	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	before := testutil.ToFloat64(metrics.WSDropsTotal.WithLabelValues("queue_full"))
	evt := DeliveryEvent{
		Subject: mustParseSubjectForSession(t, "aggregation.snapshot/binance/BTC-USDT/raw"),
		Env: envelope.Envelope{
			Type:       "aggregation.snapshot",
			Version:    1,
			Venue:      "binance",
			Instrument: "BTC-USDT",
			Seq:        1,
			TsIngest:   time.Now().UnixMilli(),
			Payload:    []byte(`{"seq":1}`),
		},
	}

	e.Send(sessionPID, evt)
	e.Send(sessionPID, evt)
	e.Send(sessionPID, evt)

	deadline := time.After(2 * time.Second)
	for {
		after := testutil.ToFloat64(metrics.WSDropsTotal.WithLabelValues("queue_full"))
		if after >= before+1 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("expected ws drop increment, got before=%f after=%f", before, after)
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func TestWSBackpressureSlowClientThresholdDisconnects(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-capture-threshold")
	defer e.Poison(routerPID)

	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{
		RouterPID:               routerPID,
		Conn:                    conn,
		OutboundQueueSize:       1,
		SlowClientDropThreshold: 2,
	}), "ws-session-backpressure-threshold")
	defer e.Poison(sessionPID)

	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)
	before := testutil.ToFloat64(metrics.WSDropsTotal.WithLabelValues("slow_client_disconnect"))
	evt := DeliveryEvent{
		Subject: mustParseSubjectForSession(t, "aggregation.snapshot/binance/BTC-USDT/raw"),
		Env: envelope.Envelope{
			Type:       "aggregation.snapshot",
			Version:    1,
			Venue:      "binance",
			Instrument: "BTC-USDT",
			Seq:        1,
			TsIngest:   time.Now().UnixMilli(),
			Payload:    []byte(`{"seq":1}`),
		},
	}

	e.Send(sessionPID, evt)
	e.Send(sessionPID, evt)
	e.Send(sessionPID, evt)
	e.Send(sessionPID, evt)

	_ = waitForMessage[UnregisterSession](t, routerCh, 2*time.Second)
	if !conn.closed.Load() {
		t.Fatal("connection should be closed after threshold disconnect")
	}
	after := testutil.ToFloat64(metrics.WSDropsTotal.WithLabelValues("slow_client_disconnect"))
	if after < before+1 {
		t.Fatalf("expected slow_client_disconnect increment, got before=%f after=%f", before, after)
	}
}

func TestWSBackpressureDropOldest_EmitsNewestEvent(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-capture-drop-oldest")
	defer e.Poison(routerPID)

	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{
		RouterPID:              routerPID,
		Conn:                   conn,
		OutboundQueueSize:      1,
		BackpressurePolicy:     domain.BackpressureDropOldest,
		BackpressurePriorities: nil,
	}), "ws-session-drop-oldest")
	defer e.Poison(sessionPID)

	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	evt1 := DeliveryEvent{
		Subject: mustParseSubjectForSession(t, "aggregation.snapshot/binance/BTC-USDT/raw"),
		Env: envelope.Envelope{
			Type:       "aggregation.snapshot",
			Version:    1,
			Venue:      "binance",
			Instrument: "BTC-USDT",
			Seq:        1,
			TsIngest:   1,
			Payload:    []byte(`{"seq":1}`),
		},
	}
	evt2 := evt1
	evt2.Env.Seq = 2
	evt2.Env.Payload = []byte(`{"seq":2}`)

	e.Send(sessionPID, evt1)
	e.Send(sessionPID, evt2)

	msg := <-conn.writeCh
	event, ok := msg.(wsEventFrame)
	if !ok || event.Type != "event" {
		t.Fatalf("expected event message, got %#v", msg)
	}
	if got := event.Seq; got != 2 {
		t.Fatalf("seq=%d want=2", got)
	}
}

func TestWSBackpressurePriorityDrop_HighPriorityReplacesLow(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-capture-priority")
	defer e.Poison(routerPID)

	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{
		RouterPID:          routerPID,
		Conn:               conn,
		OutboundQueueSize:  1,
		BackpressurePolicy: domain.BackpressurePriorityDrop,
	}), "ws-session-priority")
	defer e.Poison(sessionPID)

	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	low := DeliveryEvent{
		Subject: mustParseSubjectForSession(t, "aggregation.stats/binance/BTC-USDT/raw"),
		Env: envelope.Envelope{
			Type:       "aggregation.stats",
			Version:    1,
			Venue:      "binance",
			Instrument: "BTC-USDT",
			Seq:        1,
			TsIngest:   1,
			Payload:    []byte(`{"seq":1}`),
		},
	}
	high := low
	high.Subject = mustParseSubjectForSession(t, "marketdata.trade/binance/BTC-USDT/raw")
	high.Env.Type = "marketdata.trade"
	high.Env.Seq = 2
	high.Env.Payload = []byte(`{"seq":2}`)

	e.Send(sessionPID, low)
	e.Send(sessionPID, high)

	msg := <-conn.writeCh
	event, ok := msg.(wsEventFrame)
	if !ok || event.Type != "event" {
		t.Fatalf("expected event message, got %#v", msg)
	}
	if got := event.Seq; got != 2 {
		t.Fatalf("seq=%d want=2", got)
	}
}
