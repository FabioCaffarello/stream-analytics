// Package ids provides typed identifiers and UUID parsing/generation helpers.
package ids

import (
	"crypto/rand"
	"encoding/hex"
	"strings"

	"github.com/market-raccoon/internal/shared/problem"
)

// SessionID identifies a client session.
type SessionID string

// CorrelationID ties related events together across the system.
type CorrelationID string

// RequestID identifies a single request within a session.
type RequestID string

// NewSessionID generates a new random SessionID.
func NewSessionID() SessionID { return SessionID(newUUID()) }

// NewCorrelationID generates a new random CorrelationID.
func NewCorrelationID() CorrelationID { return CorrelationID(newUUID()) }

// NewRequestID generates a new random RequestID.
func NewRequestID() RequestID { return RequestID(newUUID()) }

// ParseSessionID parses and validates a SessionID.
func ParseSessionID(s string) (SessionID, *problem.Problem) {
	if p := validateID("session_id", s); p != nil {
		return "", p
	}
	return SessionID(s), nil
}

// ParseCorrelationID parses and validates a CorrelationID.
func ParseCorrelationID(s string) (CorrelationID, *problem.Problem) {
	if p := validateID("correlation_id", s); p != nil {
		return "", p
	}
	return CorrelationID(s), nil
}

// ParseRequestID parses and validates a RequestID.
func ParseRequestID(s string) (RequestID, *problem.Problem) {
	if p := validateID("request_id", s); p != nil {
		return "", p
	}
	return RequestID(s), nil
}

// String implementations for readable logging.
func (id SessionID) String() string     { return string(id) }
func (id CorrelationID) String() string { return string(id) }
func (id RequestID) String() string     { return string(id) }

// validateID checks that an ID string is non-empty and has valid UUID format.
func validateID(field, value string) *problem.Problem {
	if strings.TrimSpace(value) == "" {
		return problem.WithDetail(
			problem.Newf(problem.InvalidArgument, "%s must not be empty", field),
			"field", field,
		)
	}
	// Validate UUID format: 8-4-4-4-12 hex digits
	if !isValidUUID(value) {
		return problem.WithDetail(
			problem.WithDetail(
				problem.Newf(problem.InvalidArgument, "%s has invalid UUID format", field),
				"field", field,
			),
			"value", value,
		)
	}
	return nil
}

// isValidUUID checks the canonical UUID format xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx.
func isValidUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !isHexChar(byte(c)) {
				return false
			}
		}
	}
	return true
}

func isHexChar(c byte) bool {
	return (c >= '0' && c <= '9') ||
		(c >= 'a' && c <= 'f') ||
		(c >= 'A' && c <= 'F')
}

// newUUID generates a v4 UUID (random) without external dependencies.
func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failure is a fatal infrastructure issue, not a domain problem.
		// In production this should never happen; fall through with best effort.
		b = [16]byte{}
	}
	// Set version 4 and variant bits.
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80

	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}
