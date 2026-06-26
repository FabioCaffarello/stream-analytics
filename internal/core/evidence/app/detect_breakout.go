package app

import (
	"math"

	"github.com/FabioCaffarello/stream-analytics/internal/core/evidence/domain"
)

// BreakoutPolicy controls breakout detection thresholds.
type BreakoutPolicy struct {
	Window              int
	MinSamples          int
	MinPriceMoveRatio   float64
	MinVolumeSpikeRatio float64
}

// DefaultBreakoutPolicy returns production defaults.
func DefaultBreakoutPolicy() BreakoutPolicy {
	return BreakoutPolicy{
		Window:              20,
		MinSamples:          21,
		MinPriceMoveRatio:   0.0075,
		MinVolumeSpikeRatio: 1.8,
	}
}

// BreakoutRegimeDetector detects breakout regimes from price move + volume spike.
type BreakoutRegimeDetector struct {
	policy BreakoutPolicy
}

// NewBreakoutRegimeDetector creates a breakout detector with normalized policy.
func NewBreakoutRegimeDetector(policy BreakoutPolicy) *BreakoutRegimeDetector {
	if policy.Window <= 1 {
		policy.Window = 20
	}
	if policy.MinSamples <= 1 {
		policy.MinSamples = policy.Window + 1
	}
	if policy.MinPriceMoveRatio <= 0 {
		policy.MinPriceMoveRatio = 0.0075
	}
	if policy.MinVolumeSpikeRatio <= 0 {
		policy.MinVolumeSpikeRatio = 1.8
	}
	return &BreakoutRegimeDetector{policy: policy}
}

func (d *BreakoutRegimeDetector) Name() string {
	return "breakout"
}

// Detect emits breakout regime when both price and volume thresholds are crossed.
func (d *BreakoutRegimeDetector) Detect(key domain.RegimeStoreKey, candles []domain.RegimeCandleSample) (domain.RegimeSignal, bool) {
	if len(candles) < d.policy.MinSamples {
		return domain.RegimeSignal{}, false
	}

	window := d.policy.Window
	if window >= len(candles) {
		window = len(candles) - 1
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

	last := segment[len(segment)-1]
	prev := segment[:len(segment)-1]
	prevClose := prev[len(prev)-1].Close
	if prevClose <= 0 || last.Close <= 0 {
		return domain.RegimeSignal{}, false
	}
	priceMoveRatio := math.Abs(last.Close-prevClose) / prevClose

	volumeSum := 0.0
	for i := range prev {
		if prev[i].Volume < 0 {
			return domain.RegimeSignal{}, false
		}
		volumeSum += prev[i].Volume
	}
	meanVolume := volumeSum / float64(len(prev))
	if meanVolume <= 0 {
		return domain.RegimeSignal{}, false
	}
	volumeSpikeRatio := last.Volume / meanVolume

	if priceMoveRatio < d.policy.MinPriceMoveRatio || volumeSpikeRatio < d.policy.MinVolumeSpikeRatio {
		return domain.RegimeSignal{}, false
	}

	moveScore := clamp01(priceMoveRatio / (d.policy.MinPriceMoveRatio * 2))
	volumeScore := clamp01(volumeSpikeRatio / (d.policy.MinVolumeSpikeRatio * 2))
	strength := clamp01(0.5*moveScore + 0.5*volumeScore)
	confidence := clamp01(float64(window) / float64(d.policy.Window))

	signal := domain.RegimeSignal{
		Venue:       key.Venue,
		Instrument:  key.Instrument,
		Timeframe:   key.Timeframe,
		Kind:        domain.RegimeBreakout,
		Strength:    strength,
		Confidence:  confidence,
		WindowStart: segment[0].WindowStart,
		WindowEnd:   last.WindowEnd,
		Features: []domain.FeaturePair{
			{Name: "price_move_ratio", Value: priceMoveRatio},
			{Name: "volume_spike_ratio", Value: volumeSpikeRatio},
			{Name: "mean_volume", Value: meanVolume},
		},
	}
	if p := signal.Validate(); p != nil {
		return domain.RegimeSignal{}, false
	}
	return signal, true
}
