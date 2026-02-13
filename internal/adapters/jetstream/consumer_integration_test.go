//go:build integration

package jetstream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/problem"
	"github.com/nats-io/nats.go"
)

func TestConsumerIntegration_DurableRestart(t *testing.T) {
	url, cleanup := startJetStreamNATS(t)
	defer cleanup()

	pub := mustPublisher(t, url)
	defer func() { _ = pub.Close(context.Background()) }()

	for i := 0; i < 100; i++ {
		env := testEnvelope(i, fmt.Sprintf("idem-dr-100-%d", i), "BTCUSDT")
		if p := pub.Publish(context.Background(), env); p != nil {
			t.Fatalf("publish[%d] failed: %v", i, p)
		}
	}

	consumerCfg := testConsumerConfig(url, "processor-w7-2-durable")
	c1, p := NewConsumer(context.Background(), consumerCfg, metrics.NewBusObserver())
	if p != nil {
		t.Fatalf("new consumer c1 failed: %v", p)
	}

	if got := consumeUntilCount(t, c1, 100, func(int) *problem.Problem { return nil }, 20*time.Second); got != 100 {
		t.Fatalf("first consume got=%d want=100", got)
	}
	_ = c1.Close(context.Background())

	for i := 0; i < 50; i++ {
		env := testEnvelope(100+i, fmt.Sprintf("idem-dr-50-%d", i), "BTCUSDT")
		if p := pub.Publish(context.Background(), env); p != nil {
			t.Fatalf("publish restart[%d] failed: %v", i, p)
		}
	}

	c2, p := NewConsumer(context.Background(), consumerCfg, metrics.NewBusObserver())
	if p != nil {
		t.Fatalf("new consumer c2 failed: %v", p)
	}
	defer func() { _ = c2.Close(context.Background()) }()

	if got := consumeUntilCount(t, c2, 50, func(int) *problem.Problem { return nil }, 20*time.Second); got != 50 {
		t.Fatalf("second consume got=%d want=50", got)
	}
}

func TestConsumerIntegration_PoisonMessageTerminated(t *testing.T) {
	url, cleanup := startJetStreamNATS(t)
	defer cleanup()

	pub := mustPublisher(t, url)
	defer func() { _ = pub.Close(context.Background()) }()

	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("nats connect failed: %v", err)
	}
	defer nc.Close()
	js, err := nc.JetStream()
	if err != nil {
		t.Fatalf("jetstream context failed: %v", err)
	}

	msg := nats.NewMsg("marketdata.bookdelta.v1.binance.BTCUSDT")
	msg.Data = []byte("{invalid-envelope")
	msg.Header.Set(nats.MsgIdHdr, "poison-1")
	if _, err := js.PublishMsg(msg); err != nil {
		t.Fatalf("publish poison failed: %v", err)
	}

	consumerCfg := testConsumerConfig(url, "processor-w7-2-poison")
	c, p := NewConsumer(context.Background(), consumerCfg, metrics.NewBusObserver())
	if p != nil {
		t.Fatalf("new consumer failed: %v", p)
	}
	defer func() { _ = c.Close(context.Background()) }()

	var handlerCalls atomic.Int64
	consumeCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	done := make(chan *problem.Problem, 1)
	go func() {
		done <- c.Consume(consumeCtx, func(context.Context, envelope.Envelope) *problem.Problem {
			handlerCalls.Add(1)
			return nil
		})
	}()
	time.Sleep(1200 * time.Millisecond)
	cancel()
	<-done

	if handlerCalls.Load() != 0 {
		t.Fatalf("poison payload should not reach handler, got %d calls", handlerCalls.Load())
	}

	quarantineSub, err := js.PullSubscribe("quarantine.v1.>", "", nats.BindStream("MARKETDATA"))
	if err != nil {
		t.Fatalf("quarantine pull subscribe failed: %v", err)
	}
	qmsgs, err := quarantineSub.Fetch(1, nats.MaxWait(2*time.Second))
	if err != nil {
		t.Fatalf("fetch quarantine failed: %v", err)
	}
	if len(qmsgs) != 1 {
		t.Fatalf("expected one quarantine message, got %d", len(qmsgs))
	}
	qEnv, p := envelope.UnmarshalBinary(qmsgs[0].Data)
	if p != nil {
		t.Fatalf("decode quarantine envelope failed: %v", p)
	}
	var q quarantinePayload
	if err := json.Unmarshal(qEnv.Payload, &q); err != nil {
		t.Fatalf("decode quarantine payload failed: %v", err)
	}
	if q.ReasonCode != ingestReasonDecodeFailed {
		t.Fatalf("quarantine reason=%q want=%q", q.ReasonCode, ingestReasonDecodeFailed)
	}
	_ = qmsgs[0].Ack()

	// Verify no infinite redelivery for poison: no pending message remains.
	sub, err := js.PullSubscribe("marketdata.>", "processor-w7-2-poison", nats.Bind("MARKETDATA", "processor-w7-2-poison"))
	if err != nil {
		t.Fatalf("pull subscribe bind failed: %v", err)
	}
	msgs, err := sub.Fetch(1, nats.MaxWait(1200*time.Millisecond))
	if err != nil && !errors.Is(err, nats.ErrTimeout) {
		t.Fatalf("fetch after poison term failed: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected no redelivery after term, got %d message(s)", len(msgs))
	}
}

