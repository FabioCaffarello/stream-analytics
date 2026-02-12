package contracts

import (
	insightsdomain "github.com/market-raccoon/internal/core/insights/domain"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/problem"
)

const insightsV1Version int32 = 1

// RegisterInsightsV1 registers insights v1 payload codecs for JSON.
// Protobuf is intentionally deferred for W10-1.
func RegisterInsightsV1(reg *codec.Registry) *problem.Problem {
	return RegisterInsightsPayloadV1(reg)
}

// RegisterInsightsPayloadV1 registers runtime payload codecs for insights events.
func RegisterInsightsPayloadV1(reg *codec.Registry) *problem.Problem {
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
	return reg.Register(codec.SchemaKey{
		Type:    insightsdomain.CrossVenueSpreadSignalType,
		Version: insightsV1Version,
		Format:  codec.FormatJSON,
	}, codec.JSONCodec[insightsdomain.CrossVenueSpreadSignalV1]{}, codec.JSONCodec[insightsdomain.CrossVenueSpreadSignalV1]{})
}
