package domain

import "math"

const (
	binRoundShift = 1e8
	binFactorV    = 0.005 // 0.5% grouping for volume profile (MM parity).
	binFactorP    = 0.025 // 2.5% grouping for heatmap (MM parity).
	binMinSize    = 0.00000001
)

// binSteps are the canonical multipliers tried in descending order.
// A step is selected when step*factor <= n and is tick-divisible.
var binSteps = [6]float64{1.00, 0.50, 0.25, 0.20, 0.10, 0.05}

// CalculateVolumeBinSize returns the volume-profile bin size for a given
// price and tick size, using 0.5% grouping (matching MarketMonkey binFactorV).
func CalculateVolumeBinSize(price, tickSize float64) float64 {
	return CalculateBinSize(price, tickSize, binFactorV)
}

// CalculateHeatmapBinSize returns the heatmap bin size for a given
// price and tick size, using 2.5% grouping (matching MarketMonkey binFactorP).
func CalculateHeatmapBinSize(price, tickSize float64) float64 {
	return CalculateBinSize(price, tickSize, binFactorP)
}

// CalculateHeatmapBinSizeWithFactor returns the heatmap bin size using an
// explicit grouping factor instead of the default 2.5%.
func CalculateHeatmapBinSizeWithFactor(price, tickSize, factor float64) float64 {
	return CalculateBinSize(price, tickSize, factor)
}

// HeatmapBinFactorForTimeframe returns a timeframe-aware grouping factor for
// heatmap bins. Short timeframes use finer bins so that the visible price
// range (typically narrow on 5s charts) contains multiple distinct levels
// instead of 1-2 solid blocks.
func HeatmapBinFactorForTimeframe(tf string) float64 {
	switch tf {
	case "1s", "5s", "10s":
		return 0.001 // 0.1% — BTC ~$90 bins
	case "1m", "5m":
		return 0.005 // 0.5% — BTC ~$450 bins
	default:
		return binFactorP // 2.5% — BTC ~$2,250 bins (unchanged)
	}
}

// CalculateBinSize computes a tick-aligned bin size for the given price and
// grouping factor. The algorithm matches MarketMonkey common.CalculateBinSize
// exactly: it finds the largest canonical step (1, 0.5, 0.25, 0.2, 0.1, 0.05)
// × power-of-10 that is <= the target (price×grouping/100) and evenly divisible
// by tickSize.
//
// Guarantees:
//   - Result is always >= tickSize (or tickSize itself as floor).
//   - Result is always a multiple of tickSize (zero fractional remainder).
//   - Result is always >= binMinSize (1e-8).
func CalculateBinSize(currentPrice, tickSize, grouping float64) float64 {
	n := currentPrice * grouping / 100

	exponent := -8
	for n > math.Pow10(exponent) {
		exponent++
	}
	factor := math.Pow10(exponent)

	match := false
	for _, step := range binSteps {
		candidate := step * factor
		if n >= candidate {
			if binFraction(candidate/tickSize) > 0.0 {
				continue
			}
			n = candidate
			match = true
			break
		}
	}
	if !match {
		n = 0.05 * factor
	}
	if n < tickSize {
		return tickSize
	}
	if n < binMinSize {
		return binMinSize
	}
	return binRound(n)
}

// binRound rounds to 8 decimal places, matching MM common.Round.
func binRound(n float64) float64 {
	return math.Round(n*binRoundShift) / binRoundShift
}

// binFraction returns the fractional part of num rounded to 8 decimal places,
// matching MM common.Fraction.
func binFraction(num float64) float64 {
	_, frac := math.Modf(num)
	return binRound(frac)
}
