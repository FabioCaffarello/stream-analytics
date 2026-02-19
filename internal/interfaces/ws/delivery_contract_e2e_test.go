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

type captureActor struct {
	ch chan any
}

func (a *captureActor) Receive(c *actor.Context) {
	switch c.Message().(type) {
	case actor.Initialized, actor.Started, actor.Stopped:
		return
	default:
		select {
		case a.ch <- c.Message():
		default:
		}
	}
}

type staticRangeStore struct {
	items []ports.RangeItem
}

func (s *staticRangeStore) GetRange(_ context.Context, _ domain.Subject, fromMs, toMs int64, limit int) ([]ports.RangeItem, *problem.Problem) {
	out := make([]ports.RangeItem, 0, len(s.items))
	for _, it := range s.items {
		if fromMs > 0 && it.TsIngest < fromMs {
			continue
		}
		if toMs > 0 && it.TsIngest > toMs {
			continue
		}
		out = append(out, it)
	}
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

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

func waitRouterMessage[T any](t *testing.T, ch <-chan any, timeout time.Duration) T {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case m := <-ch:
			if v, ok := m.(T); ok {
				return v
			}
		case <-deadline:
			var zero T
			t.Fatalf("timeout waiting for %T", zero)
		}
	}
}

func extractRangeSignature(t *testing.T, msg map[string]any) []string {
	t.Helper()
	rawItems, ok := msg["items"].([]any)
	if !ok {
		t.Fatalf("items type=%T", msg["items"])
	}
	out := make([]string, 0, len(rawItems))
	for _, it := range rawItems {
		item, ok := it.(map[string]any)
		if !ok {
			t.Fatalf("item type=%T", it)
		}
		seqVal, ok := item["seq"]
		if !ok {
			seqVal = item["Seq"]
		}
		tsVal, ok := item["ts_ingest"]
		if !ok {
			tsVal = item["TsIngest"]
		}
		payloadVal, ok := item["payload"]
		if !ok {
			payloadVal = item["Payload"]
		}
		seq, ok := seqVal.(float64)
		if !ok {
			t.Fatalf("seq type=%T", seqVal)
		}
		ts, ok := tsVal.(float64)
		if !ok {
			t.Fatalf("ts type=%T", tsVal)
		}
		payload, ok := payloadVal.(string)
		if !ok {
			t.Fatalf("payload type=%T", payloadVal)
		}
		out = append(out, strconv.FormatInt(int64(seq), 10)+"|"+strconv.FormatInt(int64(ts), 10)+"|"+payload)
	}
	return out
}

func TestWSRangeDeterminismReplay(t *testing.T) {
	requireLoopbackListener(t)

	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "delivery-router-e2e")
	defer e.Poison(routerPID)

	ws := NewServer(e, routerPID, nil, &staticRangeStore{
		items: []ports.RangeItem{
			{Seq: 3, TsIngest: 3000, Payload: []byte(`{"v":3}`)},
			{Seq: 1, TsIngest: 1000, Payload: []byte(`{"v":1}`)},
			{Seq: 2, TsIngest: 2000, Payload: []byte(`{"v":2}`)},
		},
	}, 256)
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", ws.HandleWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURLFromHTTP(srv.URL)+"/ws", nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	defer func() { _ = conn.Close() }()

	_ = waitRouterMessage[deliveryruntime.RegisterSession](t, routerCh, time.Second)

	req := map[string]any{
		"op":         "getrange",
		"subject":    "aggregation.snapshot/binance/BTC-USDT/raw",
		"request_id": "r1",
		"params": map[string]any{
			"from_ms": 0,
			"to_ms":   0,
			"limit":   3,
			"page":    1,
		},
	}
	if err := conn.WriteJSON(req); err != nil {
		t.Fatalf("write req1: %v", err)
	}
	var resp1 map[string]any
	if err := conn.ReadJSON(&resp1); err != nil {
		t.Fatalf("read resp1: %v", err)
	}
	if got, want := resp1["type"], "range"; got != want {
		t.Fatalf("type=%v want=%v", got, want)
	}

	if err := conn.WriteJSON(req); err != nil {
		t.Fatalf("write req2: %v", err)
	}
	var resp2 map[string]any
	if err := conn.ReadJSON(&resp2); err != nil {
		t.Fatalf("read resp2: %v", err)
	}
	sig1 := extractRangeSignature(t, resp1)
	sig2 := extractRangeSignature(t, resp2)
	if len(sig1) != len(sig2) {
		t.Fatalf("signature length mismatch: %d vs %d", len(sig1), len(sig2))
	}
	for i := range sig1 {
		if sig1[i] != sig2[i] {
			t.Fatalf("range response is not deterministic: %v vs %v", sig1, sig2)
		}
	}
}

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
