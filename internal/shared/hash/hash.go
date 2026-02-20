// Package hash provides deterministic, stable hashing utilities.
// All functions are pure and side-effect free.
//
//nolint:revive // domain naming intentionally uses "hash".
package hash

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"strconv"
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
// Deprecated: prefer HashFieldsFast for hot-path idempotency keys.
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

// FNV-1a-64 constants (same as hash/fnv).
const (
	fnv64aOffset = 14695981039346656037
	fnv64aPrime  = 1099511628211
)

// HashFieldsFast returns a hex-encoded FNV-1a-64 hash of fields joined by
// null-byte separators. The null-byte separator prevents ambiguous collisions
// (e.g. HashFieldsFast("ab","c") != HashFieldsFast("a","bc")).
//
// This is the recommended function for idempotency keys and hot-path hashing
// where cryptographic strength is not needed. The inline FNV-1a computation
// avoids all intermediate allocations (no hash.Hash, no []byte conversions).
//
//nolint:revive // API kept stable as HashFieldsFast.
func HashFieldsFast(fields ...string) string {
	h := uint64(fnv64aOffset)
	for i, f := range fields {
		if i > 0 {
			h ^= 0x00
			h *= fnv64aPrime
		}
		for j := 0; j < len(f); j++ {
			h ^= uint64(f[j])
			h *= fnv64aPrime
		}
	}
	return strconv.FormatUint(h, 16)
}

// HashFloat64Sequence returns a stable FNV-1a-64 hash for a slice of float64 values.
// It hashes the IEEE 754 bit representation of each float64 directly, avoiding
// all intermediate allocations (no []string, no strconv.FormatFloat). Values
// are separated by a null-byte sentinel to prevent collision between sequences
// of different lengths.
//
//nolint:revive // API kept stable as HashFloat64Sequence.
func HashFloat64Sequence(values []float64) string {
	h := uint64(fnv64aOffset)
	for i, v := range values {
		if i > 0 {
			// Null-byte separator between elements (same pattern as HashFieldsFast).
			h ^= 0x00
			h *= fnv64aPrime
		}
		bits := math.Float64bits(v)
		// Hash 8 bytes of the IEEE 754 representation, big-endian order.
		h ^= (bits >> 56) & 0xff
		h *= fnv64aPrime
		h ^= (bits >> 48) & 0xff
		h *= fnv64aPrime
		h ^= (bits >> 40) & 0xff
		h *= fnv64aPrime
		h ^= (bits >> 32) & 0xff
		h *= fnv64aPrime
		h ^= (bits >> 24) & 0xff
		h *= fnv64aPrime
		h ^= (bits >> 16) & 0xff
		h *= fnv64aPrime
		h ^= (bits >> 8) & 0xff
		h *= fnv64aPrime
		h ^= bits & 0xff
		h *= fnv64aPrime
	}
	return strconv.FormatUint(h, 16)
}
