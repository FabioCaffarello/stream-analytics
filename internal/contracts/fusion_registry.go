package contracts

import (
	"github.com/FabioCaffarello/stream-analytics/internal/shared/codec"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

const (
	fusionEventTypeFusedDepth               = "aggregation.fused_depth"
	fusionEventTypeFusedVolumeProfile       = "insights.fused_volume_profile_snapshot"
	fusionEventTypeFusedHeatmap             = "insights.fused_heatmap_snapshot"
	fusionV1Version                   int32 = 1
)

// RegisterFusionPayloadV1 registers runtime payload codecs for fusion events.
func RegisterFusionPayloadV1(reg *codec.Registry) *problem.Problem {
	if reg == nil {
		return problem.New(problem.ValidationFailed, "codec registry must not be nil")
	}

	if p := reg.Register(codec.SchemaKey{
		Type:    fusionEventTypeFusedDepth,
		Version: fusionV1Version,
		Format:  codec.FormatJSON,
	}, codec.JSONCodec[FusedDepthSnapshotV1]{}, codec.JSONCodec[FusedDepthSnapshotV1]{}); p != nil {
		return p
	}

	if p := reg.Register(codec.SchemaKey{
		Type:    fusionEventTypeFusedVolumeProfile,
		Version: fusionV1Version,
		Format:  codec.FormatJSON,
	}, codec.JSONCodec[FusedVolumeProfileSnapshotV1]{}, codec.JSONCodec[FusedVolumeProfileSnapshotV1]{}); p != nil {
		return p
	}

	if p := reg.Register(codec.SchemaKey{
		Type:    fusionEventTypeFusedHeatmap,
		Version: fusionV1Version,
		Format:  codec.FormatJSON,
	}, codec.JSONCodec[FusedHeatmapArtifactV1]{}, codec.JSONCodec[FusedHeatmapArtifactV1]{}); p != nil {
		return p
	}

	return nil
}
