package common

import (
	"math"
)

const (
	roundShift = 1e8
	hdFactor   = 0.5
	binFactorP = 0.025
	binFactorV = 0.005
)

func RoundDown(num, tickSize float64) float64 {
	f := int64(tickSize * roundShift)
	x := int64(num*roundShift) / f * f
	return float64(x) / roundShift
}

func RoundToTick(value, tick float64) float64 {
	return math.Round(value/tick) * tick
}

func Round(n float64) float64 {
	return math.Round(n*roundShift) / roundShift
}

func Fraction(num float64) float64 {
	_, frac := math.Modf(num)
	return Round(frac)
}

func CalculateVolumeBinSize(price, tickSize float64) float64 {
	return CalculateBinSize(price, tickSize, binFactorV)
}

func CalculateHeatmapBinSize(price, tickSize float64) float64 {
	return CalculateBinSize(price, tickSize, binFactorP)
}

func CalculateBinSize(currentPrice, tickSize, grouping float64) float64 {
	var n = currentPrice * grouping / 100
	var exponent = -8
	for n > math.Pow10(exponent) {
		exponent += 1
	}
	factor := math.Pow10(exponent)
	steps := []float64{1.00, 0.50, 0.25, 0.20, 0.10, 0.05}
	match := false

	for _, step := range steps {
		if n >= step*factor {
			if Fraction((step*factor)/tickSize) > 0.0 {
				continue
			}
			n = step * factor
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
	if n < 0.00000001 {
		return 0.00000001
	}
	return Round(n)
}

func IndexOfFloats(arr []float64, value float64) int {
	for k, v := range arr {
		if Round(value) == Round(v) {
			return k
		}
	}
	return -1
}

func Normalize(size, maxSize float64) float64 {
	if maxSize == 0 {
		return 0
	}
	return size / maxSize
}

func NormalizeCurve(size, maxSize float64) float64 {
	if maxSize <= 0 {
		return 0
	}
	linear := size / maxSize
	return math.Sqrt(linear)
}

func NormalizeLog(size, maxSize float64) float64 {
	if maxSize <= 0 {
		return 0
	}
	return math.Log(size+1) / math.Log(maxSize+1)
}

func Clamp[T float32 | float64 | int | int64 | int32](val, min, max T) T {
	if val > max {
		return max
	} else if val < min {
		return min
	}
	return val
}
