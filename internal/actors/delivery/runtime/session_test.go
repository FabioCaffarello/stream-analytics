package deliveryruntime

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anthdm/hollywood/actor"
	"github.com/gorilla/websocket"
	"github.com/market-raccoon/internal/core/delivery/domain"
	"github.com/market-raccoon/internal/core/delivery/ports"
	mddomain "github.com/market-raccoon/internal/core/marketdata/domain"
	sharedclock "github.com/market-raccoon/internal/shared/clock"
	"github.com/market-raccoon/internal/shared/codec"
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
	closed  atomic.Bool
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
func (f *fakeConn) Close() error {
	f.closed.Store(true)
	return nil
}

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
	ackMsg, ok := ack.(wsAckFrame)
	if !ok || ackMsg.Type != "ack" {
		t.Fatalf("expected ack message, got %#v", ack)
	}

	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"getrange","subject":"marketdata.trade/binance/BTC-USDT/raw","request_id":"r2","params":{"from_ms":0,"to_ms":10,"limit":5}}`)}
	rangeResp := <-conn.writeCh
	rangeMsg, ok := rangeResp.(wsErrorFrame)
	if !ok || rangeMsg.Type != "error" {
		t.Fatalf("expected error message for unavailable range store, got %#v", rangeResp)
	}

	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"unsubscribe","subject":"marketdata.trade/binance/BTC-USDT/raw","request_id":"r3"}`)}
	<-conn.writeCh
	_ = waitForMessage[UnsubscribeSession](t, routerCh, time.Second)
}

