package deliveryruntime

import (
	"errors"
	"testing"
	"time"

	"github.com/anthdm/hollywood/actor"
	"github.com/gorilla/websocket"
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

func (f *fakeConn) SetReadLimit(limit int64)            {}
func (f *fakeConn) SetReadDeadline(t time.Time) error   { return nil }
func (f *fakeConn) SetPongHandler(h func(string) error) {}
func (f *fakeConn) Close() error                        { f.closed = true; return nil }

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
	if got, want := sub.Subject.String(), "marketdata.trade/binance/BTC-USDT/raw"; got != want {
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
	_ = <-conn.writeCh
	_ = waitForMessage[UnsubscribeSession](t, routerCh, time.Second)
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
	_ = <-conn.writeCh
	conn.readCh <- fakeRead{err: errors.New("disconnect")}

	_ = waitForMessage[UnsubscribeSession](t, routerCh, time.Second)
	_ = waitForMessage[UnregisterSession](t, routerCh, time.Second)
	<-e.Poison(sessionPID).Done()
	if !conn.closed {
		t.Fatal("connection should be closed")
	}
}
