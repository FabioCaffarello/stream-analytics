//go:build integration

package jetstream

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/FabioCaffarello/stream-analytics/internal/shared/envelope"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/metrics"
	"github.com/nats-io/nats.go"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestPublisherIntegration_Publish100AndConsume(t *testing.T) {
	url, cleanup := startJetStreamNATS(t)
	defer cleanup()

	pub := mustPublisher(t, url)
	defer func() {
		_ = pub.Close(context.Background())
	}()

	for i := 0; i < 100; i++ {
		env := testEnvelope(i, fmt.Sprintf("idem-100-%d", i), "BTC-USDT")
		if p := pub.Publish(context.Background(), env); p != nil {
			t.Fatalf("publish[%d] failed: %v", i, p)
		}
	}

	count := consumeCount(t, url, "MARKETDATA", "marketdata.>", 100, 12*time.Second)
	if count != 100 {
		t.Fatalf("consumed=%d want=100", count)
	}
}

func TestPublisherIntegration_DedupByMsgID(t *testing.T) {
	url, cleanup := startJetStreamNATS(t)
	defer cleanup()

	pub := mustPublisher(t, url)
	defer func() {
		_ = pub.Close(context.Background())
	}()

	env := testEnvelope(1, "idem-dup-1", "BTC-USDT")
	if p := pub.Publish(context.Background(), env); p != nil {
		t.Fatalf("first publish failed: %v", p)
	}
	if p := pub.Publish(context.Background(), env); p != nil {
		t.Fatalf("second publish failed: %v", p)
	}

	count := consumeCount(t, url, "MARKETDATA", "marketdata.>", 2, 6*time.Second)
	if count != 1 {
		t.Fatalf("dedup failed: consumed=%d want=1", count)
	}
}

func TestPublisherIntegration_SubjectFromEnvelope(t *testing.T) {
	url, cleanup := startJetStreamNATS(t)
	defer cleanup()

	pub := mustPublisher(t, url)
	defer func() {
		_ = pub.Close(context.Background())
	}()

	env := testEnvelope(1, "idem-subject-1", "BTCUSDT")
	if p := pub.Publish(context.Background(), env); p != nil {
		t.Fatalf("publish failed: %v", p)
	}

	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("nats connect failed: %v", err)
	}
	defer nc.Close()
	js, err := nc.JetStream()
	if err != nil {
		t.Fatalf("jetstream context failed: %v", err)
	}

	sub, err := js.PullSubscribe("marketdata.>", "", nats.BindStream("MARKETDATA"))
	if err != nil {
		t.Fatalf("pull subscribe failed: %v", err)
	}
	msgs, err := sub.Fetch(1, nats.MaxWait(5*time.Second))
	if err != nil {
		t.Fatalf("fetch failed: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected one message, got %d", len(msgs))
	}
	msg := msgs[0]
	_ = msg.Ack()

	want := "marketdata.trade.v1.binance.BTCUSDT"
	if msg.Subject != want {
		t.Fatalf("subject=%q want=%q", msg.Subject, want)
	}
}

func mustPublisher(t *testing.T, url string) *Publisher {
	t.Helper()
	pub, p := NewPublisher(context.Background(), PublisherConfig{
		URL:            url,
		StreamName:     "MARKETDATA",
		DedupWindow:    5 * time.Minute,
		MaxAge:         24 * time.Hour,
		MaxBytes:       50_000_000,
		PublishTimeout: 3 * time.Second,
	}, metrics.NewBusObserver())
	if p != nil {
		t.Fatalf("new publisher failed: %v", p)
	}
	return pub
}

func testEnvelope(seq int, idem, instrument string) envelope.Envelope {
	return envelope.Envelope{
		Type:           "marketdata.trade",
		Version:        1,
		Venue:          "binance",
		Instrument:     instrument,
		TsExchange:     1_710_000_000_000 + int64(seq),
		TsIngest:       1_710_000_000_100 + int64(seq),
		Seq:            int64(seq),
		IdempotencyKey: idem,
		ContentType:    envelope.ContentTypeJSON,
		Payload:        []byte(`{"price":"50000.1"}`),
	}
}

func consumeCount(t *testing.T, url, stream, subject string, target int, timeout time.Duration) int {
	t.Helper()

	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("nats connect failed: %v", err)
	}
	defer nc.Close()
	js, err := nc.JetStream()
	if err != nil {
		t.Fatalf("jetstream context failed: %v", err)
	}

	sub, err := js.PullSubscribe(subject, "", nats.BindStream(stream))
	if err != nil {
		t.Fatalf("pull subscribe failed: %v", err)
	}

	deadline := time.Now().Add(timeout)
	count := 0
	for time.Now().Before(deadline) && count < target {
		batch := target - count
		if batch > 50 {
			batch = 50
		}
		msgs, fetchErr := sub.Fetch(batch, nats.MaxWait(400*time.Millisecond))
		if fetchErr != nil {
			if errors.Is(fetchErr, nats.ErrTimeout) {
				continue
			}
			t.Fatalf("fetch failed: %v", fetchErr)
		}
		for _, msg := range msgs {
			count++
			_ = msg.Ack()
		}
	}
	return count
}

func startJetStreamNATS(t *testing.T) (string, func()) {
	t.Helper()

	ctx := context.Background()
	req := testcontainers.ContainerRequest{
		Image:        "nats:2.10.18-alpine",
		ExposedPorts: []string{"4222/tcp"},
		Cmd:          []string{"-js"},
		WaitingFor: wait.ForLog("Server is ready").
			WithStartupTimeout(60 * time.Second),
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("start nats container failed: %v", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("resolve nats host failed: %v", err)
	}
	mappedPort, err := container.MappedPort(ctx, "4222/tcp")
	if err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("resolve nats port failed: %v", err)
	}

	url := fmt.Sprintf("nats://%s:%s", host, mappedPort.Port())
	cleanup := func() {
		_ = container.Terminate(ctx)
	}
	return url, cleanup
}
