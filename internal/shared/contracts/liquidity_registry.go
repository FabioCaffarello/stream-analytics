package contracts

import (
	"strings"

	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/problem"
	liquidityv1 "github.com/market-raccoon/internal/shared/proto/gen/liquidity/v1"
)

const liquidityEventTypeEvidence = "liquidity.evidence"

// RegisterLiquidityPayloadV1 registers runtime payload codecs for liquidity evidence.
func RegisterLiquidityPayloadV1(reg *codec.Registry) *problem.Problem {
	if reg == nil {
		return problem.New(problem.ValidationFailed, "codec registry must not be nil")
	}
	if p := registerPayloadDual(
		reg,
		liquidityEventTypeEvidence,
		codec.JSONCodec[LiquidityEvidenceV1]{},
		domainProtoPayloadCodec[LiquidityEvidenceV1, *liquidityv1.LiquidityEvidenceV1]{
			newProto: func() *liquidityv1.LiquidityEvidenceV1 { return &liquidityv1.LiquidityEvidenceV1{} },
			toProto:  WireDTOToProtoLiquidityEvidenceV1,
			toDomain: ProtoToWireDTOLiquidityEvidenceV1,
		},
	); p != nil {
		return p
	}
	return nil
}

// WireDTOToProtoLiquidityEvidenceV1 converts a wire DTO to protobuf message.
func WireDTOToProtoLiquidityEvidenceV1(in LiquidityEvidenceV1) *liquidityv1.LiquidityEvidenceV1 {
	metrics := make([]*liquidityv1.EvidenceMetric, len(in.Metrics))
	for i := range in.Metrics {
		metrics[i] = &liquidityv1.EvidenceMetric{
			Key:   in.Metrics[i].Key,
			Value: in.Metrics[i].Value,
		}
	}
	explain := make([]string, len(in.Explain))
	copy(explain, in.Explain)
	return &liquidityv1.LiquidityEvidenceV1{
		EvidenceType: in.EvidenceType,
		TsIngestMs:   in.TsIngestMs,
		Venue:        in.Venue,
		Symbol:       in.Symbol,
		WindowMs:     in.WindowMs,
		Severity:     severityToProto(in.Severity),
		Confidence:   in.Confidence,
		Metrics:      metrics,
		Explain:      explain,
		Version:      in.Version,
		StreamId:     in.StreamID,
		Seq:          in.Seq,
		Watermark: &liquidityv1.InputWatermark{
			SeqStart: in.Watermark.SeqStart,
			SeqEnd:   in.Watermark.SeqEnd,
		},
	}
}

// ProtoToWireDTOLiquidityEvidenceV1 converts a protobuf message to wire DTO.
func ProtoToWireDTOLiquidityEvidenceV1(in *liquidityv1.LiquidityEvidenceV1) LiquidityEvidenceV1 {
	if in == nil {
		return LiquidityEvidenceV1{}
	}
	metrics := make([]LiquidityEvidenceMetric, len(in.GetMetrics()))
	for i := range in.GetMetrics() {
		metrics[i] = LiquidityEvidenceMetric{
			Key:   in.GetMetrics()[i].GetKey(),
			Value: in.GetMetrics()[i].GetValue(),
		}
	}
	explain := make([]string, len(in.GetExplain()))
	copy(explain, in.GetExplain())
	out := LiquidityEvidenceV1{
		EvidenceType: in.GetEvidenceType(),
		TsIngestMs:   in.GetTsIngestMs(),
		Venue:        in.GetVenue(),
		Symbol:       in.GetSymbol(),
		WindowMs:     in.GetWindowMs(),
		Severity:     severityFromProto(in.GetSeverity()),
		Confidence:   in.GetConfidence(),
		Metrics:      metrics,
		Explain:      explain,
		Version:      in.GetVersion(),
		StreamID:     in.GetStreamId(),
		Seq:          in.GetSeq(),
	}
	if wm := in.GetWatermark(); wm != nil {
		out.Watermark = LiquidityInputWatermark{
			SeqStart: wm.GetSeqStart(),
			SeqEnd:   wm.GetSeqEnd(),
		}
	}
	return out
}

func severityToProto(in string) liquidityv1.LiquiditySeverity {
	switch strings.ToLower(strings.TrimSpace(in)) {
	case "low":
		return liquidityv1.LiquiditySeverity_LIQUIDITY_SEVERITY_LOW
	case "medium":
		return liquidityv1.LiquiditySeverity_LIQUIDITY_SEVERITY_MEDIUM
	case "high":
		return liquidityv1.LiquiditySeverity_LIQUIDITY_SEVERITY_HIGH
	case "critical":
		return liquidityv1.LiquiditySeverity_LIQUIDITY_SEVERITY_CRITICAL
	default:
		return liquidityv1.LiquiditySeverity_LIQUIDITY_SEVERITY_UNSPECIFIED
	}
}

func severityFromProto(in liquidityv1.LiquiditySeverity) string {
	switch in {
	case liquidityv1.LiquiditySeverity_LIQUIDITY_SEVERITY_LOW:
		return "low"
	case liquidityv1.LiquiditySeverity_LIQUIDITY_SEVERITY_MEDIUM:
		return "medium"
	case liquidityv1.LiquiditySeverity_LIQUIDITY_SEVERITY_HIGH:
		return "high"
	case liquidityv1.LiquiditySeverity_LIQUIDITY_SEVERITY_CRITICAL:
		return "critical"
	default:
		return "low"
	}
}
