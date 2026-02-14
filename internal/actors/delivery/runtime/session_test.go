package deliveryruntime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/anthdm/hollywood/actor"
	"github.com/gorilla/websocket"
	"github.com/market-raccoon/internal/core/delivery/domain"
	"github.com/market-raccoon/internal/core/delivery/ports"
	"github.com/market-raccoon/internal/shared/contracts"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/problem"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

type fakeRead struct {
	typ  int
	data []byte
	err  error
}

type fakeConn struct {
	readCh  chan fakeRead
	writeCh chan any
	closed  bool
}

func newFakeConn() *fakeConn {
	return &fakeConn{readCh: make(chan fakeRead, 16), writeCh: make(chan any, 16)}
}

func (f *fakeConn) ReadMessage() (int, []byte, error) {
	msg := <-f.readCh
	return msg.typ, msg.data, msg.err
}

func (f *fakeConn) WriteJSON(v any) error {
	f.writeCh <- v
	return nil
}

type fakeBinaryWrite struct {
	messageType int
	payload     []byte
}

func (f *fakeConn) WriteMessage(messageType int, data []byte) error {
	cp := append([]byte(nil), data...)
	f.writeCh <- fakeBinaryWrite{messageType: messageType, payload: cp}
	return nil
}

func (f *fakeConn) SetReadLimit(limit int64)            {}
func (f *fakeConn) SetReadDeadline(t time.Time) error   { return nil }
func (f *fakeConn) SetPongHandler(h func(string) error) {}
func (f *fakeConn) Close() error                        { f.closed = true; return nil }

func mustParseSubjectForSession(t *testing.T, raw string) domain.Subject {
	t.Helper()
	s, p := domain.ParseSubject(raw)
	if p != nil {
		t.Fatalf("ParseSubject(%q): %v", raw, p)
	}
	return s
}

type stubRangeStore struct {
	bySubject map[string][]ports.RangeItem
	calls     int
	lastLimit int
}

func (s *stubRangeStore) GetRange(_ context.Context, subject domain.Subject, fromMs, toMs int64, limit int) ([]ports.RangeItem, *problem.Problem) {
	s.calls++
	s.lastLimit = limit
	items := append([]ports.RangeItem(nil), s.bySubject[subject.String()]...)
	filtered := make([]ports.RangeItem, 0, len(items))
	for _, it := range items {
		if fromMs > 0 && it.TsIngest < fromMs {
			continue
		}
		if toMs > 0 && it.TsIngest > toMs {
			continue
		}
		filtered = append(filtered, it)
	}
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[len(filtered)-limit:]
	}
	return filtered, nil
}

func TestSession_parseSubscribeUnsubscribeGetRange(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-capture")
	defer e.Poison(routerPID)

	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{RouterPID: routerPID, Conn: conn}), "ws-session")
	defer e.Poison(sessionPID)

	reg := waitForMessage[RegisterSession](t, routerCh, time.Second)
	if reg.SessionID == "" {
		t.Fatal("expected register with session id")
	}

	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"subscribe","subject":"marketdata.trade/binance/BTC-USDT/raw","request_id":"r1"}`)}
	sub := waitForMessage[SubscribeSession](t, routerCh, time.Second)
	if got, want := sub.Subject.String(), "marketdata.trade/binance/BTCUSDT/raw"; got != want {
		t.Fatalf("subscribe subject = %q, want %q", got, want)
	}
	ack := <-conn.writeCh
	ackMap, ok := ack.(map[string]any)
	if !ok || ackMap["type"] != "ack" {
		t.Fatalf("expected ack message, got %#v", ack)
	}

	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"getrange","subject":"marketdata.trade/binance/BTC-USDT/raw","request_id":"r2","params":{"from_ms":0,"to_ms":10,"limit":5}}`)}
	rangeResp := <-conn.writeCh
	rangeMap, ok := rangeResp.(map[string]any)
	if !ok || rangeMap["type"] != "error" {
		t.Fatalf("expected error message for unavailable range store, got %#v", rangeResp)
	}

	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"unsubscribe","subject":"marketdata.trade/binance/BTC-USDT/raw","request_id":"r3"}`)}
	<-conn.writeCh
	_ = waitForMessage[UnsubscribeSession](t, routerCh, time.Second)
}

func TestSession_getLastVPVRSnapshot(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-capture")
	defer e.Poison(routerPID)

	sub := mustParseSubjectForSession(t, "insights.volume_profile_snapshot.v1/binance/BTCUSDT/1m")
	store := &stubRangeStore{
		bySubject: map[string][]ports.RangeItem{
			sub.String(): {
				{Seq: 100, TsIngest: 1700000000000, Payload: []byte(`{"seq":100}`)},
				{Seq: 101, TsIngest: 1700000001000, Payload: []byte(`{"seq":101}`)},
			},
		},
	}

	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{RouterPID: routerPID, Conn: conn, RangeStore: store}), "ws-session")
	defer e.Poison(sessionPID)

	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)
	beforeQuery := testutil.ToFloat64(metrics.WSQueryTotal.WithLabelValues("getlast", "insights"))
	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"getlast","subject":"insights.volume_profile_snapshot.v1/binance/BTC-USDT/1m","request_id":"r-last"}`)}

	resp := <-conn.writeCh
	msg, ok := resp.(map[string]any)
	if !ok {
		t.Fatalf("response type = %T, want map[string]any", resp)
	}
	if got, want := msg["type"], "last"; got != want {
		t.Fatalf("type=%v want=%v", got, want)
	}
	item, ok := msg["item"].(ports.RangeItem)
	if !ok {
		t.Fatalf("item type = %T, want ports.RangeItem", msg["item"])
	}
	if got, want := item.Seq, int64(101); got != want {
		t.Fatalf("last seq=%d want=%d", got, want)
	}
	if got := testutil.ToFloat64(metrics.WSQueryTotal.WithLabelValues("getlast", "insights")); got < beforeQuery+1 {
		t.Fatalf("expected ws_query_total getlast/insights increment, got=%f before=%f", got, beforeQuery)
	}
}

