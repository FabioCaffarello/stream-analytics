// Package domain contains the evidence bounded context domain model.
//
// Evidence events are deterministic microstructure observations — same input
// events always produce the same evidence output. They NEVER issue buy/sell
// directives (ADR-0008). Evidence explains what was detected and exposes
// observable structural facts about order books and trade flow.
package domain

import (
	"math"
	"strings"

	"github.com/market-raccoon/internal/shared/problem"
)

const (
	MicrostructureEvidenceType    = "insights.microstructure_evidence"
	MicrostructureEvidenceVersion = 1
)

// EvidenceKind classifies the type of microstructure observation.
type EvidenceKind string

const (
	SpreadExplosion     EvidenceKind = "spread_explosion"
	LiquidityThinning   EvidenceKind = "liquidity_thinning"
	PersistentImbalance EvidenceKind = "persistent_imbalance"
	Absorption          EvidenceKind = "absorption"
	Sweep               EvidenceKind = "sweep"
)

var validKinds = map[EvidenceKind]struct{}{
	SpreadExplosion:     {},
	LiquidityThinning:   {},
	PersistentImbalance: {},
	Absorption:          {},
	Sweep:               {},
}

// Severity classifies the urgency of an evidence event.
type Severity string

const (
	SeverityLow      Severity = "low"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

var validSeverities = map[Severity]struct{}{
	SeverityLow:      {},
	SeverityMedium:   {},
	SeverityHigh:     {},
	SeverityCritical: {},
}

// EvidenceEvent is the canonical domain output for a microstructure observation.
// JSON tags are lowercase (matches insights pattern for delivery to clients).
type EvidenceEvent struct {
	Kind        EvidenceKind `json:"kind"`
	TsServer    int64        `json:"ts_server"`
	Venue       string       `json:"venue"`
	Symbol      string       `json:"symbol"`
	Severity    Severity     `json:"severity"`
	Confidence  float64      `json:"confidence"`
	Features    []string     `json:"features"`
	FeatureVals []float64    `json:"feature_values"`
	Reason      string       `json:"reason"`
	SeqTrigger  int64        `json:"seq_trigger"`
}

// Validate checks EvidenceEvent invariants.
func (e EvidenceEvent) Validate() *problem.Problem {
	if _, ok := validKinds[e.Kind]; !ok {
		return problem.New(problem.ValidationFailed, "evidence kind must be a recognized value")
	}
	if _, ok := validSeverities[e.Severity]; !ok {
		return problem.New(problem.ValidationFailed, "evidence severity must be a recognized value")
	}
	if e.TsServer <= 0 {
		return problem.New(problem.ValidationFailed, "evidence ts_server must be positive")
	}
	if strings.TrimSpace(e.Venue) == "" {
		return problem.New(problem.ValidationFailed, "evidence venue must not be empty")
	}
	if strings.TrimSpace(e.Symbol) == "" {
		return problem.New(problem.ValidationFailed, "evidence symbol must not be empty")
	}
	if !isFiniteFloat(e.Confidence) || e.Confidence < 0 || e.Confidence > 1 {
		return problem.New(problem.ValidationFailed, "evidence confidence must be in [0,1]")
	}
	if len(e.Features) == 0 {
		return problem.New(problem.ValidationFailed, "evidence must have at least one feature")
	}
	if len(e.Features) != len(e.FeatureVals) {
		return problem.New(problem.ValidationFailed, "evidence features and feature_values must have equal length")
	}
	for _, v := range e.FeatureVals {
		if !isFiniteFloat(v) {
			return problem.New(problem.ValidationFailed, "evidence feature values must be finite")
		}
	}
	if strings.TrimSpace(e.Reason) == "" {
		return problem.New(problem.ValidationFailed, "evidence reason must not be empty")
	}
	return nil
}

func isFiniteFloat(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
