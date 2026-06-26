package contracts

import (
	"github.com/FabioCaffarello/stream-analytics/internal/shared/codec"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
	envelopev1 "github.com/FabioCaffarello/stream-analytics/internal/shared/proto/gen/envelope/v1"
)

// RegisterEnvelopeV1 registers envelope v1 protobuf codec capability.
func RegisterEnvelopeV1(reg *codec.Registry) *problem.Problem {
	if reg == nil {
		return problem.New(problem.ValidationFailed, "codec registry must not be nil")
	}
	c := codec.ProtoCodec[*envelopev1.Envelope]{
		New: func() *envelopev1.Envelope { return &envelopev1.Envelope{} },
	}
	return reg.Register(codec.SchemaKey{
		Type:    "envelope",
		Version: 1,
		Format:  codec.FormatProto,
	}, c, c)
}
