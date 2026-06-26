package deliveryruntime

import (
	"time"

	"github.com/FabioCaffarello/stream-analytics/internal/shared/clock"
)

// RateLimitConfig configures per-session read-path limiting.
type RateLimitConfig struct {
	Enabled       bool
	MaxPerSecond  int
	BurstCapacity int
}

// RateLimiter is a simple token bucket limiter.
type RateLimiter struct {
	tokens     int
	maxTokens  int
	refillRate int // tokens per second
	lastRefill time.Time
	clock      clock.Clock
}

func NewRateLimiter(maxTokens, refillRate int, clk clock.Clock) *RateLimiter {
	if clk == nil {
		clk = clock.NewSystemClock()
	}
	if maxTokens <= 0 {
		maxTokens = 1
	}
	if refillRate <= 0 {
		refillRate = 1
	}
	now := clk.Now()
	return &RateLimiter{
		tokens:     maxTokens,
		maxTokens:  maxTokens,
		refillRate: refillRate,
		lastRefill: now,
		clock:      clk,
	}
}

func (r *RateLimiter) Allow() bool {
	if r == nil {
		return true
	}
	now := r.clock.Now()
	elapsed := now.Sub(r.lastRefill)
	refill := int(elapsed.Seconds()) * r.refillRate
	if refill > 0 {
		r.tokens = minInt(r.tokens+refill, r.maxTokens)
		r.lastRefill = now
	}
	if r.tokens <= 0 {
		return false
	}
	r.tokens--
	return true
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
