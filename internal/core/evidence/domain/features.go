package domain

import "sort"

// SortedFeatures returns a deterministic copy sorted by key asc.
func SortedFeatures(in []EvidenceFeature) []EvidenceFeature {
	if len(in) == 0 {
		return nil
	}
	out := make([]EvidenceFeature, len(in))
	copy(out, in)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Key < out[j].Key
	})
	dedup := out[:0]
	for i := range out {
		if i > 0 && out[i].Key == out[i-1].Key {
			continue
		}
		dedup = append(dedup, out[i])
	}
	return dedup
}

// FeaturesFromMap builds deterministic features from a map.
func FeaturesFromMap(m map[string]float64) []EvidenceFeature {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]EvidenceFeature, 0, len(keys))
	for _, k := range keys {
		out = append(out, EvidenceFeature{Key: k, Value: m[k]})
	}
	return out
}
