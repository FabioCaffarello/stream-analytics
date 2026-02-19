//go:build integration

package jetstream

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/replay"
)

func TestReplaySourceIntegration_FullDeterministicOrder(t *testing.T) {
	url, cleanup := startJetStreamNATS(t)
	defer cleanup()

	pub := mustPublisher(t, url)
	defer func() { _ = pub.Close(context.Background()) }()

	envs := []envelope.Envelope{
		testEnvelope(3, "idem-full-3", "ETHUSDT"),
		testEnvelope(1, "idem-full-1", "BTCUSDT"),
		testEnvelope(4, "idem-full-4", "BTCUSDT"),
		testEnvelope(2, "idem-full-2", "ETHUSDT"),
		testEnvelope(5, "idem-full-5", "BTCUSDT"),
	}
	for i := range envs {
		if p := pub.Publish(context.Background(), envs[i]); p != nil {
			t.Fatalf("publish[%d] failed: %v", i, p)
		}
	}

	src, p := NewJetStreamReplaySource(ReplaySourceConfig{
		URL:              url,
		StreamName:       "MARKETDATA",
		SubjectFilter:    "marketdata.>",
		ConsumerDurable:  "processor-replay-int-full",
		DeliverPolicy:    replayDeliverAll,
		MaxMessages:      len(envs),
		FetchTimeout:     200 * time.Millisecond,
		IdleTimeoutLimit: 1,
		MergeBufferSize:  16,
		OutputBufferSize: 4,
	})
	if p != nil {
		t.Fatalf("NewJetStreamReplaySource failed: %v", p)
	}

	received, err := readAllFromSource(t, src, 8*time.Second)
	if err != nil {
		t.Fatalf("readAllFromSource failed: %v", err)
	}
	if len(received) != len(envs) {
		t.Fatalf("received=%d want=%d", len(received), len(envs))
	}

	expected := append([]envelope.Envelope(nil), envs...)
	sort.Slice(expected, func(i, j int) bool {
		return envelopeLess(expected[i], expected[j])
	})

	for i := range expected {
		if received[i].IdempotencyKey != expected[i].IdempotencyKey {
			t.Fatalf("order mismatch at %d: got=%q want=%q", i, received[i].IdempotencyKey, expected[i].IdempotencyKey)
		}
	}
}

func TestReplaySourceIntegration_WindowMode(t *testing.T) {
	url, cleanup := startJetStreamNATS(t)
	defer cleanup()

	pub := mustPublisher(t, url)
	defer func() { _ = pub.Close(context.Background()) }()

	nowMillis := time.Now().UnixMilli()
	oldEnv := testEnvelope(1, "idem-window-old", "BTCUSDT")
	oldEnv.TsIngest = nowMillis - int64((2*time.Hour)/time.Millisecond)

	newEnv := testEnvelope(2, "idem-window-new", "BTCUSDT")
	newEnv.TsIngest = nowMillis - int64((2*time.Minute)/time.Millisecond)

	for _, env := range []envelope.Envelope{oldEnv, newEnv} {
		if p := pub.Publish(context.Background(), env); p != nil {
			t.Fatalf("publish failed: %v", p)
		}
	}

	src, p := NewJetStreamReplaySource(ReplaySourceConfig{
		URL:              url,
		StreamName:       "MARKETDATA",
		SubjectFilter:    "marketdata.>",
		ConsumerDurable:  "processor-replay-int-window",
		DeliverPolicy:    replayDeliverByStartTime,
		Window:           30 * time.Minute,
		MaxMessages:      10,
		FetchTimeout:     200 * time.Millisecond,
		IdleTimeoutLimit: 1,
		MergeBufferSize:  8,
		OutputBufferSize: 2,
	})
	if p != nil {
		t.Fatalf("NewJetStreamReplaySource failed: %v", p)
	}

	received, err := readAllFromSource(t, src, 8*time.Second)
	if err != nil {
		t.Fatalf("readAllFromSource failed: %v", err)
	}
	if len(received) != 1 {
		t.Fatalf("received=%d want=1", len(received))
	}
	if received[0].IdempotencyKey != newEnv.IdempotencyKey {
		t.Fatalf("window filter mismatch: got=%q want=%q", received[0].IdempotencyKey, newEnv.IdempotencyKey)
	}
}

