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
	"github.com/market-raccoon/internal/contracts"
	marketmodel "github.com/market-raccoon/internal/core/marketmodel"
	"github.com/market-raccoon/internal/shared/codec"
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

func TestWSDelivery_SignalEventFrame_RoutedToSubscriber(t *testing.T) {
	requireLoopbackListener(t)
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		t.Fatalf("bootstrap codec registry: %v", p)
	}

	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	routerPID := e.Spawn(deliveryruntime.NewRouterActor(deliveryruntime.RouterConfig{}), "delivery-router-signal-event")
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

	subscribeSignalEventRaw(t, conn)
	payload := encodeSignalEventContractPayload(t)

	e.Send(routerPID, deliveryruntime.DeliverEnvelope{Envelope: envelope.Envelope{
		Type:        "signal.event",
		Version:     int(marketmodel.SignalVersion),
		Venue:       "binance",
		Instrument:  "BTCUSDT",
		Seq:         9,
		TsIngest:    1710000000123,
		ContentType: envelope.ContentTypeJSON,
		Payload:     payload,
		Meta: map[string]string{
			"timeframe": "raw",
			"kind":      "regime_change",
		},
	}})

	frame := readFrameSkipHello(t, conn, 2*time.Second)
	assertSignalEventFrameContract(t, frame)
}

func subscribeSignalEventRaw(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	if err := conn.WriteJSON(map[string]any{
		"op":         "subscribe",
		"subject":    "signal/regime_change/binance/BTC-USDT/raw",
		"request_id": "sub-signal-event-1",
	}); err != nil {
		t.Fatalf("subscribe write: %v", err)
	}
	ack := readFrameSkipHello(t, conn, 2*time.Second)
	if got, want := ack["type"], "ack"; got != want {
		t.Fatalf("ack type=%v want=%v", got, want)
	}
}

func encodeSignalEventContractPayload(t *testing.T) []byte {
	t.Helper()
	payload, p := codec.EncodePayload(
		"signal.event",
		int(marketmodel.SignalVersion),
		envelope.ContentTypeJSON,
		marketmodel.SignalEvent{
			Type:       "regime_change",
			TsServer:   1710000000123,
			Scope:      marketmodel.SignalScopeStream,
			Venue:      "BINANCE",
			Symbol:     "BTCUSDT",
			Severity:   "high",
			Confidence: 0.89,
			Features: []marketmodel.SignalFeature{
				{Key: "burst_count", Value: 3},
				{Key: "mean_confidence", Value: 0.85},
			},
			Explanation: "evidence burst indicates regime transition pressure",
			Explain:     []string{"burst threshold reached", "cross-feature confirmation present"},
			SignalID:    "sig-1",
			RuleID:      "regime_change_rule",
			RuleVersion: "v1",
			InputWatermark: []marketmodel.SignalInputSeqRange{{
				Venue:    "BINANCE",
				Symbol:   "BTCUSDT",
				SeqStart: 1,
				SeqEnd:   3,
			}},
			CorrelationID:  "cid-1",
			CorrelationIDs: []string{"cid-1", "evidence:abc"},
		},
	)
	if p != nil {
		t.Fatalf("encode signal payload: %v", p)
	}
	return payload
}

func assertSignalEventFrameContract(t *testing.T, frame map[string]any) {
	t.Helper()
	if got, want := frame["type"], "signal"; got != want {
		t.Fatalf("frame type=%v want=%v", got, want)
	}
	if got, want := frame["subject"], "signal/regime_change/binance/BTCUSDT/raw"; got != want {
		t.Fatalf("subject=%v want=%v", got, want)
	}
	payloadMap, ok := frame["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload type=%T want map[string]any", frame["payload"])
	}
	if got, want := payloadMap["kind"], "regime_change"; got != want {
		t.Fatalf("payload.kind=%v want=%v", got, want)
	}
	if got, want := payloadMap["severity"], "high"; got != want {
		t.Fatalf("payload.severity=%v want=%v", got, want)
	}
	if got, want := payloadMap["signal_id"], "sig-1"; got != want {
		t.Fatalf("payload.signal_id=%v want=%v", got, want)
	}
	if got, want := payloadMap["rule_id"], "regime_change_rule"; got != want {
		t.Fatalf("payload.rule_id=%v want=%v", got, want)
	}
	assertEvidenceIDs(t, payloadMap["evidence_ids"])
}

func assertEvidenceIDs(t *testing.T, value any) {
	t.Helper()
	evidenceIDs, ok := value.([]any)
	if !ok {
		t.Fatalf("payload.evidence_ids type=%T want=[]any", value)
	}
	if got, want := len(evidenceIDs), 1; got != want {
		t.Fatalf("payload.evidence_ids len=%d want=%d", got, want)
	}
	if got, want := evidenceIDs[0], "BINANCE|BTCUSDT|1-3"; got != want {
		t.Fatalf("payload.evidence_ids[0]=%v want=%v", got, want)
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
