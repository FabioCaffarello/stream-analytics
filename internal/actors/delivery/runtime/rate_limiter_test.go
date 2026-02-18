package deliveryruntime

import (
	"testing"
	"time"

	"github.com/market-raccoon/internal/shared/clock"
)

func TestRateLimiter_BurstThenReject(t *testing.T) {
	clk := clock.NewFakeClock(time.Unix(100, 0))
	rl := NewRateLimiter(3, 1, clk)

	if !rl.Allow() || !rl.Allow() || !rl.Allow() {
		t.Fatal("expected first 3 requests to pass")
	}
	if rl.Allow() {
		t.Fatal("expected request over burst to be rejected")
	}
}

func TestRateLimiter_RefillOverTime(t *testing.T) {
	clk := clock.NewFakeClock(time.Unix(100, 0))
	rl := NewRateLimiter(2, 2, clk)

	if !rl.Allow() || !rl.Allow() {
		t.Fatal("expected initial burst to pass")
	}
	if rl.Allow() {
		t.Fatal("expected no tokens before refill")
	}

	clk.Advance(1 * time.Second)
	if !rl.Allow() || !rl.Allow() {
		t.Fatal("expected refill tokens after 1 second")
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
