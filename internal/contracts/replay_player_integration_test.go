package contracts_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/market-raccoon/internal/contracts"
	"github.com/market-raccoon/internal/shared/clock"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/problem"
	"github.com/market-raccoon/internal/shared/replay"
)

func writeTestFixture(t *testing.T, count int) (string, []envelope.Envelope) {
	t.Helper()
	mustBootstrapPayloadRegistry(t)
	path := filepath.Join(t.TempDir(), "player-fixture.jsonl")
	envs := make([]envelope.Envelope, count)
	for i := 0; i < count; i++ {
		envs[i] = buildJSONFixtureEnvelope(t, i)
	}
	if p := replay.WriteFixtureFromEnvelopes(path, envs); p != nil {
		t.Fatalf("WriteFixtureFromEnvelopes: %v", p)
	}
	return path, envs
}

func TestPlayer_Replay_InvokesHandlerInOrder(t *testing.T) {
	path, envs := writeTestFixture(t, 5)
	fc := clock.NewFakeClock(time.Unix(0, 0))
	player, p := replay.NewPlayer(path, fc, contracts.BootstrapPayloadCodecRegistry)
	if p != nil {
		t.Fatalf("NewPlayer: %v", p)
	}

	var received []int64
	summary, p := player.Replay(context.Background(), func(_ context.Context, env envelope.Envelope) *problem.Problem {
		received = append(received, env.Seq)
		return nil
	})
	if p != nil {
		t.Fatalf("Replay: %v", p)
	}
	if summary.InputCount != 5 {
		t.Fatalf("InputCount=%d want=5", summary.InputCount)
	}
	if len(received) != 5 {
		t.Fatalf("handler calls=%d want=5", len(received))
	}
	for i, seq := range received {
		if seq != envs[i].Seq {
			t.Fatalf("received[%d].Seq=%d want=%d", i, seq, envs[i].Seq)
		}
	}
}

func TestPlayer_Replay_FakeClockAdvancement(t *testing.T) {
	path, envs := writeTestFixture(t, 3)
	fc := clock.NewFakeClock(time.Unix(0, 0))
	player, p := replay.NewPlayer(path, fc, contracts.BootstrapPayloadCodecRegistry)
	if p != nil {
		t.Fatalf("NewPlayer: %v", p)
	}

	summary, p := player.Replay(context.Background(), func(_ context.Context, _ envelope.Envelope) *problem.Problem {
		return nil
	})
	if p != nil {
		t.Fatalf("Replay: %v", p)
	}
	if summary.InputCount != 3 {
		t.Fatalf("InputCount=%d want=3", summary.InputCount)
	}
	lastTsIngest := envs[len(envs)-1].TsIngest
	expected := time.UnixMilli(lastTsIngest)
	if !fc.Now().Equal(expected) {
		t.Fatalf("clock=%v want=%v", fc.Now(), expected)
	}
}

func TestPlayer_Replay_ContextCancellation(t *testing.T) {
	path, _ := writeTestFixture(t, 5)
	fc := clock.NewFakeClock(time.Unix(0, 0))
	player, p := replay.NewPlayer(path, fc, contracts.BootstrapPayloadCodecRegistry)
	if p != nil {
		t.Fatalf("NewPlayer: %v", p)
	}

	ctx, cancel := context.WithCancel(context.Background())
	callCount := 0
	_, p = player.Replay(ctx, func(_ context.Context, _ envelope.Envelope) *problem.Problem {
		callCount++
		if callCount == 2 {
			cancel()
			return problem.New(problem.Unavailable, "context canceled")
		}
		return nil
	})
	if p == nil {
		t.Fatal("expected problem after cancellation")
	}
	if callCount != 2 {
		t.Fatalf("handler calls=%d want=2", callCount)
	}
}

