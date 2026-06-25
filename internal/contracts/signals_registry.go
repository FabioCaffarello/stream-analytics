package contracts

import (
	signalsdomain "github.com/market-raccoon/internal/core/signals/domain"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/problem"
	signalsv1 "github.com/market-raccoon/internal/shared/proto/gen/signals/v1"
)

// RegisterSignalsPayloadV1 registers runtime payload codecs for composed signals.
func RegisterSignalsPayloadV1(reg *codec.Registry) *problem.Problem {
	if reg == nil {
		return problem.New(problem.ValidationFailed, "codec registry must not be nil")
	}
	return registerPayloadDual(
		reg,
		signalsdomain.CompositeSignalType,
		codec.JSONCodec[signalsdomain.CompositeSignalV1]{},
		domainProtoPayloadCodec[signalsdomain.CompositeSignalV1, *signalsv1.CompositeSignalV1]{
			newProto: func() *signalsv1.CompositeSignalV1 { return &signalsv1.CompositeSignalV1{} },
			toProto:  DomainToProtoCompositeSignalV1,
			toDomain: ProtoToDomainCompositeSignalV1,
		},
	)
}
