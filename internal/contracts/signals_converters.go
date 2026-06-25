package contracts

import (
	signalsdomain "github.com/market-raccoon/internal/core/signals/domain"
	signalsv1 "github.com/market-raccoon/internal/shared/proto/gen/signals/v1"
)

// DomainToProtoCompositeSignalV1 converts a domain CompositeSignalV1 to protobuf.
func DomainToProtoCompositeSignalV1(d signalsdomain.CompositeSignalV1) *signalsv1.CompositeSignalV1 {
	evidence := make([]*signalsv1.Feature, len(d.Evidence))
	for i, f := range d.Evidence {
		evidence[i] = &signalsv1.Feature{Label: f.Label, Value: f.Value}
	}
	sourceKinds := make([]string, len(d.SourceKinds))
	copy(sourceKinds, d.SourceKinds)
	return &signalsv1.CompositeSignalV1{
		Kind:           d.Kind,
		Venue:          d.Venue,
		Instrument:     d.Instrument,
		Timeframe:      d.Timeframe,
		TsServerMs:     d.TsServer,
		Severity:       d.Severity,
		Confidence:     d.Confidence,
		Evidence:       evidence,
		RegimeKind:     d.RegimeKind,
		RegimeStrength: d.RegimeStrength,
		Reason:         d.Reason,
		Seq:            d.Seq,
		SourceKinds:    sourceKinds,
	}
}

// ProtoToDomainCompositeSignalV1 converts a protobuf CompositeSignalV1 to domain.
func ProtoToDomainCompositeSignalV1(p *signalsv1.CompositeSignalV1) signalsdomain.CompositeSignalV1 {
	if p == nil {
		return signalsdomain.CompositeSignalV1{}
	}
	evidence := make([]signalsdomain.SignalFeature, len(p.GetEvidence()))
	for i, f := range p.GetEvidence() {
		evidence[i] = signalsdomain.SignalFeature{Label: f.GetLabel(), Value: f.GetValue()}
	}
	return signalsdomain.CompositeSignalV1{
		Kind:           p.GetKind(),
		Venue:          p.GetVenue(),
		Instrument:     p.GetInstrument(),
		Timeframe:      p.GetTimeframe(),
		TsServer:       p.GetTsServerMs(),
		Severity:       p.GetSeverity(),
		Confidence:     p.GetConfidence(),
		Evidence:       evidence,
		RegimeKind:     p.GetRegimeKind(),
		RegimeStrength: p.GetRegimeStrength(),
		Reason:         p.GetReason(),
		Seq:            p.GetSeq(),
		SourceKinds:    p.GetSourceKinds(),
	}
}
