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
	readCh    chan fakeRead
	writeCh   chan any
	dropHello bool
	closed    atomic.Bool
}

func newFakeConn() *fakeConn {
	return &fakeConn{readCh: make(chan fakeRead, 16), writeCh: make(chan any, 16), dropHello: true}
}

func (f *fakeConn) ReadMessage() (int, []byte, error) {
	msg := <-f.readCh
	return msg.typ, msg.data, msg.err
}

func (f *fakeConn) WriteJSON(v any) error {
	if f.dropHello {
		if _, ok := v.(wsHelloFrame); ok {
			return nil
		}
	}
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

func TestSession_emitsHelloOnAttach(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-capture-hello")
	defer e.Poison(routerPID)

	conn := newFakeConn()
	conn.dropHello = false
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{RouterPID: routerPID, Conn: conn}), "ws-session-hello")
	defer e.Poison(sessionPID)

	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)
	msg := <-conn.writeCh
	hello, ok := msg.(wsHelloFrame)
	if !ok {
		t.Fatalf("message type=%T want wsHelloFrame", msg)
	}
	if got, want := hello.Type, "hello"; got != want {
		t.Fatalf("type=%q want=%q", got, want)
	}
	if got, want := hello.Payload.ProtoVer, wsProtocolVersion; got != want {
		t.Fatalf("proto_ver=%d want=%d", got, want)
	}
	if hello.Payload.ServerTime <= 0 {
		t.Fatalf("server_time=%d want > 0", hello.Payload.ServerTime)
	}
	if len(hello.Payload.Capabilities.Topics) == 0 {
		t.Fatal("expected non-empty capabilities.topics")
	}
	if len(hello.Payload.Capabilities.Venues) == 0 {
		t.Fatal("expected non-empty capabilities.venues")
	}
}

func TestSession_EmitsPeriodicMetricsFrame(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-capture-metrics")
	defer e.Poison(routerPID)

	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{RouterPID: routerPID, Conn: conn}), "ws-session-metrics")
	defer e.Poison(sessionPID)
	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	before := testutil.ToFloat64(metrics.WSControlFramesTotal.WithLabelValues("metrics"))
	e.Send(sessionPID, sessionMetricsTick{})
	msg := <-conn.writeCh
	frame, ok := msg.(wsMetricsFrame)
	if !ok || frame.Type != "metrics" {
		t.Fatalf("expected metrics frame, got %#v", msg)
	}
	if frame.Payload.ActiveSubscriptions != 0 {
		t.Fatalf("active_subscriptions=%d want=0", frame.Payload.ActiveSubscriptions)
	}
	if frame.Payload.WSQueueLen < 0 {
		t.Fatalf("ws_queue_len=%d want >= 0", frame.Payload.WSQueueLen)
	}
	if after := testutil.ToFloat64(metrics.WSControlFramesTotal.WithLabelValues("metrics")); after < before+1 {
		t.Fatalf("expected metrics control frame counter increment, before=%f after=%f", before, after)
	}
}

