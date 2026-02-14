package contracts

import (
	insightsdomain "github.com/market-raccoon/internal/core/insights/domain"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/problem"
	insightsv1 "github.com/market-raccoon/internal/shared/proto/gen/insights/v1"
)

const insightsV1Version int32 = 1

type InsightsCodecOptions struct {
	EnableVolumeProfileSnapshotProto bool
}

// RegisterInsightsV1 registers insights v1 payload codecs.
func RegisterInsightsV1(reg *codec.Registry) *problem.Problem {
	return RegisterInsightsPayloadV1WithOptions(reg, InsightsCodecOptions{})
}

// RegisterInsightsPayloadV1 registers runtime payload codecs for insights events.
func RegisterInsightsPayloadV1(reg *codec.Registry) *problem.Problem {
	return RegisterInsightsPayloadV1WithOptions(reg, InsightsCodecOptions{})
}

// RegisterInsightsPayloadV1WithOptions registers runtime payload codecs for insights events
// with optional protobuf contracts gated by explicit opt-in flags.
func RegisterInsightsPayloadV1WithOptions(reg *codec.Registry, opts InsightsCodecOptions) *problem.Problem {
	if reg == nil {
		return problem.New(problem.ValidationFailed, "codec registry must not be nil")
	}
	if p := reg.Register(codec.SchemaKey{
		Type:    insightsdomain.CrossVenueTradeSnapshotType,
		Version: insightsV1Version,
		Format:  codec.FormatJSON,
	}, codec.JSONCodec[insightsdomain.CrossVenueTradeSnapshotV1]{}, codec.JSONCodec[insightsdomain.CrossVenueTradeSnapshotV1]{}); p != nil {
		return p
	}
	if p := reg.Register(codec.SchemaKey{
		Type:    insightsdomain.CrossVenueSpreadSignalType,
		Version: insightsV1Version,
		Format:  codec.FormatJSON,
	}, codec.JSONCodec[insightsdomain.CrossVenueSpreadSignalV1]{}, codec.JSONCodec[insightsdomain.CrossVenueSpreadSignalV1]{}); p != nil {
		return p
	}
	if p := reg.Register(codec.SchemaKey{
		Type:    insightsdomain.HeatmapSnapshotType,
		Version: insightsV1Version,
		Format:  codec.FormatJSON,
	}, codec.JSONCodec[insightsdomain.HeatmapArtifactV1]{}, codec.JSONCodec[insightsdomain.HeatmapArtifactV1]{}); p != nil {
		return p
	}
	if p := reg.Register(codec.SchemaKey{
		Type:    insightsdomain.VolumeProfileSnapshotType,
		Version: insightsV1Version,
		Format:  codec.FormatJSON,
	}, codec.JSONCodec[insightsdomain.VolumeProfileSnapshotV1]{}, codec.JSONCodec[insightsdomain.VolumeProfileSnapshotV1]{}); p != nil {
		return p
	}
	if !opts.EnableVolumeProfileSnapshotProto {
		return nil
	}
	vpvrCodec := domainProtoPayloadCodec[insightsdomain.VolumeProfileSnapshotV1, *insightsv1.VolumeProfileSnapshotV1]{
		newProto: func() *insightsv1.VolumeProfileSnapshotV1 { return &insightsv1.VolumeProfileSnapshotV1{} },
		toProto:  DomainToProtoVolumeProfileSnapshotV1,
		toDomain: ProtoToDomainVolumeProfileSnapshotV1,
	}
	return reg.Register(codec.SchemaKey{
		Type:    insightsdomain.VolumeProfileSnapshotType,
		Version: insightsV1Version,
		Format:  codec.FormatProto,
	}, vpvrCodec, vpvrCodec)
}