func TestSession_getLastVPVRSnapshot_unorderedStore(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-capture")
	defer e.Poison(routerPID)

	sub := mustParseSubjectForSession(t, "insights.volume_profile_snapshot.v1/binance/BTCUSDT/1m")
	store := &stubRangeStore{
		bySubject: map[string][]ports.RangeItem{
			sub.String(): {
				{Seq: 10, TsIngest: 1700000000010, Payload: []byte(`{"seq":10}`)},
				{Seq: 12, TsIngest: 1700000000012, Payload: []byte(`{"seq":12}`)},
				{Seq: 11, TsIngest: 1700000000011, Payload: []byte(`{"seq":11}`)},
			},
		},
	}

	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{RouterPID: routerPID, Conn: conn, RangeStore: store}), "ws-session")
	defer e.Poison(sessionPID)

	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)
	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"getlast","subject":"insights.volume_profile_snapshot.v1/binance/BTC-USDT/1m","request_id":"r-last-u"}`)}

	resp := <-conn.writeCh
	msg := resp.(map[string]any)
	item := msg["item"].(ports.RangeItem)
	if got, want := item.Seq, int64(12); got != want {
		t.Fatalf("last seq=%d want=%d", got, want)
	}
}

func TestSession_getRangeVPVRPagination(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-capture")
	defer e.Poison(routerPID)

	sub := mustParseSubjectForSession(t, "insights.volume_profile_snapshot.v1/binance/BTCUSDT/1m")
	store := &stubRangeStore{
		bySubject: map[string][]ports.RangeItem{
			sub.String(): {
				{Seq: 1, TsIngest: 1700000000001, Payload: []byte(`{"seq":1}`)},
				{Seq: 2, TsIngest: 1700000000002, Payload: []byte(`{"seq":2}`)},
				{Seq: 3, TsIngest: 1700000000003, Payload: []byte(`{"seq":3}`)},
				{Seq: 4, TsIngest: 1700000000004, Payload: []byte(`{"seq":4}`)},
				{Seq: 5, TsIngest: 1700000000005, Payload: []byte(`{"seq":5}`)},
			},
		},
	}

	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{RouterPID: routerPID, Conn: conn, RangeStore: store}), "ws-session")
	defer e.Poison(sessionPID)

	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)
	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"getrange","subject":"insights.volume_profile_snapshot.v1/binance/BTC-USDT/1m","request_id":"r-range","params":{"from_ms":0,"to_ms":0,"limit":2,"page":2}}`)}

	resp := <-conn.writeCh
	msg, ok := resp.(map[string]any)
	if !ok {
		t.Fatalf("response type = %T, want map[string]any", resp)
	}
	if got, want := msg["type"], "range"; got != want {
		t.Fatalf("type=%v want=%v", got, want)
	}
	items, ok := msg["items"].([]ports.RangeItem)
	if !ok {
		t.Fatalf("items type = %T, want []ports.RangeItem", msg["items"])
	}
	if got, want := len(items), 2; got != want {
		t.Fatalf("items len=%d want=%d", got, want)
	}
	if got, want := items[0].Seq, int64(2); got != want {
		t.Fatalf("items[0].seq=%d want=%d", got, want)
	}
	if got, want := items[1].Seq, int64(3); got != want {
		t.Fatalf("items[1].seq=%d want=%d", got, want)
	}
}

