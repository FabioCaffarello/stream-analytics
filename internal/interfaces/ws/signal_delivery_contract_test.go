package wsserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/anthdm/hollywood/actor"
	"github.com/gorilla/websocket"
	deliveryruntime "github.com/market-raccoon/internal/actors/delivery/runtime"
	"github.com/market-raccoon/internal/shared/envelope"
)

func TestWSDelivery_SignalFrame_RoutedToSubscriber(t *testing.T) {
	requireLoopbackListener(t)

	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	routerPID := e.Spawn(deliveryruntime.NewRouterActor(deliveryruntime.RouterConfig{}), "delivery-router-signal")
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
		"subject":    "signal/absorption/binance/BTC-USDT/1m",
		"request_id": "sub-signal-1",
	}); err != nil {
		t.Fatalf("subscribe write: %v", err)
	}
	ack := readFrameSkipHello(t, conn, 2*time.Second)
	if got, want := ack["type"], "ack"; got != want {
		t.Fatalf("ack type=%v want=%v", got, want)
	}

	e.Send(routerPID, deliveryruntime.DeliverEnvelope{Envelope: envelope.Envelope{
		Type:       "signal.composite",
		Version:    1,
		Venue:      "binance",
		Instrument: "BTC-USDT",
		Seq:        7,
		TsIngest:   time.Now().UnixMilli(),
		Meta: map[string]string{
			"timeframe": "1m",
			"kind":      "absorption",
		},
		Payload: []byte(`{
			"kind":"absorption",
			"venue":"binance",
			"instrument":"BTC-USDT",
			"timeframe":"1m",
			"severity":"high",
			"confidence":0.87,
			"evidence":[{"label":"volume_ratio","value":"2.1"}],
			"regime_kind":"trending",
			"regime_strength":0.72,
			"reason":"absorption with trending regime"
		}`),
	}})

	frame := readFrameSkipHello(t, conn, 2*time.Second)
	if got, want := frame["type"], "signal"; got != want {
		t.Fatalf("frame type=%v want=%v", got, want)
	}
	if got, want := frame["subject"], "signal/absorption/binance/BTCUSDT/1m"; got != want {
		t.Fatalf("subject=%v want=%v", got, want)
	}
}

func TestWSDelivery_SignalPayload_NoExecutionFields(t *testing.T) {
	requireLoopbackListener(t)

	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	routerPID := e.Spawn(deliveryruntime.NewRouterActor(deliveryruntime.RouterConfig{}), "delivery-router-signal-safe")
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
		"subject":    "signal/absorption/binance/BTC-USDT/1m",
		"request_id": "sub-signal-safe-1",
	}); err != nil {
		t.Fatalf("subscribe write: %v", err)
	}
	_ = readFrameSkipHello(t, conn, 2*time.Second)

	e.Send(routerPID, deliveryruntime.DeliverEnvelope{Envelope: envelope.Envelope{
		Type:       "signal.composite",
		Version:    1,
		Venue:      "binance",
		Instrument: "BTC-USDT",
		Seq:        8,
		TsIngest:   time.Now().UnixMilli(),
		Meta: map[string]string{
			"timeframe": "1m",
			"kind":      "absorption",
		},
		Payload: []byte(`{
			"kind":"absorption",
			"venue":"binance",
			"instrument":"BTC-USDT",
			"timeframe":"1m",
			"severity":"high",
			"confidence":0.87,
			"evidence":[{"label":"volume_ratio","value":"2.1"}],
			"regime_kind":"trending",
			"regime_strength":0.72,
			"reason":"absorption with trending regime"
		}`),
	}})

	frame := readFrameSkipHello(t, conn, 2*time.Second)
	payload, ok := frame["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload type=%T want map[string]any", frame["payload"])
	}
	forbidden := map[string]struct{}{
		"action":  {},
		"order":   {},
		"execute": {},
		"buy":     {},
		"sell":    {},
	}
	if hasForbiddenSignalPayloadKey(payload, forbidden) {
		raw, _ := json.Marshal(payload)
		t.Fatalf("payload contains forbidden execution key: %s", string(raw))
	}
}

func hasForbiddenSignalPayloadKey(value any, forbidden map[string]struct{}) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			if _, bad := forbidden[key]; bad {
				return true
			}
			if hasForbiddenSignalPayloadKey(nested, forbidden) {
				return true
			}
		}
	case []any:
		for i := range typed {
			if hasForbiddenSignalPayloadKey(typed[i], forbidden) {
				return true
			}
		}
	}
	return false
}
