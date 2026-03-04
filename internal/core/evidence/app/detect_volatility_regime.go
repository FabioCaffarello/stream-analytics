package app

import (
	"math"

	"github.com/market-raccoon/internal/core/evidence/domain"
)

// VolatilityPolicy controls ATR-based volatility thresholds.
type VolatilityPolicy struct {
	Window       int
	MinSamples   int
	HighATRRatio float64
	LowATRRatio  float64
}

// DefaultVolatilityPolicy returns production defaults.
func DefaultVolatilityPolicy() VolatilityPolicy {
	return VolatilityPolicy{
		Window:       14,
		MinSamples:   15, // ATR uses previous close.
		HighATRRatio: 0.015,
		LowATRRatio:  0.004,
	}
}

// VolatilityRegimeDetector classifies high/low volatility from ATR ratio.
type VolatilityRegimeDetector struct {
	policy VolatilityPolicy
}

// NewVolatilityRegimeDetector creates a volatility detector with normalized policy.
func NewVolatilityRegimeDetector(policy VolatilityPolicy) *VolatilityRegimeDetector {
	if policy.Window <= 1 {
		policy.Window = 14
	}
	if policy.MinSamples <= 1 {
		policy.MinSamples = policy.Window + 1
	}
	if policy.HighATRRatio <= 0 {
		policy.HighATRRatio = 0.015
	}
	if policy.LowATRRatio <= 0 {
		policy.LowATRRatio = 0.004
	}
	if policy.LowATRRatio >= policy.HighATRRatio {
		policy.LowATRRatio = policy.HighATRRatio * 0.5
	}
	return &VolatilityRegimeDetector{policy: policy}
}

func (d *VolatilityRegimeDetector) Name() string {
	return "volatility"
}

// Detect emits high/low volatility regime when ATR ratio crosses thresholds.
//
//nolint:gocyclo // Threshold and guard branches are explicit to preserve deterministic classification behavior.
func (d *VolatilityRegimeDetector) Detect(key domain.RegimeStoreKey, candles []domain.RegimeCandleSample) (domain.RegimeSignal, bool) {
	if len(candles) < d.policy.MinSamples {
		return domain.RegimeSignal{}, false
	}

	end := len(candles) - 1
	window := d.policy.Window
	if window > end {
		window = end
	}
	if window <= 0 {
		return domain.RegimeSignal{}, false
	}

	start := len(candles) - window - 1
	if start < 0 {
		start = 0
	}
	segment := candles[start:]
	if len(segment) < window+1 {
		return domain.RegimeSignal{}, false
	}

	trSum := 0.0
	for i := 1; i < len(segment); i++ {
		curr := segment[i]
		prevClose := segment[i-1].Close
		if curr.High <= 0 || curr.Low <= 0 || prevClose <= 0 || curr.Close <= 0 {
			return domain.RegimeSignal{}, false
		}
		if curr.High < curr.Low {
			return domain.RegimeSignal{}, false
		}
		tr := trueRange(curr.High, curr.Low, prevClose)
		trSum += tr
	}

	atr := trSum / float64(window)
	last := segment[len(segment)-1]
	atrRatio := atr / last.Close

	kind := domain.RegimeKind("")
	strength := 0.0
	if atrRatio >= d.policy.HighATRRatio {
		kind = domain.RegimeHighVolatility
		strength = clamp01((atrRatio-d.policy.HighATRRatio)/d.policy.HighATRRatio + 0.5)
	} else if atrRatio <= d.policy.LowATRRatio {
		kind = domain.RegimeLowVolatility
		strength = clamp01((d.policy.LowATRRatio-atrRatio)/d.policy.LowATRRatio + 0.5)
	}
	if kind == "" {
		return domain.RegimeSignal{}, false
	}

	confidence := clamp01(float64(window) / float64(d.policy.Window))
	signal := domain.RegimeSignal{
		Venue:       key.Venue,
		Instrument:  key.Instrument,
		Timeframe:   key.Timeframe,
		Kind:        kind,
		Strength:    strength,
		Confidence:  confidence,
		WindowStart: segment[0].WindowStart,
		WindowEnd:   last.WindowEnd,
		Features: []domain.FeaturePair{
			{Name: "atr", Value: atr},
			{Name: "atr_ratio", Value: atrRatio},
			{Name: "high_threshold", Value: d.policy.HighATRRatio},
			{Name: "low_threshold", Value: d.policy.LowATRRatio},
		},
	}
	if p := signal.Validate(); p != nil {
		return domain.RegimeSignal{}, false
	}
	return signal, true
}

func trueRange(high, low, prevClose float64) float64 {
	base := high - low
	upGap := math.Abs(high - prevClose)
	downGap := math.Abs(low - prevClose)
	return maxFloat(base, maxFloat(upGap, downGap))
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
