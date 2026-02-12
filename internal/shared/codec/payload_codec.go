package codec

import (
	"math"
	"strconv"
	"strings"
	"sync"

	"github.com/market-raccoon/internal/shared/problem"
)

var (
	payloadRegistryMu sync.RWMutex
	payloadRegistry   *Registry
)

// SetPayloadRegistry configures the registry used by EncodePayload/DecodePayload.
func SetPayloadRegistry(reg *Registry) *problem.Problem {
	if reg == nil {
		return problem.New(problem.ValidationFailed, "payload codec registry must not be nil")
	}
	payloadRegistryMu.Lock()
	payloadRegistry = reg
	payloadRegistryMu.Unlock()
	return nil
}

// EncodePayload encodes a domain payload using event schema key + content type.
// Empty contentType defaults to application/json for backward compatibility.
func EncodePayload(eventType string, version int, contentType string, domainPayload any) ([]byte, *problem.Problem) {
	if domainPayload == nil {
		return nil, problem.New(problem.ValidationFailed, "payload must not be nil")
	}
	key, p := payloadSchemaKey(eventType, version, contentType)
	if p != nil {
		return nil, p
	}
	reg, p := getPayloadRegistry()
	if p != nil {
		return nil, p
	}
	enc, ok := reg.Encoder(key)
	if !ok {
		if key.Format == FormatJSON {
			// Backward-compatible fallback for event types that are not yet
			// registered in the typed payload registry.
			return MarshalPayload(eventType, version, domainPayload)
		}
		return nil, missingPayloadCodecProblem("encoder", key)
	}
	data, p := enc.Encode(domainPayload)
	if p != nil {
		return nil, problem.WithDetail(
			problem.WithDetail(
				problem.WithDetail(p, "event_type", key.Type),
				"version", key.Version,
			),
			"content_type", string(key.Format),
		)
	}
	return data, nil
}

// DecodePayload decodes payload bytes using event schema key + content type.
// Empty contentType defaults to application/json for backward compatibility.
func DecodePayload(eventType string, version int, contentType string, payload []byte) (any, *problem.Problem) {
	key, p := payloadSchemaKey(eventType, version, contentType)
	if p != nil {
		return nil, p
	}
	reg, p := getPayloadRegistry()
	if p != nil {
		return nil, p
	}
	dec, ok := reg.Decoder(key)
	if !ok {
		return nil, missingPayloadCodecProblem("decoder", key)
	}
	out, p := dec.Decode(payload)
	if p != nil {
		return nil, problem.WithDetail(
			problem.WithDetail(
				problem.WithDetail(p, "event_type", key.Type),
				"version", key.Version,
			),
			"content_type", string(key.Format),
		)
	}
	return out, nil
}

func getPayloadRegistry() (*Registry, *problem.Problem) {
	payloadRegistryMu.RLock()
	reg := payloadRegistry
	payloadRegistryMu.RUnlock()
	if reg == nil {
		return nil, problem.New(problem.ValidationFailed, "payload codec registry is not configured")
	}
	return reg, nil
}

func payloadSchemaKey(eventType string, version int, contentType string) (SchemaKey, *problem.Problem) {
	if strings.TrimSpace(eventType) == "" {
		return SchemaKey{}, problem.WithDetail(
			problem.New(problem.ValidationFailed, "event_type must not be empty"),
			"field", "event_type",
		)
	}
	if version < 1 || version > math.MaxInt32 {
		return SchemaKey{}, problem.WithDetail(
			problem.WithDetail(
				problem.Newf(problem.ValidationFailed, "version must be in [1,%d], got %d", math.MaxInt32, version),
				"field", "version",
			),
			"value", version,
		)
	}
	int32Version, p := payloadSchemaVersion(version)
	if p != nil {
		return SchemaKey{}, p
	}
	format, p := payloadFormat(contentType)
	if p != nil {
		return SchemaKey{}, p
	}
	return SchemaKey{
		Type:    strings.TrimSpace(eventType),
		Version: int32Version,
		Format:  format,
	}, nil
}

func payloadSchemaVersion(version int) (int32, *problem.Problem) {
	v64, err := strconv.ParseInt(strconv.Itoa(version), 10, 32)
	if err != nil {
		return 0, problem.WithDetail(
			problem.WithDetail(
				problem.Wrap(err, problem.ValidationFailed, "version conversion failed"),
				"field", "version",
			),
			"value", version,
		)
	}
	return int32(v64), nil
}

func payloadFormat(contentType string) (Format, *problem.Problem) {
	switch strings.ToLower(strings.TrimSpace(contentType)) {
	case "", string(FormatJSON):
		return FormatJSON, nil
	case string(FormatProto):
		return FormatProto, nil
	default:
		return "", problem.WithDetail(
			problem.WithDetail(
				problem.Newf(problem.ValidationFailed, "unsupported payload content_type %q", contentType),
				"field", "content_type",
			),
			"value", contentType,
		)
	}
}

func missingPayloadCodecProblem(kind string, key SchemaKey) *problem.Problem {
	return problem.WithDetail(
		problem.WithDetail(
			problem.WithDetail(
				problem.Newf(problem.ValidationFailed, "no payload %s registered for type=%q version=%d format=%q", kind, key.Type, key.Version, key.Format),
				"type", key.Type,
			),
			"version", key.Version,
		),
		"format", key.Format,
	)
}
