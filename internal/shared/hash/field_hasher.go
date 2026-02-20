package hash

import (
	"math"
	"strconv"
)

// FieldHasher is a zero-allocation FNV-1a-64 builder for mixed-type hash keys.
// It hashes raw binary representations of numeric types, avoiding strconv.Format*
// allocations on the hot path. The only allocation is the final Hex() call.
//
// FieldHasher is a value type (16 bytes). Methods return FieldHasher by value,
// enabling chained calls. The compiler inlines aggressively.
//
// Usage:
//
//	key := hash.NewFieldHasher().
//	    String(venue).
//	    Int(version).
//	    Float64(price).
//	    Int64(timestamp).
//	    Hex()
type FieldHasher struct {
	h     uint64
	count int
}

// NewFieldHasher returns a FieldHasher seeded with the FNV-1a offset basis.
func NewFieldHasher() FieldHasher {
	return FieldHasher{h: fnv64aOffset}
}

// separator hashes a null-byte separator between fields to prevent collisions
// (e.g. String("ab").String("c") != String("a").String("bc")).
func (f FieldHasher) separator() FieldHasher {
	if f.count > 0 {
		f.h ^= 0x00
		f.h *= fnv64aPrime
	}
	f.count++
	return f
}

// String hashes a string field byte-by-byte (same algorithm as SumFieldsFast64).
func (f FieldHasher) String(s string) FieldHasher {
	f = f.separator()
	for i := 0; i < len(s); i++ {
		f.h ^= uint64(s[i])
		f.h *= fnv64aPrime
	}
	return f
}

// Int64 hashes an int64 by its big-endian 8-byte representation
// (same algorithm as SumIdempotencyKeyFast64).
func (f FieldHasher) Int64(v int64) FieldHasher {
	f = f.separator()
	u := uint64(v) // #nosec G115 -- raw bits used for hashing
	f.h ^= (u >> 56) & 0xff
	f.h *= fnv64aPrime
	f.h ^= (u >> 48) & 0xff
	f.h *= fnv64aPrime
	f.h ^= (u >> 40) & 0xff
	f.h *= fnv64aPrime
	f.h ^= (u >> 32) & 0xff
	f.h *= fnv64aPrime
	f.h ^= (u >> 24) & 0xff
	f.h *= fnv64aPrime
	f.h ^= (u >> 16) & 0xff
	f.h *= fnv64aPrime
	f.h ^= (u >> 8) & 0xff
	f.h *= fnv64aPrime
	f.h ^= u & 0xff
	f.h *= fnv64aPrime
	return f
}

// Float64 hashes a float64 by its IEEE 754 bit representation in big-endian
// order (same algorithm as HashFloat64Sequence).
func (f FieldHasher) Float64(v float64) FieldHasher {
	f = f.separator()
	bits := math.Float64bits(v)
	f.h ^= (bits >> 56) & 0xff
	f.h *= fnv64aPrime
	f.h ^= (bits >> 48) & 0xff
	f.h *= fnv64aPrime
	f.h ^= (bits >> 40) & 0xff
	f.h *= fnv64aPrime
	f.h ^= (bits >> 32) & 0xff
	f.h *= fnv64aPrime
	f.h ^= (bits >> 24) & 0xff
	f.h *= fnv64aPrime
	f.h ^= (bits >> 16) & 0xff
	f.h *= fnv64aPrime
	f.h ^= (bits >> 8) & 0xff
	f.h *= fnv64aPrime
	f.h ^= bits & 0xff
	f.h *= fnv64aPrime
	return f
}

// Int hashes an int by casting to int64 and hashing its big-endian representation.
// Int(42) produces the same hash as Int64(42).
func (f FieldHasher) Int(v int) FieldHasher {
	return f.Int64(int64(v))
}

// Sum64 returns the raw FNV-1a-64 hash value. Zero allocations.
func (f FieldHasher) Sum64() uint64 {
	return f.h
}

// Hex returns the hash as a lowercase hex string. This is the only method
// that allocates (one string via strconv.FormatUint).
func (f FieldHasher) Hex() string {
	return strconv.FormatUint(f.h, 16)
}
