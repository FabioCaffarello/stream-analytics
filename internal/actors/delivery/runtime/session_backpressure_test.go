package deliveryruntime

import (
	"testing"
	"time"

	"github.com/anthdm/hollywood/actor"
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
