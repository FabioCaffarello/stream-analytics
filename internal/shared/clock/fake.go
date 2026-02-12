package clock

import (
	"sync"
	"time"
)

// FakeClock is a deterministic clock for use in tests.
// The zero value starts at the Unix epoch; call Set or Advance before use.
type FakeClock struct {
	mu      sync.Mutex
	current time.Time
}

// NewFakeClock creates a FakeClock set to t.
func NewFakeClock(t time.Time) *FakeClock {
	return &FakeClock{current: t}
}

// Set moves the clock to an absolute time.
func (c *FakeClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current = t
}

// Advance moves the clock forward by d. d must be non-negative.
func (c *FakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current = c.current.Add(d)
}

// Now returns the current fake time.
func (c *FakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.current
}

// NowUnixMilli returns the current fake time as Unix milliseconds.
func (c *FakeClock) NowUnixMilli() int64 {
	return c.Now().UnixMilli()
}