func TestSession_ConfigurableCadenceAndKeepalive_Applied(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-capture-cadence")
	defer e.Poison(routerPID)

	conn := newFakeConn()
	conn.dropHello = false
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{
		RouterPID:         routerPID,
		Conn:              conn,
		MetricsCadence:    15 * time.Millisecond,
		KeepaliveInterval: 10 * time.Millisecond,
	}), "ws-session-cadence")
	defer e.Poison(sessionPID)
	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	hello := (<-conn.writeCh).(wsHelloFrame)
	if got, want := hello.Payload.Capabilities.MetricsCadenceMs, 15; got != want {
		t.Fatalf("hello metrics_cadence_ms=%d want=%d", got, want)
	}
	if got, want := hello.Payload.Capabilities.KeepaliveIntervalMs, 10; got != want {
		t.Fatalf("hello keepalive_interval_ms=%d want=%d", got, want)
	}

	deadline := time.After(200 * time.Millisecond)
	gotMetrics := false
	gotPing := false
	for !gotMetrics || !gotPing {
		select {
		case msg := <-conn.writeCh:
			switch frame := msg.(type) {
			case wsMetricsFrame:
				if frame.Type == "metrics" {
					gotMetrics = true
				}
			case fakeBinaryWrite:
				if frame.messageType == websocket.PingMessage {
					gotPing = true
				}
			}
		case <-deadline:
			t.Fatalf("did not observe periodic cadence events (metrics=%v ping=%v)", gotMetrics, gotPing)
		}
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

func TestSession_subscribeCandleEmitsDeterministicBackfillWithWatermark(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-capture-backfill")
	defer e.Poison(routerPID)

	sub := mustParseSubjectForSession(t, "aggregation.candle/binance/BTCUSDT/1m")
	store := &stubRangeStore{
		bySubject: map[string][]ports.RangeItem{
			sub.String(): {
				{Seq: 11, TsIngest: 1700000000011, Payload: []byte(`{"seq":11}`)},
				{Seq: 10, TsIngest: 1700000000010, Payload: []byte(`{"seq":10}`)},
				{Seq: 12, TsIngest: 1700000000012, Payload: []byte(`{"seq":12}`)},
			},
		},
	}

	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{RouterPID: routerPID, Conn: conn, RangeStore: store}), "ws-session-backfill")
	defer e.Poison(sessionPID)

	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)
	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"hello","request_id":"h1","requested_features":["batching"]}`)}
	helloAck := <-conn.writeCh
	if ack, ok := helloAck.(wsHelloAckFrame); !ok || ack.Op != "hello" {
		t.Fatalf("expected hello ack, got %#v", helloAck)
	}

	readBackfill := func(requestID string) wsRangeFrame {
		conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"subscribe","subject":"aggregation.candle/binance/BTC-USDT/1m","request_id":"` + requestID + `"}`)}
		msg1 := <-conn.writeCh
		msg2 := <-conn.writeCh

		var backfill wsRangeFrame
		var ack wsAckFrame
		switch v := msg1.(type) {
		case wsRangeFrame:
			backfill = v
		case wsAckFrame:
			ack = v
		default:
			t.Fatalf("unexpected message type %T", msg1)
		}
		switch v := msg2.(type) {
		case wsRangeFrame:
			backfill = v
		case wsAckFrame:
			ack = v
		default:
			t.Fatalf("unexpected message type %T", msg2)
		}
		if ack.Type != "ack" || ack.Op != "subscribe" {
			t.Fatalf("ack=%+v want subscribe ack", ack)
		}
		if got, want := backfill.Op, "backfill"; got != want {
			t.Fatalf("range op=%q want=%q", got, want)
		}
		if got, want := backfill.WatermarkSeq, int64(12); got != want {
			t.Fatalf("watermark_seq=%d want=%d", got, want)
		}
		return backfill
	}

	first := readBackfill("b1")
	items1, ok := first.Items.([]ports.RangeItem)
	if !ok {
		t.Fatalf("items type=%T want []ports.RangeItem", first.Items)
	}
	if got, want := len(items1), 3; got != want {
		t.Fatalf("items len=%d want=%d", got, want)
	}
	if items1[0].Seq != 10 || items1[1].Seq != 11 || items1[2].Seq != 12 {
		got := []int64{items1[0].Seq, items1[1].Seq, items1[2].Seq}
		want := []int64{10, 11, 12}
		t.Fatalf("ordered seq=%v want=%v", got, want)
	}

	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"unsubscribe","subject":"aggregation.candle/binance/BTC-USDT/1m","request_id":"u1"}`)}
	<-conn.writeCh
	_ = waitForMessage[UnsubscribeSession](t, routerCh, time.Second)

	second := readBackfill("b2")
	items2 := second.Items.([]ports.RangeItem)
	if len(items2) != len(items1) {
		t.Fatalf("items len mismatch=%d want=%d", len(items2), len(items1))
	}
	for i := range items1 {
		if items1[i].Seq != items2[i].Seq || items1[i].TsIngest != items2[i].TsIngest || string(items1[i].Payload) != string(items2[i].Payload) {
			t.Fatalf("non-idempotent backfill at idx=%d first=%+v second=%+v", i, items1[i], items2[i])
		}
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

func TestSession_PingReturnsPong(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-capture-ping")
	defer e.Poison(routerPID)

	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{RouterPID: routerPID, Conn: conn}), "ws-session-ping")
	defer e.Poison(sessionPID)
	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	before := testutil.ToFloat64(metrics.WSControlFramesTotal.WithLabelValues("pong"))
	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"type":"ping","request_id":"p1","ts_client":123}`)}
	msg := <-conn.writeCh
	out, ok := msg.(wsPongFrame)
	if !ok {
		t.Fatalf("message type=%T want wsPongFrame", msg)
	}
	if got, want := out.Type, "pong"; got != want {
		t.Fatalf("type=%v want=%v", got, want)
	}
	if got, want := out.RequestID, "p1"; got != want {
		t.Fatalf("request_id=%v want=%v", got, want)
	}
	if out.TsServer <= 0 {
		t.Fatalf("ts_server=%d want > 0", out.TsServer)
	}
	if after := testutil.ToFloat64(metrics.WSControlFramesTotal.WithLabelValues("pong")); after < before+1 {
		t.Fatalf("expected pong control frame counter increment, before=%f after=%f", before, after)
	}
}

