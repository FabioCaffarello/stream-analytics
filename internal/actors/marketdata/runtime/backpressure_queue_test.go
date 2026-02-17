package mdruntime

import (
	"fmt"
	"sync"
	"testing"

	"github.com/market-raccoon/internal/actors/marketdata/ws"
)

// ── correctness ─────────────────────────────────────────────────────────────

func TestWSQueue_EnqueuePop_FIFO(t *testing.T) {
	q := newWSQueue(4, BackpressureDropOldest)
	for i := 0; i < 4; i++ {
		q.Enqueue(tradeMsg(i))
	}
	for i := 0; i < 4; i++ {
		msg, ok := q.Pop()
		if !ok {
			t.Fatalf("Pop returned !ok at i=%d", i)
		}
		if string(msg.Data) != fmt.Sprintf("trade-%d", i) {
			t.Fatalf("expected trade-%d, got %s", i, msg.Data)
		}
	}
}

func TestWSQueue_DropOldest_RingWrap(t *testing.T) {
	q := newWSQueue(3, BackpressureDropOldest)
	// Fill queue
	q.Enqueue(tradeMsg(0))
	q.Enqueue(tradeMsg(1))
	q.Enqueue(tradeMsg(2))
	// Pop one so head advances
	msg, _ := q.Pop()
	if string(msg.Data) != "trade-0" {
		t.Fatalf("expected trade-0, got %s", msg.Data)
	}
	// Fill again to capacity (now head=1, count=2)
	q.Enqueue(tradeMsg(3))
	// Now full at capacity=3 with items at indices 1,2,0
	if q.Len() != 3 {
		t.Fatalf("expected len=3, got %d", q.Len())
	}
	// Enqueue one more → drops oldest (trade-1)
	dropped, bp := q.Enqueue(tradeMsg(4))
	if dropped != 1 || !bp {
		t.Fatalf("expected dropped=1 bp=true, got dropped=%d bp=%v", dropped, bp)
	}
	// Remaining should be: trade-2, trade-3, trade-4
	for _, want := range []string{"trade-2", "trade-3", "trade-4"} {
		msg, ok := q.Pop()
		if !ok {
			t.Fatal("unexpected !ok")
		}
		if string(msg.Data) != want {
			t.Fatalf("expected %s, got %s", want, msg.Data)
		}
	}
}

func TestWSQueue_DropDepthKeepTrades(t *testing.T) {
	q := newWSQueue(3, BackpressureDropDepthKeepOps)
	q.Enqueue(tradeMsg(0))
	q.Enqueue(depthMsg(1))
	q.Enqueue(tradeMsg(2))

	// Queue full; enqueue trade → should drop depth-1
	dropped, bp := q.Enqueue(tradeMsg(3))
	if dropped != 1 || !bp {
		t.Fatalf("expected dropped=1 bp=true, got dropped=%d bp=%v", dropped, bp)
	}
	// Remaining: trade-0, trade-2, trade-3
	for _, want := range []string{"trade-0", "trade-2", "trade-3"} {
		msg, ok := q.Pop()
		if !ok {
			t.Fatal("unexpected !ok")
		}
		if string(msg.Data) != want {
			t.Fatalf("expected %s, got %s", want, msg.Data)
		}
	}
}

func TestWSQueue_DropDepthKeepTrades_IncomingDepthDropped(t *testing.T) {
	q := newWSQueue(2, BackpressureDropDepthKeepOps)
	q.Enqueue(tradeMsg(0))
	q.Enqueue(tradeMsg(1))

	// Queue full of trades; enqueue depth → incoming depth is dropped
	dropped, bp := q.Enqueue(depthMsg(2))
	if dropped != 1 || !bp {
		t.Fatalf("expected dropped=1 bp=true, got dropped=%d bp=%v", dropped, bp)
	}
	if q.Len() != 2 {
		t.Fatalf("expected len=2, got %d", q.Len())
	}
}

func TestWSQueue_Close_UnblocksPop(t *testing.T) {
	q := newWSQueue(4, BackpressureDropOldest)
	done := make(chan struct{})
	go func() {
		_, ok := q.Pop()
		if ok {
			t.Error("Pop should return !ok after Close")
		}
		close(done)
	}()
	q.Close()
	<-done
}

func TestWSQueue_Close_DropsEnqueue(t *testing.T) {
	q := newWSQueue(4, BackpressureDropOldest)
	q.Close()
	dropped, bp := q.Enqueue(tradeMsg(0))
	if dropped != 1 || bp {
		t.Fatalf("expected dropped=1 bp=false on closed queue, got dropped=%d bp=%v", dropped, bp)
	}
}

