package signal

import (
	"math"
	"sort"
	"strings"

	marketmodel "github.com/market-raccoon/internal/core/marketmodel"
)

// FeatureCombiner provides pure deterministic feature composition.
type FeatureCombiner struct{}

func (FeatureCombiner) MergeSorted(groups ...[]marketmodel.SignalFeature) []marketmodel.SignalFeature {
	flattened := make([]marketmodel.SignalFeature, 0)
	for i := range groups {
		for j := range groups[i] {
			key := strings.TrimSpace(groups[i][j].Key)
			if key == "" || !isFiniteFloat(groups[i][j].Value) {
				continue
			}
			flattened = append(flattened, marketmodel.SignalFeature{Key: key, Value: groups[i][j].Value})
		}
	}
	if len(flattened) == 0 {
		return nil
	}
	sort.SliceStable(flattened, func(i, j int) bool {
		if flattened[i].Key == flattened[j].Key {
			return flattened[i].Value < flattened[j].Value
		}
		return flattened[i].Key < flattened[j].Key
	})

	out := make([]marketmodel.SignalFeature, 0, len(flattened))
	for i := range flattened {
		if len(out) == 0 || out[len(out)-1].Key != flattened[i].Key {
			out = append(out, flattened[i])
			continue
		}
		out[len(out)-1].Value += flattened[i].Value
	}
	return out
}

func isFiniteFloat(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
