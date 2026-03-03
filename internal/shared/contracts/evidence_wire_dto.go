package contracts

import (
	evidencedomain "github.com/market-raccoon/internal/core/evidence/domain"
	evidencev1 "github.com/market-raccoon/internal/shared/proto/gen/evidence/v1"
)

// DomainToProtoEvidenceV1 converts a domain EvidenceEvent to protobuf.
func DomainToProtoEvidenceV1(d evidencedomain.EvidenceEvent) *evidencev1.MicrostructureEvidenceV1 {
	features := make([]*evidencev1.EvidenceFeature, len(d.Features))
	for i, name := range d.Features {
		val := 0.0
		if i < len(d.FeatureVals) {
			val = d.FeatureVals[i]
		}
		features[i] = &evidencev1.EvidenceFeature{
			Name:  name,
			Value: val,
		}
	}
	return &evidencev1.MicrostructureEvidenceV1{
		Kind:       string(d.Kind),
		TsServer:   d.TsServer,
		Venue:      d.Venue,
		Symbol:     d.Symbol,
		Severity:   domainSeverityToProto(d.Severity),
		Confidence: d.Confidence,
		Features:   features,
		Reason:     d.Reason,
		SeqTrigger: d.SeqTrigger,
	}
}

// ProtoToDomainEvidenceV1 converts a protobuf MicrostructureEvidenceV1 to domain.
func ProtoToDomainEvidenceV1(p *evidencev1.MicrostructureEvidenceV1) evidencedomain.EvidenceEvent {
	names := make([]string, len(p.Features))
	vals := make([]float64, len(p.Features))
	for i, f := range p.Features {
		names[i] = f.Name
		vals[i] = f.Value
	}
	return evidencedomain.EvidenceEvent{
		Kind:        evidencedomain.EvidenceKind(p.Kind),
		TsServer:    p.TsServer,
		Venue:       p.Venue,
		Symbol:      p.Symbol,
		Severity:    protoSeverityToDomain(p.Severity),
		Confidence:  p.Confidence,
		Features:    names,
		FeatureVals: vals,
		Reason:      p.Reason,
		SeqTrigger:  p.SeqTrigger,
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
