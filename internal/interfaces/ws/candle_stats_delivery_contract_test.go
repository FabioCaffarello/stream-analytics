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
	routerPID := e.Spawn(deliveryruntime.NewRouterActor(deliveryruntime.RouterConfig{
		Timeframe: "raw",
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
	ack := readFrameSkipHello(t, conn, 2*time.Second)
	if got, want := ack["type"], "ack"; got != want {
		t.Fatalf("ack type=%v want=%v", got, want)
	}

	e.Send(routerPID, deliveryruntime.DeliverEnvelope{Envelope: envelope.Envelope{
		Type:       "aggregation.candle",
		Version:    1,
		Venue:      "binance",
		Instrument: "BTC-USDT",
		Seq:        1,
		TsIngest:   time.Now().UnixMilli(),
		Payload:    []byte(`{"candle":"closed"}`),
	}})

	evt := readFrameSkipHello(t, conn, 2*time.Second)
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
	routerPID := e.Spawn(deliveryruntime.NewRouterActor(deliveryruntime.RouterConfig{
		Timeframe: "raw",
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
	ack := readFrameSkipHello(t, conn, 2*time.Second)
	if got, want := ack["type"], "ack"; got != want {
		t.Fatalf("ack type=%v want=%v", got, want)
	}

	e.Send(routerPID, deliveryruntime.DeliverEnvelope{Envelope: envelope.Envelope{
		Type:       "aggregation.stats",
		Version:    1,
		Venue:      "binance",
		Instrument: "BTC-USDT",
		Seq:        2,
		TsIngest:   time.Now().UnixMilli(),
		Payload:    []byte(`{"stats":"closed"}`),
	}})

	evt := readFrameSkipHello(t, conn, 2*time.Second)
	if got, want := evt["type"], "event"; got != want {
		t.Fatalf("event type=%v want=%v", got, want)
	}
	if got, want := evt["subject"], "aggregation.stats/binance/BTCUSDT/raw"; got != want {
		t.Fatalf("subject=%v want=%v", got, want)
	}
	if got, want := int64(evt["seq"].(float64)), int64(1); got != want {
		t.Fatalf("seq=%d want=%d", got, want)
	}
}

func TestWSDelivery_CandleClosed_MultiInstrumentSubscriptions(t *testing.T) {
	requireLoopbackListener(t)

	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	routerPID := e.Spawn(deliveryruntime.NewRouterActor(deliveryruntime.RouterConfig{
		Timeframe: "raw",
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
		ack := readFrameSkipHello(t, conn, 2*time.Second)
		if got, want := ack["type"], "ack"; got != want {
			t.Fatalf("ack[%d] type=%v want=%v", i, got, want)
		}
	}

	e.Send(routerPID, deliveryruntime.DeliverEnvelope{Envelope: envelope.Envelope{
		Type:       "aggregation.candle",
		Version:    1,
		Venue:      "binance",
		Instrument: "BTC-USDT",
		Seq:        1,
		TsIngest:   time.Now().UnixMilli(),
		Payload:    []byte(`{"venue":"binance"}`),
	}})
	e.Send(routerPID, deliveryruntime.DeliverEnvelope{Envelope: envelope.Envelope{
		Type:       "aggregation.candle",
		Version:    1,
		Venue:      "bybit",
		Instrument: "ETH-USDT",
		Seq:        2,
		TsIngest:   time.Now().UnixMilli(),
		Payload:    []byte(`{"venue":"bybit"}`),
	}})

	first := readFrameSkipHello(t, conn, 2*time.Second)
	second := readFrameSkipHello(t, conn, 2*time.Second)

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