func TestSession_SubscribeFromStreamFields(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-capture-stream-fields")
	defer e.Poison(routerPID)

	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{RouterPID: routerPID, Conn: conn}), "ws-session-stream-fields")
	defer e.Poison(sessionPID)
	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"type":"subscribe","request_id":"s-fields","venue":"binance","symbol":"BTC-USDT","channel":"marketdata.trade"}`)}
	sub := waitForMessage[SubscribeSession](t, routerCh, time.Second)
	if got, want := sub.Subject.String(), "marketdata.trade/binance/BTCUSDT/raw"; got != want {
		t.Fatalf("subject=%q want=%q", got, want)
	}
}

func TestSession_SubscribeRespectsMaxSubscriptions(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-capture-max-subs")
	defer e.Poison(routerPID)

	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{
		RouterPID:               routerPID,
		Conn:                    conn,
		MaxSubscriptions:        1,
		MaxSymbolsPerConnection: 10,
	}), "ws-session-max-subs")
	defer e.Poison(sessionPID)
	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"subscribe","subject":"marketdata.trade/binance/BTC-USDT/raw","request_id":"s1"}`)}
	<-conn.writeCh
	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"subscribe","subject":"marketdata.trade/binance/ETH-USDT/raw","request_id":"s2"}`)}
	msg := <-conn.writeCh
	errFrame, ok := msg.(wsErrorFrame)
	if !ok {
		t.Fatalf("message type=%T want wsErrorFrame", msg)
	}
	if got, want := errFrame.Type, "error"; got != want {
		t.Fatalf("type=%q want=%q", got, want)
	}
}

func TestSession_ResyncEmitsSnapshotAndAck(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-capture-resync")
	defer e.Poison(routerPID)

	subject := mustParseSubjectForSession(t, "marketdata.trade/binance/BTC-USDT/raw")
	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{
		RouterPID:           routerPID,
		Conn:                conn,
		HotSnapshotProvider: stubSnapshotProvider{bySubject: map[string][]byte{subject.String(): []byte(`{"seq":101}`)}},
	}), "ws-session-resync")
	defer e.Poison(sessionPID)
	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"subscribe","subject":"marketdata.trade/binance/BTC-USDT/raw","request_id":"sub"}`)}
	<-conn.writeCh // snapshot
	<-conn.writeCh // ack

	before := testutil.ToFloat64(metrics.WSResyncTotal)
	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"type":"resync","stream_id":"marketdata.trade/binance/BTC-USDT/raw","request_id":"re1","last_seq":100}`)}
	snapshot := <-conn.writeCh
	if out, ok := snapshot.(wsSnapshotFrame); !ok || out.Type != "snapshot" {
		t.Fatalf("expected snapshot frame, got %#v", snapshot)
	}
	ack := <-conn.writeCh
	if out, ok := ack.(wsAckFrame); !ok || out.Op != "resync" {
		t.Fatalf("expected resync ack, got %#v", ack)
	}
	after := testutil.ToFloat64(metrics.WSResyncTotal)
	if after < before+1 {
		t.Fatalf("expected resync metric increment, before=%f after=%f", before, after)
	}
}

func TestSession_ResyncSnapshotUnavailable_IncrementsRejectedMetric(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-capture-resync-rejected")
	defer e.Poison(routerPID)

	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{
		RouterPID:           routerPID,
		Conn:                conn,
		HotSnapshotProvider: stubSnapshotProvider{},
	}), "ws-session-resync-rejected")
	defer e.Poison(sessionPID)
	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"subscribe","subject":"marketdata.trade/binance/BTC-USDT/raw","request_id":"sub-r"}`)}
	<-conn.writeCh // subscribe ack

	before := testutil.ToFloat64(metrics.WSResyncRejectedTotal.WithLabelValues("snapshot_unavailable"))
	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"type":"resync","subject":"marketdata.trade/binance/BTC-USDT/raw","request_id":"re-miss","last_seq":10}`)}
	resp := <-conn.writeCh
	if out, ok := resp.(wsErrorFrame); !ok || out.Type != "error" {
		t.Fatalf("expected resync error, got %#v", resp)
	}
	after := testutil.ToFloat64(metrics.WSResyncRejectedTotal.WithLabelValues("snapshot_unavailable"))
	if after < before+1 {
		t.Fatalf("expected resync rejected metric increment, before=%f after=%f", before, after)
	}
}

func TestSession_DeliveryEventNormalizesMissingTsServer(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-capture-ts-server")
	defer e.Poison(routerPID)

	clk := sharedclock.NewFakeClock(time.Unix(0, 0))
	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{
		RouterPID: routerPID,
		Conn:      conn,
		Clock:     clk,
	}), "ws-session-ts-server")
	defer e.Poison(sessionPID)
	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	before := testutil.ToFloat64(metrics.WSContractViolationsTotal.WithLabelValues("missing_ts_server"))
	e.Send(sessionPID, DeliveryEvent{
		Subject: mustParseSubjectForSession(t, "marketdata.trade/binance/BTCUSDT/raw"),
		Env: envelope.Envelope{
			Type:        "marketdata.trade",
			Version:     1,
			Venue:       "binance",
			Instrument:  "BTC-USDT",
			Seq:         1,
			TsIngest:    100,
			ContentType: envelope.ContentTypeJSON,
			Payload:     []byte(`{"price":1}`),
		},
	})
	msg := <-conn.writeCh
	event, ok := msg.(wsEventFrame)
	if !ok {
		t.Fatalf("message type=%T want wsEventFrame", msg)
	}
	if event.TsServer <= 0 {
		t.Fatalf("ts_server=%d want > 0", event.TsServer)
	}
	after := testutil.ToFloat64(metrics.WSContractViolationsTotal.WithLabelValues("missing_ts_server"))
	if after < before+1 {
		t.Fatalf("expected contract violation counter increment, before=%f after=%f", before, after)
	}
}

// ---------------------------------------------------------------------------
// F1: Extended Capability Negotiation
// ---------------------------------------------------------------------------

func TestSession_HelloIncludesExtendedCapabilities(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-cap")
	defer e.Poison(routerPID)

	conn := newFakeConn()
	conn.dropHello = false
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{
		RouterPID:               routerPID,
		Conn:                    conn,
		MaxSubscriptions:        100,
		MaxSymbolsPerConnection: 64,
		OutboundQueueSize:       512,
		RateLimit:               RateLimitConfig{Enabled: true, MaxPerSecond: 50, BurstCapacity: 200},
	}), "ws-session-cap")
	defer e.Poison(sessionPID)
	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	msg := <-conn.writeCh
	hello, ok := msg.(wsHelloFrame)
	if !ok {
		t.Fatalf("type=%T want wsHelloFrame", msg)
	}
	caps := hello.Payload.Capabilities
	if caps.MaxSymbolsPerConnection != 64 {
		t.Fatalf("max_symbols_per_connection=%d want=64", caps.MaxSymbolsPerConnection)
	}
	if caps.OutboundQueueSize != 512 {
		t.Fatalf("outbound_queue_size=%d want=512", caps.OutboundQueueSize)
	}
	if caps.MetricsCadenceMs <= 0 {
		t.Fatalf("metrics_cadence_ms=%d want>0", caps.MetricsCadenceMs)
	}
	if caps.KeepaliveIntervalMs <= 0 {
		t.Fatalf("keepalive_interval_ms=%d want>0", caps.KeepaliveIntervalMs)
	}
	if caps.MaxFrameBytes <= 0 {
		t.Fatalf("max_frame_bytes=%d want>0", caps.MaxFrameBytes)
	}
	if caps.RateLimit == nil {
		t.Fatal("rate_limit should be non-nil when enabled")
	}
	if !caps.RateLimit.Enabled {
		t.Fatal("rate_limit.enabled=false want=true")
	}
	if caps.RateLimit.MaxPerSecond != 50 {
		t.Fatalf("rate_limit.max_per_second=%d want=50", caps.RateLimit.MaxPerSecond)
	}
	if len(caps.SupportedFeatures) == 0 {
		t.Fatal("supported_features should be non-empty")
	}
}

func TestSession_HelloOmitsRateLimitWhenDisabled(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-cap-no-rl")
	defer e.Poison(routerPID)

	conn := newFakeConn()
	conn.dropHello = false
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{
		RouterPID: routerPID,
		Conn:      conn,
	}), "ws-session-cap-no-rl")
	defer e.Poison(sessionPID)
	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	msg := <-conn.writeCh
	hello := msg.(wsHelloFrame)
	if hello.Payload.Capabilities.RateLimit != nil {
		t.Fatal("rate_limit should be nil when disabled")
	}
}

// ---------------------------------------------------------------------------
// F2: Error Taxonomy with action_hint
// ---------------------------------------------------------------------------

func TestWsErrorMapping_AllCodes(t *testing.T) {
	tests := []struct {
		code      problem.ProblemCode
		retryable bool
		wantCode  string
		wantHint  string
	}{
		{problem.ValidationFailed, false, "ERROR_CODE_VALIDATION", "ACTION_HINT_NONE"},
		{problem.InvalidArgument, false, "ERROR_CODE_VALIDATION", "ACTION_HINT_NONE"},
		{problem.NotFound, false, "ERROR_CODE_NOT_FOUND", "ACTION_HINT_NONE"},
		{problem.NotFound, true, "ERROR_CODE_NOT_FOUND", "ACTION_HINT_RETRY"},
		{problem.Unavailable, false, "ERROR_CODE_RATE_LIMITED", "ACTION_HINT_RETRY"},
		{problem.Conflict, false, "ERROR_CODE_RESYNC_REQUIRED", "ACTION_HINT_RESYNC"},
		{problem.IntegrityViolation, false, "ERROR_CODE_RESYNC_REQUIRED", "ACTION_HINT_RESUBSCRIBE"},
		{problem.Internal, false, "ERROR_CODE_INTERNAL", "ACTION_HINT_RECONNECT"},
	}
	for _, tt := range tests {
		p := problem.New(tt.code, "test")
		p.Retryable = tt.retryable
		gotCode, gotHint := wsErrorMappingFromProblem(p)
		if gotCode != tt.wantCode {
			t.Errorf("code=%s retryable=%v: errorCode=%q want=%q", tt.code, tt.retryable, gotCode, tt.wantCode)
		}
		if gotHint != tt.wantHint {
			t.Errorf("code=%s retryable=%v: actionHint=%q want=%q", tt.code, tt.retryable, gotHint, tt.wantHint)
		}
	}
}

func TestSession_ErrorTaxonomy_ValidationError_HintNone(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-err-tax")
	defer e.Poison(routerPID)

	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{RouterPID: routerPID, Conn: conn}), "ws-session-err-tax")
	defer e.Poison(sessionPID)
	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"subscribe","request_id":"r-bad"}`)}
	msg := <-conn.writeCh
	errFrame, ok := msg.(wsErrorFrame)
	if !ok {
		t.Fatalf("type=%T want wsErrorFrame", msg)
	}
	if errFrame.Problem.ActionHint != "ACTION_HINT_NONE" {
		t.Fatalf("action_hint=%q want=ACTION_HINT_NONE", errFrame.Problem.ActionHint)
	}
}

