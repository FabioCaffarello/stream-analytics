package contracts

import (
	evidencedomain "github.com/FabioCaffarello/stream-analytics/internal/core/evidence/domain"
	evidencev1 "github.com/FabioCaffarello/stream-analytics/internal/shared/proto/gen/evidence/v1"
)

// DomainToProtoEvidenceV1 converts a domain EvidenceEvent to protobuf.
func DomainToProtoEvidenceV1(d evidencedomain.EvidenceEvent) *evidencev1.MicrostructureEvidenceV1 {
	features := make([]*evidencev1.EvidenceFeature, len(d.Features))
	for i, f := range d.Features {
		features[i] = &evidencev1.EvidenceFeature{
			Key:   f.Key,
			Value: f.Value,
		}
	}
	return &evidencev1.MicrostructureEvidenceV1{
		Type:        string(d.Type),
		TsServer:    d.TsServer,
		Venue:       d.Venue,
		Symbol:      d.Symbol,
		StreamId:    d.StreamID,
		Seq:         d.Seq,
		Severity:    domainSeverityToProto(d.Severity),
		Confidence:  d.Confidence,
		Features:    features,
		Explanation: d.Explanation,
		RuleVersion: d.RuleVersion,
		InputWatermark: &evidencev1.InputWatermark{
			SeqStart: d.InputWatermark.SeqStart,
			SeqEnd:   d.InputWatermark.SeqEnd,
		},
	}
}

// ProtoToDomainEvidenceV1 converts a protobuf MicrostructureEvidenceV1 to domain.
func ProtoToDomainEvidenceV1(p *evidencev1.MicrostructureEvidenceV1) evidencedomain.EvidenceEvent {
	features := make([]evidencedomain.EvidenceFeature, len(p.Features))
	for i, f := range p.Features {
		features[i] = evidencedomain.EvidenceFeature{
			Key:   f.GetKey(),
			Value: f.GetValue(),
		}
	}
	inputWatermark := evidencedomain.InputWatermark{}
	if p.GetInputWatermark() != nil {
		inputWatermark.SeqStart = p.GetInputWatermark().GetSeqStart()
		inputWatermark.SeqEnd = p.GetInputWatermark().GetSeqEnd()
	}
	return evidencedomain.EvidenceEvent{
		Type:           evidencedomain.EvidenceType(p.GetType()),
		TsServer:       p.GetTsServer(),
		Venue:          p.GetVenue(),
		Symbol:         p.GetSymbol(),
		StreamID:       p.GetStreamId(),
		Seq:            p.GetSeq(),
		Severity:       protoSeverityToDomain(p.GetSeverity()),
		Confidence:     p.GetConfidence(),
		Features:       evidencedomain.SortedFeatures(features),
		Explanation:    p.GetExplanation(),
		RuleVersion:    p.GetRuleVersion(),
		InputWatermark: inputWatermark,
	}
}

func domainSeverityToProto(s evidencedomain.Severity) evidencev1.Severity {
	switch s {
	case evidencedomain.SeverityLow:
		return evidencev1.Severity_SEVERITY_LOW
	case evidencedomain.SeverityMedium:
		return evidencev1.Severity_SEVERITY_MEDIUM
	case evidencedomain.SeverityHigh:
		return evidencev1.Severity_SEVERITY_HIGH
	case evidencedomain.SeverityCritical:
		return evidencev1.Severity_SEVERITY_CRITICAL
	default:
		return evidencev1.Severity_SEVERITY_UNSPECIFIED
	}
}

func protoSeverityToDomain(s evidencev1.Severity) evidencedomain.Severity {
	switch s {
	case evidencev1.Severity_SEVERITY_LOW:
		return evidencedomain.SeverityLow
	case evidencev1.Severity_SEVERITY_MEDIUM:
		return evidencedomain.SeverityMedium
	case evidencev1.Severity_SEVERITY_HIGH:
		return evidencedomain.SeverityHigh
	case evidencev1.Severity_SEVERITY_CRITICAL:
		return evidencedomain.SeverityCritical
	default:
		return evidencedomain.SeverityLow
	}
}
