package marketmodel

import "math"

type PrecisionRule struct {
	PriceDecimals int
	SizeDecimals  int
}

func (r PrecisionRule) NormalizePrice(v float64) float64 {
	return roundToDecimals(v, r.PriceDecimals)
}

func (r PrecisionRule) NormalizeSize(v float64) float64 {
	return roundToDecimals(v, r.SizeDecimals)
}

func roundToDecimals(v float64, decimals int) float64 {
	if decimals < 0 {
		return v
	}
	pow := math.Pow10(decimals)
	return math.Round(v*pow) / pow
}
