package domain

import (
	"math"
	"time"
)

// ApplyConfidenceDecay applies deterministic exponential confidence decay.
//
// Decay model:
//
//	decayed = base * 0.5^(age/half_life)
//
// If halfLife <= 0, confidence is returned unchanged.
func ApplyConfidenceDecay(base float64, age, halfLife time.Duration) float64 {
	if !isFiniteFloat(base) {
		return 0
	}
	if base <= 0 {
		return 0
	}
	if base > 1 {
		base = 1
	}
	if age <= 0 || halfLife <= 0 {
		return base
	}
	factor := math.Pow(0.5, float64(age)/float64(halfLife))
	out := base * factor
	if out < 0 {
		return 0
	}
	if out > 1 {
		return 1
	}
	return out
}
