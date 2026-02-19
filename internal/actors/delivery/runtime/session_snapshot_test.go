package deliveryruntime

import (
	"testing"
	"time"

	"github.com/anthdm/hollywood/actor"
	"github.com/gorilla/websocket"
	"github.com/market-raccoon/internal/core/delivery/domain"
	"github.com/market-raccoon/internal/core/delivery/ports"
)

type stubSnapshotProvider struct {
	bySubject map[string][]byte
}

func (s stubSnapshotProvider) GetLatest(subject domain.Subject) ([]byte, bool) {
	v, ok := s.bySubject[subject.String()]
	return v, ok
}

func TestSession_SubscribeEmitsHotSnapshot(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-capture-snapshot")
	defer e.Poison(routerPID)

	subject := mustParseSubjectForSession(t, "marketdata.trade/binance/BTC-USDT/raw")
	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{
		RouterPID:              routerPID,
		Conn:                   conn,
		HotSnapshotProvider:    stubSnapshotProvider{bySubject: map[string][]byte{subject.String(): []byte(`{"seq":101}`)}},
		OutboundQueueSize:      8,
		BackpressurePolicy:     domain.BackpressureDropNewest,
		BackpressurePriorities: nil,
	}), "ws-session-snapshot")
	defer e.Poison(sessionPID)

	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)
	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"subscribe","subject":"marketdata.trade/binance/BTC-USDT/raw","request_id":"s1"}`)}

	first := <-conn.writeCh
	snap, ok := first.(map[string]any)
	if !ok || snap["type"] != "snapshot" {
		t.Fatalf("expected snapshot first, got %#v", first)
	}
	second := <-conn.writeCh
	ack, ok := second.(map[string]any)
	if !ok || ack["type"] != "ack" {
		t.Fatalf("expected ack second, got %#v", second)
	}
}

func TestSession_SubscribeNoSnapshot_WhenEmpty(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-capture-no-snapshot")
	defer e.Poison(routerPID)

	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{
		RouterPID:           routerPID,
		Conn:                conn,
		HotSnapshotProvider: stubSnapshotProvider{},
	}), "ws-session-no-snapshot")
	defer e.Poison(sessionPID)

	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)
	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"subscribe","subject":"marketdata.trade/binance/BTC-USDT/raw","request_id":"s2"}`)}

	msg := <-conn.writeCh
	ack, ok := msg.(map[string]any)
	if !ok || ack["type"] != "ack" {
		t.Fatalf("expected ack when no snapshot, got %#v", msg)
	}
}

func TestSession_GetRange_ReturnsItems(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-capture-range")
	defer e.Poison(routerPID)

	sub := mustParseSubjectForSession(t, "marketdata.trade/binance/BTCUSDT/raw")
	store := &stubRangeStore{
		bySubject: map[string][]ports.RangeItem{
			sub.String(): {
				{Seq: 1, TsIngest: 1, Payload: []byte(`{"seq":1}`)},
				{Seq: 2, TsIngest: 2, Payload: []byte(`{"seq":2}`)},
			},
		},
	}

	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{RouterPID: routerPID, Conn: conn, RangeStore: store}), "ws-session-range")
	defer e.Poison(sessionPID)
	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"getrange","subject":"marketdata.trade/binance/BTC-USDT/raw","request_id":"r1","params":{"from_ms":0,"to_ms":0,"limit":2}}`)}
	msg := <-conn.writeCh
	resp, ok := msg.(map[string]any)
	if !ok || resp["type"] != "range" {
		t.Fatalf("expected range response, got %#v", msg)
	}
}

func TestSession_GetRange_EmptyRange(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-capture-empty-range")
	defer e.Poison(routerPID)

	sub := mustParseSubjectForSession(t, "marketdata.trade/binance/BTCUSDT/raw")
	store := &stubRangeStore{bySubject: map[string][]ports.RangeItem{sub.String(): {}}}

	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{RouterPID: routerPID, Conn: conn, RangeStore: store}), "ws-session-empty-range")
	defer e.Poison(sessionPID)
	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"getrange","subject":"marketdata.trade/binance/BTC-USDT/raw","request_id":"r2","params":{"from_ms":0,"to_ms":0,"limit":2}}`)}
	msg := <-conn.writeCh
	resp, ok := msg.(map[string]any)
	if !ok || resp["type"] != "range" {
		t.Fatalf("expected range response, got %#v", msg)
	}
	items, ok := resp["items"].([]ports.RangeItem)
	if !ok {
		t.Fatalf("items type=%T", resp["items"])
	}
	if len(items) != 0 {
		t.Fatalf("expected empty items, got %d", len(items))
	}
}

func TestSession_GetRange_LimitEnforced(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	routerCh := make(chan any, 32)
	routerPID := e.Spawn(func() actor.Receiver { return &captureActor{ch: routerCh} }, "router-capture-limit")
	defer e.Poison(routerPID)

	conn := newFakeConn()
	sessionPID := e.Spawn(NewSessionActor(SessionConfig{RouterPID: routerPID, Conn: conn, RangeStore: &stubRangeStore{}}), "ws-session-limit")
	defer e.Poison(sessionPID)
	_ = waitForMessage[RegisterSession](t, routerCh, time.Second)

	conn.readCh <- fakeRead{typ: websocket.TextMessage, data: []byte(`{"op":"getrange","subject":"marketdata.trade/binance/BTC-USDT/raw","request_id":"r3","params":{"from_ms":0,"to_ms":0,"limit":1001}}`)}
	msg := <-conn.writeCh
	resp, ok := msg.(map[string]any)
	if !ok || resp["type"] != "error" {
		t.Fatalf("expected error response, got %#v", msg)
	}
}
