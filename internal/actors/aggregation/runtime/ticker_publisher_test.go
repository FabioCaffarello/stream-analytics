package aggruntime

import (
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/anthdm/hollywood/actor"
)

type snapshotTickSink struct {
	ch chan SnapshotTick
}

func (s *snapshotTickSink) Receive(c *actor.Context) {
	switch msg := c.Message().(type) {
	case SnapshotTick:
		select {
		case s.ch <- msg:
		default:
		}
	}
}

type fakeTicker struct {
	ch      chan time.Time
	mu      sync.Mutex
	stopped bool
}

func newFakeTicker() *fakeTicker {
	return &fakeTicker{ch: make(chan time.Time, 8)}
}

func (f *fakeTicker) C() <-chan time.Time { return f.ch }

func (f *fakeTicker) Stop() {
	f.mu.Lock()
	f.stopped = true
	f.mu.Unlock()
}

func (f *fakeTicker) isStopped() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stopped
}

func waitForCondition(t *testing.T, timeout time.Duration, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}

func TestTickerPublisherActor_IntervalZero_DisablesTicker(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	var created int
	cfg := TickerPublisherConfig{
		OrderbookInterval: 0,
		HeatmapInterval:   0,
		VolumeInterval:    0,
		NewTicker: func(interval time.Duration) runtimeTicker {
			created++
			return newFakeTicker()
		},
	}
	pid := e.Spawn(NewTickerPublisherActor(cfg), "ticker-disabled", actor.WithID("ticker-disabled"))
	<-e.Poison(pid).Done()

	if created != 0 {
		t.Fatalf("created tickers=%d want=0", created)
	}
}

func TestTickerPublisherActor_StopsOnShutdown(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	sinkCh := make(chan SnapshotTick, 8)
	sinkPID := e.Spawn(func() actor.Receiver {
		return &snapshotTickSink{ch: sinkCh}
	}, "ticker-sink", actor.WithID("ticker-sink"))

	var tickers []*fakeTicker
	var mu sync.Mutex
	cfg := TickerPublisherConfig{
		Target:            sinkPID,
		OrderbookInterval: 5 * time.Millisecond,
		NewTicker: func(interval time.Duration) runtimeTicker {
			ft := newFakeTicker()
			mu.Lock()
			tickers = append(tickers, ft)
			mu.Unlock()
			return ft
		},
	}
	pid := e.Spawn(NewTickerPublisherActor(cfg), "ticker-stop", actor.WithID("ticker-stop"))

	waitForCondition(t, time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(tickers) == 1
	})

	mu.Lock()
	active := tickers[0]
	mu.Unlock()

	active.ch <- time.Now()
	select {
	case msg := <-sinkCh:
		if msg.Kind != SnapshotTickOrderBook {
			t.Fatalf("tick kind=%s want=%s", msg.Kind, SnapshotTickOrderBook)
		}
	case <-time.After(time.Second):
		t.Fatal("expected orderbook tick")
	}

	<-e.Poison(pid).Done()
	waitForCondition(t, time.Second, active.isStopped)

	active.ch <- time.Now()
	select {
	case msg := <-sinkCh:
		t.Fatalf("unexpected tick after stop: %+v", msg)
	case <-time.After(100 * time.Millisecond):
	}

	<-e.Poison(sinkPID).Done()
}

func TestTickerPublisherActor_NoGoroutineLeak(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	sinkPID := e.Spawn(func() actor.Receiver {
		return &snapshotTickSink{ch: make(chan SnapshotTick, 8)}
	}, "ticker-leak-sink", actor.WithID("ticker-leak-sink"))

	base := runtime.NumGoroutine()
	for i := 0; i < 30; i++ {
		cfg := TickerPublisherConfig{
			Target:            sinkPID,
			OrderbookInterval: 10 * time.Millisecond,
			NewTicker: func(interval time.Duration) runtimeTicker {
				return newFakeTicker()
			},
		}
		id := fmt.Sprintf("ticker-leak-%d", i)
		pid := e.Spawn(NewTickerPublisherActor(cfg), id, actor.WithID(id))
		<-e.Poison(pid).Done()
	}

	waitForCondition(t, 2*time.Second, func() bool {
		return runtime.NumGoroutine() <= base+4
	})
	<-e.Poison(sinkPID).Done()
}
