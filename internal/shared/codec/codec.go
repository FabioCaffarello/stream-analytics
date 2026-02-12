// Package codec provides serialization utilities for event payloads.
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
// context to any error returned. This helper keeps JSON as default runtime path.
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
// context to any error returned. This helper keeps JSON as default runtime path.
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
