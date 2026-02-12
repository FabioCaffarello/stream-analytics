// Package domain contains the marketdata bounded context domain model.
// It has no dependencies on ports, app, actors, or infrastructure.
package domain

import (
	"github.com/market-raccoon/internal/shared/naming"
	"github.com/market-raccoon/internal/shared/problem"
	"github.com/market-raccoon/internal/shared/validation"
)

// VenueID is the canonical identifier for a trading venue.
type VenueID string

// InstrumentID is the canonical identifier for a tradeable instrument.
type InstrumentID string

// EventType is the stable event-type name (e.g. "marketdata.trade").
type EventType string

// SchemaVersion is the integer payload schema version. Must be >= 1.
type SchemaVersion int

// Sequence is a monotonic per-stream counter.
type Sequence int64

// Timestamp represents a Unix millisecond timestamp.
type Timestamp int64

// IdempotencyKey is a deterministic deduplication key.
type IdempotencyKey string

// DedupWindow is the configured size of the deduplication window.
// Must be strictly positive.
type DedupWindow int

// NewVenueID parses and normalizes a venue identifier.
func NewVenueID(raw string) (VenueID, *problem.Problem) {
	if p := validation.NonEmptyString("venue_id", raw); p != nil {
		return "", p
	}
	return VenueID(naming.CanonicalVenue(raw)), nil
}

// NewInstrumentID parses and normalizes an instrument identifier.
func NewInstrumentID(raw string) (InstrumentID, *problem.Problem) {
	if p := validation.NonEmptyString("instrument_id", raw); p != nil {
		return "", p
	}
	return InstrumentID(naming.CanonicalInstrument(raw)), nil
}

// NewEventType validates and normalizes an event type.
func NewEventType(raw string) (EventType, *problem.Problem) {
	if p := validation.NonEmptyString("event_type", raw); p != nil {
		return "", p
	}
	return EventType(naming.NormalizeEventType(raw)), nil
}

// NewSchemaVersion validates a schema version.
func NewSchemaVersion(v int) (SchemaVersion, *problem.Problem) {
	if v < 1 {
		return 0, problem.WithDetail(
			problem.Newf(problem.ValidationFailed, "schema_version must be >= 1, got %d", v),
			"field", "schema_version",
		)
	}
	return SchemaVersion(v), nil
}

// NewSequence validates a sequence number (must be non-negative).
func NewSequence(s int64) (Sequence, *problem.Problem) {
	if p := validation.NonNegativeInt("seq", s); p != nil {
		return 0, p
	}
	return Sequence(s), nil
}

// NewTimestamp validates a timestamp (must be positive).
func NewTimestamp(ms int64) (Timestamp, *problem.Problem) {
	if p := validation.PositiveInt("timestamp", ms); p != nil {
		return 0, p
	}
	return Timestamp(ms), nil
}

// NewIdempotencyKey validates an idempotency key.
func NewIdempotencyKey(key string) (IdempotencyKey, *problem.Problem) {
	if p := validation.NonEmptyString("idempotency_key", key); p != nil {
		return "", p
	}
	return IdempotencyKey(key), nil
}

// NewDedupWindow validates the dedup cache window size.
func NewDedupWindow(size int) (DedupWindow, *problem.Problem) {
	if size <= 0 {
		return 0, problem.WithDetail(
			problem.Newf(problem.ValidationFailed, "dedup_window must be > 0, got %d", size),
			"field", "dedup_window",
		)
	}
	return DedupWindow(size), nil
}

// String accessors.
func (v VenueID) String() string      { return string(v) }
func (i InstrumentID) String() string { return string(i) }
func (e EventType) String() string    { return string(e) }

// Int64 returns the sequence as int64.
func (s Sequence) Int64() int64 { return int64(s) }

// UnixMilli returns the timestamp as Unix milliseconds.
func (t Timestamp) UnixMilli() int64    { return int64(t) }
func (k IdempotencyKey) String() string { return string(k) }

// Size returns the configured dedup window size.
func (w DedupWindow) Size() int { return int(w) }