func TestSession_ErrorTaxonomy_RateLimited_HintRetry(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-err-rl")
	defer e.Poison(routerPID)

	clk := sharedclock.NewFakeClock(time.Now())
	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{
		RouterPID: routerPID,
		Conn:      conn,
		Clock:     clk,
		RateLimit: RateLimitConfig{Enabled: true, MaxPerSecond: 1, BurstCapacity: 1},
	}), "ws-session-err-rl")
	defer e.Poison(sessionPID)
	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	// First subscribe exhausts the burst.
	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"subscribe","subject":"marketdata.trade/binance/BTC-USDT/raw","request_id":"r1"}`)}
	<-conn.writeCh // snapshot or ack
	<-conn.writeCh // ack

	// Second subscribe should be rate-limited.
	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"subscribe","subject":"marketdata.trade/bybit/BTC-USDT/raw","request_id":"r2"}`)}
	msg := <-conn.writeCh
	errFrame, ok := msg.(wsErrorFrame)
	if !ok {
		t.Fatalf("type=%T want wsErrorFrame", msg)
	}
	if errFrame.Problem.ActionHint != "ACTION_HINT_RETRY" {
		t.Fatalf("action_hint=%q want=ACTION_HINT_RETRY", errFrame.Problem.ActionHint)
	}
}

// ---------------------------------------------------------------------------
// F3: Snapshot Sequencing and prev_seq
// ---------------------------------------------------------------------------

