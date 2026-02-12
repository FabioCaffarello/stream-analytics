package codec

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"sync"

	"github.com/market-raccoon/internal/shared/problem"
)

var (
	payloadRegistryMu       sync.RWMutex
	payloadRegistry         *Registry
	payloadFallbackPolicyMu sync.RWMutex
	payloadFallbackPolicy   = FallbackPolicyAllowUnknownJSON
)

// FallbackPolicy defines unknown event-type handling when content_type resolves to JSON.
type FallbackPolicy string

const (
	// FallbackPolicyAllowUnknownJSON keeps backward-compatible JSON decoding/encoding
	// for event types that are not yet present in the typed payload registry.
	FallbackPolicyAllowUnknownJSON FallbackPolicy = "allow_unknown_json"
	// FallbackPolicyRejectUnknown disables unknown event-type fallback.
	FallbackPolicyRejectUnknown FallbackPolicy = "reject_unknown"
)

const (
	reasonUnknownContentType        = "validation_failed_unknown_content_type"
	reasonUnknownEventTypeProto     = "validation_failed_unknown_event_type_proto"
	reasonUnknownEventTypeRejected  = "validation_failed_unknown_event_type_rejected"
	reasonMissingPayloadCodec       = "validation_failed_missing_payload_codec"
	reasonInvalidFallbackPolicy     = "validation_failed_invalid_fallback_policy"
	reasonUnknownJSONFallbackFailed = "validation_failed_unknown_event_type_json_fallback_decode"
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

// SetFallbackPolicy configures unknown event-type behavior for JSON payloads.
func SetFallbackPolicy(policy FallbackPolicy) *problem.Problem {
	if !policy.valid() {
		return problem.WithDetail(
			problem.WithDetail(
				problem.Newf(problem.ValidationFailed, "unsupported payload fallback policy %q", policy),
				"field", "fallback_policy",
			),
			"reason", reasonInvalidFallbackPolicy,
		)
	}
	payloadFallbackPolicyMu.Lock()
	payloadFallbackPolicy = policy
	payloadFallbackPolicyMu.Unlock()
	return nil
}

// FallbackPolicyValue returns the currently configured unknown-event fallback policy.
func FallbackPolicyValue() FallbackPolicy {
	payloadFallbackPolicyMu.RLock()
	p := payloadFallbackPolicy
	payloadFallbackPolicyMu.RUnlock()
	return p
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
			switch FallbackPolicyValue() {
			case FallbackPolicyAllowUnknownJSON:
				// Backward-compatible fallback for event types that are not yet
				// registered in the typed payload registry.
				return MarshalPayload(eventType, version, domainPayload)
			case FallbackPolicyRejectUnknown:
				return nil, unknownJSONEventTypeRejectedProblem(key)
			}
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
		if key.Format == FormatJSON {
			switch FallbackPolicyValue() {
			case FallbackPolicyAllowUnknownJSON:
				return decodeUnknownJSONPayload(key, payload)
			case FallbackPolicyRejectUnknown:
				return nil, unknownJSONEventTypeRejectedProblem(key)
			}
		}
		if key.Format == FormatProto && !registryHasAnyCodecForTypeVersion(reg, key.Type, key.Version) {
			return nil, unknownProtoEventTypeProblem(key)
		}
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

func decodeUnknownJSONPayload(key SchemaKey, payload []byte) (any, *problem.Problem) {
	var out any
	if err := json.Unmarshal(payload, &out); err != nil {
		return nil, problem.WithDetail(
			problem.WithDetail(
				problem.WithDetail(
					problem.Wrap(err, problem.ValidationFailed, "unknown event_type JSON fallback decode failed"),
					"event_type", key.Type,
				),
				"version", key.Version,
			),
			"reason", reasonUnknownJSONFallbackFailed,
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
				problem.WithDetail(
					problem.Newf(problem.ValidationFailed, "unsupported payload content_type %q", contentType),
					"field", "content_type",
				),
				"value", contentType,
			),
			"reason", reasonUnknownContentType,
		)
	}
}

func missingPayloadCodecProblem(kind string, key SchemaKey) *problem.Problem {
	return problem.WithDetail(
		problem.WithDetail(
			problem.WithDetail(
				problem.WithDetail(
					problem.Newf(problem.ValidationFailed, "no payload %s registered for type=%q version=%d format=%q", kind, key.Type, key.Version, key.Format),
					"type", key.Type,
				),
				"version", key.Version,
			),
			"format", key.Format,
		),
		"reason", reasonMissingPayloadCodec,
	)
}

func unknownProtoEventTypeProblem(key SchemaKey) *problem.Problem {
	return problem.WithDetail(
		problem.WithDetail(
			problem.WithDetail(
				problem.WithDetail(
					problem.Newf(problem.ValidationFailed, "unknown protobuf event_type %q version=%d", key.Type, key.Version),
					"type", key.Type,
				),
				"version", key.Version,
			),
			"content_type", string(key.Format),
		),
		"reason", reasonUnknownEventTypeProto,
	)
}

func unknownJSONEventTypeRejectedProblem(key SchemaKey) *problem.Problem {
	return problem.WithDetail(
		problem.WithDetail(
			problem.WithDetail(
				problem.WithDetail(
					problem.Newf(problem.ValidationFailed, "unknown JSON event_type %q version=%d with fallback policy reject_unknown", key.Type, key.Version),
					"type", key.Type,
				),
				"version", key.Version,
			),
			"content_type", string(key.Format),
		),
		"reason", reasonUnknownEventTypeRejected,
	)
}

func registryHasAnyCodecForTypeVersion(reg *Registry, eventType string, version int32) bool {
	if reg == nil {
		return false
	}
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	for key := range reg.decoders {
		if key.Type == eventType && key.Version == version {
			return true
		}
	}
	for key := range reg.encoders {
		if key.Type == eventType && key.Version == version {
			return true
		}
	}
	return false
}

func (p FallbackPolicy) valid() bool {
	switch p {
	case FallbackPolicyAllowUnknownJSON, FallbackPolicyRejectUnknown:
		return true
	default:
		return false
	}
}
