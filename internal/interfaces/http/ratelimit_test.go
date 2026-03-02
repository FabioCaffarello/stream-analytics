package httpserver_test

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/anthdm/hollywood/actor"
	"github.com/gorilla/websocket"
	deliveryruntime "github.com/market-raccoon/internal/actors/delivery/runtime"
	httpserver "github.com/market-raccoon/internal/interfaces/http"
	wsserver "github.com/market-raccoon/internal/interfaces/ws"
)

func readFrameSkipHello(t *testing.T, conn *websocket.Conn, timeout time.Duration) map[string]any {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		_ = conn.SetReadDeadline(deadline)
		var msg map[string]any
		if err := conn.ReadJSON(&msg); err != nil {
			t.Fatalf("read frame: %v", err)
		}
		if typ, _ := msg["type"].(string); typ == "hello" {
			continue
		}
		return msg
	}
}

type noopRouterActor struct{}

func (a *noopRouterActor) Receive(c *actor.Context) {
	switch c.Message().(type) {
	case actor.Initialized, actor.Started, actor.Stopped:
		return
	default:
		return
	}
}

func TestWSRateLimit_TokenBucket(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	routerPID := e.Spawn(func() actor.Receiver { return &noopRouterActor{} }, "ws-router-ratelimit")
	defer e.Poison(routerPID)

	ws := wsserver.NewServer(
		e,
		routerPID,
		nil,
		nil,
		256,
		wsserver.WithRateLimit(deliveryruntime.RateLimitConfig{
			Enabled:       true,
			MaxPerSecond:  1,
			BurstCapacity: 1,
		}),
	)
	srv := httpserver.NewServer(e, nil, ":0", false, nil, httpserver.WithWSHandler(ws.HandleWS))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURLFromHTTP(ts.URL)+"/ws", nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))

	if err := conn.WriteJSON(map[string]any{
		"op":         "subscribe",
		"subject":    "marketdata.trade/binance/BTC-USDT/raw",
		"request_id": "s1",
	}); err != nil {
		t.Fatalf("write first subscribe: %v", err)
	}
	first := readFrameSkipHello(t, conn, 2*time.Second)
	if got, want := first["type"], "ack"; got != want {
		t.Fatalf("first response type=%v want=%v", got, want)
	}

	if err := conn.WriteJSON(map[string]any{
		"op":         "subscribe",
		"subject":    "marketdata.trade/binance/ETH-USDT/raw",
		"request_id": "s2",
	}); err != nil {
		t.Fatalf("write second subscribe: %v", err)
	}
	second := readFrameSkipHello(t, conn, 2*time.Second)
	if got, want := second["type"], "error"; got != want {
		t.Fatalf("second response type=%v want=%v", got, want)
	}
	prob, ok := second["problem"].(map[string]any)
	if !ok {
		t.Fatalf("problem type=%T", second["problem"])
	}
	if got, want := prob["message"], "rate limit exceeded"; got != want {
		t.Fatalf("problem.message=%v want=%q", got, want)
	}
}
