package domain

import (
	"math"
	"strings"

	"github.com/market-raccoon/internal/shared/problem"
)

const (
	RegimeEvidenceType    = "evidence.regime_evidence"
	RegimeEvidenceVersion = 1
)

// RegimeKind classifies market regime signals derived from candles.
type RegimeKind string

const (
	RegimeTrending       RegimeKind = "trending"
	RegimeRanging        RegimeKind = "ranging"
	RegimeBreakout       RegimeKind = "breakout"
	RegimeHighVolatility RegimeKind = "high_volatility"
	RegimeLowVolatility  RegimeKind = "low_volatility"
)

var validRegimeKinds = map[RegimeKind]struct{}{
	RegimeTrending:       {},
	RegimeRanging:        {},
	RegimeBreakout:       {},
	RegimeHighVolatility: {},
	RegimeLowVolatility:  {},
}

// FeaturePair is one deterministic numeric feature backing regime classification.
type FeaturePair struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
}

// RegimeSignal is the canonical domain output for regime detection.
type RegimeSignal struct {
	Venue       string        `json:"venue"`
	Instrument  string        `json:"instrument"`
	Timeframe   string        `json:"timeframe"`
	Kind        RegimeKind    `json:"kind"`
	Strength    float64       `json:"strength"`
	Confidence  float64       `json:"confidence"`
	WindowStart int64         `json:"window_start_ms"`
	WindowEnd   int64         `json:"window_end_ms"`
	Features    []FeaturePair `json:"features"`
}

// Validate checks regime signal invariants.
func (r RegimeSignal) Validate() *problem.Problem {
	if _, ok := validRegimeKinds[r.Kind]; !ok {
		return problem.New(problem.ValidationFailed, "regime kind must be a recognized value")
	}
	if strings.TrimSpace(r.Venue) == "" {
		return problem.New(problem.ValidationFailed, "regime venue must not be empty")
	}
	if strings.TrimSpace(r.Instrument) == "" {
		return problem.New(problem.ValidationFailed, "regime instrument must not be empty")
	}
	if strings.TrimSpace(r.Timeframe) == "" {
		return problem.New(problem.ValidationFailed, "regime timeframe must not be empty")
	}
	if r.WindowStart <= 0 || r.WindowEnd <= 0 || r.WindowEnd <= r.WindowStart {
		return problem.New(problem.ValidationFailed, "regime window must satisfy 0 < start < end")
	}
	if !isRegimeUnitInterval(r.Strength) {
		return problem.New(problem.ValidationFailed, "regime strength must be in [0,1]")
	}
	if !isRegimeUnitInterval(r.Confidence) {
		return problem.New(problem.ValidationFailed, "regime confidence must be in [0,1]")
	}
	if len(r.Features) == 0 {
		return problem.New(problem.ValidationFailed, "regime features must not be empty")
	}
	for _, f := range r.Features {
		if strings.TrimSpace(f.Name) == "" {
			return problem.New(problem.ValidationFailed, "regime feature name must not be empty")
		}
		if math.IsNaN(f.Value) || math.IsInf(f.Value, 0) {
			return problem.New(problem.ValidationFailed, "regime feature values must be finite")
		}
	}
	return nil
}

func isRegimeUnitInterval(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 && v <= 1
}
