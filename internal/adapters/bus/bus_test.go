package bus_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/market-raccoon/internal/adapters/bus"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/problem"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func makeEnvelope(venue, instrument, evtype string, seq int64) envelope.Envelope {
	return envelope.Envelope{
		Type:           evtype,
		Version:        1,
		Venue:          venue,
		Instrument:     instrument,
		TsExchange:     1_700_000_000_000,
		TsIngest:       1_700_000_000_001,
		Seq:            seq,
		IdempotencyKey: "idem-" + instrument,
		Payload:        []byte(`{"price":"42000"}`),
	}
}

// ---------------------------------------------------------------------------
// LogPublisher
// ---------------------------------------------------------------------------

func TestLogPublisher_Publish_nilLogger(t *testing.T) {
	// Should not panic when constructed with nil logger.
	p := bus.NewLogPublisher(nil)
	env := makeEnvelope("binance", "BTC-USDT", "marketdata.trade", 1)
	if err := p.Publish(context.Background(), env); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLogPublisher_Publish_writesToLogger(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	p := bus.NewLogPublisher(logger)

	env := makeEnvelope("binance", "ETH-USDT", "marketdata.trade", 7)
	if err := p.Publish(context.Background(), env); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "ETH-USDT") {
		t.Errorf("expected instrument in log output, got: %s", out)
	}
	if !strings.Contains(out, "marketdata.trade") {
		t.Errorf("expected event type in log output, got: %s", out)
	}
}

func TestLogPublisher_Publish_returnsNil(t *testing.T) {
	p := bus.NewLogPublisher(nil)
	env := makeEnvelope("kraken", "BTC-USD", "marketdata.trade", 1)
	if p.Publish(context.Background(), env) != nil {
		t.Fatal("LogPublisher.Publish must always return nil")
	}
}

// ---------------------------------------------------------------------------
// InMemoryBus
// ---------------------------------------------------------------------------

func TestInMemoryBus_defaultCapacity(t *testing.T) {
	b := bus.NewInMemoryBus(0)
	ch := b.Subscribe()
	if cap(ch) != 1024 {
		t.Fatalf("expected default capacity 1024, got %d", cap(ch))
	}
}

func TestInMemoryBus_customCapacity(t *testing.T) {
	b := bus.NewInMemoryBus(16)
	ch := b.Subscribe()
	if cap(ch) != 16 {
		t.Fatalf("expected capacity 16, got %d", cap(ch))
	}
}

func TestInMemoryBus_Len(t *testing.T) {
	b := bus.NewInMemoryBus(8)
	if b.Len() != 0 {
		t.Fatal("expected 0 subscribers initially")
	}
	b.Subscribe()
	b.Subscribe()
	if b.Len() != 2 {
		t.Fatalf("expected 2 subscribers, got %d", b.Len())
	}
}

func TestInMemoryBus_Publish_deliversToAllSubscribers(t *testing.T) {
	b := bus.NewInMemoryBus(8)
	ch1 := b.Subscribe()
	ch2 := b.Subscribe()

	env := makeEnvelope("binance", "BTC-USDT", "marketdata.trade", 1)
	if err := b.Publish(context.Background(), env); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got1 := <-ch1
	got2 := <-ch2
	if got1.Seq != 1 || got2.Seq != 1 {
		t.Fatalf("unexpected seq: got1=%d got2=%d", got1.Seq, got2.Seq)
	}
}

func TestInMemoryBus_Publish_dropsWhenFull(t *testing.T) {
	b := bus.NewInMemoryBus(1)
	ch := b.Subscribe()

	env := makeEnvelope("binance", "BTC-USDT", "marketdata.trade", 1)
	// First publish fills the buffer.
	if err := b.Publish(context.Background(), env); err != nil {
		t.Fatalf("unexpected error on first publish: %v", err)
	}
	// Second publish should drop silently without blocking.
	env2 := makeEnvelope("binance", "BTC-USDT", "marketdata.trade", 2)
	if err := b.Publish(context.Background(), env2); err != nil {
		t.Fatalf("unexpected error on second publish (drop): %v", err)
	}

	got := <-ch
	if got.Seq != 1 {
		t.Fatalf("expected seq 1 (first), got %d", got.Seq)
	}
	// Channel should now be empty (second was dropped).
	select {
	case extra := <-ch:
		t.Fatalf("expected empty channel after drop, got seq=%d", extra.Seq)
	default:
	}

	if got := testutil.ToFloat64(metrics.BusDroppedTotal.WithLabelValues("0")); got < 1 {
		t.Fatalf("expected bus drop metric increment for subscriber 0, got %f", got)
	}
}

func TestInMemoryBus_Publish_concurrentSafe(t *testing.T) {
	b := bus.NewInMemoryBus(512)
	ch := b.Subscribe()

	const goroutines = 50
	const perGoroutine = 10

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(base int) {
			defer wg.Done()
			for j := 0; j < perGoroutine; j++ {
				env := makeEnvelope("binance", "BTC-USDT", "marketdata.trade", int64(base*perGoroutine+j))
				_ = b.Publish(context.Background(), env)
			}
		}(i)
	}
	wg.Wait()

	// Drain what was received (some may have been dropped if buffer got full).
	count := 0
	for {
		select {
		case <-ch:
			count++
		default:
			goto done
		}
	}
done:
	// At least 1 envelope must have been delivered.
	if count == 0 {
		t.Fatal("expected at least one delivered envelope")
	}
}

func TestInMemoryBus_Close_closesSubscriberChannels(t *testing.T) {
	b := bus.NewInMemoryBus(8)
	ch1 := b.Subscribe()
	ch2 := b.Subscribe()

	b.Close()

	// Both channels must be closed.
	if _, ok := <-ch1; ok {
		t.Fatal("ch1 should be closed after Close()")
	}
	if _, ok := <-ch2; ok {
		t.Fatal("ch2 should be closed after Close()")
	}
	// Subsequent Publish should not panic.
	env := makeEnvelope("binance", "BTC-USDT", "marketdata.trade", 1)
	if err := b.Publish(context.Background(), env); err != nil {
		t.Fatalf("unexpected error after Close: %v", err)
	}
}

func TestInMemoryBus_Publish_afterClose_isNoop(t *testing.T) {
	b := bus.NewInMemoryBus(8)
	b.Close()

	env := makeEnvelope("binance", "BTC-USDT", "marketdata.trade", 99)
	if err := b.Publish(context.Background(), env); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b.Len() != 0 {
		t.Fatal("expected 0 subscribers after Close")
	}
}

// ---------------------------------------------------------------------------
// Interface compliance — compile-time assertions
// ---------------------------------------------------------------------------
// These blank assignments verify that both types satisfy the exact signature
// of core/marketdata/ports.EventPublisher without importing that package.
// We mirror the interface locally to avoid a module dependency cycle.

type eventPublisher interface {
	Publish(ctx context.Context, env envelope.Envelope) *problem.Problem
}

var (
	_ eventPublisher = (*bus.LogPublisher)(nil)
	_ eventPublisher = (*bus.InMemoryBus)(nil)
)
