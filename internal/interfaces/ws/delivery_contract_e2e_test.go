//go:build integration
// +build integration

package wsserver

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/anthdm/hollywood/actor"
	"github.com/gorilla/websocket"
	deliveryruntime "github.com/market-raccoon/internal/actors/delivery/runtime"
	"github.com/market-raccoon/internal/core/delivery/domain"
	"github.com/market-raccoon/internal/core/delivery/ports"
	"github.com/market-raccoon/internal/shared/problem"
)

// Integration helpers moved to untagged test helpers so unit wrappers can reuse them.

func TestWSRaceSubscribeUnsubscribeNoLeak(t *testing.T) {
	requireLoopbackListener(t)

	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	routerCh := make(chan any, 256)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "delivery-router-race")
	defer e.Poison(routerPID)

	ws := NewServer(e, routerPID, nil, &staticRangeStore{}, 256)
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", ws.HandleWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURLFromHTTP(srv.URL)+"/ws", nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	_ = waitRouterMessage[deliveryruntime.RegisterSession](t, routerCh, time.Second)

	subject := "marketdata.trade/binance/BTC-USDT/raw"
	for i := 0; i < 10; i++ {
		if err := conn.WriteJSON(map[string]any{
			"op":         "subscribe",
			"subject":    subject,
			"request_id": "s",
		}); err != nil {
			t.Fatalf("subscribe write: %v", err)
		}
		var ack map[string]any
		if err := conn.ReadJSON(&ack); err != nil {
			t.Fatalf("subscribe read: %v", err)
		}
		if got, want := ack["type"], "ack"; got != want {
			t.Fatalf("ack type=%v want=%v", got, want)
		}

		if err := conn.WriteJSON(map[string]any{
			"op":         "unsubscribe",
			"subject":    subject,
			"request_id": "u",
		}); err != nil {
			t.Fatalf("unsubscribe write: %v", err)
		}
		if err := conn.ReadJSON(&ack); err != nil {
			t.Fatalf("unsubscribe read: %v", err)
		}
	}

	_ = conn.Close()
	_ = waitRouterMessage[deliveryruntime.UnregisterSession](t, routerCh, 2*time.Second)
}

func TestWSReconnectResubscribeIdempotent(t *testing.T) {
	requireLoopbackListener(t)

	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	routerCh := make(chan any, 256)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "delivery-router-reconnect")
	defer e.Poison(routerPID)

	ws := NewServer(e, routerPID, nil, &staticRangeStore{}, 256)
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", ws.HandleWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	connectAndSubscribe := func() {
		conn, _, err := websocket.DefaultDialer.Dial(wsURLFromHTTP(srv.URL)+"/ws", nil)
		if err != nil {
			t.Fatalf("dial ws: %v", err)
		}
		_ = waitRouterMessage[deliveryruntime.RegisterSession](t, routerCh, time.Second)
		if err := conn.WriteJSON(map[string]any{
			"op":         "subscribe",
			"subject":    "marketdata.trade/binance/BTC-USDT/raw",
			"request_id": "s",
		}); err != nil {
			t.Fatalf("subscribe write: %v", err)
		}
		var ack map[string]any
		if err := conn.ReadJSON(&ack); err != nil {
			t.Fatalf("subscribe read: %v", err)
		}
		if got, want := ack["type"], "ack"; got != want {
			t.Fatalf("ack type=%v want=%v", got, want)
		}
		_ = conn.Close()
		_ = waitRouterMessage[deliveryruntime.UnregisterSession](t, routerCh, 2*time.Second)
	}

	connectAndSubscribe()
	connectAndSubscribe()
}