func TestSession_attachConnStartsReadLoop(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-capture")
	defer e.Poison(routerPID)

	sessionPID := e.Spawn(NewSessionActor(SessionConfig{RouterPID: routerPID}), "ws-session")
	defer e.Poison(sessionPID)

	reg := waitForMessage[RegisterSession](t, routerCh, time.Second)
	if reg.SessionID == "" {
		t.Fatal("expected register with session id")
	}

	conn := newFakeConn()
	e.Send(sessionPID, AttachConn{Conn: conn})
	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"subscribe","subject":"marketdata.trade/binance/BTC-USDT/raw","request_id":"r-attach"}`)}

	sub := waitForMessage[SubscribeSession](t, routerCh, time.Second)
	if got, want := sub.Subject.String(), "marketdata.trade/binance/BTCUSDT/raw"; got != want {
		t.Fatalf("subscribe subject = %q, want %q", got, want)
	}
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
	msg, ok := resp.(wsLastFrame)
	if !ok {
		t.Fatalf("response type = %T, want wsLastFrame", resp)
	}
	if got, want := msg.Type, "last"; got != want {
		t.Fatalf("type=%v want=%v", got, want)
	}
	item, ok := msg.Item.(ports.RangeItem)
	if !ok {
		t.Fatalf("item type = %T, want ports.RangeItem", msg.Item)
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
	msg := resp.(wsLastFrame)
	item := msg.Item.(ports.RangeItem)
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
	msg, ok := resp.(wsRangeFrame)
	if !ok {
		t.Fatalf("response type = %T, want wsRangeFrame", resp)
	}
	if got, want := msg.Type, "range"; got != want {
		t.Fatalf("type=%v want=%v", got, want)
	}
	items, ok := msg.Items.([]ports.RangeItem)
	if !ok {
		t.Fatalf("items type = %T, want []ports.RangeItem", msg.Items)
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
	msg := resp.(wsRangeFrame)
	items := msg.Items.([]ports.RangeItem)
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
	msg := resp.(wsErrorFrame)
	if got, want := msg.Type, "error"; got != want {
		t.Fatalf("type=%v want=%v", got, want)
	}
	if store.calls != 0 {
		t.Fatalf("store calls=%d want=0", store.calls)
	}
	if got := testutil.ToFloat64(metrics.WSQueryRejectedTotal.WithLabelValues("query_cap")); got < beforeRejected+1 {
		t.Fatalf("expected ws_query_rejected_total query_cap increment, got=%f before=%f", got, beforeRejected)
	}
}

func TestSession_RateLimit_RejectsSubscribeWhenBucketEmpty(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-capture-rate-limit")
	defer e.Poison(routerPID)

	clk := sharedclock.NewFakeClock(time.Unix(100, 0))
	conn := newFakeConn()
	beforeRejected := testutil.ToFloat64(metrics.WSQueryRejectedTotal.WithLabelValues("rate_limited"))

	sessionPID := e.Spawn(NewSessionActor(SessionConfig{
		RouterPID: routerPID,
		Conn:      conn,
		RateLimit: RateLimitConfig{
			Enabled:       true,
			MaxPerSecond:  1,
			BurstCapacity: 1,
		},
		Clock: clk,
	}), "ws-session-rate-limit")
	defer e.Poison(sessionPID)

	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)
	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"subscribe","subject":"marketdata.trade/binance/BTC-USDT/raw","request_id":"s1"}`)}
	first := <-conn.writeCh
	firstMsg, ok := first.(wsAckFrame)
	if !ok || firstMsg.Type != "ack" {
		t.Fatalf("expected first subscribe ack, got %#v", first)
	}

	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"subscribe","subject":"marketdata.trade/binance/BTC-USDT/raw","request_id":"s2"}`)}
	second := <-conn.writeCh
	secondMsg, ok := second.(wsErrorFrame)
	if !ok || secondMsg.Type != "error" {
		t.Fatalf("expected second subscribe error, got %#v", second)
	}

	afterRejected := testutil.ToFloat64(metrics.WSQueryRejectedTotal.WithLabelValues("rate_limited"))
	if afterRejected < beforeRejected+1 {
		t.Fatalf("expected rate_limited metric increment, before=%f after=%f", beforeRejected, afterRejected)
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
	msg := resp.(wsLastFrame)
	if got, want := msg.Type, "last"; got != want {
		t.Fatalf("type=%v want=%v", got, want)
	}
	if msg.Item != nil {
		t.Fatalf("item=%v want=nil", msg.Item)
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
	if !conn.closed.Load() {
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
	event, ok := msg.(wsEventFrame)
	if !ok {
		t.Fatalf("message type=%T want wsEventFrame", msg)
	}
	if got, want := event.Type, "event"; got != want {
		t.Fatalf("type=%v want=%v", got, want)
	}
}

func TestSession_deliveryEventProtoFrameWhenEnabled(t *testing.T) {
	t.Setenv(contracts.EnvProtoMarketDataTrade, "1")

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

func TestSession_deliveryEventProtoJSONTranscode_TradeUsesPascalCase(t *testing.T) {
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		t.Fatalf("bootstrap codec registry: %v", p)
	}
	payload, p := codec.EncodePayload("marketdata.trade", 1, envelope.ContentTypeProto, mddomain.TradeTickV1{
		Price:     123.45,
		Size:      0.25,
		Side:      "buy",
		TradeID:   "t-1",
		Timestamp: 1_710_000_000_123,
	})
	if p != nil {
		t.Fatalf("encode trade proto payload: %v", p)
	}

	event := captureJSONDeliveryEventFrame(t, "marketdata.trade/binance/BTCUSDT/raw", envelope.Envelope{
		Type:        "marketdata.trade",
		Version:     1,
		Venue:       "binance",
		Instrument:  "BTC-USDT",
		Seq:         1,
		TsIngest:    1_710_000_000_200,
		ContentType: envelope.ContentTypeProto,
		Payload:     payload,
	})

	var got map[string]any
	if err := json.Unmarshal(event.Payload, &got); err != nil {
		t.Fatalf("unmarshal trade payload: %v", err)
	}
	for _, k := range []string{"Price", "Size", "Side", "TradeID", "Timestamp"} {
		if _, ok := got[k]; !ok {
			t.Fatalf("missing PascalCase key %q in payload=%s", k, string(event.Payload))
		}
	}
	if _, ok := got["price"]; ok {
		t.Fatalf("unexpected lowercase key in payload=%s", string(event.Payload))
	}
}

func TestSession_deliveryEventProtoJSONTranscode_BookDeltaUsesPascalCase(t *testing.T) {
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		t.Fatalf("bootstrap codec registry: %v", p)
	}
	payload, p := codec.EncodePayload("marketdata.bookdelta", 1, envelope.ContentTypeProto, mddomain.BookDeltaV1{
		Bids:       []mddomain.PriceLevel{{Price: 100, Size: 2}},
		Asks:       []mddomain.PriceLevel{{Price: 101, Size: 3}},
		FirstID:    10,
		FinalID:    12,
		PrevFinal:  9,
		Timestamp:  1_710_000_000_300,
		IsSnapshot: true,
	})
	if p != nil {
		t.Fatalf("encode bookdelta proto payload: %v", p)
	}

	event := captureJSONDeliveryEventFrame(t, "marketdata.bookdelta/binance/BTCUSDT/raw", envelope.Envelope{
		Type:        "marketdata.bookdelta",
		Version:     1,
		Venue:       "binance",
		Instrument:  "BTC-USDT",
		Seq:         2,
		TsIngest:    1_710_000_000_400,
		ContentType: envelope.ContentTypeProto,
		Payload:     payload,
	})

	var got map[string]any
	if err := json.Unmarshal(event.Payload, &got); err != nil {
		t.Fatalf("unmarshal bookdelta payload: %v", err)
	}
	for _, k := range []string{"Bids", "Asks", "FirstID", "FinalID", "PrevFinal", "Timestamp", "IsSnapshot"} {
		if _, ok := got[k]; !ok {
			t.Fatalf("missing PascalCase key %q in payload=%s", k, string(event.Payload))
		}
	}
	bids, ok := got["Bids"].([]any)
	if !ok || len(bids) == 0 {
		t.Fatalf("Bids type/len invalid: %T payload=%s", got["Bids"], string(event.Payload))
	}
	firstBid, ok := bids[0].(map[string]any)
	if !ok {
		t.Fatalf("first bid type=%T", bids[0])
	}
	for _, k := range []string{"Price", "Size"} {
		if _, ok := firstBid[k]; !ok {
			t.Fatalf("missing nested PascalCase key %q in payload=%s", k, string(event.Payload))
		}
	}
	if _, ok := got["bids"]; ok {
		t.Fatalf("unexpected lowercase key in payload=%s", string(event.Payload))
	}
}

func TestSession_deliveryEventProtoJSONTranscode_AggregationStatsKeepsWrapper(t *testing.T) {
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		t.Fatalf("bootstrap codec registry: %v", p)
	}
	payload, p := codec.EncodePayload("aggregation.stats", 1, envelope.ContentTypeProto, contracts.AggregationStatsWindowClosedV1{
		Stats: contracts.AggregationStatsWindowV1{
			Venue:           "binance",
			Instrument:      "BTCUSDT",
			Timeframe:       "1m",
			WindowStartTs:   1_710_000_000_000,
			WindowEndTs:     1_710_000_060_000,
			LiqBuyVolume:    10,
			LiqSellVolume:   20,
			LiqTotalVolume:  30,
			MarkPriceClose:  123.45,
			FundingRateLast: 0.0001,
			IsClosed:        true,
		},
	})
	if p != nil {
		t.Fatalf("encode stats proto payload: %v", p)
	}

	event := captureJSONDeliveryEventFrame(t, "aggregation.stats/binance/BTCUSDT/1m", envelope.Envelope{
		Type:        "aggregation.stats",
		Version:     1,
		Venue:       "binance",
		Instrument:  "BTC-USDT",
		Seq:         3,
		TsIngest:    1_710_000_060_100,
		ContentType: envelope.ContentTypeProto,
		Payload:     payload,
	})

	var got map[string]any
	if err := json.Unmarshal(event.Payload, &got); err != nil {
		t.Fatalf("unmarshal stats payload: %v", err)
	}
	statsAny, ok := got["Stats"]
	if !ok {
		t.Fatalf("missing Stats wrapper in payload=%s", string(event.Payload))
	}
	if _, ok := got["stats"]; ok {
		t.Fatalf("unexpected lowercase stats wrapper in payload=%s", string(event.Payload))
	}
	stats, ok := statsAny.(map[string]any)
	if !ok {
		t.Fatalf("Stats wrapper type=%T", statsAny)
	}
	for _, k := range []string{"WindowEndTs", "LiqBuyVolume", "LiqSellVolume", "MarkPriceClose", "FundingRateLast"} {
		if _, ok := stats[k]; !ok {
			t.Fatalf("missing inner stats key %q in payload=%s", k, string(event.Payload))
		}
	}
}

func captureJSONDeliveryEventFrame(t *testing.T, subjectRaw string, env envelope.Envelope) wsEventFrame {
	t.Helper()
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-capture-json-transcode")
	defer e.Poison(routerPID)

	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{
		RouterPID: routerPID,
		Conn:      conn,
	}), "ws-session-json-transcode")
	defer e.Poison(sessionPID)
	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	e.Send(sessionPID, DeliveryEvent{
		Subject: mustParseSubjectForSession(t, subjectRaw),
		Env:     env,
	})

	msg := <-conn.writeCh
	event, ok := msg.(wsEventFrame)
	if !ok {
		t.Fatalf("message type=%T want wsEventFrame", msg)
	}
	if got, want := event.Type, "event"; got != want {
		t.Fatalf("type=%v want=%v", got, want)
	}
	return event
}
