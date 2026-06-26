package envelope

import (
	"encoding/json"
	"strings"

	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

// MarshalBinary serializes the canonical envelope as JSON bytes for bus transport.
func MarshalBinary(env Envelope) ([]byte, *problem.Problem) {
	if p := env.Validate(); p != nil {
		return nil, p
	}
	b, err := json.Marshal(env)
	if err != nil {
		return nil, problem.Wrap(err, problem.Internal, "envelope marshal binary failed")
	}
	return b, nil
}

// UnmarshalBinary parses JSON bytes into Envelope and validates invariants.
func UnmarshalBinary(data []byte) (Envelope, *problem.Problem) {
	if strings.TrimSpace(string(data)) == "" {
		return Envelope{}, problem.New(problem.ValidationFailed, "envelope binary payload must not be empty")
	}

	var env Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return Envelope{}, problem.Wrap(err, problem.ValidationFailed, "envelope unmarshal binary failed")
	}
	if p := env.Validate(); p != nil {
		return Envelope{}, p
	}
	return env, nil
}
