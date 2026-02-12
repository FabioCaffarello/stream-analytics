package ws

import (
	"context"
	"errors"
	"fmt"
	"runtime"
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

	readDeadline time.Time
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

func (f *fakeConn) SetReadDeadline(t time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.readDeadline = t
	return nil
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

func TestConsumer_ErrorEmitsWsErrorAndReconnectState(t *testing.T) {
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
	gotReconnectState := false

	for gotErr == nil || !gotReconnectState {
		select {
		case raw := <-sinkCh:
			switch msg := raw.(type) {
			case *WsError:
				if msg.Kind == "read" {
					gotErr = msg
				}
			case *WsState:
				if msg.Status == "reconnecting" {
					gotReconnectState = true
				}
			}
		case <-deadline:
			t.Fatalf("timeout waiting error events; gotErr=%v gotReconnectState=%v", gotErr != nil, gotReconnectState)
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

func TestConsumer_ConnectSetsReadDeadlineAndPongHandler(t *testing.T) {
	fake := newFakeConn()
	fake.readCh <- readResult{err: errors.New("force read exit")}

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

	time.Sleep(100 * time.Millisecond)

	fake.mu.Lock()
	deadlineSet := !fake.readDeadline.IsZero()
	hasPongHandler := fake.pongHandler != nil
	fake.mu.Unlock()

	if !deadlineSet {
		t.Fatal("expected read deadline to be set on connect")
	}
	if !hasPongHandler {
		t.Fatal("expected pong handler to be set on connect")
	}
}

func TestConsumer_ConnectDisconnectCycle_NoGoroutineLeak(t *testing.T) {
	origDial := consumerDial
	consumerDialMu.Lock()
	consumerDial = func(ctx context.Context, url string) (wsConn, error) {
		return newFakeConn(), nil
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
	sinkCh := make(chan any, 512)
	sinkPID := e.Spawn(func() actor.Receiver { return &sinkActor{ch: sinkCh} }, "sink-leak")
	defer e.Poison(sinkPID)

	base := runtime.NumGoroutine()
	const cycles = 40
	for i := 0; i < cycles; i++ {
		id := fmt.Sprintf("consumer-leak-%d", i)
		consumerPID := e.Spawn(NewConsumer(ConsumerConfig{
			Exchange:   "binance",
			Endpoint:   "wss://fake",
			BucketID:   int64(i % 4),
			ConsumerID: id,
			SendTo:     sinkPID,
		}), id)
		time.Sleep(5 * time.Millisecond)
		<-e.Poison(consumerPID).Done()
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= base+3 {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	after := runtime.NumGoroutine()
	t.Fatalf("goroutine leak suspected: base=%d after=%d", base, after)
}

func TestConsumer_ConnectDisconnectCycle_HeapStable(t *testing.T) {
	origDial := consumerDial
	consumerDialMu.Lock()
	consumerDial = func(ctx context.Context, url string) (wsConn, error) {
		return newFakeConn(), nil
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
	sinkPID := e.Spawn(func() actor.Receiver { return &sinkActor{ch: make(chan any, 1024)} }, "sink-heap")
	defer e.Poison(sinkPID)

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	const cycles = 60
	for i := 0; i < cycles; i++ {
		id := fmt.Sprintf("consumer-heap-%d", i)
		consumerPID := e.Spawn(NewConsumer(ConsumerConfig{
			Exchange:   "binance",
			Endpoint:   "wss://fake",
			BucketID:   int64(i % 4),
			ConsumerID: id,
			SendTo:     sinkPID,
		}), id)
		time.Sleep(5 * time.Millisecond)
		<-e.Poison(consumerPID).Done()
	}

	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	// Allow moderate fluctuation from test harness allocations.
	limit := before.HeapAlloc*2 + 5*1024*1024
	if after.HeapAlloc > limit {
		t.Fatalf("heap growth above limit: before=%d after=%d limit=%d", before.HeapAlloc, after.HeapAlloc, limit)
	}
}