func TestSession_SnapshotSeq_IncrementsPerSubject(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-snap-seq")
	defer e.Poison(routerPID)

	subject := mustParseSubjectForSession(t, "marketdata.trade/binance/BTCUSDT/raw")
	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{
		RouterPID:           routerPID,
		Conn:                conn,
		HotSnapshotProvider: stubSnapshotProvider{bySubject: map[string][]byte{subject.String(): []byte(`{"price":100}`)}},
	}), "ws-session-snap-seq")
	defer e.Poison(sessionPID)
	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	// Subscribe → snapshot_seq=1
	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"subscribe","subject":"marketdata.trade/binance/BTC-USDT/raw","request_id":"s1"}`)}
	snap1 := (<-conn.writeCh).(wsSnapshotFrame)
	<-conn.writeCh // ack
	if snap1.SnapshotSeq != 1 {
		t.Fatalf("snapshot_seq=%d want=1", snap1.SnapshotSeq)
	}
	if snap1.SnapshotHash == "" {
		t.Fatal("snapshot_hash should be non-empty")
	}

	// Resync → snapshot_seq=2
	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"resync","subject":"marketdata.trade/binance/BTC-USDT/raw","request_id":"r1"}`)}
	snap2 := (<-conn.writeCh).(wsSnapshotFrame)
	<-conn.writeCh // ack
	if snap2.SnapshotSeq != 2 {
		t.Fatalf("snapshot_seq=%d want=2", snap2.SnapshotSeq)
	}
	if snap2.SnapshotHash != snap1.SnapshotHash {
		t.Fatalf("snapshot_hash changed for same payload: %q vs %q", snap1.SnapshotHash, snap2.SnapshotHash)
	}
}

func TestSession_EventFrame_PrevSeq_ChainedCorrectly(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-prevseq")
	defer e.Poison(routerPID)

	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{RouterPID: routerPID, Conn: conn}), "ws-session-prevseq")
	defer e.Poison(sessionPID)
	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	subject := mustParseSubjectForSession(t, "marketdata.trade/binance/BTCUSDT/raw")
	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"subscribe","subject":"marketdata.trade/binance/BTC-USDT/raw","request_id":"s1"}`)}
	<-conn.writeCh // ack

	// Deliver 3 events.
	for _, seq := range []int64{10, 20, 30} {
		e.Send(sessionPID, DeliveryEvent{
			Subject: subject,
			Env: envelope.Envelope{
				Type:        "marketdata.trade",
				Version:     1,
				Venue:       "binance",
				Instrument:  "BTC-USDT",
				Seq:         seq,
				TsIngest:    1000 + seq,
				ContentType: envelope.ContentTypeJSON,
				Payload:     []byte(`{"p":1}`),
			},
		})
	}

	evt1 := (<-conn.writeCh).(wsEventFrame)
	evt2 := (<-conn.writeCh).(wsEventFrame)
	evt3 := (<-conn.writeCh).(wsEventFrame)

	if evt1.PrevSeq != 0 {
		t.Fatalf("evt1.prev_seq=%d want=0 (first event)", evt1.PrevSeq)
	}
	if evt2.PrevSeq != 10 {
		t.Fatalf("evt2.prev_seq=%d want=10", evt2.PrevSeq)
	}
	if evt3.PrevSeq != 20 {
		t.Fatalf("evt3.prev_seq=%d want=20", evt3.PrevSeq)
	}
}

func TestSession_PrevSeq_IndependentPerSubject(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-prevseq-multi")
	defer e.Poison(routerPID)

	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{RouterPID: routerPID, Conn: conn}), "ws-session-prevseq-multi")
	defer e.Poison(sessionPID)
	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	subA := mustParseSubjectForSession(t, "marketdata.trade/binance/BTCUSDT/raw")
	subB := mustParseSubjectForSession(t, "aggregation.candle/binance/BTCUSDT/1m")

	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"subscribe","subject":"marketdata.trade/binance/BTC-USDT/raw","request_id":"s1"}`)}
	<-conn.writeCh
	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"subscribe","subject":"aggregation.candle/binance/BTC-USDT/1m","request_id":"s2"}`)}
	<-conn.writeCh

	// Interleave: A(seq=1), B(seq=5), A(seq=2)
	e.Send(sessionPID, DeliveryEvent{Subject: subA, Env: envelope.Envelope{Type: "marketdata.trade", Version: 1, Venue: "binance", Instrument: "BTC-USDT", Seq: 1, TsIngest: 1001, ContentType: envelope.ContentTypeJSON, Payload: []byte(`{}`)}})
	e.Send(sessionPID, DeliveryEvent{Subject: subB, Env: envelope.Envelope{Type: "aggregation.candle", Version: 1, Venue: "binance", Instrument: "BTC-USDT", Seq: 5, TsIngest: 1005, ContentType: envelope.ContentTypeJSON, Payload: []byte(`{}`)}})
	e.Send(sessionPID, DeliveryEvent{Subject: subA, Env: envelope.Envelope{Type: "marketdata.trade", Version: 1, Venue: "binance", Instrument: "BTC-USDT", Seq: 2, TsIngest: 1002, ContentType: envelope.ContentTypeJSON, Payload: []byte(`{}`)}})

	evtA1 := (<-conn.writeCh).(wsEventFrame)
	evtB1 := (<-conn.writeCh).(wsEventFrame)
	evtA2 := (<-conn.writeCh).(wsEventFrame)

	if evtA1.PrevSeq != 0 {
		t.Fatalf("A1 prev_seq=%d want=0", evtA1.PrevSeq)
	}
	if evtB1.PrevSeq != 0 {
		t.Fatalf("B1 prev_seq=%d want=0", evtB1.PrevSeq)
	}
	if evtA2.PrevSeq != 1 {
		t.Fatalf("A2 prev_seq=%d want=1", evtA2.PrevSeq)
	}
}

// ---------------------------------------------------------------------------
// F4: Feature Negotiation + Frame Size Guard
// ---------------------------------------------------------------------------

func TestSession_HelloAdvertisesSupportedFeatures(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-feat")
	defer e.Poison(routerPID)

	conn := newFakeConn()
	conn.dropHello = false
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{RouterPID: routerPID, Conn: conn}), "ws-session-feat")
	defer e.Poison(sessionPID)
	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	hello := (<-conn.writeCh).(wsHelloFrame)
	feats := hello.Payload.Capabilities.SupportedFeatures
	if len(feats) == 0 {
		t.Fatal("supported_features should be non-empty")
	}
	found := map[string]bool{}
	for _, f := range feats {
		found[f] = true
	}
	for _, want := range []string{"batching", "snapshot_hash", "prev_seq"} {
		if !found[want] {
			t.Fatalf("missing supported_feature %q", want)
		}
	}
}

func TestSession_ClientHello_StoresRequestedFeatures(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-cli-feat")
	defer e.Poison(routerPID)

	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{RouterPID: routerPID, Conn: conn}), "ws-session-cli-feat")
	defer e.Poison(sessionPID)
	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"hello","request_id":"h1","requested_features":["batching","prev_seq"]}`)}
	ack := (<-conn.writeCh).(wsHelloAckFrame)
	if ack.Op != "hello" {
		t.Fatalf("ack op=%q want=hello", ack.Op)
	}
	if len(ack.NegotiatedFeatures) != 2 {
		t.Fatalf("negotiated_features len=%d want=2", len(ack.NegotiatedFeatures))
	}
}

