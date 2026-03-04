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
	SubsystemEvidence    Subsystem = "evidence"
	SubsystemSignals     Subsystem = "signals"
	SubsystemStorage     Subsystem = "storage"
)

var orderedSubsystems = []Subsystem{
	SubsystemMarketData,
	SubsystemAggregation,
	SubsystemDelivery,
	SubsystemInsights,
	SubsystemEvidence,
	SubsystemSignals,
	SubsystemStorage,
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
	Running          bool
	Degraded         bool
	Connected        bool
	HasChild         bool
	ChildPID         string
	LastError        string
	LastFailureAt    time.Time
	LastTransitionAt time.Time
	LastMessageAt    time.Time
	LastPublishAt    time.Time
	RestartCount     int
	CooldownUntil    time.Time
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

// Recovered is emitted when a subsystem is considered recovered.
// Runtime v1 emits this after a successful spawn (assumed recovery).
// A future handshake (e.g. ChildReady) should confirm operational readiness.
type Recovered struct {
	Subsystem Subsystem
}

// ReadyQuery requests readiness state from the Guardian.
// If ReplyTo is nil the response is sent to c.Sender() (enables engine.Request).
type ReadyQuery struct {
	ReplyTo *actor.PID
}

// ReadyResponse is returned in reply to ReadyQuery.
type ReadyResponse struct {
	// Ready is true when all expected subsystems have started at least once.
	Ready bool
	// Pending lists the expected subsystems that have not yet started.
	Pending []Subsystem
}

// SubsystemHeartbeat updates runtime health signals for a subsystem.
type SubsystemHeartbeat struct {
	Subsystem     Subsystem
	Connected     bool
	LastMessageAt time.Time
	LastPublishAt time.Time
}
