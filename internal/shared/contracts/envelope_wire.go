package contracts

import (
	"math"
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

// MarshalEnvelopeV1FromDomain marshals a domain envelope projection into envelope.v1 wire bytes.
func MarshalEnvelopeV1FromDomain(env envelope.Envelope) ([]byte, *problem.Problem) {
	if env.Version < 0 || env.Version > math.MaxInt32 {
		return nil, problem.Newf(problem.ValidationFailed, "envelope version out of int32 range: %d", env.Version)
	}
	msg := &envelopev1.Envelope{
		Type:           strings.TrimSpace(env.Type),
		Version:        int32(env.Version),
		Venue:          strings.TrimSpace(env.Venue),
		Instrument:     strings.TrimSpace(env.Instrument),
		TsExchange:     env.TsExchange,
		TsIngest:       env.TsIngest,
		Seq:            env.Seq,
		IdempotencyKey: strings.TrimSpace(env.IdempotencyKey),
		Meta:           env.Meta,
		Payload:        env.Payload,
		ContentType:    strings.TrimSpace(env.ContentType),
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