func TestSession_getRangeVPVRPagination_unorderedStore_ordersBeforePaginate(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-capture")
	defer e.Poison(routerPID)

	sub := mustParseSubjectForSession(t, "insights.volume_profile_snapshot.v1/binance/BTCUSDT/1m")
	store := &stubRangeStore{
		bySubject: map[string][]ports.RangeItem{
			sub.String(): {
				{Seq: 5, TsIngest: 1700000000005, Payload: []byte(`{"seq":5}`)},
				{Seq: 1, TsIngest: 1700000000001, Payload: []byte(`{"seq":1}`)},
				{Seq: 3, TsIngest: 1700000000003, Payload: []byte(`{"seq":3}`)},
				{Seq: 4, TsIngest: 1700000000004, Payload: []byte(`{"seq":4}`)},
				{Seq: 2, TsIngest: 1700000000002, Payload: []byte(`{"seq":2}`)},
			},
		},
	}

	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{RouterPID: routerPID, Conn: conn, RangeStore: store}), "ws-session")
	defer e.Poison(sessionPID)

	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)
	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"getrange","subject":"insights.volume_profile_snapshot.v1/binance/BTC-USDT/1m","request_id":"r-range-u","params":{"from_ms":0,"to_ms":0,"limit":2,"page":2}}`)}

	resp := <-conn.writeCh
	msg := resp.(map[string]any)
	items := msg["items"].([]ports.RangeItem)
	if got, want := items[0].Seq, int64(2); got != want {
		t.Fatalf("items[0].seq=%d want=%d", got, want)
	}
	if got, want := items[1].Seq, int64(3); got != want {
		t.Fatalf("items[1].seq=%d want=%d", got, want)
	}
}

func TestSession_getRangeVPVRPagination_capsRejectExplosive(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-capture")
	defer e.Poison(routerPID)

	sub := mustParseSubjectForSession(t, "insights.volume_profile_snapshot.v1/binance/BTCUSDT/1m")
	store := &stubRangeStore{bySubject: map[string][]ports.RangeItem{sub.String(): {}}}

	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{RouterPID: routerPID, Conn: conn, RangeStore: store}), "ws-session")
	defer e.Poison(sessionPID)

	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)
	beforeRejected := testutil.ToFloat64(metrics.WSQueryRejectedTotal.WithLabelValues("query_cap"))
	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"getrange","subject":"insights.volume_profile_snapshot.v1/binance/BTC-USDT/1m","request_id":"r-cap","params":{"from_ms":0,"to_ms":0,"limit":1000,"page":100}}`)}

	resp := <-conn.writeCh
	msg := resp.(map[string]any)
	if got, want := msg["type"], "error"; got != want {
		t.Fatalf("type=%v want=%v", got, want)
	}
	if store.calls != 0 {
		t.Fatalf("store calls=%d want=0", store.calls)
	}
	if got := testutil.ToFloat64(metrics.WSQueryRejectedTotal.WithLabelValues("query_cap")); got < beforeRejected+1 {
		t.Fatalf("expected ws_query_rejected_total query_cap increment, got=%f before=%f", got, beforeRejected)
	}
}

func TestSession_getLastVPVRSnapshot_empty_returnsNotFoundOrEmpty(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-capture")
	defer e.Poison(routerPID)

	sub := mustParseSubjectForSession(t, "insights.volume_profile_snapshot.v1/binance/BTCUSDT/1m")
	store := &stubRangeStore{bySubject: map[string][]ports.RangeItem{sub.String(): {}}}

	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{RouterPID: routerPID, Conn: conn, RangeStore: store}), "ws-session")
	defer e.Poison(sessionPID)

	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)
	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"getlast","subject":"insights.volume_profile_snapshot.v1/binance/BTC-USDT/1m","request_id":"r-empty"}`)}

	resp := <-conn.writeCh
	msg := resp.(map[string]any)
	if got, want := msg["type"], "last"; got != want {
		t.Fatalf("type=%v want=%v", got, want)
	}
	if _, ok := msg["item"]; !ok {
		t.Fatalf("expected item key in response")
	}
	if msg["item"] != nil {
		t.Fatalf("item=%v want=nil", msg["item"])
	}
}

func TestSession_disconnectTriggersUnregister(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-capture")
	defer e.Poison(routerPID)

	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{RouterPID: routerPID, Conn: conn}), "ws-session")

	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)
	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"subscribe","subject":"marketdata.trade/binance/BTC-USDT/raw","request_id":"r1"}`)}
	_ = waitForMessage[SubscribeSession](t, routerCh, time.Second)
	<-conn.writeCh
	conn.readCh <- fakeRead{err: errors.New("disconnect")}

	_ = waitForMessage[UnsubscribeSession](t, routerCh, time.Second)
	_ = waitForMessage[UnregisterSession](t, routerCh, time.Second)
	<-e.Poison(sessionPID).Done()
	if !conn.closed {
		t.Fatal("connection should be closed")
	}
}

