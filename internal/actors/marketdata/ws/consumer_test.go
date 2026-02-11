package ws

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/anthdm/hollywood/actor"
)

type readResult struct {
	msg []byte
	err error
}

type fakeConn struct {
	readCh  chan readResult
	closeCh chan struct{}

	mu         sync.Mutex
	closed     bool
	closeCount int

	pingHandler func(string) error
	pongHandler func(string) error
}

func newFakeConn() *fakeConn {
	return &fakeConn{
		readCh:  make(chan readResult, 8),
		closeCh: make(chan struct{}),
	}
}

func (f *fakeConn) WriteMessage(messageType int, data []byte) error {
	return nil
}

func (f *fakeConn) WriteControl(messageType int, data []byte, deadline time.Time) error {
	return nil
}

func (f *fakeConn) ReadMessage() (int, []byte, error) {
	select {
	case <-f.closeCh:
		return 0, nil, errors.New("closed")
	case res := <-f.readCh:
		if res.err != nil {
			return 0, nil, res.err
		}
		return 1, res.msg, nil
	}
}

func (f *fakeConn) SetPingHandler(h func(appData string) error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pingHandler = h
}

func (f *fakeConn) SetPongHandler(h func(appData string) error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pongHandler = h
}

func (f *fakeConn) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.closed {
		f.closed = true
		close(f.closeCh)
	}
	f.closeCount++
	return nil
}

func (f *fakeConn) WriteJSON(v any) error {
	return nil
}

func (f *fakeConn) getCloseCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closeCount
}

type sinkActor struct {
	ch chan any
}

func (s *sinkActor) Receive(c *actor.Context) {
	switch msg := c.Message().(type) {
	case *WsState, *WsMessage, *WsError:
		s.ch <- msg
	}
}

func TestConsumer_ShutdownWithoutSpuriousError(t *testing.T) {
	fake := newFakeConn()

	origDial := consumerDial
	consumerDialMu.Lock()
	consumerDial = func(ctx context.Context, url string) (wsConn, error) {
		return fake, nil
	}
	consumerDialMu.Unlock()
	defer func() {
		consumerDialMu.Lock()
		consumerDial = origDial
		consumerDialMu.Unlock()
	}()

	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	sinkCh := make(chan any, 64)
	sinkPID := e.Spawn(func() actor.Receiver { return &sinkActor{ch: sinkCh} }, "sink")
	consumerPID := e.Spawn(NewConsumer(ConsumerConfig{
		Exchange:   "binance",
		Endpoint:   "wss://fake",
		BucketID:   7,
		ConsumerID: "c-1",
		SendTo:     sinkPID,
	}), "consumer")

	<-e.Poison(consumerPID).Done()
	<-e.Poison(sinkPID).Done()

	deadline := time.After(500 * time.Millisecond)
	for {
		select {
		case raw := <-sinkCh:
			if wsErr, ok := raw.(*WsError); ok {
				t.Fatalf("unexpected WsError during graceful shutdown: kind=%s err=%v", wsErr.Kind, wsErr.Err)
			}
		case <-deadline:
			return
		}
	}
}

func TestConsumer_ErrorEmitsWsErrorAndStateOnce(t *testing.T) {
	fake := newFakeConn()
	fake.readCh <- readResult{err: errors.New("read failed")}

	origDial := consumerDial
	consumerDialMu.Lock()
	consumerDial = func(ctx context.Context, url string) (wsConn, error) {
		return fake, nil
	}
	consumerDialMu.Unlock()
	defer func() {
		consumerDialMu.Lock()
		consumerDial = origDial
		consumerDialMu.Unlock()
	}()

	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	sinkCh := make(chan any, 64)
	sinkPID := e.Spawn(func() actor.Receiver { return &sinkActor{ch: sinkCh} }, "sink")
	consumerPID := e.Spawn(NewConsumer(ConsumerConfig{
		Exchange:   "binance",
		Endpoint:   "wss://fake",
		BucketID:   7,
		ConsumerID: "c-1",
		SendTo:     sinkPID,
	}), "consumer")
	defer e.Poison(consumerPID)
	defer e.Poison(sinkPID)

	deadline := time.After(2 * time.Second)
	var gotErr *WsError
	errorStateCount := 0

	for gotErr == nil || errorStateCount == 0 {
		select {
		case raw := <-sinkCh:
			switch msg := raw.(type) {
			case *WsError:
				if msg.Kind == "read" {
					if gotErr != nil {
						t.Fatalf("received duplicate WsError: %+v", msg)
					}
					gotErr = msg
				}
			case *WsState:
				if msg.Status == "error" {
					errorStateCount++
					if errorStateCount > 1 {
						t.Fatalf("received duplicate error state: %+v", msg)
					}
				}
			}
		case <-deadline:
			t.Fatalf("timeout waiting error events; gotErr=%v errorStateCount=%d", gotErr != nil, errorStateCount)
		}
	}

	if gotErr.Err == nil {
		t.Fatal("expected non-nil WsError.Err")
	}
}

func TestConsumer_StopIsIdempotent(t *testing.T) {
	fake := newFakeConn()
	cancelCount := 0
	c := &Consumer{
		quitch: make(chan struct{}),
		conn:   fake,
		cancel: func() { cancelCount++ },
	}

	c.Stop()
	c.Stop()

	if cancelCount != 1 {
		t.Fatalf("cancel calls = %d, want 1", cancelCount)
	}
	if got := fake.getCloseCount(); got != 1 {
		t.Fatalf("close calls = %d, want 1", got)
	}
}
