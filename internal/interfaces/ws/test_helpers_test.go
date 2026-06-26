package wsserver

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	deliveryruntime "github.com/FabioCaffarello/stream-analytics/internal/actors/delivery/runtime"
	"github.com/FabioCaffarello/stream-analytics/internal/core/delivery/domain"
	"github.com/FabioCaffarello/stream-analytics/internal/core/delivery/ports"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
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

// TestWSRangeDeterminismReplay provides a deterministic getrange scenario used by
// several contract tests. Kept untagged so unit wrappers can delegate to it.
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
	resp1 := readFrameSkipHello(t, conn, 2*time.Second)
	if got, want := resp1["type"], "range"; got != want {
		t.Fatalf("type=%v want=%v", got, want)
	}

	if err := conn.WriteJSON(req); err != nil {
		t.Fatalf("write req2: %v", err)
	}
	resp2 := readFrameSkipHello(t, conn, 2*time.Second)
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
		var payloadStr string
		switch pv := payloadVal.(type) {
		case string:
			payloadStr = pv
		case map[string]any:
			// json.RawMessage payloads are inlined as JSON objects.
			b, _ := json.Marshal(pv)
			payloadStr = string(b)
		default:
			t.Fatalf("payload type=%T", payloadVal)
		}
		out = append(out, strconv.FormatInt(int64(seq), 10)+"|"+strconv.FormatInt(int64(ts), 10)+"|"+payloadStr)
	}
	return out
}

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
