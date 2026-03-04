package app

import (
	"math"

	"github.com/market-raccoon/internal/core/evidence/domain"
)

// RegimeDetector is a deterministic strategy for classifying market regime.
type RegimeDetector interface {
	Name() string
	Detect(key domain.RegimeStoreKey, candles []domain.RegimeCandleSample) (domain.RegimeSignal, bool)
}

// TrendPolicy controls trending/ranging classification sensitivity.
type TrendPolicy struct {
	Window             int
	MinSamples         int
	TrendSlopeMinRatio float64
	TrendMoveMinRatio  float64
}

// DefaultTrendPolicy returns production defaults.
func DefaultTrendPolicy() TrendPolicy {
	return TrendPolicy{
		Window:             20,
		MinSamples:         20,
		TrendSlopeMinRatio: 0.0015,
		TrendMoveMinRatio:  0.006,
	}
}

// TrendRegimeDetector classifies candles as trending or ranging.
type TrendRegimeDetector struct {
	policy TrendPolicy
}

// NewTrendRegimeDetector creates a trend detector with normalized policy.
func NewTrendRegimeDetector(policy TrendPolicy) *TrendRegimeDetector {
	if policy.Window <= 0 {
		policy.Window = 20
	}
	if policy.MinSamples <= 0 {
		policy.MinSamples = policy.Window
	}
	if policy.TrendSlopeMinRatio <= 0 {
		policy.TrendSlopeMinRatio = 0.0015
	}
	if policy.TrendMoveMinRatio <= 0 {
		policy.TrendMoveMinRatio = 0.006
	}
	return &TrendRegimeDetector{policy: policy}
}

func (d *TrendRegimeDetector) Name() string {
	return "trend"
}

// Detect emits a deterministic trending/ranging regime when enough samples exist.
func (d *TrendRegimeDetector) Detect(key domain.RegimeStoreKey, candles []domain.RegimeCandleSample) (domain.RegimeSignal, bool) {
	if len(candles) < d.policy.MinSamples {
		return domain.RegimeSignal{}, false
	}

	window := d.policy.Window
	if window > len(candles) {
		window = len(candles)
	}
	start := len(candles) - window
	segment := candles[start:]
	if len(segment) < d.policy.MinSamples {
		return domain.RegimeSignal{}, false
	}

	closes := make([]float64, 0, len(segment))
	mean := 0.0
	maxClose := 0.0
	minClose := math.MaxFloat64
	for i := range segment {
		closePrice := segment[i].Close
		if closePrice <= 0 || math.IsNaN(closePrice) || math.IsInf(closePrice, 0) {
			return domain.RegimeSignal{}, false
		}
		closes = append(closes, closePrice)
		mean += closePrice
		if closePrice > maxClose {
			maxClose = closePrice
		}
		if closePrice < minClose {
			minClose = closePrice
		}
	}
	mean /= float64(len(closes))
	if mean <= 0 {
		return domain.RegimeSignal{}, false
	}

	slope := linearRegressionSlope(closes)
	slopeRatio := math.Abs(slope) / mean
	startClose := closes[0]
	endClose := closes[len(closes)-1]
	netMoveRatio := math.Abs(endClose-startClose) / startClose
	rangeRatio := (maxClose - minClose) / mean

	kind := domain.RegimeRanging
	strength := rangingStrength(slopeRatio, rangeRatio, d.policy)
	if slopeRatio >= d.policy.TrendSlopeMinRatio && netMoveRatio >= d.policy.TrendMoveMinRatio {
		kind = domain.RegimeTrending
		strength = trendingStrength(slopeRatio, netMoveRatio, d.policy)
	}

	confidence := clamp01(float64(len(segment)) / float64(d.policy.Window))
	signal := domain.RegimeSignal{
		Venue:       key.Venue,
		Instrument:  key.Instrument,
		Timeframe:   key.Timeframe,
		Kind:        kind,
		Strength:    strength,
		Confidence:  confidence,
		WindowStart: segment[0].WindowStart,
		WindowEnd:   segment[len(segment)-1].WindowEnd,
		Features: []domain.FeaturePair{
			{Name: "slope_ratio", Value: slopeRatio},
			{Name: "net_move_ratio", Value: netMoveRatio},
			{Name: "range_ratio", Value: rangeRatio},
		},
	}
	if p := signal.Validate(); p != nil {
		return domain.RegimeSignal{}, false
	}
	return signal, true
}

func linearRegressionSlope(values []float64) float64 {
	n := float64(len(values))
	if n < 2 {
		return 0
	}
	sumX := 0.0
	sumY := 0.0
	sumXY := 0.0
	sumXX := 0.0
	for i := range values {
		x := float64(i)
		y := values[i]
		sumX += x
		sumY += y
		sumXY += x * y
		sumXX += x * x
	}
	den := n*sumXX - sumX*sumX
	if den == 0 {
		return 0
	}
	return (n*sumXY - sumX*sumY) / den
}

func trendingStrength(slopeRatio, netMoveRatio float64, policy TrendPolicy) float64 {
	slopeScore := clamp01(slopeRatio / (policy.TrendSlopeMinRatio * 2))
	moveScore := clamp01(netMoveRatio / (policy.TrendMoveMinRatio * 2))
	return clamp01(0.5*slopeScore + 0.5*moveScore)
}

func rangingStrength(slopeRatio, rangeRatio float64, policy TrendPolicy) float64 {
	flatness := 1 - clamp01(slopeRatio/(policy.TrendSlopeMinRatio*2))
	compression := 1 - clamp01(rangeRatio/(policy.TrendMoveMinRatio*4))
	return clamp01(0.5*flatness + 0.5*compression)
}

func clamp01(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
