package app

import "math"

// SpreadBps returns the bid-ask spread in basis points.
// Returns 0 if mid is zero or non-positive inputs.
func SpreadBps(bestBid, bestAsk float64) float64 {
	mid := (bestBid + bestAsk) / 2
	if mid <= 0 {
		return 0
	}
	return (bestAsk - bestBid) / mid * 10000
}

// SpreadAbsolute returns the raw bid-ask spread.
func SpreadAbsolute(bestBid, bestAsk float64) float64 {
	return bestAsk - bestBid
}

// MidPrice returns the mid price between best bid and best ask.
func MidPrice(bestBid, bestAsk float64) float64 {
	return (bestBid + bestAsk) / 2
}

// DepthImbalance returns (bidDepth - askDepth) / (bidDepth + askDepth).
// Returns 0 if total depth is zero.
func DepthImbalance(bidDepth, askDepth float64) float64 {
	total := bidDepth + askDepth
	if total <= 0 {
		return 0
	}
	return (bidDepth - askDepth) / total
}

// AggressorDelta returns buyVol - sellVol.
func AggressorDelta(buyVol, sellVol float64) float64 {
	return buyVol - sellVol
}

// ZScore returns the standard z-score: (value - mean) / stddev.
// Returns 0 if stddev is zero or non-finite.
func ZScore(value, mean, stddev float64) float64 {
	if stddev <= 0 || math.IsNaN(stddev) || math.IsInf(stddev, 0) {
		return 0
	}
	return (value - mean) / stddev
}
