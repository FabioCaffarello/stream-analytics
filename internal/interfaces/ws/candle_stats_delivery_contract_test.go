package wsserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/anthdm/hollywood/actor"
	"github.com/gorilla/websocket"
	deliveryruntime "github.com/market-raccoon/internal/actors/delivery/runtime"
	"github.com/market-raccoon/internal/shared/envelope"
)

func TestWSDelivery_CandleClosed_RoutedToSubscriber(t *testing.T) {
	requireLoopbackListener(t)

	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	envCh := make(chan envelope.Envelope, 32)
	routerPID := e.Spawn(deliveryruntime.NewRouterActor(deliveryruntime.RouterConfig{
		EnvelopeCh: envCh,
		Timeframe:  "raw",
	}), "delivery-router-candle")
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
	defer func() { _ = conn.Close() }()

	if err := conn.WriteJSON(map[string]any{
		"op":         "subscribe",
		"subject":    "aggregation.candle/binance/BTC-USDT/raw",
		"request_id": "sub-1",
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

	envCh <- envelope.Envelope{
		Type:       "aggregation.candle",
		Version:    1,
		Venue:      "binance",
		Instrument: "BTC-USDT",
		Seq:        1,
		TsIngest:   time.Now().UnixMilli(),
		Payload:    []byte(`{"candle":"closed"}`),
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var evt map[string]any
	if err := conn.ReadJSON(&evt); err != nil {
		t.Fatalf("event read: %v", err)
	}
	if got, want := evt["type"], "event"; got != want {
		t.Fatalf("event type=%v want=%v", got, want)
	}
	if got, want := evt["subject"], "aggregation.candle/binance/BTCUSDT/raw"; got != want {
		t.Fatalf("subject=%v want=%v", got, want)
	}
	if got, want := int64(evt["seq"].(float64)), int64(1); got != want {
		t.Fatalf("seq=%d want=%d", got, want)
	}
}

func TestWSDelivery_StatsClosed_RoutedToSubscriber(t *testing.T) {
	requireLoopbackListener(t)

	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	envCh := make(chan envelope.Envelope, 32)
	routerPID := e.Spawn(deliveryruntime.NewRouterActor(deliveryruntime.RouterConfig{
		EnvelopeCh: envCh,
		Timeframe:  "raw",
	}), "delivery-router-stats")
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
	defer func() { _ = conn.Close() }()

	if err := conn.WriteJSON(map[string]any{
		"op":         "subscribe",
		"subject":    "aggregation.stats/binance/BTC-USDT/raw",
		"request_id": "sub-1",
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

	envCh <- envelope.Envelope{
		Type:       "aggregation.stats",
		Version:    1,
		Venue:      "binance",
		Instrument: "BTC-USDT",
		Seq:        2,
		TsIngest:   time.Now().UnixMilli(),
		Payload:    []byte(`{"stats":"closed"}`),
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var evt map[string]any
	if err := conn.ReadJSON(&evt); err != nil {
		t.Fatalf("event read: %v", err)
	}
	if got, want := evt["type"], "event"; got != want {
		t.Fatalf("event type=%v want=%v", got, want)
	}
	if got, want := evt["subject"], "aggregation.stats/binance/BTCUSDT/raw"; got != want {
		t.Fatalf("subject=%v want=%v", got, want)
	}
	if got, want := int64(evt["seq"].(float64)), int64(2); got != want {
		t.Fatalf("seq=%d want=%d", got, want)
	}
}

func TestWSDelivery_CandleClosed_MultiInstrumentSubscriptions(t *testing.T) {
	requireLoopbackListener(t)

	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	envCh := make(chan envelope.Envelope, 32)
	routerPID := e.Spawn(deliveryruntime.NewRouterActor(deliveryruntime.RouterConfig{
		EnvelopeCh: envCh,
		Timeframe:  "raw",
	}), "delivery-router-candle-multi")
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
	defer func() { _ = conn.Close() }()

	subjects := []string{
		"aggregation.candle/binance/BTC-USDT/raw",
		"aggregation.candle/bybit/ETH-USDT/raw",
	}
	for i := range subjects {
		if err := conn.WriteJSON(map[string]any{
			"op":         "subscribe",
			"subject":    subjects[i],
			"request_id": "sub",
		}); err != nil {
			t.Fatalf("subscribe[%d] write: %v", i, err)
		}
		var ack map[string]any
		if err := conn.ReadJSON(&ack); err != nil {
			t.Fatalf("subscribe[%d] read: %v", i, err)
		}
		if got, want := ack["type"], "ack"; got != want {
			t.Fatalf("ack[%d] type=%v want=%v", i, got, want)
		}
	}

	envCh <- envelope.Envelope{
		Type:       "aggregation.candle",
		Version:    1,
		Venue:      "binance",
		Instrument: "BTC-USDT",
		Seq:        1,
		TsIngest:   time.Now().UnixMilli(),
		Payload:    []byte(`{"venue":"binance"}`),
	}
	envCh <- envelope.Envelope{
		Type:       "aggregation.candle",
		Version:    1,
		Venue:      "bybit",
		Instrument: "ETH-USDT",
		Seq:        2,
		TsIngest:   time.Now().UnixMilli(),
		Payload:    []byte(`{"venue":"bybit"}`),
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var first map[string]any
	if err := conn.ReadJSON(&first); err != nil {
		t.Fatalf("first event read: %v", err)
	}
	var second map[string]any
	if err := conn.ReadJSON(&second); err != nil {
		t.Fatalf("second event read: %v", err)
	}

	gotSubjects := map[string]bool{
		first["subject"].(string):  true,
		second["subject"].(string): true,
	}
	if !gotSubjects["aggregation.candle/binance/BTCUSDT/raw"] {
		t.Fatalf("missing binance candle event: got=%v", gotSubjects)
	}
	if !gotSubjects["aggregation.candle/bybit/ETHUSDT/raw"] {
		t.Fatalf("missing bybit candle event: got=%v", gotSubjects)
	}
}
