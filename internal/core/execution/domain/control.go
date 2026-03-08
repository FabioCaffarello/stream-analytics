package domain

import (
	"strings"

	"github.com/market-raccoon/internal/shared/problem"
)

// ControlState represents the current operational state of the execution subsystem.
type ControlState string

const (
	ControlStateActive  ControlState = "active"  // Normal operation.
	ControlStatePaused  ControlState = "paused"  // Intake open, execution halted.
	ControlStateDrained ControlState = "drained" // Intake closed, finishing in-flight.
	ControlStateHalted  ControlState = "halted"  // Emergency stop, reject all.
)

// ControlCommand represents a control plane instruction.
type ControlCommand string

const (
	CommandPause           ControlCommand = "pause"
	CommandResume          ControlCommand = "resume"
	CommandDrain           ControlCommand = "drain"
	CommandHalt            ControlCommand = "halt"
	CommandEnableStrategy  ControlCommand = "enable_strategy"
	CommandDisableStrategy ControlCommand = "disable_strategy"
	CommandEnableAdapter   ControlCommand = "enable_adapter"
	CommandDisableAdapter  ControlCommand = "disable_adapter"
	CommandSetSimProfile   ControlCommand = "set_simulation_profile"
	CommandUpdateAllowlist ControlCommand = "update_allowlist"
)

// ControlDirective is a command + parameters for the control plane.
type ControlDirective struct {
	Command    ControlCommand
	TargetID   string            // strategy ID, adapter ID, or profile name.
	Parameters map[string]string // command-specific key-value pairs.
	Reason     string            // operator reason for audit trail.
	IssuedAtMs int64
	Issuer     string // operator identity.
}

// Validate checks structural invariants of a ControlDirective.
func (d ControlDirective) Validate() *problem.Problem {
	cmd := ControlCommand(strings.TrimSpace(string(d.Command)))
	switch cmd {
	case CommandPause, CommandResume, CommandDrain, CommandHalt:
		// lifecycle commands require no target
	case CommandEnableStrategy, CommandDisableStrategy,
		CommandEnableAdapter, CommandDisableAdapter:
		if strings.TrimSpace(d.TargetID) == "" {
			return problem.New(problem.ValidationFailed, "target_id must not be empty for strategy/adapter commands")
		}
	case CommandSetSimProfile:
		// target_id is the profile name; empty means "reset to default"
	case CommandUpdateAllowlist:
		// parameters carry the restriction sets; validated at apply time
	default:
		return problem.New(problem.ValidationFailed, "unknown control command")
	}
	if strings.TrimSpace(d.Issuer) == "" {
		return problem.New(problem.ValidationFailed, "issuer must not be empty")
	}
	if d.IssuedAtMs <= 0 {
		return problem.New(problem.ValidationFailed, "issued_at_ms must be > 0")
	}
	return nil
}

// ControlSnapshot is the immutable snapshot of control plane state at evaluation time.
type ControlSnapshot struct {
	State              ControlState
	DisabledStrategies map[string]struct{} // strategy IDs.
	DisabledAdapters   map[string]struct{} // adapter IDs.
	SimulationProfile  string              // active simulation profile name (empty = default).
	AllowlistOverrides *AllowlistOverride  // nil = no overrides, use grant.
	LastDirective      ControlDirective    // last applied directive (for audit).
	DirectiveHistory   []ControlDirective  // recent directives, newest last, capped at 32.
	UpdatedAtMs        int64
}

// AllowlistOverride allows runtime narrowing (never widening) of the boot-time grant scope.
type AllowlistOverride struct {
	RestrictVenues  map[string]struct{} // if non-empty, intersect with grant allowlist.
	RestrictSymbols map[string]struct{} // if non-empty, intersect with grant allowlist.
}

// IsExecutionAllowed returns whether the snapshot permits execution of a given intent scope.
// The returned string describes the denial reason when allowed is false.
//
//nolint:gocyclo // explicit per-check branches for fail-closed determinism.
func (s ControlSnapshot) IsExecutionAllowed(strategyID, adapterID, venue, symbol string) (bool, string) {
	switch s.State {
	case ControlStateHalted:
		return false, ReasonControlPlaneHalted
	case ControlStatePaused:
		return false, ReasonControlPlanePaused
	case ControlStateDrained:
		return false, ReasonControlPlaneDrained
	case ControlStateActive:
		// continue checks
	default:
		// unknown state → fail closed
		return false, ReasonControlPlaneHalted
	}

	sid := strings.TrimSpace(strategyID)
	if sid != "" && s.DisabledStrategies != nil {
		if _, disabled := s.DisabledStrategies[sid]; disabled {
			return false, ReasonControlPlaneStrategyDisabled
		}
	}

	aid := strings.ToLower(strings.TrimSpace(adapterID))
	if aid != "" && s.DisabledAdapters != nil {
		if _, disabled := s.DisabledAdapters[aid]; disabled {
			return false, ReasonControlPlaneAdapterDisabled
		}
	}

	if s.AllowlistOverrides != nil {
		if len(s.AllowlistOverrides.RestrictVenues) > 0 {
			v := strings.ToLower(strings.TrimSpace(venue))
			if _, ok := s.AllowlistOverrides.RestrictVenues[v]; !ok {
				return false, ReasonControlPlaneVenueRestricted
			}
		}
		if len(s.AllowlistOverrides.RestrictSymbols) > 0 {
			sym := strings.ToUpper(strings.TrimSpace(symbol))
			if _, ok := s.AllowlistOverrides.RestrictSymbols[sym]; !ok {
				return false, ReasonControlPlaneSymbolRestricted
			}
		}
	}

	return true, ""
}
