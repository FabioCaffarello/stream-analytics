package app

import (
	"context"
	"math"
	"sort"
	"time"

	"github.com/market-raccoon/internal/core/aggregation/domain"
	"github.com/market-raccoon/internal/core/aggregation/ports"
	"github.com/market-raccoon/internal/shared/clock"
	"github.com/market-raccoon/internal/shared/ds"
	"github.com/market-raccoon/internal/shared/naming"
	"github.com/market-raccoon/internal/shared/problem"
	"github.com/market-raccoon/internal/shared/validation"
)

const (
	// oiCadenceWindowSize is the circular buffer capacity for arrival timestamps.
	oiCadenceWindowSize = 10
	// oiConfidenceHighMs is the upper bound (exclusive) for HIGH confidence cadence.
	oiConfidenceHighMs = 5000
	// oiConfidenceMediumMs is the upper bound (exclusive) for MEDIUM confidence cadence.
	oiConfidenceMediumMs = 30000
)

type openInterestKey struct {
	Venue      string
	Instrument string
}

type openInterestState struct {
	LastSeq          int64
	LastOpenInterest float64
	HasLast          bool
	ArrivalTimes     [oiCadenceWindowSize]int64
	ArrivalCount     int
	ArrivalIdx       int
}

// BuildOpenInterestConfig controls bounded state for open-interest aggregation.
type BuildOpenInterestConfig struct {
	MaxStreams int
	StreamTTL  time.Duration
	Clock      clock.Clock
}

// BuildOpenInterestRequest is one normalized open-interest input update.
type BuildOpenInterestRequest struct {
	Venue        string
	Instrument   string
	OpenInterest float64
	Seq          int64
	TsIngest     int64
	Timestamp    int64
}

// BuildOpenInterestResponse reports the emitted projection and active state cardinality.
type BuildOpenInterestResponse struct {
	Emitted       domain.OpenInterestClosed
	HasEmission   bool
	ActiveStreams int
}

// BuildOpenInterestFromEvents builds deterministic aggregation.oi projections.
type BuildOpenInterestFromEvents struct {
	publisher ports.ArtifactPublisher
	store     ports.OIHotReadModelStore
	state     *ds.BoundedMap[openInterestKey, *openInterestState]
	clock     clock.Clock
}

// NewBuildOpenInterestFromEvents constructs BuildOpenInterestFromEvents.
func NewBuildOpenInterestFromEvents(
	pub ports.ArtifactPublisher,
	store ports.OIHotReadModelStore,
	cfg BuildOpenInterestConfig,
) *BuildOpenInterestFromEvents {
	if cfg.MaxStreams <= 0 {
		cfg.MaxStreams = 50_000
	}
	if cfg.StreamTTL <= 0 {
		cfg.StreamTTL = time.Hour
	}
	if cfg.Clock == nil {
		cfg.Clock = clock.NewSystemClock()
	}
	state := ds.NewBoundedMap[openInterestKey, *openInterestState](cfg.MaxStreams, cfg.StreamTTL, cfg.Clock)
	state.SetSweepEveryOps(1024)
	state.SetSweepMinInterval(time.Second)
	return &BuildOpenInterestFromEvents{
		publisher: pub,
		store:     store,
		state:     state,
		clock:     cfg.Clock,
	}
}

