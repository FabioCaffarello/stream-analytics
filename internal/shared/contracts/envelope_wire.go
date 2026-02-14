package contracts

import (
	"strings"

	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/problem"
	envelopev1 "github.com/market-raccoon/internal/shared/proto/gen/envelope/v1"
	"google.golang.org/protobuf/proto"
)

// MarshalEnvelopeV1FromPayload builds an envelope.v1 wrapper and marshals it to protobuf wire bytes.
func MarshalEnvelopeV1FromPayload(eventType string, payload []byte, contentType string) ([]byte, *problem.Problem) {
	msg := &envelopev1.Envelope{
		Type:        strings.TrimSpace(eventType),
		Version:     1,
		Payload:     payload,
		ContentType: strings.TrimSpace(contentType),
	}
	raw, err := proto.Marshal(msg)
	if err != nil {
		return nil, problem.Wrap(err, problem.ValidationFailed, "marshal envelope.v1 failed")
	}
	return raw, nil
}

// UnmarshalEnvelopeV1ToDomain decodes protobuf envelope.v1 bytes into the domain envelope projection.
func UnmarshalEnvelopeV1ToDomain(raw []byte) (envelope.Envelope, *problem.Problem) {
	var msg envelopev1.Envelope
	if err := proto.Unmarshal(raw, &msg); err != nil {
		return envelope.Envelope{}, problem.Wrap(err, problem.ValidationFailed, "unmarshal envelope.v1 failed")
	}
	return envelope.Envelope{
		Type:           msg.GetType(),
		Version:        int(msg.GetVersion()),
		Venue:          msg.GetVenue(),
		Instrument:     msg.GetInstrument(),
		TsExchange:     msg.GetTsExchange(),
		TsIngest:       msg.GetTsIngest(),
		Seq:            msg.GetSeq(),
		IdempotencyKey: msg.GetIdempotencyKey(),
		Meta:           msg.GetMeta(),
		Payload:        msg.GetPayload(),
		ContentType:    msg.GetContentType(),
	}, nil
}
