//go:build integration

package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	deliveryruntime "github.com/FabioCaffarello/stream-analytics/internal/actors/delivery/runtime"
	"github.com/FabioCaffarello/stream-analytics/internal/adapters/bus"
	"github.com/FabioCaffarello/stream-analytics/internal/adapters/storage/timescale"
	wsserver "github.com/FabioCaffarello/stream-analytics/internal/interfaces/ws"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/envelope"
	"github.com/anthdm/hollywood/actor"
	"github.com/gorilla/websocket"
)

func wsURLFromHTTP(httpURL string) string {
	return "ws" + strings.TrimPrefix(httpURL, "http")
}

func requireLoopbackListener(t *testing.T) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("loopback listener unavailable in this environment: %v", err)
		return
	}
	_ = ln.Close()
}

func mustNewEngine(t *testing.T) *actor.Engine {
	t.Helper()
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	return e
}

func mustReadPID(t *testing.T, ch <-chan *actor.PID, name string) *actor.PID {
	t.Helper()
	select {
	case pid := <-ch:
		return pid
	case <-time.After(time.Second):
		t.Fatalf("timeout waiting for %s pid", name)
		return nil
	}
}

func mustDialWS(t *testing.T, serverURL string) *websocket.Conn {
	t.Helper()
	headers := http.Header{}
	headers.Set("X-API-Key", "k1")
	conn, _, err := websocket.DefaultDialer.Dial(wsURLFromHTTP(serverURL)+"/ws", headers)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	return conn
}

func mustWriteOp(t *testing.T, conn *websocket.Conn, op, subject, requestID string) {
	t.Helper()
	if err := conn.WriteJSON(map[string]any{
		"op":         op,
		"subject":    subject,
		"request_id": requestID,
	}); err != nil {
		t.Fatalf("%s write: %v", op, err)
	}
}

func mustReadType(t *testing.T, conn *websocket.Conn, wantType string, timeout time.Duration, context string) map[string]any {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	msg := map[string]any{}
	if err := conn.ReadJSON(&msg); err != nil {
		t.Fatalf("%s read: %v", context, err)
	}
	if got := msg["type"]; got != wantType {
		t.Fatalf("%s type=%v want=%v", context, got, wantType)
	}
	return msg
}

func publishCandle(eventBus *bus.InMemoryBus, seq int64) {
	_ = eventBus.Publish(context.Background(), envelope.Envelope{
		Type:       "aggregation.candle",
		Version:    1,
		Venue:      "binance",
		Instrument: "BTC-USDT",
		Seq:        seq,
		TsIngest:   time.Now().UnixMilli(),
		Payload:    []byte(`{"v":1}`),
	})
}

func startLoopbackHTTPServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	srv := httptest.NewUnstartedServer(handler)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("loopback listener unavailable in this environment: %v", err)
		return nil
	}
	srv.Listener = ln
	srv.Start()
	return srv
}

func TestE2E_WSDelivery_SubscribeReceiveUnsubscribe(t *testing.T) {
	requireLoopbackListener(t)
	e := mustNewEngine(t)
	eventBus := bus.NewInMemoryBus(128)
	defer eventBus.Close()

	routerPIDCh := make(chan *actor.PID, 1)
	subsystemPIDCh := make(chan *actor.PID, 1)
	rangeStore := timescale.NewDeliveryRangeStore(256)

	subsystemPID := e.Spawn(deliveryruntime.NewSubsystemActor(deliveryruntime.SubsystemConfig{
		EnvelopeCh: eventBus.Subscribe(),
		Router: deliveryruntime.RouterConfig{
			Timeframe:     "raw",
			EnvelopeStore: rangeStore,
		},
		OnRouterReady: func(pid *actor.PID) {
			select {
			case routerPIDCh <- pid:
			default:
			}
		},
		OnReady: func(subPID, _ *actor.PID) {
			select {
			case subsystemPIDCh <- subPID:
			default:
			}
		},
	}), "delivery-subsystem")
	defer e.Poison(subsystemPID)

	routerPID := mustReadPID(t, routerPIDCh, "router")
	subsystemParentPID := mustReadPID(t, subsystemPIDCh, "subsystem")

	ws := wsserver.NewServer(
		e,
		routerPID,
		nil,
		rangeStore,
		256,
		wsserver.WithAuthConfig(wsserver.AuthConfig{
			Enabled: true,
			APIKeys: map[string]string{"k1": "client-a"},
		}),
		wsserver.WithSessionSpawner(func(cfg deliveryruntime.SessionConfig) *actor.PID {
			resp := e.Request(subsystemParentPID, deliveryruntime.SpawnSession{Config: cfg}, time.Second)
			result, reqErr := resp.Result()
			if reqErr != nil {
				return nil
			}
			ack, ok := result.(deliveryruntime.SpawnSessionAck)
			if !ok {
				return nil
			}
			return ack.PID
		}),
	)

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", ws.HandleUpgrade)
	srv := startLoopbackHTTPServer(t, mux)
	defer srv.Close()

	conn := mustDialWS(t, srv.URL)
	defer func() { _ = conn.Close() }()

	subject := "aggregation.candle/binance/BTC-USDT/raw"
	mustWriteOp(t, conn, "subscribe", subject, "s1")
	mustReadType(t, conn, "ack", time.Second, "subscribe ack")
	publishCandle(eventBus, 1)
	mustReadType(t, conn, "event", time.Second, "event")
	mustWriteOp(t, conn, "unsubscribe", subject, "u1")
	mustReadType(t, conn, "ack", time.Second, "unsubscribe ack")

	time.Sleep(50 * time.Millisecond)
	publishCandle(eventBus, 2)

	_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	evt := map[string]any{}
	if err := conn.ReadJSON(&evt); err == nil {
		t.Fatal("expected no event after unsubscribe")
	}
}
