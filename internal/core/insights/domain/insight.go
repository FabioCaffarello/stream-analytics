// Package domain contains the insights bounded context domain model.
//
// Insights are decision-support signals — they explain what was detected
// and why it might matter. They NEVER issue buy/sell directives (ADR-0008).
package domain

import "github.com/FabioCaffarello/stream-analytics/internal/shared/problem"

// InsightType is a stable name for a class of insight.
type InsightType string

// Confidence is a value in [0,1] representing the model's certainty.
type Confidence float64

// Evidence is a single piece of observable data supporting the insight.
type Evidence struct {
	Label string
	Value any
}

// Insight is the root aggregate for a detected market signal.
//
// Invariants:
//   - Confidence ∈ [0, 1].
//   - At least one Evidence item must be provided.
//   - No buy/sell directives are ever emitted.
type Insight struct {
	Type                   InsightType
	Confidence             Confidence
	Evidence               []Evidence
	Window                 string // e.g. "5m", "1h"
	Venue                  string
	Instrument             string
	InvalidationConditions []string
}

// Validate checks the insight invariants.
func (ins *Insight) Validate() *problem.Problem {
	if ins.Type == "" {
		return problem.New(problem.ValidationFailed, "insight type must not be empty")
	}
	if ins.Confidence < 0 || ins.Confidence > 1 {
		return problem.WithDetail(
			problem.Newf(problem.ValidationFailed,
				"confidence must be in [0,1], got %f", float64(ins.Confidence)),
			"value", float64(ins.Confidence),
		)
	}
	if len(ins.Evidence) == 0 {
		return problem.New(problem.ValidationFailed, "at least one evidence item is required")
	}
	return nil
}
