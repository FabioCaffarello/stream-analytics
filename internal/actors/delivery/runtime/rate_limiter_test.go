package deliveryruntime

import (
	"testing"
	"time"

	"github.com/FabioCaffarello/stream-analytics/internal/shared/clock"
)

func TestRateLimiter_BurstThenReject(t *testing.T) {
	clk := clock.NewFakeClock(time.Unix(100, 0))
	rl := NewRateLimiter(3, 1, clk)

	for i := 0; i < 3; i++ {
		if !rl.Allow() {
			t.Fatalf("expected allow for initial burst at i=%d", i)
		}
	}
	if rl.Allow() {
		t.Fatal("expected request over burst to be rejected")
	}
}

func TestRateLimiter_RefillOverTime(t *testing.T) {
	clk := clock.NewFakeClock(time.Unix(100, 0))
	rl := NewRateLimiter(2, 2, clk)

	for i := 0; i < 2; i++ {
		if !rl.Allow() {
			t.Fatalf("expected initial burst allow at i=%d", i)
		}
	}
	if rl.Allow() {
		t.Fatal("expected no tokens before refill")
	}

	clk.Advance(1 * time.Second)
	for i := 0; i < 2; i++ {
		if !rl.Allow() {
			t.Fatalf("expected refill allow after 1 second at i=%d", i)
		}
	}
	if rl.Allow() {
		t.Fatal("expected burst cap to be enforced")
	}
}

func TestRateLimiter_RefillCappedByBurst(t *testing.T) {
	clk := clock.NewFakeClock(time.Unix(100, 0))
	rl := NewRateLimiter(5, 5, clk)
	for i := 0; i < 5; i++ {
		if !rl.Allow() {
			t.Fatalf("expected allow at i=%d", i)
		}
	}
	clk.Advance(10 * time.Second)
	for i := 0; i < 5; i++ {
		if !rl.Allow() {
			t.Fatalf("expected allow after refill at i=%d", i)
		}
	}
	if rl.Allow() {
		t.Fatal("expected tokens to cap at burst capacity")
	}
}
