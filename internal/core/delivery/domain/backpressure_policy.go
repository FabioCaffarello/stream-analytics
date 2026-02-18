package domain

import "strings"

type BackpressurePolicy string

const (
	// BackpressureDropNewest drops incoming events when session queue is full.
	BackpressureDropNewest BackpressurePolicy = "drop_newest"
)

func NormalizeBackpressurePolicy(raw BackpressurePolicy) BackpressurePolicy {
	switch BackpressurePolicy(strings.ToLower(strings.TrimSpace(string(raw)))) {
	case BackpressureDropNewest:
		return BackpressureDropNewest
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
	default:
		return true
	}
}
