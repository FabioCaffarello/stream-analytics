package contracts

import (
	"strings"

	evidencedomain "github.com/market-raccoon/internal/core/evidence/domain"
	marketmodel "github.com/market-raccoon/internal/core/marketmodel"
	marketmodelv1 "github.com/market-raccoon/internal/shared/proto/gen/marketmodel/v1"
)

func DomainToProtoSignalEventV1(d marketmodel.SignalEvent) *marketmodelv1.SignalEvent {
	features := make([]*marketmodelv1.SignalFeature, len(d.Features))
	for i := range d.Features {
		features[i] = &marketmodelv1.SignalFeature{
			Key:   d.Features[i].Key,
			Value: d.Features[i].Value,
		}
	}
	watermarks := make([]*marketmodelv1.SignalInputSeqRange, len(d.InputWatermark))
	for i := range d.InputWatermark {
		watermarks[i] = &marketmodelv1.SignalInputSeqRange{
			Venue:    d.InputWatermark[i].Venue,
			Symbol:   d.InputWatermark[i].Symbol,
			SeqStart: d.InputWatermark[i].SeqStart,
			SeqEnd:   d.InputWatermark[i].SeqEnd,
		}
	}
	return &marketmodelv1.SignalEvent{
		Type:           d.Type,
		TsServer:       d.TsServer,
		Scope:          marketmodelv1.SignalScope(d.Scope.ProtoValue()),
		Venue:          d.Venue,
		Symbol:         d.Symbol,
		Severity:       domainSeverityToProto(evidencedomain.Severity(d.Severity)),
		Confidence:     d.Confidence,
		Features:       features,
		Explanation:    d.Explanation,
		SignalId:       d.SignalID,
		RuleId:         d.RuleID,
		RuleVersion:    d.RuleVersion,
		Explain:        append([]string(nil), d.Explain...),
		InputWatermark: watermarks,
		CorrelationId:  d.CorrelationID,
		CorrelationIds: append([]string(nil), d.CorrelationIDs...),
	}
}

func ProtoToDomainSignalEventV1(p *marketmodelv1.SignalEvent) marketmodel.SignalEvent {
	if p == nil {
		return marketmodel.SignalEvent{}
	}
	features := make([]marketmodel.SignalFeature, len(p.GetFeatures()))
	for i := range p.GetFeatures() {
		features[i] = marketmodel.SignalFeature{
			Key:   p.GetFeatures()[i].GetKey(),
			Value: p.GetFeatures()[i].GetValue(),
		}
	}
	watermarks := make([]marketmodel.SignalInputSeqRange, len(p.GetInputWatermark()))
	for i := range p.GetInputWatermark() {
		watermarks[i] = marketmodel.SignalInputSeqRange{
			Venue:    p.GetInputWatermark()[i].GetVenue(),
			Symbol:   p.GetInputWatermark()[i].GetSymbol(),
			SeqStart: p.GetInputWatermark()[i].GetSeqStart(),
			SeqEnd:   p.GetInputWatermark()[i].GetSeqEnd(),
		}
	}
	explain := append([]string(nil), p.GetExplain()...)
	if len(explain) == 0 && strings.TrimSpace(p.GetExplanation()) != "" {
		explain = []string{strings.TrimSpace(p.GetExplanation())}
	}
	correlationIDs := append([]string(nil), p.GetCorrelationIds()...)
	if len(correlationIDs) == 0 && strings.TrimSpace(p.GetCorrelationId()) != "" {
		correlationIDs = []string{strings.TrimSpace(p.GetCorrelationId())}
	}
	return marketmodel.SignalEvent{
		Type:           p.GetType(),
		TsServer:       p.GetTsServer(),
		Scope:          marketmodel.SignalScopeFromProtoValue(int32(p.GetScope())),
		Venue:          p.GetVenue(),
		Symbol:         p.GetSymbol(),
		Severity:       string(protoSeverityToDomain(p.GetSeverity())),
		Confidence:     p.GetConfidence(),
		Features:       features,
		Explanation:    p.GetExplanation(),
		SignalID:       p.GetSignalId(),
		RuleID:         p.GetRuleId(),
		RuleVersion:    p.GetRuleVersion(),
		Explain:        explain,
		InputWatermark: watermarks,
		CorrelationID:  p.GetCorrelationId(),
		CorrelationIDs: correlationIDs,
	}
}
