package replay

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/market-raccoon/internal/shared/clock"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/contracts"
	"github.com/market-raccoon/internal/shared/envelope"
	sharedhash "github.com/market-raccoon/internal/shared/hash"
	"github.com/market-raccoon/internal/shared/problem"
)

// ReplaySummary captures deterministic replay execution output.
type ReplaySummary struct {
	InputCount int
	InputSHA   string
}

// Player replays fixture envelopes deterministically through a handler.
type Player struct {
	records []FixtureRecord
	clock   *clock.FakeClock
}

// NewPlayer loads and validates all fixture records from path.
func NewPlayer(path string, fakeClock *clock.FakeClock) (*Player, *problem.Problem) {
	r, p := NewReader(path)
	if p != nil {
		return nil, p
	}
	defer func() {
		_ = r.Close()
	}()

	records := make([]FixtureRecord, 0, 1024)
	for {
		rec, ok, p := r.Next()
		if p != nil {
			return nil, p
		}
		if !ok {
			break
		}
		records = append(records, rec)
	}

	return &Player{records: records, clock: fakeClock}, nil
}

// Replay executes records in order, validating sequence and payload decode invariants.
func (p *Player) Replay(ctx context.Context, handler func(context.Context, envelope.Envelope) *problem.Problem) (ReplaySummary, *problem.Problem) {
	if p == nil {
		return ReplaySummary{}, problem.New(problem.ValidationFailed, "player must not be nil")
	}
	if handler == nil {
		return ReplaySummary{}, problem.New(problem.ValidationFailed, "replay handler must not be nil")
	}
	if pp := contracts.BootstrapPayloadCodecRegistry(); pp != nil {
		return ReplaySummary{}, pp
	}

	lastSeqByStream := make(map[string]int64, 256)
	inputHashes := make([]string, 0, len(p.records))

	for i := range p.records {
		rec := p.records[i]
		env := rec.Envelope

		if p.clock != nil {
			p.clock.Set(time.UnixMilli(env.TsIngest))
		}
		if pp := validateMonotonicSeq(lastSeqByStream, env); pp != nil {
			return ReplaySummary{}, annotateReplayIndex(pp, i)
		}
		if _, pp := codec.DecodePayload(env.Type, env.Version, env.ContentType, env.Payload); pp != nil {
			return ReplaySummary{}, annotateReplayIndex(pp, i)
		}
		if pp := handler(ctx, env); pp != nil {
			return ReplaySummary{}, annotateReplayIndex(pp, i)
		}
		inputHashes = append(inputHashes, rec.SHA256)
	}

	return ReplaySummary{
		InputCount: len(p.records),
		InputSHA:   sharedhash.HashFields(inputHashes...),
	}, nil
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
	return strings.TrimSpace(env.Venue) + "|" + strings.TrimSpace(env.Instrument) + "|" + strings.TrimSpace(env.Type)
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