// ── concurrency ─────────────────────────────────────────────────────────────

func TestWSQueue_ConcurrentEnqueuePop_Race(t *testing.T) {
	const (
		capacity  = 64
		producers = 4
		consumers = 2
		perGo     = 1000
	)
	q := newWSQueue(capacity, BackpressureDropOldest)
	var prodWG sync.WaitGroup
	var consWG sync.WaitGroup

	// Producers
	for p := 0; p < producers; p++ {
		prodWG.Add(1)
		go func() {
			defer prodWG.Done()
			for i := 0; i < perGo; i++ {
				q.Enqueue(tradeMsg(i))
			}
		}()
	}

	// Consumers drain until queue is closed.
	consumed := make([]int, consumers)
	for c := 0; c < consumers; c++ {
		consWG.Add(1)
		go func(idx int) {
			defer consWG.Done()
			for {
				_, ok := q.Pop()
				if !ok {
					return
				}
				consumed[idx]++
			}
		}(c)
	}

	// Wait for producers to finish, then close queue so consumers exit.
	prodWG.Wait()
	q.Close()
	consWG.Wait()

	total := 0
	for _, c := range consumed {
		total += c
	}
	// With backpressure some messages are dropped, but we should consume
	// at least the queue capacity worth.
	if total == 0 {
		t.Fatal("consumed zero messages")
	}
}

// ── backpressure burst ──────────────────────────────────────────────────────

func TestWSQueue_BackpressureBurst_1000(t *testing.T) {
	const capacity = 64
	q := newWSQueue(capacity, BackpressureDropOldest)

	totalDropped := 0
	for i := 0; i < 1000; i++ {
		dropped, _ := q.Enqueue(tradeMsg(i))
		totalDropped += dropped
	}

	if q.Len() != capacity {
		t.Fatalf("expected queue at capacity=%d, got len=%d", capacity, q.Len())
	}
	wantDropped := 1000 - capacity
	if totalDropped != wantDropped {
		t.Fatalf("expected %d drops, got %d", wantDropped, totalDropped)
	}

	// Verify we get the last `capacity` messages in order
	for i := 0; i < capacity; i++ {
		msg, ok := q.Pop()
		if !ok {
			t.Fatalf("unexpected !ok at i=%d", i)
		}
		want := fmt.Sprintf("trade-%d", 1000-capacity+i)
		if string(msg.Data) != want {
			t.Fatalf("at i=%d: expected %s, got %s", i, want, msg.Data)
		}
	}
}

// ── benchmarks ──────────────────────────────────────────────────────────────

func BenchmarkWSQueue_EnqueuePop_Steady(b *testing.B) {
	q := newWSQueue(1024, BackpressureDropOldest)
	// Pre-fill to half capacity.
	for i := 0; i < 512; i++ {
		q.Enqueue(tradeMsg(i))
	}
	msg := tradeMsg(0)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q.Enqueue(msg)
		q.Pop()
	}
}

func BenchmarkWSQueue_Enqueue_Backpressure_DropOldest(b *testing.B) {
	q := newWSQueue(1024, BackpressureDropOldest)
	// Fill to capacity.
	for i := 0; i < 1024; i++ {
		q.Enqueue(tradeMsg(i))
	}
	msg := tradeMsg(0)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q.Enqueue(msg)
	}
}

func BenchmarkWSQueue_Enqueue_Backpressure_DropDepth(b *testing.B) {
	q := newWSQueue(1024, BackpressureDropDepthKeepOps)
	// Fill: alternating depth and trade.
	for i := 0; i < 1024; i++ {
		if i%2 == 0 {
			q.Enqueue(depthMsg(i))
		} else {
			q.Enqueue(tradeMsg(i))
		}
	}
	msg := tradeMsg(0)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q.Enqueue(msg)
	}
}

// ── helpers ─────────────────────────────────────────────────────────────────

func tradeMsg(i int) *ws.WsMessage {
	return &ws.WsMessage{
		Data:     []byte(fmt.Sprintf("trade-%d", i)),
		Exchange: "test",
	}
}

func depthMsg(i int) *ws.WsMessage {
	return &ws.WsMessage{
		Data:     []byte(fmt.Sprintf(`{"e":"depthUpdate","d":%d}`, i)),
		Exchange: "test",
	}
}
