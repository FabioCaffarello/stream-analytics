package app

// DepthTotal returns aggregate top-N depth.
func DepthTotal(bidDepth, askDepth float64) float64 {
	return bidDepth + askDepth
}

// VolumeRatio returns total/mean with deterministic zero guards.
func VolumeRatio(total, mean float64) float64 {
	if mean <= 0 {
		return 0
	}
	return total / mean
}