func TestPlayer_Replay_HandlerError_Aborts(t *testing.T) {
	path, _ := writeTestFixture(t, 5)
	fc := clock.NewFakeClock(time.Unix(0, 0))
	player, p := replay.NewPlayer(path, fc, contracts.BootstrapPayloadCodecRegistry)
	if p != nil {
		t.Fatalf("NewPlayer: %v", p)
	}

	callCount := 0
	_, p = player.Replay(context.Background(), func(_ context.Context, _ envelope.Envelope) *problem.Problem {
		callCount++
		if callCount == 3 {
			return problem.New(problem.Internal, "handler boom")
		}
		return nil
	})
	if p == nil {
		t.Fatal("expected problem from handler error")
	}
	if callCount != 3 {
		t.Fatalf("handler calls=%d want=3 (should stop on 3rd)", callCount)
	}
}

func TestPlayer_Replay_EmptyFixture(t *testing.T) {
	mustBootstrapPayloadRegistry(t)
	path := filepath.Join(t.TempDir(), "empty-fixture.jsonl")
	if p := replay.WriteFixtureFromEnvelopes(path, nil); p != nil {
		t.Fatalf("WriteFixtureFromEnvelopes: %v", p)
	}

	fc := clock.NewFakeClock(time.Unix(0, 0))
	player, p := replay.NewPlayer(path, fc, contracts.BootstrapPayloadCodecRegistry)
	if p != nil {
		t.Fatalf("NewPlayer: %v", p)
	}

	summary, p := player.Replay(context.Background(), func(_ context.Context, _ envelope.Envelope) *problem.Problem {
		t.Fatal("handler should not be called for empty fixture")
		return nil
	})
	if p != nil {
		t.Fatalf("Replay empty: %v", p)
	}
	if summary.InputCount != 0 {
		t.Fatalf("InputCount=%d want=0", summary.InputCount)
	}
}

func TestPlayer_Replay_WithSequencer(t *testing.T) {
	path, envs := writeTestFixture(t, 3)
	fc := clock.NewFakeClock(time.Unix(0, 0))
	player, p := replay.NewPlayer(path, fc, contracts.BootstrapPayloadCodecRegistry)
	if p != nil {
		t.Fatalf("NewPlayer: %v", p)
	}

	seq := replay.NewReplaySequencer()
	player.SetReplaySequencer(seq)

	summary, p := player.Replay(context.Background(), func(_ context.Context, _ envelope.Envelope) *problem.Problem {
		return nil
	})
	if p != nil {
		t.Fatalf("Replay: %v", p)
	}
	if summary.InputCount != len(envs) {
		t.Fatalf("InputCount=%d want=%d", summary.InputCount, len(envs))
	}
}

func TestPlayer_Replay_Summary_InputCountAndSHA(t *testing.T) {
	path, _ := writeTestFixture(t, 4)
	fc := clock.NewFakeClock(time.Unix(0, 0))
	player, p := replay.NewPlayer(path, fc, contracts.BootstrapPayloadCodecRegistry)
	if p != nil {
		t.Fatalf("NewPlayer: %v", p)
	}

	summary, p := player.Replay(context.Background(), func(_ context.Context, _ envelope.Envelope) *problem.Problem {
		return nil
	})
	if p != nil {
		t.Fatalf("Replay: %v", p)
	}
	if summary.InputCount != 4 {
		t.Fatalf("InputCount=%d want=4", summary.InputCount)
	}
	if summary.InputSHA == "" {
		t.Fatal("InputSHA must not be empty")
	}
	// Verify determinism: replay again and SHA should match.
	player2, p := replay.NewPlayer(path, clock.NewFakeClock(time.Unix(0, 0)), contracts.BootstrapPayloadCodecRegistry)
	if p != nil {
		t.Fatalf("NewPlayer #2: %v", p)
	}
	summary2, p := player2.Replay(context.Background(), func(_ context.Context, _ envelope.Envelope) *problem.Problem {
		return nil
	})
	if p != nil {
		t.Fatalf("Replay #2: %v", p)
	}
	if summary.InputSHA != summary2.InputSHA {
		t.Fatalf("non-deterministic SHA: %q vs %q", summary.InputSHA, summary2.InputSHA)
	}
}
