package contracts

import (
	evidencedomain "github.com/market-raccoon/internal/core/evidence/domain"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/problem"
	evidencev1 "github.com/market-raccoon/internal/shared/proto/gen/evidence/v1"
)

const evidenceV1Version int32 = 1

// RegisterEvidencePayloadV1 registers runtime payload codecs for evidence events.
func RegisterEvidencePayloadV1(reg *codec.Registry) *problem.Problem {
	if reg == nil {
		return problem.New(problem.ValidationFailed, "codec registry must not be nil")
	}
	if p := reg.Register(codec.SchemaKey{
		Type:    evidencedomain.MicrostructureEvidenceType,
		Version: evidenceV1Version,
		Format:  codec.FormatJSON,
	}, codec.JSONCodec[evidencedomain.EvidenceEvent]{}, codec.JSONCodec[evidencedomain.EvidenceEvent]{}); p != nil {
		return p
	}
	protoCodec := domainProtoPayloadCodec[evidencedomain.EvidenceEvent, *evidencev1.MicrostructureEvidenceV1]{
		newProto: func() *evidencev1.MicrostructureEvidenceV1 {
			return &evidencev1.MicrostructureEvidenceV1{}
		},
		toProto:  DomainToProtoEvidenceV1,
		toDomain: ProtoToDomainEvidenceV1,
	}
	return reg.Register(codec.SchemaKey{
		Type:    evidencedomain.MicrostructureEvidenceType,
		Version: evidenceV1Version,
		Format:  codec.FormatProto,
	}, protoCodec, protoCodec)
}