func TestSession_MaxFrameBytes_DropsOversizedProtoFrame(t *testing.T) {
	_ = contracts.BootstrapPayloadCodecRegistry()
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-frame-sz")
	defer e.Poison(routerPID)

	conn := newFakeConn()
	before := testutil.ToFloat64(metrics.WSDropsTotal.WithLabelValues("frame_too_large"))
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{
		RouterPID:     routerPID,
		Conn:          conn,
		PreferProto:   true,
		MaxFrameBytes: 10, // tiny limit
	}), "ws-session-frame-sz")
	defer e.Poison(sessionPID)
	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	subject := mustParseSubjectForSession(t, "marketdata.trade/binance/BTCUSDT/raw")
	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"subscribe","subject":"marketdata.trade/binance/BTC-USDT/raw","request_id":"s1"}`)}
	<-conn.writeCh // ack

	// Build a trade payload that is larger than 10 bytes when encoded as proto envelope.
	tradeJSON := []byte(`{"Price":100.5,"Size":1.0,"Side":"buy","TradeID":"t1","Timestamp":1000}`)
	e.Send(sessionPID, DeliveryEvent{
		Subject: subject,
		Env: envelope.Envelope{
			Type:        "marketdata.trade",
			Version:     1,
			Venue:       "binance",
			Instrument:  "BTC-USDT",
			Seq:         1,
			TsIngest:    1000,
			ContentType: envelope.ContentTypeJSON,
			Payload:     tradeJSON,
		},
	})

	// The frame should be dropped, not written. Give a short window for the
	// drop metric to increment.
	time.Sleep(100 * time.Millisecond)
	after := testutil.ToFloat64(metrics.WSDropsTotal.WithLabelValues("frame_too_large"))
	if after <= before {
		t.Fatalf("expected frame_too_large drop increment, before=%f after=%f", before, after)
	}
}

func TestSession_MaxFrameBytes_DropsOversizedJSONFrame(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-json-frame-sz")
	defer e.Poison(routerPID)

	conn := newFakeConn()
	before := testutil.ToFloat64(metrics.WSDropsTotal.WithLabelValues("frame_too_large"))
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{
		RouterPID:     routerPID,
		Conn:          conn,
		PreferProto:   false, // JSON mode
		MaxFrameBytes: 10,    // tiny limit
	}), "ws-session-json-frame-sz")
	defer e.Poison(sessionPID)
	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"subscribe","subject":"marketdata.trade/binance/BTC-USDT/raw","request_id":"s1"}`)}
	<-conn.writeCh // ack

	tradeJSON := []byte(`{"Price":100.5,"Size":1.0,"Side":"buy","TradeID":"t1","Timestamp":1000}`)
	e.Send(sessionPID, DeliveryEvent{
		Subject: mustParseSubjectForSession(t, "marketdata.trade/binance/BTCUSDT/raw"),
		Env: envelope.Envelope{
			Type:        "marketdata.trade",
			Version:     1,
			Venue:       "binance",
			Instrument:  "BTC-USDT",
			Seq:         1,
			TsIngest:    1000,
			ContentType: envelope.ContentTypeJSON,
			Payload:     tradeJSON,
		},
	})

	time.Sleep(100 * time.Millisecond)
	after := testutil.ToFloat64(metrics.WSDropsTotal.WithLabelValues("frame_too_large"))
	if after <= before {
		t.Fatalf("expected JSON frame_too_large drop, before=%f after=%f", before, after)
	}
}

