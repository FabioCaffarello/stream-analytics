package app

import (
	"strings"

	signalsdomain "github.com/market-raccoon/internal/core/signals/domain"
)

// RateLimitPolicy controls bounded dedup and rate-limiting state.
type RateLimitPolicy struct {
	DedupWindowMs      int64
	DedupCapPerKey     int
	RateLimitPerMin    int
	GlobalRateLimitMin int
	MaxKeys            int
}

// DefaultRateLimitPolicy returns production defaults.
func DefaultRateLimitPolicy() RateLimitPolicy {
	return RateLimitPolicy{
		DedupWindowMs:      30_000,
		DedupCapPerKey:     50,
		RateLimitPerMin:    10,
		GlobalRateLimitMin: 100,
		MaxKeys:            1024,
	}
}

// RateDecision reports deterministic admission outcome.
type RateDecision struct {
	Allowed      bool
	Deduplicated bool
	RateLimited  bool
}

type dedupRecord struct {
	ts         int64
	confidence float64
}

// SignalRateLimiter applies rule 4 (dedup) and rule 5 (rate limit).
type SignalRateLimiter struct {
	policy RateLimitPolicy

	dedupState map[string][]dedupRecord
	dedupOrder []string

	rateState map[string][]int64
	rateOrder []string

	globalRate []int64
}

// NewSignalRateLimiter creates a deterministic limiter.
func NewSignalRateLimiter(policy RateLimitPolicy) *SignalRateLimiter {
	if policy.DedupWindowMs <= 0 {
		policy.DedupWindowMs = 30_000
	}
	if policy.DedupCapPerKey <= 0 {
		policy.DedupCapPerKey = 50
	}
	if policy.RateLimitPerMin <= 0 {
		policy.RateLimitPerMin = 10
	}
	if policy.GlobalRateLimitMin <= 0 {
		policy.GlobalRateLimitMin = 100
	}
	if policy.MaxKeys <= 0 {
		policy.MaxKeys = 1024
	}
	return &SignalRateLimiter{
		policy:     policy,
		dedupState: make(map[string][]dedupRecord, policy.MaxKeys),
		dedupOrder: make([]string, 0, policy.MaxKeys),
		rateState:  make(map[string][]int64, policy.MaxKeys),
		rateOrder:  make([]string, 0, policy.MaxKeys),
		globalRate: make([]int64, 0, policy.GlobalRateLimitMin),
	}
}

// Allow enforces dedup/rate rules using signal ts_server as deterministic clock.
func (l *SignalRateLimiter) Allow(signal signalsdomain.CompositeSignalV1) RateDecision {
	ts := signal.TsServer
	if ts <= 0 {
		return RateDecision{}
	}

	dedupKey := dedupKey(signal)
	rateKey := rateKey(signal)

	dedupWindow := l.getDedupWindow(dedupKey)
	dedupWindow = pruneDedup(dedupWindow, ts-l.policy.DedupWindowMs)
	if len(dedupWindow) > 0 {
		last := dedupWindow[len(dedupWindow)-1]
		if ts-last.ts <= l.policy.DedupWindowMs && signal.Confidence <= last.confidence*1.2 {
			l.dedupState[dedupKey] = dedupWindow
			return RateDecision{Deduplicated: true}
		}
	}

	rateWindow := l.getRateWindow(rateKey)
	rateWindow = pruneRate(rateWindow, ts-60_000)
	globalWindow := pruneRate(l.globalRate, ts-60_000)

	if len(rateWindow) >= l.policy.RateLimitPerMin || len(globalWindow) >= l.policy.GlobalRateLimitMin {
		l.rateState[rateKey] = rateWindow
		l.globalRate = globalWindow
		return RateDecision{RateLimited: true}
	}

	rateWindow = append(rateWindow, ts)
	globalWindow = append(globalWindow, ts)
	dedupWindow = append(dedupWindow, dedupRecord{ts: ts, confidence: signal.Confidence})
	if len(dedupWindow) > l.policy.DedupCapPerKey {
		copy(dedupWindow, dedupWindow[1:])
		dedupWindow = dedupWindow[:l.policy.DedupCapPerKey]
	}

	l.rateState[rateKey] = rateWindow
	l.globalRate = globalWindow
	l.dedupState[dedupKey] = dedupWindow
	return RateDecision{Allowed: true}
}

func (l *SignalRateLimiter) getDedupWindow(key string) []dedupRecord {
	if window, ok := l.dedupState[key]; ok {
		return window
	}
	if len(l.dedupState) >= l.policy.MaxKeys {
		l.evictOldestDedupKey()
	}
	l.dedupState[key] = nil
	l.dedupOrder = append(l.dedupOrder, key)
	return nil
}

func (l *SignalRateLimiter) getRateWindow(key string) []int64 {
	if window, ok := l.rateState[key]; ok {
		return window
	}
	if len(l.rateState) >= l.policy.MaxKeys {
		l.evictOldestRateKey()
	}
	l.rateState[key] = nil
	l.rateOrder = append(l.rateOrder, key)
	return nil
}

func (l *SignalRateLimiter) evictOldestDedupKey() {
	if len(l.dedupOrder) == 0 {
		return
	}
	victim := l.dedupOrder[0]
	l.dedupOrder = l.dedupOrder[1:]
	delete(l.dedupState, victim)
}

func (l *SignalRateLimiter) evictOldestRateKey() {
	if len(l.rateOrder) == 0 {
		return
	}
	victim := l.rateOrder[0]
	l.rateOrder = l.rateOrder[1:]
	delete(l.rateState, victim)
}

func dedupKey(signal signalsdomain.CompositeSignalV1) string {
	return strings.ToLower(strings.TrimSpace(signal.Kind)) + "|" +
		strings.ToLower(strings.TrimSpace(signal.Venue)) + "|" +
		strings.ToUpper(strings.TrimSpace(signal.Instrument)) + "|" +
		strings.ToLower(strings.TrimSpace(signal.Timeframe))
}

func rateKey(signal signalsdomain.CompositeSignalV1) string {
	return strings.ToLower(strings.TrimSpace(signal.Venue)) + "|" +
		strings.ToUpper(strings.TrimSpace(signal.Instrument))
}

func pruneDedup(window []dedupRecord, minTs int64) []dedupRecord {
	if len(window) == 0 {
		return nil
	}
	idx := 0
	for idx < len(window) && window[idx].ts < minTs {
		idx++
	}
	if idx == 0 {
		return window
	}
	if idx >= len(window) {
		return nil
	}
	return window[idx:]
}

func pruneRate(window []int64, minTs int64) []int64 {
	if len(window) == 0 {
		return nil
	}
	idx := 0
	for idx < len(window) && window[idx] < minTs {
		idx++
	}
	if idx == 0 {
		return window
	}
	if idx >= len(window) {
		return nil
	}
	return window[idx:]
}