func TestSession_backpressureDropsWhenQueueFull(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-capture")
	defer e.Poison(routerPID)

	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{
		RouterPID:         routerPID,
		Conn:              conn,
		OutboundQueueSize: 1,
	}), "ws-session")
	defer e.Poison(sessionPID)

	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	before := testutil.ToFloat64(metrics.WSDropsTotal.WithLabelValues("queue_full"))
	evt := DeliveryEvent{
		Subject: mustParseSubjectForSession(t, "aggregation.snapshot/binance/BTC-USDT/raw"),
		Env: envelope.Envelope{
			Type:       "aggregation.snapshot",
			Version:    1,
			Venue:      "binance",
			Instrument: "BTC-USDT",
			Seq:        1,
			TsIngest:   time.Now().UnixMilli(),
			Payload:    []byte(`{"seq":1}`),
		},
	}

	e.Send(sessionPID, evt)
	e.Send(sessionPID, evt)
	e.Send(sessionPID, evt)

	deadline := time.After(2 * time.Second)
	for {
		after := testutil.ToFloat64(metrics.WSDropsTotal.WithLabelValues("queue_full"))
		if after >= before+1 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("expected ws drop increment, got before=%f after=%f", before, after)
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func TestSession_deliveryEventDefaultJSONFrame(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-capture")
	defer e.Poison(routerPID)

	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{RouterPID: routerPID, Conn: conn}), "ws-session")
	defer e.Poison(sessionPID)
	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	e.Send(sessionPID, DeliveryEvent{
		Subject: mustParseSubjectForSession(t, "marketdata.trade/binance/BTCUSDT/raw"),
		Env: envelope.Envelope{
			Type:           "marketdata.trade",
			Version:        1,
			Venue:          "binance",
			Instrument:     "BTC-USDT",
			TsIngest:       1_710_000_000_100,
			Seq:            42,
			IdempotencyKey: "idem-42",
			ContentType:    envelope.ContentTypeJSON,
			Payload:        []byte(`{"price":1}`),
		},
	})

	msg := <-conn.writeCh
	event, ok := msg.(map[string]any)
	if !ok {
		t.Fatalf("message type=%T want map[string]any", msg)
	}
	if got, want := event["type"], "event"; got != want {
		t.Fatalf("type=%v want=%v", got, want)
	}
}

func TestSession_deliveryEventProtoFrameWhenEnabled(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-capture")
	defer e.Poison(routerPID)

	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{
		RouterPID:   routerPID,
		Conn:        conn,
		PreferProto: true,
	}), "ws-session")
	defer e.Poison(sessionPID)
	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	wantEnv := envelope.Envelope{
		Type:           "marketdata.trade",
		Version:        1,
		Venue:          "binance",
		Instrument:     "BTC-USDT",
		TsExchange:     1_710_000_000_050,
		TsIngest:       1_710_000_000_100,
		Seq:            43,
		IdempotencyKey: "idem-43",
		ContentType:    envelope.ContentTypeProto,
		Payload:        []byte{0x08, 0x2a},
	}
	e.Send(sessionPID, DeliveryEvent{
		Subject: mustParseSubjectForSession(t, "marketdata.trade/binance/BTCUSDT/raw"),
		Env:     wantEnv,
	})

	msg := <-conn.writeCh
	bin, ok := msg.(fakeBinaryWrite)
	if !ok {
		t.Fatalf("message type=%T want fakeBinaryWrite", msg)
	}
	if got, want := bin.messageType, websocket.BinaryMessage; got != want {
		t.Fatalf("messageType=%d want=%d", got, want)
	}
	gotEnv, p := contracts.UnmarshalEnvelopeV1ToDomain(bin.payload)
	if p != nil {
		t.Fatalf("UnmarshalEnvelopeV1ToDomain: %v", p)
	}
	if gotEnv.Type != wantEnv.Type || gotEnv.Seq != wantEnv.Seq || gotEnv.ContentType != wantEnv.ContentType {
		t.Fatalf("decoded envelope mismatch got=%+v want=%+v", gotEnv, wantEnv)
	}
}
