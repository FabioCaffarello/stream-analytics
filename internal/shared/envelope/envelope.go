// Package envelope defines the canonical event envelope (ADR-0002).
// All events flowing through the system are wrapped in an Envelope.
// Envelope is transport-agnostic; adapters (NATS, etc.) map it to wire formats.
package envelope

import (
	"fmt"
	"strings"

	"github.com/market-raccoon/internal/shared/problem"
)

const (
	// ContentTypeJSON is the default runtime payload format.
	ContentTypeJSON = "application/json"
	// ContentTypeProto is the opt-in protobuf runtime payload format.
	ContentTypeProto = "application/protobuf"
)

// Envelope is the canonical wrapper for all events in the system.
// Fields must match the contract in docs/contracts/event-bus.md.
type Envelope struct {
	// Type is the stable event-type name, e.g. "marketdata.trade".
	Type string `json:"type" cbor:"type"`

	// Version is the payload schema version. Must be >= 1.
	Version int `json:"version" cbor:"version"`

	// Venue is the exchange identifier, e.g. "binance".
	Venue string `json:"venue" cbor:"venue"`

	// Instrument is the canonical instrument symbol, e.g. "BTC-PERP".
	Instrument string `json:"instrument" cbor:"instrument"`

	// TsExchange is the exchange-reported timestamp in Unix milliseconds.
	// Advisory only — do not use for ordering.
	TsExchange int64 `json:"ts_exchange" cbor:"ts_exchange"`

	// TsIngest is the local ingest timestamp in Unix milliseconds.
	// Monotonic within the sequencer; use this for ordering.
	TsIngest int64 `json:"ts_ingest" cbor:"ts_ingest"`

	// Seq is the monotonic sequence number assigned per (venue, instrument).
	Seq int64 `json:"seq" cbor:"seq"`

	// IdempotencyKey is a stable, deterministic deduplication key.
	IdempotencyKey string `json:"idempotency_key" cbor:"idempotency_key"`

	// ContentType declares the payload wire format.
	// Empty is treated as application/json for backward compatibility.
	ContentType string `json:"content_type" cbor:"content_type"`

	// Meta holds optional free-form string metadata (e.g. source_ip, parser_version).
	Meta map[string]string `json:"meta,omitempty" cbor:"meta,omitempty"`

	// Payload is the versioned, serialized domain event (e.g. CBOR-encoded TradeTickV1).
	Payload []byte `json:"payload" cbor:"payload"`
}

// Validate checks that the envelope satisfies the invariants from ADR-0002.
// Returns a *problem.Problem (nil means valid).
func (e *Envelope) Validate() *problem.Problem {
	if strings.TrimSpace(e.Type) == "" {
		return problem.WithDetail(
			problem.New(problem.ValidationFailed, "envelope type must not be empty"),
			"field", "type",
		)
	}
	if e.Version < 1 {
		return problem.WithDetail(
			problem.WithDetail(
				problem.Newf(problem.ValidationFailed, "envelope version must be >= 1, got %d", e.Version),
				"field", "version",
			),
			"value", e.Version,
		)
	}
	if strings.TrimSpace(e.Venue) == "" {
		return problem.WithDetail(
			problem.New(problem.ValidationFailed, "envelope venue must not be empty"),
			"field", "venue",
		)
	}
	if strings.TrimSpace(e.Instrument) == "" {
		return problem.WithDetail(
			problem.New(problem.ValidationFailed, "envelope instrument must not be empty"),
			"field", "instrument",
		)
	}
	if e.TsIngest <= 0 {
		return problem.WithDetail(
			problem.WithDetail(
				problem.Newf(problem.ValidationFailed, "envelope ts_ingest must be positive, got %d", e.TsIngest),
				"field", "ts_ingest",
			),
			"value", e.TsIngest,
		)
	}
	if e.Seq < 0 {
		return problem.WithDetail(
			problem.WithDetail(
				problem.Newf(problem.ValidationFailed, "envelope seq must be non-negative, got %d", e.Seq),
				"field", "seq",
			),
			"value", e.Seq,
		)
	}
	if strings.TrimSpace(e.IdempotencyKey) == "" {
		return problem.WithDetail(
			problem.New(problem.ValidationFailed, "envelope idempotency_key must not be empty"),
			"field", "idempotency_key",
		)
	}
	contentType, p := NormalizeContentType(e.ContentType)
	if p != nil {
		return p
	}
	e.ContentType = contentType
	if len(e.Payload) == 0 {
		return problem.WithDetail(
			problem.New(problem.ValidationFailed, "envelope payload must not be empty"),
			"field", "payload",
		)
	}
	return nil
}

// TopicKey returns a deterministic, lowercase subject string usable for bus routing.
// Format: <type>.<venue>.<instrument>
// e.g. "marketdata.trade.binance.btc-perp"
//
// The returned key is stable for the same inputs and does NOT depend on runtime state.
func (e *Envelope) TopicKey() string {
	venue := strings.ToLower(strings.TrimSpace(e.Venue))
	instrument := strings.ToLower(strings.TrimSpace(e.Instrument))
	eventType := strings.ToLower(strings.TrimSpace(e.Type))
	return fmt.Sprintf("%s.%s.%s", eventType, venue, instrument)
}

// WithMeta returns a new Envelope with the given metadata key-value added.
// The original is not mutated.
func (e Envelope) WithMeta(key, value string) Envelope {
	out := e
	out.Meta = make(map[string]string, len(e.Meta)+1)
	for k, v := range e.Meta {
		out.Meta[k] = v
	}
	out.Meta[key] = value
	return out
}

// NormalizeContentType canonicalizes and validates envelope content type.
// Empty values are normalized to application/json for compatibility.
func NormalizeContentType(contentType string) (string, *problem.Problem) {
	switch strings.ToLower(strings.TrimSpace(contentType)) {
	case "":
		return ContentTypeJSON, nil
	case ContentTypeJSON:
		return ContentTypeJSON, nil
	case ContentTypeProto:
		return ContentTypeProto, nil
	default:
		return "", problem.WithDetail(
			problem.WithDetail(
				problem.Newf(problem.ValidationFailed, "unsupported envelope content_type %q", contentType),
				"field", "content_type",
			),
			"value", contentType,
		)
	}
}
