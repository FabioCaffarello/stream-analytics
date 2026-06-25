package replay

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/market-raccoon/internal/shared/clock"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/problem"
)

// ReplaySummary captures deterministic replay execution output.
type ReplaySummary struct {
	InputCount int
	InputSHA   string
}

// BootstrapFn is a function that initialises the payload codec registry.
// Callers supply contracts.BootstrapPayloadCodecRegistry (or a test stub).
type BootstrapFn func() *problem.Problem

// Player replays fixture envelopes deterministically through a handler.
type Player struct {
	path        string
	clock       *clock.FakeClock
	sequencer   *ReplaySequencer
	bootstrapFn BootstrapFn
}

// NewPlayer creates a streaming replay player for the fixture path.
// bootstrapFn is called once before replaying to ensure the codec registry is
// initialised. Pass contracts.BootstrapPayloadCodecRegistry in production.
func NewPlayer(path string, fakeClock *clock.FakeClock, bootstrapFn BootstrapFn) (*Player, *problem.Problem) {
	if strings.TrimSpace(path) == "" {
		return nil, problem.WithDetail(
			problem.New(problem.ValidationFailed, "fixture path must not be empty"),
			"field", "path",
		)
	}
	if bootstrapFn == nil {
		return nil, problem.New(problem.ValidationFailed, "bootstrapFn must not be nil")
	}
	return &Player{path: path, clock: fakeClock, bootstrapFn: bootstrapFn}, nil
}

// Replay executes records in order, validating sequence and payload decode invariants.
func (p *Player) Replay(ctx context.Context, handler func(context.Context, envelope.Envelope) *problem.Problem) (ReplaySummary, *problem.Problem) {
	if p == nil {
		return ReplaySummary{}, problem.New(problem.ValidationFailed, "player must not be nil")
	}
	if handler == nil {
		return ReplaySummary{}, problem.New(problem.ValidationFailed, "replay handler must not be nil")
	}
	if p.bootstrapFn != nil {
		if pp := p.bootstrapFn(); pp != nil {
			return ReplaySummary{}, pp
		}
	}

	r, pp := NewReader(p.path)
	if pp != nil {
		return ReplaySummary{}, pp
	}
	defer func() {
		_ = r.Close()
	}()

	lastSeqByStream := make(map[string]int64, 256)
	sum := sha256.New()
	count := 0

	for {
		rec, ok, pp := r.Next()
		if pp != nil {
			return ReplaySummary{}, annotateReplayIndex(pp, count)
		}
		if !ok {
			break
		}
		env := rec.Envelope

		if p.clock != nil {
			p.clock.Set(time.UnixMilli(env.TsIngest))
		}
		if p.sequencer != nil {
			if pp := p.sequencer.Enqueue(env); pp != nil {
				return ReplaySummary{}, annotateReplayIndex(pp, count)
			}
		}
		if pp := validateMonotonicSeq(lastSeqByStream, env); pp != nil {
			return ReplaySummary{}, annotateReplayIndex(pp, count)
		}
		if _, pp := codec.DecodePayload(env.Type, env.Version, env.ContentType, env.Payload); pp != nil {
			return ReplaySummary{}, annotateReplayIndex(pp, count)
		}
		if pp := handler(ctx, env); pp != nil {
			return ReplaySummary{}, annotateReplayIndex(pp, count)
		}
		_, _ = sum.Write([]byte(strings.ToLower(strings.TrimSpace(rec.SHA256))))
		_, _ = sum.Write([]byte{'\n'})
		count++
	}

	return ReplaySummary{
		InputCount: count,
		InputSHA:   hex.EncodeToString(sum.Sum(nil)),
	}, nil
}

// SetReplaySequencer injects a fixture-backed sequencer for replayed handlers.
func (p *Player) SetReplaySequencer(seq *ReplaySequencer) {
	if p == nil {
		return
	}
	p.sequencer = seq
}

func validateMonotonicSeq(lastSeqByStream map[string]int64, env envelope.Envelope) *problem.Problem {
	stream := replayStreamKey(env)
	if prev, ok := lastSeqByStream[stream]; ok && env.Seq <= prev {
		return problem.WithDetail(
			problem.Newf(problem.ValidationFailed, "non-monotonic sequence for stream=%q: prev=%d current=%d", stream, prev, env.Seq),
			"stream", stream,
		)
	}
	lastSeqByStream[stream] = env.Seq
	return nil
}

func replayStreamKey(env envelope.Envelope) string {
	return replaySequencerKeyFromEnvelope(env)
}

func annotateReplayIndex(p *problem.Problem, index int) *problem.Problem {
	if p == nil {
		return nil
	}
	return problem.WithDetail(p, "index", index)
}

// CapturePublisher stores all published envelopes in replay order.
type CapturePublisher struct {
	mu        sync.Mutex
	envelopes []envelope.Envelope
}

// Publish captures env in-memory.
func (c *CapturePublisher) Publish(_ context.Context, env envelope.Envelope) *problem.Problem {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.envelopes = append(c.envelopes, env)
	return nil
}

// Envelopes returns a stable snapshot copy of captured envelopes.
func (c *CapturePublisher) Envelopes() []envelope.Envelope {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]envelope.Envelope, len(c.envelopes))
	copy(out, c.envelopes)
	return out
}

// WriteFixtureFromEnvelopes writes envelopes to a JSONL replay fixture file.
func WriteFixtureFromEnvelopes(path string, envs []envelope.Envelope) *problem.Problem {
	if strings.TrimSpace(path) == "" {
		return problem.WithDetail(problem.New(problem.ValidationFailed, "path must not be empty"), "field", "path")
	}
	w, p := NewWriter(path)
	if p != nil {
		return p
	}
	defer func() {
		_ = w.Close()
	}()

	for i := range envs {
		if p := w.Append(envs[i]); p != nil {
			return annotateReplayIndex(p, i)
		}
	}
	return w.Close()
}

// CompareFixtureFiles compares two fixture files byte-for-byte.
func CompareFixtureFiles(actualPath, expectedPath string) *problem.Problem {
	// #nosec G304 -- fixture paths are explicit test/runtime inputs.
	actual, err := os.ReadFile(actualPath)
	if err != nil {
		return problem.Wrap(err, problem.Internal, "read actual fixture failed")
	}
	// #nosec G304 -- fixture paths are explicit test/runtime inputs.
	expected, err := os.ReadFile(expectedPath)
	if err != nil {
		return problem.Wrap(err, problem.Internal, "read expected fixture failed")
	}
	if string(actual) == string(expected) {
		return nil
	}
	return problem.WithDetail(
		problem.New(problem.ValidationFailed, "fixture mismatch"),
		"actual", filepath.Base(actualPath),
	)
}
