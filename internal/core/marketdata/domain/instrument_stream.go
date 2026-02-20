package domain

import (
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/hash"
	"github.com/market-raccoon/internal/shared/problem"
)

// StreamState is the health state for an InstrumentStream.
type StreamState string

const (
	// StreamHealthy indicates that no out-of-order/duplicate anomalies were observed.
	StreamHealthy StreamState = "HEALTHY"
	// StreamNeedsAttention indicates that anomalies were observed and monitoring should react.
	StreamNeedsAttention StreamState = "NEEDS_ATTENTION"
)

// StreamHealth carries observable counters for an InstrumentStream.
// It is a read-only snapshot — not part of aggregate identity.
type StreamHealth struct {
	LastSeq         Sequence
	OutOfOrderCount int
	DuplicateCount  int
	// IsHealthy is true when no out-of-order or duplicate events have been observed.
	IsHealthy bool
	State     StreamState
}

// StreamID uniquely identifies a stream as (venue, instrument, market_type).
type StreamID struct {
	Venue      VenueID
	Instrument InstrumentID
	MarketType MarketType
}

// SequencerInstrumentKey returns the key suffix used by sequencers to isolate
// sequences per market type while preserving canonical instrument in envelopes.
func (s StreamID) SequencerInstrumentKey() string {
	return s.Instrument.String() + ":" + s.MarketType.String()
}

// InstrumentStream is the aggregate for a single (venue, instrument, market_type) event stream.
//
// Invariants:
//   - Sequence is strictly monotonic (each new event must have seq > last).
//   - Events are deduplicated by IdempotencyKey (bounded FIFO cache, size from DedupWindow).
//   - Venue and Instrument are always in canonical form.
type InstrumentStream struct {
	id              StreamID
	dedupWindow     DedupWindow
	lastSeq         Sequence
	seen            map[IdempotencyKey]struct{}
	seenOrd         []IdempotencyKey
	outOfOrderCount int
	duplicateCount  int
}

// NewInstrumentStream creates an InstrumentStream with market type defaulted to SPOT.
// Venue and instrument are accepted in raw form and normalized internally.
func NewInstrumentStream(rawVenue, rawInstrument string, dedupWindow DedupWindow) (*InstrumentStream, *problem.Problem) {
	return NewInstrumentStreamWithMarketType(rawVenue, rawInstrument, MarketTypeSpot.String(), dedupWindow)
}

// NewInstrumentStreamWithMarketType creates an InstrumentStream with explicit market type.
func NewInstrumentStreamWithMarketType(
	rawVenue, rawInstrument, rawMarketType string,
	dedupWindow DedupWindow,
) (*InstrumentStream, *problem.Problem) {
	venue, p := NewVenueID(rawVenue)
	if p != nil {
		return nil, p
	}
	instrument, p := NewInstrumentID(rawInstrument)
	if p != nil {
		return nil, p
	}
	if rawMarketType == "" {
		rawMarketType = MarketTypeSpot.String()
	}
	marketType, p := NewMarketType(rawMarketType)
	if p != nil {
		return nil, p
	}
	if dedupWindow.Size() <= 0 {
		return nil, problem.New(problem.ValidationFailed, "dedup_window must be > 0")
	}
	cap := dedupWindow.Size()
	return &InstrumentStream{
		id:          StreamID{Venue: venue, Instrument: instrument, MarketType: marketType},
		dedupWindow: dedupWindow,
		seen:        make(map[IdempotencyKey]struct{}, cap),
	}, nil
}

// ID returns the stream identity.
func (s *InstrumentStream) ID() StreamID { return s.id }

// Health returns a snapshot of the stream's observable health counters.
func (s *InstrumentStream) Health() StreamHealth {
	state := StreamHealthy
	if s.outOfOrderCount > 0 || s.duplicateCount > 0 {
		state = StreamNeedsAttention
	}
	return StreamHealth{
		LastSeq:         s.lastSeq,
		OutOfOrderCount: s.outOfOrderCount,
		DuplicateCount:  s.duplicateCount,
		IsHealthy:       state == StreamHealthy,
		State:           state,
	}
}