func TestConsumerIntegration_TransientNakThenAck(t *testing.T) {
	url, cleanup := startJetStreamNATS(t)
	defer cleanup()

	pub := mustPublisher(t, url)
	defer func() { _ = pub.Close(context.Background()) }()

	env := testEnvelope(1, "transient-1", "BTCUSDT")
	if p := pub.Publish(context.Background(), env); p != nil {
		t.Fatalf("publish failed: %v", p)
	}

	cfg := testConsumerConfig(url, "processor-w7-2-transient")
	cfg.AckWait = 2 * time.Second
	cfg.MaxDeliver = 5

	c, p := NewConsumer(context.Background(), cfg, metrics.NewBusObserver())
	if p != nil {
		t.Fatalf("new consumer failed: %v", p)
	}
	defer func() { _ = c.Close(context.Background()) }()

	var attempts atomic.Int64
	var acked atomic.Int64
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	done := make(chan *problem.Problem, 1)
	go func() {
		done <- c.Consume(ctx, func(context.Context, envelope.Envelope) *problem.Problem {
			n := attempts.Add(1)
			if n <= 2 {
				return problem.WithRetryable(problem.New(problem.Unavailable, "temporary failure"))
			}
			if acked.Add(1) == 1 {
				cancel()
			}
			return nil
		})
	}()

	select {
	case p := <-done:
		if p != nil {
			t.Fatalf("consume failed: %v", p)
		}
	case <-time.After(17 * time.Second):
		t.Fatal("consume timed out")
	}

	if acked.Load() != 1 {
		t.Fatalf("expected exactly one successful ack, got %d", acked.Load())
	}
	if attempts.Load() < 3 {
		t.Fatalf("expected redelivery attempts >= 3, got %d", attempts.Load())
	}
}

func TestConsumerIntegration_StartStopCycles(t *testing.T) {
	url, cleanup := startJetStreamNATS(t)
	defer cleanup()

	cfg := testConsumerConfig(url, "processor-w7-2-cycle")
	for i := 0; i < 10; i++ {
		c, p := NewConsumer(context.Background(), cfg, metrics.NewBusObserver())
		if p != nil {
			t.Fatalf("cycle %d: new consumer failed: %v", i, p)
		}

		runCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		done := make(chan *problem.Problem, 1)
		go func() {
			done <- c.Consume(runCtx, func(context.Context, envelope.Envelope) *problem.Problem { return nil })
		}()
		<-runCtx.Done()
		cancel()
		<-done
		if p := c.Close(context.Background()); p != nil {
			t.Fatalf("cycle %d: close failed: %v", i, p)
		}
	}
}

