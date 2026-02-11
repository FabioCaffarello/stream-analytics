package runtime

import (
	"time"

	"github.com/anthdm/hollywood/actor"
)

// Subsystem identifies a runtime-managed actor subsystem.
type Subsystem string

const (
	SubsystemMarketData  Subsystem = "marketdata"
	SubsystemAggregation Subsystem = "aggregation"
	SubsystemDelivery    Subsystem = "delivery"
	SubsystemInsights    Subsystem = "insights"
)

var orderedSubsystems = []Subsystem{
	SubsystemMarketData,
	SubsystemAggregation,
	SubsystemDelivery,
	SubsystemInsights,
}

// Start requests Guardian startup orchestration.
type Start struct{}

// Stop requests Guardian graceful shutdown orchestration.
type Stop struct{}

// ReloadConfig requests a controlled runtime reload.
type ReloadConfig struct{}

// Ping requests liveliness information from Guardian.
type Ping struct {
	ReplyTo *actor.PID
}

// Pong is returned as a Ping response.
type Pong struct {
	At time.Time
}

// Snapshot requests a point-in-time runtime state snapshot.
type Snapshot struct {
	ReplyTo *actor.PID
}

// SnapshotState is returned as a Snapshot response.
type SnapshotState struct {
	At         time.Time
	Subsystems map[Subsystem]SubsystemState
}

// SubsystemState contains health/lifecycle data for one subsystem.
type SubsystemState struct {
	Running       bool
	Degraded      bool
	LastError     string
	RestartCount  int
	CooldownUntil time.Time
}

// ChildFailed reports a child failure with a stable error kind.
type ChildFailed struct {
	Subsystem Subsystem
	Kind      string
	Err       error
}

// Degraded is emitted when a subsystem enters degraded mode.
type Degraded struct {
	Subsystem Subsystem
	Reason    string
}

// Recovered is emitted when a subsystem exits degraded mode.
type Recovered struct {
	Subsystem Subsystem
}
