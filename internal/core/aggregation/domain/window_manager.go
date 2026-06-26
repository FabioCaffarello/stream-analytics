package domain

import "github.com/FabioCaffarello/stream-analytics/internal/shared/problem"

// WindowKey identifies one event-time window partition.
type WindowKey struct {
	Venue      string
	Instrument string
	Timeframe  string
}

// ForcedWindowClose declares one forced-closure request emitted by WindowManager.
type ForcedWindowClose struct {
	Key         WindowKey
	WindowStart int64
}

// WindowDecision is the deterministic lifecycle decision for one incoming event.
type WindowDecision struct {
	WindowStart         int64
	ShouldCloseCurrent  bool
	PreviousWindowStart int64
	IsLate              bool
	ForcedClose         *ForcedWindowClose
}

// WindowManager centralizes deterministic event-time lifecycle decisions.
type WindowManager interface {
	Observe(key WindowKey, eventTsMs int64, windowDurationMs int64) (WindowDecision, *problem.Problem)
	ActiveWindows() int
}
