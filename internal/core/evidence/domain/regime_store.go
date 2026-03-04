package domain

import (
	"strings"

	"github.com/market-raccoon/internal/shared/problem"
	"github.com/market-raccoon/internal/shared/validation"
)

// RegimeStoreKey partitions regime state by venue/instrument/timeframe.
type RegimeStoreKey struct {
	Venue      string
	Instrument string
	Timeframe  string
}

// RegimeCandleSample is one closed candle sample used for regime detection.
type RegimeCandleSample struct {
	TsServer    int64
	WindowStart int64
	WindowEnd   int64
	Open        float64
	High        float64
	Low         float64
	Close       float64
	Volume      float64
}

// RegimeStorePolicy defines bounded storage limits for regime detection.
type RegimeStorePolicy struct {
	MaxStreams       int
	HistoryCap       int
	CandleHistoryCap int
}

// NewRegimeStorePolicy validates policy values.
func NewRegimeStorePolicy(maxStreams, historyCap int) (RegimeStorePolicy, *problem.Problem) {
	if maxStreams <= 0 {
		return RegimeStorePolicy{}, problem.New(problem.ValidationFailed, "regime store max_streams must be > 0")
	}
	if historyCap <= 0 {
		return RegimeStorePolicy{}, problem.New(problem.ValidationFailed, "regime store history_cap must be > 0")
	}
	return RegimeStorePolicy{
		MaxStreams:       maxStreams,
		HistoryCap:       historyCap,
		CandleHistoryCap: historyCap,
	}, nil
}

type regimeStreamState struct {
	candles []RegimeCandleSample
	regimes []RegimeSignal
}

// RegimeStore is a deterministic bounded store for candle/regime histories.
type RegimeStore struct {
	policy RegimeStorePolicy
	order  []RegimeStoreKey
	state  map[RegimeStoreKey]*regimeStreamState
}

// NewRegimeStore creates an empty store with deterministic eviction order.
func NewRegimeStore(policy RegimeStorePolicy) *RegimeStore {
	return &RegimeStore{
		policy: policy,
		order:  make([]RegimeStoreKey, 0, policy.MaxStreams),
		state:  make(map[RegimeStoreKey]*regimeStreamState, policy.MaxStreams),
	}
}

// PutCandle appends a closed candle sample with bounded history.
func (s *RegimeStore) PutCandle(key RegimeStoreKey, sample RegimeCandleSample) *problem.Problem {
	if s == nil {
		return problem.New(problem.ValidationFailed, "regime store is nil")
	}
	if p := validateRegimeKey(key); p != nil {
		return p
	}
	if p := validateCandleSample(sample); p != nil {
		return p
	}

	st := s.getOrCreateState(key)
	st.candles = appendBoundedCandle(st.candles, sample, s.policy.CandleHistoryCap)
	return nil
}

// PutRegime appends a regime signal with bounded history.
func (s *RegimeStore) PutRegime(key RegimeStoreKey, signal RegimeSignal) *problem.Problem {
	if s == nil {
		return problem.New(problem.ValidationFailed, "regime store is nil")
	}
	if p := validateRegimeKey(key); p != nil {
		return p
	}
	if p := signal.Validate(); p != nil {
		return p
	}

	st := s.getOrCreateState(key)
	st.regimes = appendBoundedRegime(st.regimes, signal, s.policy.HistoryCap)
	return nil
}

// Candles returns a copy of candle history in chronological order.
func (s *RegimeStore) Candles(key RegimeStoreKey) []RegimeCandleSample {
	if s == nil {
		return nil
	}
	st, ok := s.state[key]
	if !ok || len(st.candles) == 0 {
		return nil
	}
	out := make([]RegimeCandleSample, len(st.candles))
	copy(out, st.candles)
	return out
}

// LastRegime returns the latest regime signal for a key.
func (s *RegimeStore) LastRegime(key RegimeStoreKey) (RegimeSignal, bool) {
	if s == nil {
		return RegimeSignal{}, false
	}
	st, ok := s.state[key]
	if !ok || len(st.regimes) == 0 {
		return RegimeSignal{}, false
	}
	return st.regimes[len(st.regimes)-1], true
}

// RegimeHistory returns a copy of regime history in chronological order.
func (s *RegimeStore) RegimeHistory(key RegimeStoreKey) []RegimeSignal {
	if s == nil {
		return nil
	}
	st, ok := s.state[key]
	if !ok || len(st.regimes) == 0 {
		return nil
	}
	out := make([]RegimeSignal, len(st.regimes))
	copy(out, st.regimes)
	return out
}

// StreamCount returns current active stream count.
func (s *RegimeStore) StreamCount() int {
	if s == nil {
		return 0
	}
	return len(s.state)
}

func (s *RegimeStore) getOrCreateState(key RegimeStoreKey) *regimeStreamState {
	if st, ok := s.state[key]; ok {
		return st
	}
	if len(s.state) >= s.policy.MaxStreams {
		s.evictOldestStream()
	}
	st := &regimeStreamState{
		candles: make([]RegimeCandleSample, 0, s.policy.CandleHistoryCap),
		regimes: make([]RegimeSignal, 0, s.policy.HistoryCap),
	}
	s.state[key] = st
	s.order = append(s.order, key)
	return st
}

func (s *RegimeStore) evictOldestStream() {
	if len(s.order) == 0 {
		return
	}
	evicted := s.order[0]
	s.order = s.order[1:]
	delete(s.state, evicted)
}

func appendBoundedCandle(history []RegimeCandleSample, sample RegimeCandleSample, cap int) []RegimeCandleSample {
	if cap <= 0 {
		return history
	}
	if len(history) < cap {
		return append(history, sample)
	}
	copy(history, history[1:])
	history[len(history)-1] = sample
	return history
}

func appendBoundedRegime(history []RegimeSignal, signal RegimeSignal, cap int) []RegimeSignal {
	if cap <= 0 {
		return history
	}
	if len(history) < cap {
		return append(history, signal)
	}
	copy(history, history[1:])
	history[len(history)-1] = signal
	return history
}

func validateRegimeKey(key RegimeStoreKey) *problem.Problem {
	return validation.Collect(
		validation.NonEmptyString("venue", strings.TrimSpace(key.Venue)),
		validation.NonEmptyString("instrument", strings.TrimSpace(key.Instrument)),
		validation.NonEmptyString("timeframe", strings.TrimSpace(key.Timeframe)),
	)
}

func validateCandleSample(sample RegimeCandleSample) *problem.Problem {
	if sample.WindowStart <= 0 || sample.WindowEnd <= sample.WindowStart {
		return problem.New(problem.ValidationFailed, "regime candle sample window must satisfy 0 < start < end")
	}
	if sample.Close <= 0 || sample.High <= 0 || sample.Low <= 0 {
		return problem.New(problem.ValidationFailed, "regime candle sample prices must be > 0")
	}
	return nil
}
