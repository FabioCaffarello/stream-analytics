package domain

import (
	"strings"

	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

const (
	LiquidityEvidenceEventType = "liquidity.evidence"
	LiquidityEvidenceVersion   = int32(1)
)

// LiquidityEvidenceType is the stable taxonomy for LEL v1.
type LiquidityEvidenceType string

const (
	LiquidityEvidenceTypeBookImbalance LiquidityEvidenceType = "BOOK_IMBALANCE"
	LiquidityEvidenceTypeAbsorption    LiquidityEvidenceType = "ABSORPTION"
	LiquidityEvidenceTypeSweep         LiquidityEvidenceType = "SWEEP"
	LiquidityEvidenceTypeThinning      LiquidityEvidenceType = "THINNING"
	LiquidityEvidenceTypeSpreadRegime  LiquidityEvidenceType = "SPREAD_REGIME"
)

var validLiquidityEvidenceTypes = map[LiquidityEvidenceType]struct{}{
	LiquidityEvidenceTypeBookImbalance: {},
	LiquidityEvidenceTypeAbsorption:    {},
	LiquidityEvidenceTypeSweep:         {},
	LiquidityEvidenceTypeThinning:      {},
	LiquidityEvidenceTypeSpreadRegime:  {},
}

// LiquidityEvidenceSeverity is the bounded severity set for LEL v1.
type LiquidityEvidenceSeverity string

const (
	LiquidityEvidenceSeverityLow      LiquidityEvidenceSeverity = "low"
	LiquidityEvidenceSeverityMedium   LiquidityEvidenceSeverity = "medium"
	LiquidityEvidenceSeverityHigh     LiquidityEvidenceSeverity = "high"
	LiquidityEvidenceSeverityCritical LiquidityEvidenceSeverity = "critical"
)

var validLiquidityEvidenceSeverities = map[LiquidityEvidenceSeverity]struct{}{
	LiquidityEvidenceSeverityLow:      {},
	LiquidityEvidenceSeverityMedium:   {},
	LiquidityEvidenceSeverityHigh:     {},
	LiquidityEvidenceSeverityCritical: {},
}

// LiquidityEvidenceMetric is one deterministic metric entry.
type LiquidityEvidenceMetric struct {
	Key   string  `json:"key"`
	Value float64 `json:"value"`
}

// LiquidityInputWatermark captures source sequence range used to build one evidence.
type LiquidityInputWatermark struct {
	SeqStart int64 `json:"seq_start"`
	SeqEnd   int64 `json:"seq_end"`
}

// LiquidityEvidence is the canonical LEL v1 domain payload.
type LiquidityEvidence struct {
	EvidenceType LiquidityEvidenceType     `json:"evidence_type"`
	TsIngestMs   int64                     `json:"ts_ingest_ms"`
	Venue        string                    `json:"venue"`
	Symbol       string                    `json:"symbol"`
	WindowMs     int64                     `json:"window_ms"`
	Severity     LiquidityEvidenceSeverity `json:"severity"`
	Confidence   float64                   `json:"confidence"`
	Metrics      []LiquidityEvidenceMetric `json:"metrics"`
	Explain      []string                  `json:"explain"`
	Version      int32                     `json:"version"`
	StreamID     string                    `json:"stream_id"`
	Seq          int64                     `json:"seq"`
	Watermark    LiquidityInputWatermark   `json:"watermark"`
}

// Validate checks LEL v1 payload invariants.
func (e LiquidityEvidence) Validate() *problem.Problem {
	if p := e.validateIdentity(); p != nil {
		return p
	}
	if p := e.validateEnvelope(); p != nil {
		return p
	}
	if p := e.validateMetrics(); p != nil {
		return p
	}
	if p := e.validateExplain(); p != nil {
		return p
	}
	return e.validateWatermark()
}

func (e LiquidityEvidence) validateIdentity() *problem.Problem {
	if _, ok := validLiquidityEvidenceTypes[e.EvidenceType]; !ok {
		return problem.New(problem.ValidationFailed, "liquidity evidence type must be recognized")
	}
	if _, ok := validLiquidityEvidenceSeverities[e.Severity]; !ok {
		return problem.New(problem.ValidationFailed, "liquidity evidence severity must be recognized")
	}
	if e.TsIngestMs <= 0 {
		return problem.New(problem.ValidationFailed, "liquidity evidence ts_ingest_ms must be > 0")
	}
	if strings.TrimSpace(e.Venue) == "" {
		return problem.New(problem.ValidationFailed, "liquidity evidence venue must not be empty")
	}
	if strings.TrimSpace(e.Symbol) == "" {
		return problem.New(problem.ValidationFailed, "liquidity evidence symbol must not be empty")
	}
	if strings.TrimSpace(e.StreamID) == "" {
		return problem.New(problem.ValidationFailed, "liquidity evidence stream_id must not be empty")
	}
	if e.WindowMs <= 0 {
		return problem.New(problem.ValidationFailed, "liquidity evidence window_ms must be > 0")
	}
	if e.Seq <= 0 {
		return problem.New(problem.ValidationFailed, "liquidity evidence seq must be > 0")
	}
	return nil
}

func (e LiquidityEvidence) validateEnvelope() *problem.Problem {
	if e.Version != LiquidityEvidenceVersion {
		return problem.New(problem.ValidationFailed, "liquidity evidence version must be 1")
	}
	if !isFiniteFloat(e.Confidence) || e.Confidence < 0 || e.Confidence > 1 {
		return problem.New(problem.ValidationFailed, "liquidity evidence confidence must be in [0,1]")
	}
	return nil
}

func (e LiquidityEvidence) validateMetrics() *problem.Problem {
	if len(e.Metrics) == 0 {
		return problem.New(problem.ValidationFailed, "liquidity evidence metrics must not be empty")
	}
	if len(e.Metrics) > 8 {
		return problem.New(problem.ValidationFailed, "liquidity evidence metrics must be <= 8")
	}
	for i := range e.Metrics {
		m := e.Metrics[i]
		if strings.TrimSpace(m.Key) == "" {
			return problem.New(problem.ValidationFailed, "liquidity evidence metric key must not be empty")
		}
		if !isFiniteFloat(m.Value) {
			return problem.New(problem.ValidationFailed, "liquidity evidence metric values must be finite")
		}
		if i > 0 && strings.Compare(e.Metrics[i-1].Key, m.Key) >= 0 {
			return problem.New(problem.ValidationFailed, "liquidity evidence metrics must be sorted and unique")
		}
	}
	return nil
}

func (e LiquidityEvidence) validateExplain() *problem.Problem {
	if len(e.Explain) == 0 {
		return problem.New(problem.ValidationFailed, "liquidity evidence explain must not be empty")
	}
	if len(e.Explain) > 4 {
		return problem.New(problem.ValidationFailed, "liquidity evidence explain must be <= 4")
	}
	for i := range e.Explain {
		s := strings.TrimSpace(e.Explain[i])
		if s == "" {
			return problem.New(problem.ValidationFailed, "liquidity evidence explain entries must not be empty")
		}
		if len(s) > 120 {
			return problem.New(problem.ValidationFailed, "liquidity evidence explain entries must be <= 120 chars")
		}
	}
	return nil
}

func (e LiquidityEvidence) validateWatermark() *problem.Problem {
	if e.Watermark.SeqStart <= 0 || e.Watermark.SeqEnd <= 0 {
		return problem.New(problem.ValidationFailed, "liquidity evidence watermark seq range must be > 0")
	}
	if e.Watermark.SeqEnd < e.Watermark.SeqStart {
		return problem.New(problem.ValidationFailed, "liquidity evidence watermark seq_end must be >= seq_start")
	}
	return nil
}
