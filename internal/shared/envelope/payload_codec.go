package envelope

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/FabioCaffarello/stream-analytics/internal/shared/codec"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
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
	version, p := schemaKeyVersion(env.Version)
	if p != nil {
		return codec.SchemaKey{}, p
	}
	contentType, p := NormalizeContentType(env.ContentType)
	if p != nil {
		return codec.SchemaKey{}, p
	}
	return codec.SchemaKey{
		Type:    strings.TrimSpace(env.Type),
		Version: version,
		Format:  codec.Format(contentType),
	}, nil
}

func schemaKeyVersion(version int) (int32, *problem.Problem) {
	// Parse through decimal text into int32 to avoid unsafe int -> int32 narrowing.
	var out int32
	if _, err := fmt.Sscanf(strconv.Itoa(version), "%d", &out); err != nil {
		return 0, problem.WithDetail(
			problem.WithDetail(
				problem.Wrap(err, problem.ValidationFailed, "envelope version conversion failed"),
				"field", "version",
			),
			"value", version,
		)
	}
	return out, nil
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