// Execute applies one open-interest update and emits aggregation.oi output.
func (uc *BuildOpenInterestFromEvents) Execute(
	ctx context.Context,
	req BuildOpenInterestRequest,
) (BuildOpenInterestResponse, *problem.Problem) {
	if p := validateOpenInterestRequest(req); p != nil {
		return BuildOpenInterestResponse{}, p
	}
	key := openInterestKey{
		Venue:      naming.CanonicalVenue(req.Venue),
		Instrument: naming.CanonicalInstrument(req.Instrument),
	}
	state, ok := uc.state.Get(key)
	if !ok || state == nil {
		state = &openInterestState{}
	}
	if state.LastSeq > 0 && req.Seq <= state.LastSeq {
		return BuildOpenInterestResponse{}, problem.Newf(
			problem.OutOfOrder,
			"open_interest seq must be monotonic: got=%d last=%d",
			req.Seq,
			state.LastSeq,
		)
	}
	// Record arrival time in the circular buffer.
	nowMs := uc.clock.NowUnixMilli()
	state.ArrivalTimes[state.ArrivalIdx] = nowMs
	state.ArrivalIdx = (state.ArrivalIdx + 1) % oiCadenceWindowSize
	if state.ArrivalCount < oiCadenceWindowSize {
		state.ArrivalCount++
	}

	// Compute cadence and confidence from arrival history.
	cadenceMs := computeMedianCadence(state.ArrivalTimes[:], state.ArrivalCount)
	confidence := deriveConfidence(cadenceMs)

	window := domain.BuildOpenInterestWindowV1(
		key.Venue,
		key.Instrument,
		req.Seq,
		req.TsIngest,
		req.Timestamp,
		req.OpenInterest,
		state.LastOpenInterest,
		state.HasLast,
		cadenceMs,
		confidence,
	)
	evt := domain.OpenInterestClosed{Window: window}
	if uc.store != nil {
		if p := uc.store.SaveOI(ctx, evt); p != nil {
			return BuildOpenInterestResponse{}, p
		}
	}
	if uc.publisher != nil {
		if p := uc.publisher.PublishOpenInterest(ctx, evt); p != nil {
			return BuildOpenInterestResponse{}, p
		}
	}
	state.LastSeq = req.Seq
	state.LastOpenInterest = req.OpenInterest
	state.HasLast = true
	uc.state.Put(key, state)
	return BuildOpenInterestResponse{
		Emitted:       evt,
		HasEmission:   true,
		ActiveStreams: uc.state.Len(),
	}, nil
}

// ActiveStreams returns current bounded-map cardinality.
func (uc *BuildOpenInterestFromEvents) ActiveStreams() int {
	if uc == nil || uc.state == nil {
		return 0
	}
	return uc.state.Len()
}

func validateOpenInterestRequest(req BuildOpenInterestRequest) *problem.Problem {
	if p := validation.Collect(
		validation.NonEmptyString("venue", req.Venue),
		validation.NonEmptyString("instrument", req.Instrument),
		validation.PositiveInt("seq", req.Seq),
		validation.PositiveInt("ts_ingest", req.TsIngest),
	); p != nil {
		return p
	}
	if math.IsNaN(req.OpenInterest) || math.IsInf(req.OpenInterest, 0) || req.OpenInterest < 0 {
		return problem.New(problem.ValidationFailed, "open_interest must be a finite number >= 0")
	}
	if req.Timestamp < 0 {
		return problem.New(problem.ValidationFailed, "timestamp must be >= 0")
	}
	return nil
}

// computeMedianCadence computes the median inter-arrival time from a circular buffer
// of arrival timestamps. Returns 0 if fewer than 2 arrivals are recorded.
func computeMedianCadence(times []int64, count int) int64 {
	if count < 2 {
		return 0
	}

	// Collect the recorded timestamps and sort them to get chronological order.
	sorted := make([]int64, count)
	copy(sorted, times[:count])
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	// Compute inter-arrival deltas.
	deltas := make([]int64, 0, count-1)
	for i := 1; i < len(sorted); i++ {
		d := sorted[i] - sorted[i-1]
		if d > 0 {
			deltas = append(deltas, d)
		}
	}
	if len(deltas) == 0 {
		return 0
	}

	// Return median delta.
	sort.Slice(deltas, func(i, j int) bool { return deltas[i] < deltas[j] })
	mid := len(deltas) / 2
	if len(deltas)%2 == 0 {
		return (deltas[mid-1] + deltas[mid]) / 2
	}
	return deltas[mid]
}

// deriveConfidence maps a cadence interval to a confidence level string.
func deriveConfidence(cadenceMs int64) string {
	if cadenceMs <= 0 {
		return ""
	}
	if cadenceMs < oiConfidenceHighMs {
		return "high"
	}
	if cadenceMs < oiConfidenceMediumMs {
		return "medium"
	}
	return "low"
}