func TestSession_MaxFrameBytes_JSONPassesUnderLimit(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-json-frame-pass")
	defer e.Poison(routerPID)

	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{
		RouterPID:     routerPID,
		Conn:          conn,
		PreferProto:   false,
		MaxFrameBytes: 100_000, // generous limit
	}), "ws-session-json-frame-pass")
	defer e.Poison(sessionPID)
	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"subscribe","subject":"marketdata.trade/binance/BTC-USDT/raw","request_id":"s1"}`)}
	<-conn.writeCh // ack

	tradeJSON := []byte(`{"Price":100.5,"Size":1.0,"Side":"buy","TradeID":"t1","Timestamp":1000}`)
	e.Send(sessionPID, DeliveryEvent{
		Subject: mustParseSubjectForSession(t, "marketdata.trade/binance/BTCUSDT/raw"),
		Env: envelope.Envelope{
			Type:        "marketdata.trade",
			Version:     1,
			Venue:       "binance",
			Instrument:  "BTC-USDT",
			Seq:         1,
			TsIngest:    1000,
			ContentType: envelope.ContentTypeJSON,
			Payload:     tradeJSON,
		},
	})

	select {
	case msg := <-conn.writeCh:
		if _, ok := msg.(wsEventFrame); !ok {
			t.Fatalf("expected wsEventFrame, got %T", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("expected event frame to be written, timeout")
	}
}

// ---------------------------------------------------------------------------
// F5: Backpressure Hints
// ---------------------------------------------------------------------------

func TestSession_MetricsFrame_IncludesBackpressureHints(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-bp-hints")
	defer e.Poison(routerPID)

	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{
		RouterPID:         routerPID,
		Conn:              conn,
		OutboundQueueSize: 4,
	}), "ws-session-bp-hints")
	defer e.Poison(sessionPID)
	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	e.Send(sessionPID, sessionMetricsTick{})
	msg := (<-conn.writeCh).(wsMetricsFrame)
	if msg.Payload.BackpressureLevel != 0 {
		t.Fatalf("level=%d want=0 (empty queue)", msg.Payload.BackpressureLevel)
	}
	if msg.Payload.RecommendedAction != "none" {
		t.Fatalf("action=%q want=none", msg.Payload.RecommendedAction)
	}
	if msg.Payload.QueueCapacity != 4 {
		t.Fatalf("queue_capacity=%d want=4", msg.Payload.QueueCapacity)
	}
}

func TestSession_BackpressureLevel_Critical(t *testing.T) {
	sa := &SessionActor{}
	sa.outboundCap = 100
	sa.outbound = newDeliveryRing(100)
	// Fill to 96% = critical
	for i := 0; i < 96; i++ {
		sa.outbound.PushBack(DeliveryEvent{})
	}
	level, action := sa.computeBackpressureLevel()
	if level != 3 {
		t.Fatalf("level=%d want=3 (critical)", level)
	}
	if action != "reconnect" {
		t.Fatalf("action=%q want=reconnect", action)
	}
}

func TestSession_BackpressureLevel_Normal(t *testing.T) {
	sa := &SessionActor{}
	sa.outboundCap = 100
	sa.outbound = newDeliveryRing(100)
	level, action := sa.computeBackpressureLevel()
	if level != 0 {
		t.Fatalf("level=%d want=0", level)
	}
	if action != "none" {
		t.Fatalf("action=%q want=none", action)
	}
}

// ---------------------------------------------------------------------------
// F6: Tenant Metrics
// ---------------------------------------------------------------------------

func TestSession_TenantMetrics_DropsLabeledByTenant(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-tenant-drop")
	defer e.Poison(routerPID)

	conn := newFakeConn()
	before := testutil.ToFloat64(metrics.WSTenantDropsTotal.WithLabelValues("acme", "queue_full"))
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{
		RouterPID:         routerPID,
		Conn:              conn,
		TenantID:          "acme",
		OutboundQueueSize: 1,
	}), "ws-session-tenant-drop")
	defer e.Poison(sessionPID)
	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	subject := mustParseSubjectForSession(t, "marketdata.trade/binance/BTCUSDT/raw")
	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"subscribe","subject":"marketdata.trade/binance/BTC-USDT/raw","request_id":"s1"}`)}
	<-conn.writeCh // ack

	// Fill queue to capacity=1, then enqueue another to trigger drop.
	e.Send(sessionPID, DeliveryEvent{Subject: subject, Env: envelope.Envelope{Type: "marketdata.trade", Version: 1, Venue: "binance", Instrument: "BTC-USDT", Seq: 1, TsIngest: 1000, ContentType: envelope.ContentTypeJSON, Payload: []byte(`{}`)}})
	e.Send(sessionPID, DeliveryEvent{Subject: subject, Env: envelope.Envelope{Type: "marketdata.trade", Version: 1, Venue: "binance", Instrument: "BTC-USDT", Seq: 2, TsIngest: 1001, ContentType: envelope.ContentTypeJSON, Payload: []byte(`{}`)}})
	e.Send(sessionPID, DeliveryEvent{Subject: subject, Env: envelope.Envelope{Type: "marketdata.trade", Version: 1, Venue: "binance", Instrument: "BTC-USDT", Seq: 3, TsIngest: 1002, ContentType: envelope.ContentTypeJSON, Payload: []byte(`{}`)}})

	time.Sleep(200 * time.Millisecond)
	after := testutil.ToFloat64(metrics.WSTenantDropsTotal.WithLabelValues("acme", "queue_full"))
	if after <= before {
		t.Fatalf("expected tenant drop increment for acme, before=%f after=%f", before, after)
	}
}

func TestSession_TenantMetrics_ConnectionsActive(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-tenant-conn")
	defer e.Poison(routerPID)

	before := testutil.ToFloat64(metrics.WSTenantConnectionsActive.WithLabelValues("acme"))

	conn1 := newFakeConn()
	s1 := e.Spawn(NewSessionActor(SessionConfig{RouterPID: routerPID, Conn: conn1, TenantID: "acme"}), "ws-session-t1")
	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	conn2 := newFakeConn()
	s2 := e.Spawn(NewSessionActor(SessionConfig{RouterPID: routerPID, Conn: conn2, TenantID: "acme"}), "ws-session-t2")
	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	afterSpawn := testutil.ToFloat64(metrics.WSTenantConnectionsActive.WithLabelValues("acme"))
	if afterSpawn < before+2 {
		t.Fatalf("expected +2 connections for acme, before=%f after=%f", before, afterSpawn)
	}

	<-e.Poison(s1).Done()
	time.Sleep(50 * time.Millisecond)
	afterPoison := testutil.ToFloat64(metrics.WSTenantConnectionsActive.WithLabelValues("acme"))
	if afterPoison > afterSpawn-1+0.01 {
		t.Fatalf("expected decrement after poison, afterSpawn=%f afterPoison=%f", afterSpawn, afterPoison)
	}
	<-e.Poison(s2).Done()
}

