package envelope

import (
	"math"
	"strings"

	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/problem"
)

// MarshalPayload encodes payload using the codec registered for envelope schema key.
func MarshalPayload(reg *codec.Registry, env Envelope, payload any) ([]byte, *problem.Problem) {
	if reg == nil {
		return nil, problem.New(problem.ValidationFailed, "codec registry must not be nil")
	}
	key, p := schemaKeyFromEnvelope(env)
	if p != nil {
		return nil, p
	}
	enc, ok := reg.Encoder(key)
	if !ok {
		return nil, missingCodecProblem("encoder", key)
	}
	return enc.Encode(payload)
}

// UnmarshalPayload decodes payload bytes using the codec registered for envelope schema key.
func UnmarshalPayload(reg *codec.Registry, env Envelope, b []byte) (any, *problem.Problem) {
	if reg == nil {
		return nil, problem.New(problem.ValidationFailed, "codec registry must not be nil")
	}
	key, p := schemaKeyFromEnvelope(env)
	if p != nil {
		return nil, p
	}
	dec, ok := reg.Decoder(key)
	if !ok {
		return nil, missingCodecProblem("decoder", key)
	}
	return dec.Decode(b)
}

func schemaKeyFromEnvelope(env Envelope) (codec.SchemaKey, *problem.Problem) {
	if strings.TrimSpace(env.Type) == "" {
		return codec.SchemaKey{}, problem.WithDetail(
			problem.New(problem.ValidationFailed, "envelope type must not be empty"),
			"field", "type",
		)
	}
	if env.Version < 1 || env.Version > math.MaxInt32 {
		return codec.SchemaKey{}, problem.WithDetail(
			problem.Newf(problem.ValidationFailed, "envelope version must be in [1,%d], got %d", math.MaxInt32, env.Version),
			"field", "version",
		)
	}
	contentType, p := NormalizeContentType(env.ContentType)
	if p != nil {
		return codec.SchemaKey{}, p
	}
	return codec.SchemaKey{
		Type:    strings.TrimSpace(env.Type),
		Version: int32(env.Version),
		Format:  codec.Format(contentType),
	}, nil
}

func missingCodecProblem(kind string, key codec.SchemaKey) *problem.Problem {
	return problem.WithDetail(
		problem.WithDetail(
			problem.WithDetail(
				problem.Newf(problem.ValidationFailed, "no %s registered for type=%q version=%d format=%q", kind, key.Type, key.Version, key.Format),
				"type", key.Type,
			),
			"version", key.Version,
		),
		"format", key.Format,
	)
}
