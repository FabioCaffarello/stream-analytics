package jetstream

import "hash/fnv"

// ShardKey computes a stable uint32 shard key for a canonical JetStream subject.
//
// The key is derived from the venue and instrument tokens (the last two tokens
// of the canonical {event}.{version}.{venue}.{instrument} taxonomy), so that
// all event types and versions for the same venue+instrument map to the same
// shard.  This guarantees that all state transitions for a given order book
// are processed by exactly one processor group.
//
// For subjects that cannot be parsed as canonical taxonomy (fewer than 4 tokens),
// ShardKey falls back to hashing the full subject string.
func ShardKey(subject string) uint32 {
	_, _, venue, instrument, err := splitSubjectTaxonomy(subject)
	h := fnv.New32a()
	if err != nil {
		// Unparseable or wildcard subject: hash the full string.
		_, _ = h.Write([]byte(subject))
		return h.Sum32()
	}
	_, _ = h.Write([]byte(venue))
	_, _ = h.Write([]byte{0}) // null-byte separator avoids "a"+"bc" == "ab"+"c" collisions
	_, _ = h.Write([]byte(instrument))
	return h.Sum32()
}

// subjectBelongsToOtherShard reports whether a concrete subject should be
// skipped by the consumer with the given group configuration.  Returns false
// (do not skip) when sharding is disabled (groupCount <= 1).
func subjectBelongsToOtherShard(subject string, groupCount, myGroupID int) bool {
	if groupCount <= 1 {
		return false
	}
	return ShardGroup(ShardKey(subject), groupCount) != myGroupID
}

// ShardGroup maps a shard key to a group ID in [0, groupCount).
//
// groupCount must be >= 1.  When groupCount is 0 or 1 ShardGroup always
// returns 0, which means sharding is effectively disabled and all messages
// belong to group 0.
func ShardGroup(shardKey uint32, groupCount int) int {
	if groupCount <= 1 {
		return 0
	}
	return int(shardKey % uint32(groupCount)) // #nosec G115 -- groupCount validated > 1 and reasonable shard counts fit uint32
}
