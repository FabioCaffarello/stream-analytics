package replay

import (
	"encoding/json"

	"github.com/market-raccoon/internal/shared/envelope"
)

// FixtureRecord is one replay fixture row after decode/validation.
//
// For JSON fixtures, Envelope.Payload is the canonical payload JSON bytes and
// PayloadJSON is populated.
//
// For protobuf fixtures, Envelope.Payload is the exact decoded bytes from
// PayloadB64 and PayloadB64 is populated.
type FixtureRecord struct {
	Subject     string
	Envelope    envelope.Envelope
	PayloadJSON json.RawMessage
	PayloadB64  string
	SHA256      string
}

type fixtureBase struct {
	Subject     string
	Envelope    envelope.Envelope
	ContentType string
	PayloadJSON json.RawMessage
	PayloadB64  string
}

type fixtureLine struct {
	Subject     string            `json:"subject"`
	Envelope    envelope.Envelope `json:"envelope"`
	ContentType string            `json:"content_type"`
	PayloadJSON json.RawMessage   `json:"payload_json,omitempty"`
	PayloadB64  string            `json:"payload_b64,omitempty"`
	SHA256      string            `json:"sha256"`
}
