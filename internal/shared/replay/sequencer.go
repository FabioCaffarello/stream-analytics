package replay

import (
	"strings"
	"sync"

	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/naming"
	"github.com/market-raccoon/internal/shared/problem"
)

const replayDefaultMarketType = "SPOT"

// ReplaySequencer is a deterministic sequencer backed by fixture-provided seq values.
//
// Player.Enqueue() feeds seq values in fixture order; Next() consumes them using
// the same key shape expected by the ingest pipeline (venue + instrument_key).
type ReplaySequencer struct {
	mu         sync.Mutex
	queued     map[string][]int64
	lastQueued map[string]int64
}

func NewReplaySequencer() *ReplaySequencer {
	return &ReplaySequencer{
		queued:     make(map[string][]int64, 256),
		lastQueued: make(map[string]int64, 256),
	}
}

// Enqueue appends one fixture sequence value for deterministic replay.
func (s *ReplaySequencer) Enqueue(env envelope.Envelope) *problem.Problem {
	if s == nil {
		return problem.New(problem.ValidationFailed, "replay sequencer must not be nil")
	}

	key := replaySequencerKeyFromEnvelope(env)
	seq := env.Seq

	s.mu.Lock()
	defer s.mu.Unlock()

	if prev, ok := s.lastQueued[key]; ok && seq <= prev {
		return problem.WithDetail(
			problem.Newf(problem.ValidationFailed, "non-monotonic fixture sequence for stream=%q: prev=%d current=%d", key, prev, seq),
			"stream", key,
		)
	}

	s.lastQueued[key] = seq
	s.queued[key] = append(s.queued[key], seq)
	return nil
}

// Next returns the next fixture sequence for (venue, instrument_key).
func (s *ReplaySequencer) Next(venue, instrumentKey string) (int64, *problem.Problem) {
	if s == nil {
		return 0, problem.New(problem.ValidationFailed, "replay sequencer must not be nil")
	}

	key := replaySequencerKey(venue, instrumentKey)

	s.mu.Lock()
	defer s.mu.Unlock()

	queue := s.queued[key]
	if len(queue) == 0 {
		return 0, problem.WithDetail(
			problem.Newf(problem.ValidationFailed, "replay sequence not queued for stream=%q", key),
			"stream", key,
		)
	}

	next := queue[0]
	if len(queue) == 1 {
		delete(s.queued, key)
	} else {
		s.queued[key] = queue[1:]
	}
	return next, nil
}

func replaySequencerKeyFromEnvelope(env envelope.Envelope) string {
	marketType := replayMarketTypeFromMeta(env.Meta)
	instrumentKey := naming.CanonicalInstrument(env.Instrument) + ":" + marketType
	return replaySequencerKey(env.Venue, instrumentKey)
}

func replaySequencerKey(venue, instrumentKey string) string {
	return naming.CanonicalVenue(venue) + "|" + strings.ToUpper(strings.TrimSpace(instrumentKey))
}

func replayMarketTypeFromMeta(meta map[string]string) string {
	if len(meta) == 0 {
		return replayDefaultMarketType
	}
	raw := strings.ToUpper(strings.TrimSpace(meta["instrument_market_type"]))
	if raw == "" {
		return replayDefaultMarketType
	}
	return raw
}
