// Package federation provides composite readers that route queries across
// hot (Timescale) and cold (ClickHouse) storage tiers with time-based routing.
package federation

import "time"

// Config controls federation routing behavior.
type Config struct {
	// HotWindowMs defines how far back (in milliseconds) the hot store
	// is considered authoritative. Queries entirely before
	// (now - HotWindowMs) are routed to cold only.
	// Default: 24 hours (86_400_000 ms).
	HotWindowMs int64
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{HotWindowMs: 24 * 60 * 60 * 1000} // 24h
}

// routing determines which tiers to query for a given time range.
type routing int

const (
	routeColdOnly routing = iota
	routeHotOnly
	routeBoth
)

// route decides which tiers to query based on the request time range
// and the hot window boundary.
func route(fromMs, toMs, hotWindowMs int64, nowFn func() int64) routing {
	nowMs := nowFn()
	hotBoundary := nowMs - hotWindowMs
	if hotBoundary < 0 {
		hotBoundary = 0
	}
	if toMs <= hotBoundary {
		return routeColdOnly
	}
	if fromMs >= hotBoundary {
		return routeHotOnly
	}
	return routeBoth
}

// systemNowMs returns current time in milliseconds.
func systemNowMs() int64 {
	return time.Now().UnixMilli()
}

// mergeByWindowStart merges two slices sorted by window_start ASC,
// deduplicating by the key returned by keyFn. On duplicate, hotItem wins
// (hot slice is iterated first in the merge, so its entry is kept).
func mergeByWindowStart[T any](hot, cold []T, wsFn func(T) int64) []T {
	if len(hot) == 0 {
		return cold
	}
	if len(cold) == 0 {
		return hot
	}

	out := make([]T, 0, len(hot)+len(cold))
	seen := make(map[int64]struct{}, len(hot)+len(cold))

	hi, ci := 0, 0
	for hi < len(hot) && ci < len(cold) {
		hws := wsFn(hot[hi])
		cws := wsFn(cold[ci])
		if hws <= cws {
			if _, ok := seen[hws]; !ok {
				seen[hws] = struct{}{}
				out = append(out, hot[hi])
			}
			hi++
			// skip cold duplicate at same timestamp
			if hws == cws {
				ci++
			}
		} else {
			if _, ok := seen[cws]; !ok {
				seen[cws] = struct{}{}
				out = append(out, cold[ci])
			}
			ci++
		}
	}
	for ; hi < len(hot); hi++ {
		ws := wsFn(hot[hi])
		if _, ok := seen[ws]; !ok {
			seen[ws] = struct{}{}
			out = append(out, hot[hi])
		}
	}
	for ; ci < len(cold); ci++ {
		ws := wsFn(cold[ci])
		if _, ok := seen[ws]; !ok {
			seen[ws] = struct{}{}
			out = append(out, cold[ci])
		}
	}
	return out
}

// mergeTimestamps merges two sorted int64 slices, deduplicating.
func mergeTimestamps(hot, cold []int64) []int64 {
	if len(hot) == 0 {
		return cold
	}
	if len(cold) == 0 {
		return hot
	}

	out := make([]int64, 0, len(hot)+len(cold))
	hi, ci := 0, 0
	for hi < len(hot) && ci < len(cold) {
		if hot[hi] < cold[ci] {
			out = append(out, hot[hi])
			hi++
		} else if hot[hi] > cold[ci] {
			out = append(out, cold[ci])
			ci++
		} else {
			out = append(out, hot[hi])
			hi++
			ci++
		}
	}
	out = append(out, hot[hi:]...)
	out = append(out, cold[ci:]...)
	return out
}

// capSlice returns at most limit elements from s.
func capSlice[T any](s []T, limit int) []T {
	if limit > 0 && len(s) > limit {
		return s[:limit]
	}
	return s
}
