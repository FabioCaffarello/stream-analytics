package app

import (
	"strings"
	"sync"

	executiondomain "github.com/market-raccoon/internal/core/execution/domain"
	"github.com/market-raccoon/internal/shared/problem"
)

// InMemoryControlPlane holds mutable runtime control state.
// Thread-safe: uses sync.RWMutex. Snapshot() is a read-copy, Apply() is write-locked.
type InMemoryControlPlane struct {
	mu                 sync.RWMutex
	state              executiondomain.ControlState
	disabledStrategies map[string]struct{}
	disabledAdapters   map[string]struct{}
	simulationProfile  string
	allowlistOverrides *executiondomain.AllowlistOverride
	lastDirective      executiondomain.ControlDirective
	updatedAtMs        int64
}

// NewInMemoryControlPlane returns a control plane in the Active state.
func NewInMemoryControlPlane() *InMemoryControlPlane {
	return &InMemoryControlPlane{
		state:              executiondomain.ControlStateActive,
		disabledStrategies: make(map[string]struct{}),
		disabledAdapters:   make(map[string]struct{}),
	}
}

// Snapshot returns an immutable copy of current state.
func (cp *InMemoryControlPlane) Snapshot() executiondomain.ControlSnapshot {
	cp.mu.RLock()
	defer cp.mu.RUnlock()

	strategies := make(map[string]struct{}, len(cp.disabledStrategies))
	for k, v := range cp.disabledStrategies {
		strategies[k] = v
	}

	adapters := make(map[string]struct{}, len(cp.disabledAdapters))
	for k, v := range cp.disabledAdapters {
		adapters[k] = v
	}

	var overrides *executiondomain.AllowlistOverride
	if cp.allowlistOverrides != nil {
		o := executiondomain.AllowlistOverride{
			RestrictVenues:  make(map[string]struct{}, len(cp.allowlistOverrides.RestrictVenues)),
			RestrictSymbols: make(map[string]struct{}, len(cp.allowlistOverrides.RestrictSymbols)),
		}
		for k, v := range cp.allowlistOverrides.RestrictVenues {
			o.RestrictVenues[k] = v
		}
		for k, v := range cp.allowlistOverrides.RestrictSymbols {
			o.RestrictSymbols[k] = v
		}
		overrides = &o
	}

	return executiondomain.ControlSnapshot{
		State:              cp.state,
		DisabledStrategies: strategies,
		DisabledAdapters:   adapters,
		SimulationProfile:  cp.simulationProfile,
		AllowlistOverrides: overrides,
		LastDirective:      cp.lastDirective,
		UpdatedAtMs:        cp.updatedAtMs,
	}
}

// Apply processes a control directive, mutating internal state.
//
//nolint:gocyclo // explicit per-command branches for deterministic state transitions.
func (cp *InMemoryControlPlane) Apply(directive executiondomain.ControlDirective) *problem.Problem {
	if p := directive.Validate(); p != nil {
		return p
	}

	cp.mu.Lock()
	defer cp.mu.Unlock()

	cmd := executiondomain.ControlCommand(strings.TrimSpace(string(directive.Command)))

	switch cmd {
	case executiondomain.CommandPause:
		if cp.state != executiondomain.ControlStateActive {
			return problem.New(problem.Conflict, "pause requires active state")
		}
		cp.state = executiondomain.ControlStatePaused

	case executiondomain.CommandResume:
		if cp.state != executiondomain.ControlStatePaused && cp.state != executiondomain.ControlStateDrained {
			return problem.New(problem.Conflict, "resume requires paused or drained state")
		}
		cp.state = executiondomain.ControlStateActive

	case executiondomain.CommandDrain:
		if cp.state != executiondomain.ControlStateActive && cp.state != executiondomain.ControlStatePaused {
			return problem.New(problem.Conflict, "drain requires active or paused state")
		}
		cp.state = executiondomain.ControlStateDrained

	case executiondomain.CommandHalt:
		cp.state = executiondomain.ControlStateHalted

	case executiondomain.CommandDisableStrategy:
		sid := strings.TrimSpace(directive.TargetID)
		cp.disabledStrategies[sid] = struct{}{}

	case executiondomain.CommandEnableStrategy:
		sid := strings.TrimSpace(directive.TargetID)
		delete(cp.disabledStrategies, sid)

	case executiondomain.CommandDisableAdapter:
		aid := strings.ToLower(strings.TrimSpace(directive.TargetID))
		cp.disabledAdapters[aid] = struct{}{}

	case executiondomain.CommandEnableAdapter:
		aid := strings.ToLower(strings.TrimSpace(directive.TargetID))
		delete(cp.disabledAdapters, aid)

	case executiondomain.CommandSetSimProfile:
		cp.simulationProfile = strings.TrimSpace(directive.TargetID)

	case executiondomain.CommandUpdateAllowlist:
		cp.allowlistOverrides = parseAllowlistFromParameters(directive.Parameters)

	default:
		return problem.New(problem.ValidationFailed, "unknown control command")
	}

	cp.lastDirective = directive
	cp.updatedAtMs = directive.IssuedAtMs
	return nil
}

// parseAllowlistFromParameters builds an AllowlistOverride from directive parameters.
// Keys: "venues" (comma-separated, lowercased), "symbols" (comma-separated, uppercased).
// If both are empty, returns nil (clear overrides).
func parseAllowlistFromParameters(params map[string]string) *executiondomain.AllowlistOverride {
	if len(params) == 0 {
		return nil
	}

	venuesRaw := strings.TrimSpace(params["venues"])
	symbolsRaw := strings.TrimSpace(params["symbols"])

	if venuesRaw == "" && symbolsRaw == "" {
		return nil
	}

	override := &executiondomain.AllowlistOverride{
		RestrictVenues:  make(map[string]struct{}),
		RestrictSymbols: make(map[string]struct{}),
	}

	if venuesRaw != "" {
		parts := strings.Split(venuesRaw, ",")
		for _, raw := range parts {
			v := strings.ToLower(strings.TrimSpace(raw))
			if v != "" {
				override.RestrictVenues[v] = struct{}{}
			}
		}
	}

	if symbolsRaw != "" {
		parts := strings.Split(symbolsRaw, ",")
		for _, raw := range parts {
			s := strings.ToUpper(strings.TrimSpace(raw))
			if s != "" {
				override.RestrictSymbols[s] = struct{}{}
			}
		}
	}

	return override
}
