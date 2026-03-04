package signal

import (
	"sort"
	"strings"

	evidencedomain "github.com/market-raccoon/internal/core/evidence/domain"
	"github.com/market-raccoon/internal/shared/naming"
	"github.com/market-raccoon/internal/shared/problem"
)

const lelRuleVersionV1 = "v1"

// LELToEvidenceEvent converts LEL liquidity evidence into canonical signal evidence input.
func LELToEvidenceEvent(lel evidencedomain.LiquidityEvidence) (evidencedomain.EvidenceEvent, *problem.Problem) {
	mappedType, ok := mapLELTypeToEvidenceType(lel.EvidenceType)
	if !ok {
		return evidencedomain.EvidenceEvent{}, problem.Newf(
			problem.ValidationFailed,
			"liquidity evidence type %q is not mapped to signal evidence",
			string(lel.EvidenceType),
		)
	}

	venue := naming.CanonicalVenue(lel.Venue)
	symbol := naming.CanonicalInstrument(lel.Symbol)
	explanation := joinLELExplain(lel.Explain, lel.EvidenceType)
	out := evidencedomain.EvidenceEvent{
		Type:        mappedType,
		TsServer:    lel.TsIngestMs,
		Venue:       venue,
		Symbol:      symbol,
		StreamID:    venue + "/" + symbol + "/" + string(mappedType),
		Seq:         lel.Seq,
		Severity:    evidencedomain.Severity(strings.ToLower(strings.TrimSpace(string(lel.Severity)))),
		Confidence:  lel.Confidence,
		Features:    mapLELMetricsToFeatures(lel.Metrics, lel.EvidenceType),
		Explanation: explanation,
		RuleVersion: lelRuleVersionV1,
		InputWatermark: evidencedomain.InputWatermark{
			SeqStart: lel.Watermark.SeqStart,
			SeqEnd:   lel.Watermark.SeqEnd,
		},
	}
	if p := out.Validate(); p != nil {
		return evidencedomain.EvidenceEvent{}, p
	}
	return out, nil
}

func mapLELTypeToEvidenceType(typ evidencedomain.LiquidityEvidenceType) (evidencedomain.EvidenceType, bool) {
	switch typ {
	case evidencedomain.LiquidityEvidenceTypeBookImbalance:
		return evidencedomain.PersistentImbalance, true
	case evidencedomain.LiquidityEvidenceTypeAbsorption:
		return evidencedomain.Absorption, true
	case evidencedomain.LiquidityEvidenceTypeSweep:
		return evidencedomain.Sweep, true
	case evidencedomain.LiquidityEvidenceTypeThinning:
		return evidencedomain.LiquidityThinning, true
	case evidencedomain.LiquidityEvidenceTypeSpreadRegime:
		return evidencedomain.SpreadExplosion, true
	default:
		return "", false
	}
}

func mapLELMetricsToFeatures(
	metricsIn []evidencedomain.LiquidityEvidenceMetric,
	evidenceType evidencedomain.LiquidityEvidenceType,
) []evidencedomain.EvidenceFeature {
	accum := make(map[string]float64, len(metricsIn))
	for i := range metricsIn {
		key := strings.TrimSpace(metricsIn[i].Key)
		if key == "" || !isFiniteFloat(metricsIn[i].Value) {
			continue
		}
		accum[key] += metricsIn[i].Value
	}
	if len(accum) == 0 {
		return []evidencedomain.EvidenceFeature{{
			Key:   "evidence_type",
			Value: evidenceTypeFeatureValue(evidenceType),
		}}
	}
	keys := make([]string, 0, len(accum))
	for key := range accum {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]evidencedomain.EvidenceFeature, 0, len(keys))
	for i := range keys {
		out = append(out, evidencedomain.EvidenceFeature{Key: keys[i], Value: accum[keys[i]]})
	}
	return out
}

func evidenceTypeFeatureValue(evidenceType evidencedomain.LiquidityEvidenceType) float64 {
	switch evidenceType {
	case evidencedomain.LiquidityEvidenceTypeBookImbalance:
		return 1
	case evidencedomain.LiquidityEvidenceTypeAbsorption:
		return 2
	case evidencedomain.LiquidityEvidenceTypeSweep:
		return 3
	case evidencedomain.LiquidityEvidenceTypeThinning:
		return 4
	case evidencedomain.LiquidityEvidenceTypeSpreadRegime:
		return 5
	default:
		return 0
	}
}

func joinLELExplain(explainIn []string, evidenceType evidencedomain.LiquidityEvidenceType) string {
	parts := make([]string, 0, len(explainIn))
	for i := range explainIn {
		part := strings.TrimSpace(explainIn[i])
		if part == "" {
			continue
		}
		parts = append(parts, part)
	}
	joined := strings.Join(parts, "; ")
	if joined == "" {
		joined = "liquidity evidence " + strings.ToLower(strings.ReplaceAll(string(evidenceType), "_", " "))
	}
	if len(joined) > 512 {
		joined = strings.TrimSpace(joined[:512])
	}
	joined = strings.Trim(joined, "; ")
	if joined == "" {
		return "liquidity evidence"
	}
	return joined
}
