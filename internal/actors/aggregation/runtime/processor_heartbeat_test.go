package aggruntime

import (
	"testing"
	"time"
)

func TestShouldEmitHeartbeat(t *testing.T) {
	now := time.Unix(100, 0)
	interval := 20 * time.Second

	if !shouldEmitHeartbeat(now, time.Time{}, false, interval) {
		t.Fatal("first timer heartbeat should emit when last is zero")
	}

	if shouldEmitHeartbeat(now, now.Add(-10*time.Second), false, interval) {
		t.Fatal("timer heartbeat should not emit before interval")
	}

	if !shouldEmitHeartbeat(now, now.Add(-20*time.Second), false, interval) {
		t.Fatal("timer heartbeat should emit at interval boundary")
	}

	if !shouldEmitHeartbeat(now, now.Add(-1*time.Second), true, interval) {
		t.Fatal("forced heartbeat should always emit")
	}

	if !shouldEmitHeartbeat(now, now, false, 0) {
		t.Fatal("non-positive interval should emit")
	}
}