// BuildEnvelope validates the input, assigns seq and ts_ingest, and returns
// a fully-formed Envelope ready for publishing.
//
// Returns MD_OUT_OF_ORDER if seq <= lastSeq.
// Returns MD_DUPLICATE if idempotency key was already seen.
func (s *InstrumentStream) BuildEnvelope(
	eventType EventType,
	version SchemaVersion,
	tsExchange Timestamp,
	tsIngest Timestamp,
	seq Sequence,
	contentType string,
	payload []byte,
	sourceIdempotencyKey string,
) (envelope.Envelope, *problem.Problem) {
	// 1. Validate sequence monotonicity.
	if seq <= s.lastSeq && s.lastSeq > 0 {
		s.outOfOrderCount++
		return envelope.Envelope{}, problem.WithDetail(
			problem.WithDetail(
				problem.Newf(problem.OutOfOrder,
					"seq %d is not greater than last seq %d for %s/%s",
					seq, s.lastSeq, s.id.Venue, s.id.Instrument),
				"seq", seq,
			),
			"last_seq", s.lastSeq,
		)
	}

	// 2. Resolve idempotency key (source-provided first, deterministic fallback).
	ikey, p := resolveIdempotencyKey(s.id.Venue, s.id.Instrument, eventType, seq, sourceIdempotencyKey)
	if p != nil {
		return envelope.Envelope{}, p
	}

	// 3. Check dedup.
	if _, dup := s.seen[ikey]; dup {
		s.duplicateCount++
		return envelope.Envelope{}, problem.WithDetail(
			problem.Newf(problem.Duplicate,
				"duplicate event for key %s", ikey),
			"idempotency_key", string(ikey),
		)
	}

	// 4. Build envelope.
	env := envelope.Envelope{
		Type:           eventType.String(),
		Version:        int(version),
		Venue:          s.id.Venue.String(),
		Instrument:     s.id.Instrument.String(),
		TsExchange:     tsExchange.UnixMilli(),
		TsIngest:       tsIngest.UnixMilli(),
		Seq:            seq.Int64(),
		IdempotencyKey: string(ikey),
		ContentType:    contentType,
		Payload:        payload,
	}

	// 5. Validate envelope invariants (always before committing state).
	if vp := env.Validate(); vp != nil {
		return envelope.Envelope{}, vp
	}

	// 6. Commit state (only after all checks pass).
	s.lastSeq = seq
	s.recordSeen(ikey)

	return env, nil
}

// buildIdempotencyKey constructs a stable, deterministic idempotency key via FNV-1a.
func buildIdempotencyKey(venue VenueID, instrument InstrumentID, eventType EventType, seq Sequence) IdempotencyKey {
	raw := hash.HashFieldsFast(
		venue.String(),
		instrument.String(),
		eventType.String(),
		seqToString(seq),
	)
	return IdempotencyKey(raw)
}

func resolveIdempotencyKey(
	venue VenueID,
	instrument InstrumentID,
	eventType EventType,
	seq Sequence,
	source string,
) (IdempotencyKey, *problem.Problem) {
	if source == "" {
		return buildIdempotencyKey(venue, instrument, eventType, seq), nil
	}
	return NewIdempotencyKey(source)
}

func seqToString(s Sequence) string {
	n := s.Int64()
	if n == 0 {
		return "0"
	}
	buf := [20]byte{}
	pos := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

// recordSeen adds ikey to the dedup cache, evicting oldest entry if at capacity.
func (s *InstrumentStream) recordSeen(ikey IdempotencyKey) {
	maxSize := s.dedupWindow.Size()
	if len(s.seenOrd) >= maxSize {
		oldest := s.seenOrd[0]
		s.seenOrd = s.seenOrd[1:]
		delete(s.seen, oldest)

		// Compact: when the backing array is more than 2x the live slice,
		// copy into a fresh allocation to release the unused prefix.
		if cap(s.seenOrd) > 2*len(s.seenOrd)+1 {
			compacted := make([]IdempotencyKey, len(s.seenOrd), maxSize)
			copy(compacted, s.seenOrd)
			s.seenOrd = compacted
		}
	}
	s.seen[ikey] = struct{}{}
	s.seenOrd = append(s.seenOrd, ikey)
}
