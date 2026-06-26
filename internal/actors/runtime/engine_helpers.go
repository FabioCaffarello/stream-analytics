package runtime

import (
	"context"
	"log/slog"

	"github.com/anthdm/hollywood/actor"
)

// NewDefaultEngine creates a Hollywood actor engine with default configuration.
func NewDefaultEngine() (*actor.Engine, error) {
	return actor.NewEngine(actor.NewEngineConfig())
}

// SpawnGuardian spawns a Guardian actor with the canonical "guardian" name and
// ID.  All stream-analytics binaries use this convention.
func SpawnGuardian(e *actor.Engine, cfg GuardianConfig) *actor.PID {
	return e.Spawn(NewGuardian(cfg), "guardian", actor.WithID("guardian"))
}

// ShutdownGuardian performs a graceful guardian shutdown: sends Stop, then
// waits for the actor tree to drain via Poison.  If the context expires
// before the tree stops, a warning is logged.
func ShutdownGuardian(ctx context.Context, e *actor.Engine, guardianPID *actor.PID, logger *slog.Logger) {
	e.Send(guardianPID, Stop{})
	select {
	case <-e.Poison(guardianPID).Done():
	case <-ctx.Done():
		logger.Warn("guardian did not stop in time")
	}
}
