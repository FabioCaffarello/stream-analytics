package clock_test

import (
	"testing"
	"time"

	"github.com/FabioCaffarello/stream-analytics/internal/shared/clock"
)

func TestSystemClock(t *testing.T) {
	c := clock.NewSystemClock()
	now := c.Now()
	if now.IsZero() {
		t.Error("SystemClock.Now() must not be zero")
	}
	ms := c.NowUnixMilli()
	if ms <= 0 {
		t.Errorf("NowUnixMilli must be positive, got %d", ms)
	}
}

func TestFakeClock_Set(t *testing.T) {
	fixed := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	c := clock.NewFakeClock(fixed)

	if got := c.Now(); !got.Equal(fixed) {
		t.Errorf("Now() = %v; want %v", got, fixed)
	}
	if got := c.NowUnixMilli(); got != fixed.UnixMilli() {
		t.Errorf("NowUnixMilli() = %d; want %d", got, fixed.UnixMilli())
	}
}

func TestFakeClock_Advance(t *testing.T) {
	base := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	c := clock.NewFakeClock(base)

	c.Advance(5 * time.Second)
	want := base.Add(5 * time.Second)
	if got := c.Now(); !got.Equal(want) {
		t.Errorf("after Advance, Now() = %v; want %v", got, want)
	}

	// Multiple advances should accumulate.
	c.Advance(time.Minute)
	want = want.Add(time.Minute)
	if got := c.Now(); !got.Equal(want) {
		t.Errorf("after second Advance, Now() = %v; want %v", got, want)
	}
}

func TestFakeClock_ImplementsInterface(t *testing.T) {
	var _ clock.Clock = (*clock.FakeClock)(nil)
	var _ clock.Clock = clock.SystemClock{}
}