func TestReplaySourceIntegration_RestartNoDuplicateAcked(t *testing.T) {
	url, cleanup := startJetStreamNATS(t)
	defer cleanup()

	pub := mustPublisher(t, url)
	defer func() { _ = pub.Close(context.Background()) }()

	const total = 20
	for i := 0; i < total; i++ {
		env := testEnvelope(i+1, fmt.Sprintf("idem-restart-%02d", i), "BTCUSDT")
		if p := pub.Publish(context.Background(), env); p != nil {
			t.Fatalf("publish[%d] failed: %v", i, p)
		}
	}

	cfg := ReplaySourceConfig{
		URL:              url,
		StreamName:       "MARKETDATA",
		SubjectFilter:    "marketdata.>",
		ConsumerDurable:  "processor-replay-int-restart",
		DeliverPolicy:    replayDeliverAll,
		FetchTimeout:     200 * time.Millisecond,
		IdleTimeoutLimit: 1,
		MergeBufferSize:  16,
		OutputBufferSize: 4,
	}

	src1, p := NewJetStreamReplaySource(func() ReplaySourceConfig {
		c := cfg
		c.MaxMessages = 5
		return c
	}())
	if p != nil {
		t.Fatalf("source1 init failed: %v", p)
	}
	first, err := readAllFromSource(t, src1, 8*time.Second)
	if err != nil {
		t.Fatalf("source1 read failed: %v", err)
	}
	if len(first) != 5 {
		t.Fatalf("first run read=%d want=5", len(first))
	}

	src2, p := NewJetStreamReplaySource(func() ReplaySourceConfig {
		c := cfg
		c.MaxMessages = total
		return c
	}())
	if p != nil {
		t.Fatalf("source2 init failed: %v", p)
	}
	second, err := readAllFromSource(t, src2, 8*time.Second)
	if err != nil {
		t.Fatalf("source2 read failed: %v", err)
	}

	seen := make(map[string]struct{}, total)
	for _, env := range append(first, second...) {
		if _, ok := seen[env.IdempotencyKey]; ok {
			t.Fatalf("duplicate idempotency key across restarts: %s", env.IdempotencyKey)
		}
		seen[env.IdempotencyKey] = struct{}{}
	}
	if len(seen) != total {
		t.Fatalf("unique keys=%d want=%d", len(seen), total)
	}
}

func TestReplaySourceIntegration_StartStopCycles(t *testing.T) {
	url, cleanup := startJetStreamNATS(t)
	defer cleanup()

	for i := 0; i < 5; i++ {
		src, p := NewJetStreamReplaySource(ReplaySourceConfig{
			URL:              url,
			StreamName:       "MARKETDATA",
			SubjectFilter:    "marketdata.>",
			ConsumerDurable:  fmt.Sprintf("processor-replay-int-cycle-%d", i),
			DeliverPolicy:    replayDeliverAll,
			MaxMessages:      1,
			FetchTimeout:     150 * time.Millisecond,
			IdleTimeoutLimit: 1,
			MergeBufferSize:  4,
			OutputBufferSize: 1,
		})
		if p != nil {
			t.Fatalf("cycle %d init failed: %v", i, p)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		ch, closeFn, p := src.Read(ctx)
		if p != nil {
			cancel()
			t.Fatalf("cycle %d read init failed: %v", i, p)
		}
		for range ch {
		}
		cancel()
		if err := closeFn(); err != nil {
			t.Fatalf("cycle %d close failed: %v", i, err)
		}
	}
}

func TestReplaySourceIntegration_RecordToFixtureGolden(t *testing.T) {
	url, cleanup := startJetStreamNATS(t)
	defer cleanup()

	pub := mustPublisher(t, url)
	defer func() { _ = pub.Close(context.Background()) }()

	envs := []envelope.Envelope{
		testEnvelope(5, "idem-golden-5", "BTCUSDT"),
		testEnvelope(1, "idem-golden-1", "ETHUSDT"),
		testEnvelope(3, "idem-golden-3", "BTCUSDT"),
		testEnvelope(2, "idem-golden-2", "ETHUSDT"),
		testEnvelope(4, "idem-golden-4", "BTCUSDT"),
	}
	for i := range envs {
		if p := pub.Publish(context.Background(), envs[i]); p != nil {
			t.Fatalf("publish[%d] failed: %v", i, p)
		}
	}

	src, p := NewJetStreamReplaySource(ReplaySourceConfig{
		URL:              url,
		StreamName:       "MARKETDATA",
		SubjectFilter:    "marketdata.>",
		ConsumerDurable:  "processor-replay-int-golden",
		DeliverPolicy:    replayDeliverAll,
		MaxMessages:      len(envs),
		FetchTimeout:     200 * time.Millisecond,
		IdleTimeoutLimit: 1,
		MergeBufferSize:  16,
		OutputBufferSize: 4,
	})
	if p != nil {
		t.Fatalf("NewJetStreamReplaySource failed: %v", p)
	}

	actualPath := filepath.Join(t.TempDir(), "actual.jsonl")
	summary, p := replay.RecordFromSource(context.Background(), src, actualPath, len(envs), time.Time{})
	if p != nil {
		t.Fatalf("RecordFromSource failed: %v", p)
	}
	if summary.WrittenCount != len(envs) {
		t.Fatalf("written=%d want=%d", summary.WrittenCount, len(envs))
	}

	expected := append([]envelope.Envelope(nil), envs...)
	sort.Slice(expected, func(i, j int) bool {
		return envelopeLess(expected[i], expected[j])
	})
	expectedPath := filepath.Join(t.TempDir(), "expected.jsonl")
	if p := replay.WriteFixtureFromEnvelopes(expectedPath, expected); p != nil {
		t.Fatalf("WriteFixtureFromEnvelopes expected failed: %v", p)
	}

	if p := replay.CompareFixtureFiles(actualPath, expectedPath); p != nil {
		t.Fatalf("fixture mismatch: %v", p)
	}
}

func readAllFromSource(t *testing.T, src *Source, timeout time.Duration) ([]envelope.Envelope, error) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ch, closeFn, p := src.Read(ctx)
	if p != nil {
		return nil, p
	}

	received := make([]envelope.Envelope, 0, 1024)
	for env := range ch {
		received = append(received, env)
	}
	if err := closeFn(); err != nil {
		return nil, err
	}
	return received, nil
}
