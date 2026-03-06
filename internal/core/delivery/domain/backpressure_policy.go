package domain

import "strings"

type BackpressurePolicy string

const (
	// BackpressureDropNewest drops incoming events when session queue is full.
	BackpressureDropNewest BackpressurePolicy = "drop_newest"
	// BackpressureDropOldest evicts the oldest queued event when queue is full.
	BackpressureDropOldest BackpressurePolicy = "drop_oldest"
	// BackpressurePriorityDrop evicts the lowest-priority queued event first.
	BackpressurePriorityDrop BackpressurePolicy = "priority_drop"
)

func NormalizeBackpressurePolicy(raw BackpressurePolicy) BackpressurePolicy {
	switch BackpressurePolicy(strings.ToLower(strings.TrimSpace(string(raw)))) {
	case BackpressureDropNewest, BackpressureDropOldest, BackpressurePriorityDrop:
		return BackpressurePolicy(strings.ToLower(strings.TrimSpace(string(raw))))
	default:
		return BackpressureDropNewest
	}
}

func ShouldDropOnBackpressure(policy BackpressurePolicy, queueLen, queueCap int) bool {
	if queueCap <= 0 {
		return false
	}
	if queueLen < queueCap {
		return false
	}
	switch NormalizeBackpressurePolicy(policy) {
	case BackpressureDropNewest:
		return true
	case BackpressureDropOldest:
		return false
	case BackpressurePriorityDrop:
		return false
	default:
		return true
	}
}

// DefaultBackpressurePriorities returns deterministic per-event priority.
func DefaultBackpressurePriorities() map[string]int {
	return map[string]int{
		"marketdata.trade":                     100,
		"aggregation.candle":                   90,
		"aggregation.stats":                    80,
		"aggregation.bar_stats":                78,
		"aggregation.delta_volume":             76,
		"aggregation.cvd":                      76,
		"aggregation.oi":                       72,
		"marketdata.bookdelta":                 20,
		"insights.heatmap_snapshot":            55,
		"marketdata.markprice":                 50,
		"marketdata.open_interest":             48,
		"marketdata.liquidation":               40,
		"insights.crossvenue.trade_snapshot":   30,
		"insights.crossvenue.spread_signal":    20,
		"insights.microstructure_evidence":     25,
		"liquidity.evidence":                   25,
		"signal.composite":                     65,
		"insights.volume_profile_snapshot.v1":  20,
		"insights.volume_profile_buckets.v1":   20,
		"insights.volume_profile_compact.v1":   20,
		"insights.volume_profile_overload.v1":  20,
		"insights.volume_profile_integrity.v1": 20,
	}
}
