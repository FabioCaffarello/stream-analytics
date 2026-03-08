package domain

import (
	"math"
	"strings"

	"github.com/market-raccoon/internal/shared/problem"
)

const (
	CompositeSignalType    = "signal.composite"
	CompositeSignalVersion = 1

	maxSignalEvidenceFeatures = 100
)

// SignalFeature is a compact, transport-safe feature field.
type SignalFeature struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// CompositeSignalV1 is the composed, non-execution signal contract.
//
// SignalID and CorrelationID enable strategy handoff — strategy's IntentPlanner
// requires SignalID for provenance tracking (IntentProvenance.ParentSignalIDs).
type CompositeSignalV1 struct {
	Kind           string          `json:"kind"`
	Venue          string          `json:"venue"`
	Instrument     string          `json:"instrument"`
	Timeframe      string          `json:"timeframe"`
	TsServer       int64           `json:"ts_server"`
	Severity       string          `json:"severity"`
	Confidence     float64         `json:"confidence"`
	Evidence       []SignalFeature `json:"evidence"`
	RegimeKind     string          `json:"regime_kind,omitempty"`
	RegimeStrength float64         `json:"regime_strength,omitempty"`
	Reason         string          `json:"reason"`
	Seq            int64           `json:"seq"`
	SourceKinds    []string        `json:"source_kinds"`
	SignalID       string          `json:"signal_id,omitempty"`
	CorrelationID  string          `json:"correlation_id,omitempty"`
}

//nolint:gocyclo // Validation keeps one explicit branch per invariant for deterministic errors.
func (s CompositeSignalV1) Validate() *problem.Problem {
	if strings.TrimSpace(s.Kind) == "" {
		return problem.New(problem.ValidationFailed, "signal kind must not be empty")
	}
	if strings.TrimSpace(s.Venue) == "" {
		return problem.New(problem.ValidationFailed, "signal venue must not be empty")
	}
	if strings.TrimSpace(s.Instrument) == "" {
		return problem.New(problem.ValidationFailed, "signal instrument must not be empty")
	}
	if strings.TrimSpace(s.Timeframe) == "" {
		return problem.New(problem.ValidationFailed, "signal timeframe must not be empty")
	}
	if s.TsServer <= 0 {
		return problem.New(problem.ValidationFailed, "signal ts_server must be > 0")
	}
	if !validSeverity(s.Severity) {
		return problem.New(problem.ValidationFailed, "signal severity must be one of low|medium|high|critical")
	}
	if !isUnitInterval(s.Confidence) {
		return problem.New(problem.ValidationFailed, "signal confidence must be in [0,1]")
	}
	if strings.TrimSpace(s.RegimeKind) == "" && s.RegimeStrength != 0 {
		return problem.New(problem.ValidationFailed, "signal regime_strength must be zero when regime_kind is empty")
	}
	if strings.TrimSpace(s.RegimeKind) != "" && !isUnitInterval(s.RegimeStrength) {
		return problem.New(problem.ValidationFailed, "signal regime_strength must be in [0,1]")
	}
	if len(s.Evidence) == 0 {
		return problem.New(problem.ValidationFailed, "signal evidence must not be empty")
	}
	if len(s.Evidence) > maxSignalEvidenceFeatures {
		return problem.New(problem.ValidationFailed, "signal evidence exceeds max features")
	}
	for i := range s.Evidence {
		if strings.TrimSpace(s.Evidence[i].Label) == "" {
			return problem.New(problem.ValidationFailed, "signal evidence label must not be empty")
		}
		if strings.TrimSpace(s.Evidence[i].Value) == "" {
			return problem.New(problem.ValidationFailed, "signal evidence value must not be empty")
		}
	}
	if strings.TrimSpace(s.Reason) == "" {
		return problem.New(problem.ValidationFailed, "signal reason must not be empty")
	}
	if s.Seq < 0 {
		return problem.New(problem.ValidationFailed, "signal seq must be >= 0")
	}
	if len(s.SourceKinds) == 0 {
		return problem.New(problem.ValidationFailed, "signal source_kinds must not be empty")
	}
	for i := range s.SourceKinds {
		if strings.TrimSpace(s.SourceKinds[i]) == "" {
			return problem.New(problem.ValidationFailed, "signal source_kinds entries must not be empty")
		}
	}
	return nil
}

func validSeverity(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "low", "medium", "high", "critical":
		return true
	default:
		return false
	}
}

func isUnitInterval(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 && v <= 1
}
