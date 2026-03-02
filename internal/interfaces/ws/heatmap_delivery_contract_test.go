package wsserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/anthdm/hollywood/actor"
	"github.com/gorilla/websocket"
	deliveryruntime "github.com/market-raccoon/internal/actors/delivery/runtime"
	insightsdomain "github.com/market-raccoon/internal/core/insights/domain"
	"github.com/market-raccoon/internal/shared/envelope"
)

func TestWSDelivery_HeatmapSnapshot_RoutedToSubscriber(t *testing.T) {
	requireLoopbackListener(t)

	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	routerPID := e.Spawn(deliveryruntime.NewRouterActor(deliveryruntime.RouterConfig{}), "delivery-router-heatmap")
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
		"subject":    "insights.heatmap_snapshot/binance/BTC-USDT/1m",
		"request_id": "sub-heatmap-1",
	}); err != nil {
		t.Fatalf("subscribe write: %v", err)
	}
	ack := readFrameSkipHello(t, conn, 2*time.Second)
	if got, want := ack["type"], "ack"; got != want {
		t.Fatalf("ack type=%v want=%v", got, want)
	}

	e.Send(routerPID, deliveryruntime.DeliverEnvelope{Envelope: envelope.Envelope{
		Type:       insightsdomain.HeatmapSnapshotType,
		Version:    insightsdomain.HeatmapSnapshotVersion,
		Venue:      "binance",
		Instrument: "BTC-USDT",
		Seq:        7,
		TsIngest:   time.Now().UnixMilli(),
		Meta:       map[string]string{"timeframe": "1m"},
		Payload:    []byte(`{"cells":[]}`),
	}})

	evt := readFrameSkipHello(t, conn, 2*time.Second)
	if got, want := evt["type"], "event"; got != want {
		t.Fatalf("event type=%v want=%v", got, want)
	}
	if got, want := evt["subject"], "insights.heatmap_snapshot/binance/BTCUSDT/1m"; got != want {
		t.Fatalf("subject=%v want=%v", got, want)
	}
	if got, want := int64(evt["seq"].(float64)), int64(7); got != want {
		t.Fatalf("seq=%d want=%d", got, want)
	}
}

func TestWSDelivery_VolumeProfileSnapshot_RoutedToSubscriber(t *testing.T) {
	requireLoopbackListener(t)

	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	routerPID := e.Spawn(deliveryruntime.NewRouterActor(deliveryruntime.RouterConfig{}), "delivery-router-vpvr")
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
		"subject":    "insights.volume_profile_snapshot/binance/BTC-USDT/5m",
		"request_id": "sub-vpvr-1",
	}); err != nil {
		t.Fatalf("subscribe write: %v", err)
	}
	ack := readFrameSkipHello(t, conn, 2*time.Second)
	if got, want := ack["type"], "ack"; got != want {
		t.Fatalf("ack type=%v want=%v", got, want)
	}

	e.Send(routerPID, deliveryruntime.DeliverEnvelope{Envelope: envelope.Envelope{
		Type:       insightsdomain.VolumeProfileSnapshotType,
		Version:    insightsdomain.VolumeProfileSnapshotVersion,
		Venue:      "binance",
		Instrument: "BTC-USDT",
		Seq:        3,
		TsIngest:   time.Now().UnixMilli(),
		Meta:       map[string]string{"timeframe": "5m"},
		Payload:    []byte(`{"buckets":[]}`),
	}})

	evt := readFrameSkipHello(t, conn, 2*time.Second)
	if got, want := evt["type"], "event"; got != want {
		t.Fatalf("event type=%v want=%v", got, want)
	}
	if got, want := evt["subject"], "insights.volume_profile_snapshot/binance/BTCUSDT/5m"; got != want {
		t.Fatalf("subject=%v want=%v", got, want)
	}
	if got, want := int64(evt["seq"].(float64)), int64(3); got != want {
		t.Fatalf("seq=%d want=%d", got, want)
	}
}
