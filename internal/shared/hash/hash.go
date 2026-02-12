// Package hash provides deterministic, stable hashing utilities.
// All functions are pure and side-effect free.
//
//nolint:revive // domain naming intentionally uses "hash".
package hash

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// HashBytes returns the lowercase hex-encoded SHA-256 digest of data.
// The result is always 64 hex characters. Input order matters.
//
//nolint:revive // API kept stable as HashBytes.
func HashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// HashFields concatenates fields with a null-byte separator and returns
// the SHA-256 hex digest. The separator prevents ambiguous collisions
// (e.g. HashFields("ab","c") != HashFields("a","bc")).
//
// Input order is significant — callers must normalize order themselves.
//
//nolint:revive // API kept stable as HashFields.
func HashFields(fields ...string) string {
	var b strings.Builder
	for i, f := range fields {
		if i > 0 {
			b.WriteByte(0x00)
		}
		b.WriteString(f)
	}
	return HashBytes([]byte(b.String()))
}

// HashFloat64Sequence returns a stable hash for a slice of float64 values.
// Values are formatted with full precision to avoid representation ambiguity.
//
//nolint:revive // API kept stable as HashFloat64Sequence.
func HashFloat64Sequence(values []float64) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = fmt.Sprintf("%.17g", v)
	}
	return HashFields(parts...)
}
