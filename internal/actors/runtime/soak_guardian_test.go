//go:build soak

package runtime

import (
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
	"github.com/anthdm/hollywood/actor"
)

func TestSoak_Guardian_CrashRestart_500(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping soak test in short mode")
	}
	if os.Getenv("MR_ENABLE_SOAK") != "1" {
		t.Skip("set MR_ENABLE_SOAK=1 to run soak tests")
	}

	clock := &fakeClock{now: time.Unix(2_000, 0)}
	policy, prob := NewSupervisorPolicy(SupervisorConfig{
		BaseBackoff:   time.Millisecond,
		MaxBackoff:    time.Millisecond,
		Jitter:        0,
		RestartWindow: time.Hour,
		RestartLimit:  5_000,
		Cooldown:      time.Millisecond,
	}, clock, fixedRNG{value: 0.5})
	if prob != nil {
		t.Fatalf("new policy: %v", prob)
	}

	g := newGuardianForTest(policy, clock)
	spawnCount := 0
	g.spawnFn = func(c *actor.Context, subsystem Subsystem) (*actor.PID, *problem.Problem) {
		spawnCount++
		return actor.NewPID("local", fmt.Sprintf("%s-%d", subsystem, spawnCount)), nil
	}
	g.emitFn = func(c *actor.Context, msg any) {}
	g.scheduleFn = func(delay time.Duration, fn func()) cancelSchedule {
		fn()
		return func() {}
	}
	g.sendToSelfFn = func(pid *actor.PID, msg any) {
		if retry, ok := msg.(retrySubsystem); ok {
			g.retrySubsystem(nil, retry.Subsystem, retry.Generation)
		}
	}

	g.startSubsystem(nil, SubsystemMarketData)

	const crashes = 500
	for i := 0; i < crashes; i++ {
		g.handleChildFailed(nil, ChildFailed{
			Subsystem: SubsystemMarketData,
			Kind:      "panic",
			Err:       errors.New("boom"),
		})
		clock.now = clock.now.Add(time.Millisecond)
	}

	if got, want := spawnCount, crashes+1; got != want {
		t.Fatalf("spawn count mismatch: got=%d want=%d", got, want)
	}
	if !g.running[SubsystemMarketData] {
		t.Fatal("marketdata should be running after repeated restarts")
	}
	if policy.Status(SubsystemMarketData).Degraded {
		t.Fatal("marketdata should not be degraded with high restart limit")
	}
}