func TestConsumerIntegration_HeartbeatPreventsAckWaitRedelivery(t *testing.T) {
	url, cleanup := startJetStreamNATS(t)
	defer cleanup()

	pub := mustPublisher(t, url)
	defer func() { _ = pub.Close(context.Background()) }()

	const totalMessages = 2
	for i := 0; i < totalMessages; i++ {
		env := testEnvelope(10+i, fmt.Sprintf("heartbeat-%d", i), "BTCUSDT")
		if p := pub.Publish(context.Background(), env); p != nil {
			t.Fatalf("publish[%d] failed: %v", i, p)
		}
	}

	cfg := testConsumerConfig(url, "processor-w7-2-heartbeat")
	cfg.AckWait = 1 * time.Second
	cfg.MaxDeliver = 6
	cfg.FetchTimeout = 200 * time.Millisecond

	c, p := NewConsumer(context.Background(), cfg, metrics.NewBusObserver())
	if p != nil {
		t.Fatalf("new consumer failed: %v", p)
	}
	defer func() { _ = c.Close(context.Background()) }()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	var (
		mu         sync.Mutex
		seen       = make(map[string]int, totalMessages)
		duplicates atomic.Int64
		unique     atomic.Int64
		totalCalls atomic.Int64
		scheduled  atomic.Bool
	)

	done := make(chan *problem.Problem, 1)
	go func() {
		done <- c.Consume(ctx, func(_ context.Context, env envelope.Envelope) *problem.Problem {
			totalCalls.Add(1)

			mu.Lock()
			seen[env.IdempotencyKey]++
			count := seen[env.IdempotencyKey]
			if count == 1 {
				unique.Add(1)
			} else {
				duplicates.Add(1)
			}
			seenCount := unique.Load()
			mu.Unlock()

			time.Sleep(2500 * time.Millisecond)

			if seenCount >= totalMessages && scheduled.CompareAndSwap(false, true) {
				time.AfterFunc(1500*time.Millisecond, cancel)
			}
			return nil
		})
	}()

	select {
	case p := <-done:
		if p != nil {
			t.Fatalf("consume failed: %v", p)
		}
	case <-time.After(22 * time.Second):
		t.Fatal("consume timed out")
	}

	if got := unique.Load(); got != totalMessages {
		t.Fatalf("unique processed=%d want=%d", got, totalMessages)
	}
	if got := duplicates.Load(); got > 1 {
		t.Fatalf("redelivery duplicates too high: got=%d want<=1", got)
	}
	if got := totalCalls.Load(); got > totalMessages+1 {
		t.Fatalf("handler calls unexpectedly high: got=%d want<=%d", got, totalMessages+1)
	}
}

func testConsumerConfig(url, durable string) ConsumerConfig {
	return ConsumerConfig{
		URL:             url,
		StreamName:      "MARKETDATA",
		DedupWindow:     5 * time.Minute,
		MaxAge:          24 * time.Hour,
		MaxBytes:        50_000_000,
		ConsumerDurable: durable,
		FilterSubjects:  []string{"marketdata.>"},
		AckWait:         30 * time.Second,
		MaxAckPending:   1024,
		MaxDeliver:      10,
		DeliverPolicy:   "all",
		FetchTimeout:    500 * time.Millisecond,
		LagPollInterval: 200 * time.Millisecond,
	}
}

func consumeUntilCount(
	t *testing.T,
	c *Consumer,
	target int,
	handlerFactory func(currentCount int) *problem.Problem,
	timeout time.Duration,
) int {
	t.Helper()

	var consumed atomic.Int64
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	done := make(chan *problem.Problem, 1)
	go func() {
		done <- c.Consume(ctx, func(_ context.Context, _ envelope.Envelope) *problem.Problem {
			current := int(consumed.Add(1))
			p := handlerFactory(current)
			if p == nil && current >= target {
				cancel()
			}
			return p
		})
	}()

	select {
	case p := <-done:
		if p != nil {
			t.Fatalf("consume failed: %v", p)
		}
	case <-time.After(timeout + 2*time.Second):
		t.Fatal("consume timed out")
	}
	return int(consumed.Load())
}
