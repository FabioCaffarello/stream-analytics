package contracts

import (
	marketmodel "github.com/market-raccoon/internal/core/marketmodel"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/problem"
	marketmodelv1 "github.com/market-raccoon/internal/shared/proto/gen/marketmodel/v1"
)

const signalEngineEventType = "signal.event"

func RegisterSignalEnginePayloadV1(reg *codec.Registry) *problem.Problem {
	if reg == nil {
		return problem.New(problem.ValidationFailed, "codec registry must not be nil")
	}
	return registerPayloadDual(
		reg,
		signalEngineEventType,
		codec.JSONCodec[marketmodel.SignalEvent]{},
		domainProtoPayloadCodec[marketmodel.SignalEvent, *marketmodelv1.SignalEvent]{
			newProto: func() *marketmodelv1.SignalEvent { return &marketmodelv1.SignalEvent{} },
			toProto:  DomainToProtoSignalEventV1,
			toDomain: ProtoToDomainSignalEventV1,
		},
	)
}
