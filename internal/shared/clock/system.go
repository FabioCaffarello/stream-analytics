package clock

import "time"

// SystemClock delegates to the real wall clock.
type SystemClock struct{}

// NewSystemClock returns a production-ready Clock.
func NewSystemClock() Clock { return SystemClock{} }

// Now returns the current wall-clock time.
func (SystemClock) Now() time.Time { return time.Now() }

// NowUnixMilli returns the current wall-clock time in Unix milliseconds.
func (SystemClock) NowUnixMilli() int64 { return time.Now().UnixMilli() }
