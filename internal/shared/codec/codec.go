// Package codec provides serialization utilities for event payloads.
//
// The default implementation uses encoding/json. When the project adds
// github.com/fxamacker/cbor/v2 as a dependency, swap Marshal/Unmarshal
// to use it — the API is identical.
//
// The registry maps (EventType, Version) → Decoder so consumers can decode
// raw payload bytes without knowing the concrete type up-front.
package codec

import (
	"encoding/json"
	"fmt"

	"github.com/market-raccoon/internal/shared/problem"
)

// Marshal serializes v to bytes using the canonical codec (JSON for now).
func Marshal(v any) ([]byte, *problem.Problem) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, problem.Wrap(err, problem.Internal,
			fmt.Sprintf("codec: marshal failed: %T", v))
	}
	return data, nil
}

// Unmarshal deserializes data into out using the canonical codec.
// out must be a non-nil pointer.
func Unmarshal(data []byte, out any) *problem.Problem {
	if err := json.Unmarshal(data, out); err != nil {
		return problem.Wrap(err, problem.Internal, "codec: unmarshal failed")
	}
	return nil
}

// MarshalPayload serializes a typed payload and attaches event_type/version
// context to any error returned. Use this in the ingest pipeline so that
// failures always carry routing context.
func MarshalPayload(eventType string, version int, v any) ([]byte, *problem.Problem) {
	data, p := Marshal(v)
	if p != nil {
		return nil, problem.WithDetail(
			problem.WithDetail(
				problem.WithDetail(p, "event_type", eventType),
				"version", version,
			),
			"payload_type", fmt.Sprintf("%T", v),
		)
	}
	return data, nil
}

// UnmarshalPayload deserializes raw bytes and attaches event_type/version/size
// context to any error returned.
func UnmarshalPayload(eventType string, version int, data []byte, out any) *problem.Problem {
	if p := Unmarshal(data, out); p != nil {
		return problem.WithDetail(
			problem.WithDetail(
				problem.WithDetail(p, "event_type", eventType),
				"version", version,
			),
			"payload_size", len(data),
		)
	}
	return nil
}

// RegistryKey uniquely identifies a payload schema by type and version.
type RegistryKey struct {
	EventType string
	Version   int
}

// Decoder is a function that deserializes raw bytes into a typed value.
type Decoder func([]byte) (any, *problem.Problem)

// Registry maps RegistryKeys to Decoder functions.
// It is safe for concurrent reads after registration (write during init only).
type Registry struct {
	decoders map[RegistryKey]Decoder
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{decoders: make(map[RegistryKey]Decoder)}
}

// Register adds a Decoder for the given eventType and version.
// Registering the same key twice overwrites the previous entry.
func (r *Registry) Register(eventType string, version int, d Decoder) {
	r.decoders[RegistryKey{EventType: eventType, Version: version}] = d
}

// Decode looks up the Decoder for (eventType, version) and decodes data.
// Returns problem.NotFound if no decoder is registered for the key.
func (r *Registry) Decode(eventType string, version int, data []byte) (any, *problem.Problem) {
	d, ok := r.decoders[RegistryKey{EventType: eventType, Version: version}]
	if !ok {
		return nil, problem.WithDetail(
			problem.WithDetail(
				problem.Newf(problem.NotFound,
					"no decoder registered for event_type=%q version=%d", eventType, version),
				"event_type", eventType,
			),
			"version", version,
		)
	}
	return d(data)
}
