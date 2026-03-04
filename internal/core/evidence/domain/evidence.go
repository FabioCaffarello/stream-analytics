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
	RuleVersionV0                 = "v0"
)

// EvidenceType classifies the type of liquidity evidence observation.
type EvidenceType string

const (
	SpreadExplosion     EvidenceType = "spread_explosion"
	LiquidityThinning   EvidenceType = "liquidity_thinning"
	PersistentImbalance EvidenceType = "persistent_imbalance"
	Absorption          EvidenceType = "absorption"
	Sweep               EvidenceType = "sweep"
)

var validTypes = map[EvidenceType]struct{}{
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

// EvidenceFeature is one deterministic evidence feature entry.
type EvidenceFeature struct {
	Key   string  `json:"key"`
	Value float64 `json:"value"`
}

// InputWatermark captures the input sequence span used to build one evidence event.
type InputWatermark struct {
	SeqStart int64 `json:"seq_start"`
	SeqEnd   int64 `json:"seq_end"`
}

// EvidenceEvent is the canonical liquidity-evidence payload produced by LEL.
type EvidenceEvent struct {
	Type           EvidenceType      `json:"type"`
	TsServer       int64             `json:"ts_server"`
	Venue          string            `json:"venue"`
	Symbol         string            `json:"symbol"`
	StreamID       string            `json:"stream_id"`
	Seq            int64             `json:"seq"`
	Severity       Severity          `json:"severity"`
	Confidence     float64           `json:"confidence"`
	Features       []EvidenceFeature `json:"features"`
	Explanation    string            `json:"explanation"`
	RuleVersion    string            `json:"rule_version"`
	InputWatermark InputWatermark    `json:"input_watermark"`
}

// Validate checks EvidenceEvent invariants.
func (e EvidenceEvent) Validate() *problem.Problem {
	if p := e.validateTypeAndSeverity(); p != nil {
		return p
	}
	if p := e.validateIdentity(); p != nil {
		return p
	}
	if p := e.validateConfidenceAndRuleVersion(); p != nil {
		return p
	}
	if p := e.validateInputWatermark(); p != nil {
		return p
	}
	if p := validateEvidenceFeatures(e.Features); p != nil {
		return p
	}
	if strings.TrimSpace(e.Explanation) == "" {
		return problem.New(problem.ValidationFailed, "evidence explanation must not be empty")
	}
	return nil
}

func (e EvidenceEvent) validateTypeAndSeverity() *problem.Problem {
	if _, ok := validTypes[e.Type]; !ok {
		return problem.New(problem.ValidationFailed, "evidence type must be a recognized value")
	}
	if _, ok := validSeverities[e.Severity]; !ok {
		return problem.New(problem.ValidationFailed, "evidence severity must be a recognized value")
	}
	return nil
}

func (e EvidenceEvent) validateIdentity() *problem.Problem {
	if e.TsServer <= 0 {
		return problem.New(problem.ValidationFailed, "evidence ts_server must be positive")
	}
	if strings.TrimSpace(e.Venue) == "" {
		return problem.New(problem.ValidationFailed, "evidence venue must not be empty")
	}
	if strings.TrimSpace(e.Symbol) == "" {
		return problem.New(problem.ValidationFailed, "evidence symbol must not be empty")
	}
	if strings.TrimSpace(e.StreamID) == "" {
		return problem.New(problem.ValidationFailed, "evidence stream_id must not be empty")
	}
	if e.Seq <= 0 {
		return problem.New(problem.ValidationFailed, "evidence seq must be > 0")
	}
	return nil
}

func (e EvidenceEvent) validateConfidenceAndRuleVersion() *problem.Problem {
	if !isFiniteFloat(e.Confidence) || e.Confidence < 0 || e.Confidence > 1 {
		return problem.New(problem.ValidationFailed, "evidence confidence must be in [0,1]")
	}
	if strings.TrimSpace(e.RuleVersion) == "" {
		return problem.New(problem.ValidationFailed, "evidence rule_version must not be empty")
	}
	return nil
}

func (e EvidenceEvent) validateInputWatermark() *problem.Problem {
	if e.InputWatermark.SeqStart <= 0 || e.InputWatermark.SeqEnd <= 0 {
		return problem.New(problem.ValidationFailed, "evidence input_watermark seq range must be > 0")
	}
	if e.InputWatermark.SeqEnd < e.InputWatermark.SeqStart {
		return problem.New(problem.ValidationFailed, "evidence input_watermark seq_end must be >= seq_start")
	}
	return nil
}

func validateEvidenceFeatures(features []EvidenceFeature) *problem.Problem {
	if len(features) == 0 {
		return problem.New(problem.ValidationFailed, "evidence must have at least one feature")
	}
	for i := range features {
		f := features[i]
		if strings.TrimSpace(f.Key) == "" {
			return problem.New(problem.ValidationFailed, "evidence feature key must not be empty")
		}
		if !isFiniteFloat(f.Value) {
			return problem.New(problem.ValidationFailed, "evidence feature values must be finite")
		}
		if i > 0 && strings.Compare(features[i-1].Key, f.Key) >= 0 {
			return problem.New(problem.ValidationFailed, "evidence features must be sorted and unique by key")
		}
	}
	return nil
}

func isFiniteFloat(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
