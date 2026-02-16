package bus

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/observability"
	"github.com/market-raccoon/internal/shared/problem"
)

// dropLogEveryN controls the sampling rate for bus drop warnings.
// One structured log per N drops prevents log spam under sustained backpressure.
const dropLogEveryN = 100

const defaultBusCapacity = 1024

// InMemoryBus is a non-blocking, fan-out EventPublisher backed by buffered channels.
//
// Subscribers register a receive-only channel via Subscribe().  Each call to
// Publish() attempts a non-blocking send to every registered subscriber.  If a
// subscriber's buffer is full the envelope is dropped for that subscriber only
// (the Publish call still succeeds).  This prevents a slow consumer from
// stalling the ingest pipeline.
//
// InMemoryBus is safe for concurrent use.
type InMemoryBus struct {
	mu           sync.RWMutex
	subscribers  []chan envelope.Envelope
	capacity     int
	observer     observability.BusObserver
	droppedTotal atomic.Int64
}

// NewInMemoryBus creates an InMemoryBus with the given per-subscriber channel
// capacity.  Pass 0 to use the default (1024).
func NewInMemoryBus(capacity int, observer ...observability.BusObserver) *InMemoryBus {
	if capacity <= 0 {
		capacity = defaultBusCapacity
	}
	o := observability.NopBusObserver()
	if len(observer) > 0 && observer[0] != nil {
		o = observer[0]
	}
	return &InMemoryBus{
		capacity: capacity,
		observer: o,
	}
}

// Subscribe returns a new receive-only channel that will receive published
// envelopes.  The caller must drain or close the channel to avoid blocking;
// the bus drops envelopes when the channel is full.
//
// The returned channel is closed when Close() is called.
func (b *InMemoryBus) Subscribe() <-chan envelope.Envelope {
	ch := make(chan envelope.Envelope, b.capacity)
	b.mu.Lock()
	b.subscribers = append(b.subscribers, ch)
	b.mu.Unlock()
	return ch
}

// Publish delivers env to all current subscribers using a non-blocking send.
// It never returns an error; full subscriber buffers are silently dropped.
func (b *InMemoryBus) Publish(_ context.Context, env envelope.Envelope) *problem.Problem {
	b.observer.IncPublished(env.Type, env.Venue)

	b.mu.RLock()
	subs := b.subscribers
	b.mu.RUnlock()

	for i, ch := range subs {
		select {
		case ch <- env:
		default:
			// subscriber buffer full — drop for this subscriber, continue.
			b.observer.IncDropped(i)
			n := b.droppedTotal.Add(1)
			if n%dropLogEveryN == 1 {
				slog.Warn("bus: envelope dropped (subscriber buffer full)",
					"subscriber", i,
					"type", env.Type,
					"venue", env.Venue,
					"total_dropped", n,
				)
			}
		}
	}
	return nil
}

// Close closes all subscriber channels, signalling end-of-stream.
// After Close, any subsequent Publish calls are no-ops.
func (b *InMemoryBus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ch := range b.subscribers {
		close(ch)
	}
	b.subscribers = nil
}

// Len returns the number of active subscribers (useful in tests).
func (b *InMemoryBus) Len() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subscribers)
}