func TestSession_TenantMetrics_DefaultTenantWhenEmpty(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-tenant-def")
	defer e.Poison(routerPID)

	before := testutil.ToFloat64(metrics.WSTenantConnectionsActive.WithLabelValues("default"))
	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{RouterPID: routerPID, Conn: conn, TenantID: ""}), "ws-session-tenant-def")
	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)
	defer e.Poison(sessionPID)

	after := testutil.ToFloat64(metrics.WSTenantConnectionsActive.WithLabelValues("default"))
	if after < before+1 {
		t.Fatalf("expected default tenant connection, before=%f after=%f", before, after)
	}
}

// ── Feature negotiation edge cases ───────────────────────────────────────────

func TestValidateRequestedFeatures_AllValid(t *testing.T) {
	valid, unknown := validateRequestedFeatures([]string{"batching", "prev_seq", "snapshot_hash"})
	if len(unknown) != 0 {
		t.Fatalf("unknown=%v want empty", unknown)
	}
	if len(valid) != 3 {
		t.Fatalf("valid len=%d want=3", len(valid))
	}
}

func TestValidateRequestedFeatures_UnknownRejected(t *testing.T) {
	valid, unknown := validateRequestedFeatures([]string{"batching", "foo_bar"})
	if len(unknown) != 1 || unknown[0] != "foo_bar" {
		t.Fatalf("unknown=%v want=[foo_bar]", unknown)
	}
	if len(valid) != 1 || valid[0] != "batching" {
		t.Fatalf("valid=%v want=[batching]", valid)
	}
}

func TestValidateRequestedFeatures_EmptyAndWhitespace(t *testing.T) {
	valid, unknown := validateRequestedFeatures([]string{"", "  ", " batching "})
	if len(unknown) != 0 {
		t.Fatalf("unknown=%v want empty", unknown)
	}
	if len(valid) != 1 || valid[0] != "batching" {
		t.Fatalf("valid=%v want=[batching]", valid)
	}
}

func TestValidateRequestedFeatures_DuplicateFeatures(t *testing.T) {
	valid, unknown := validateRequestedFeatures([]string{"batching", "BATCHING", "Batching"})
	if len(unknown) != 0 {
		t.Fatalf("unknown=%v want empty", unknown)
	}
	if len(valid) != 1 || valid[0] != "batching" {
		t.Fatalf("valid=%v want=[batching] (deduplicated)", valid)
	}
}

func TestValidateRequestedFeatures_CaseInsensitive(t *testing.T) {
	valid, unknown := validateRequestedFeatures([]string{"PREV_SEQ", "Snapshot_Hash"})
	if len(unknown) != 0 {
		t.Fatalf("unknown=%v want empty", unknown)
	}
	if len(valid) != 2 {
		t.Fatalf("valid=%v want 2 elements", valid)
	}
}

func TestHandleClientHello_RejectsUnknownFeature(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-feat-rej")
	defer e.Poison(routerPID)

	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{RouterPID: routerPID, Conn: conn}), "ws-session-feat-rej")
	defer e.Poison(sessionPID)
	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"hello","request_id":"h1","requested_features":["batching","unknown_feature"]}`)}
	resp := <-conn.writeCh
	errMsg, ok := resp.(wsErrorFrame)
	if !ok {
		t.Fatalf("expected wsErrorFrame, got %T: %#v", resp, resp)
	}
	if errMsg.Type != "error" || errMsg.Op != "hello" {
		t.Fatalf("type=%q op=%q want type=error op=hello", errMsg.Type, errMsg.Op)
	}
}

func TestHandleClientHello_MixedValidInvalid_NoPartialAccept(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-feat-mixed")
	defer e.Poison(routerPID)

	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{RouterPID: routerPID, Conn: conn}), "ws-session-feat-mixed")
	defer e.Poison(sessionPID)
	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"hello","request_id":"h2","requested_features":["batching","bogus"]}`)}
	resp := <-conn.writeCh
	if _, ok := resp.(wsErrorFrame); !ok {
		t.Fatalf("expected error (no partial accept), got %T", resp)
	}
}

func TestHandleClientHello_NoFeaturesOmitsField(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-feat-empty")
	defer e.Poison(routerPID)

	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{RouterPID: routerPID, Conn: conn}), "ws-session-feat-empty")
	defer e.Poison(sessionPID)
	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"hello","request_id":"h3"}`)}
	resp := <-conn.writeCh
	ack, ok := resp.(wsHelloAckFrame)
	if !ok {
		t.Fatalf("expected wsHelloAckFrame, got %T: %#v", resp, resp)
	}
	if len(ack.NegotiatedFeatures) != 0 {
		t.Fatalf("negotiated_features=%v want empty (omitempty)", ack.NegotiatedFeatures)
	}
}

func TestHandleClientHello_AllThreeSupported(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-feat-all3")
	defer e.Poison(routerPID)

	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{RouterPID: routerPID, Conn: conn}), "ws-session-feat-all3")
	defer e.Poison(sessionPID)
	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"hello","request_id":"h4","requested_features":["batching","snapshot_hash","prev_seq"]}`)}
	resp := <-conn.writeCh
	ack, ok := resp.(wsHelloAckFrame)
	if !ok {
		t.Fatalf("expected wsHelloAckFrame, got %T", resp)
	}
	if len(ack.NegotiatedFeatures) != 3 {
		t.Fatalf("negotiated_features=%v want 3 features", ack.NegotiatedFeatures)
	}
}
