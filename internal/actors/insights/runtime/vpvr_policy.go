// Package insightsruntime provides policykit bindings for the insights BC
// overload policies.  Core insights defines pure helpers; this package wires
// them to the shared policykit engine.
package insightsruntime

import (
	insightsapp "github.com/market-raccoon/internal/core/insights/app"
	"github.com/market-raccoon/internal/shared/policykit"
)

// NewPolicyKitDecideFunc returns an OverloadDecideFunc backed by a policykit.Engine.
func NewPolicyKitDecideFunc(engine policykit.Engine) insightsapp.OverloadDecideFunc {
	return func(prev insightsapp.VPVROverloadLevel, signals insightsapp.VPVROverloadSignals) (
		insightsapp.VPVROverloadLevel, bool, int, bool,
	) {
		decision := engine.Decide(policykit.Level(prev), toPolicySignals(signals))
		return insightsapp.VPVROverloadLevel(decision.Level),
			decision.HasAction(policykit.ActionCompressSnapshot),
			decision.DegradeStride(),
			decision.HasAction(policykit.ActionDropDelta)
	}
}

// DefaultDecideFunc returns an OverloadDecideFunc using the default threshold engine.
func DefaultDecideFunc() insightsapp.OverloadDecideFunc {
	return NewPolicyKitDecideFunc(policykit.NewThresholdEngine(policykit.DefaultThresholdConfig()))
}

func toPolicySignals(signals insightsapp.VPVROverloadSignals) policykit.Signals {
	return policykit.Signals{
		QueueDepth:          signals.QueueDepth,
		QueueCapacity:       signals.QueueCapacity,
		Backlog:             signals.QueueDepth,
		BacklogCap:          signals.QueueCapacity,
		Occupancy:           signals.BoundedMapOccupancy,
		Limit:               signals.BoundedMapLimit,
		ProcessingLatencyMs: signals.ProcessingLatencyMs,
	}
}
