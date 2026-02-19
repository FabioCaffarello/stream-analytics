package deliveryruntime

import (
	"encoding/json"
	"hash/fnv"
	"sync"
	"sync/atomic"

	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/problem"
)

const defaultTranscodeCacheSize = 1024

// TranscodeCache caches proto→JSON transcode results so that identical
// proto payloads are only decoded+re-marshaled once across all delivery
// sessions. Thread-safe (sync.RWMutex) and bounded (eviction on overflow).
type TranscodeCache struct {
	mu      sync.RWMutex
	entries map[uint64]transcodeCacheEntry
	maxSize int
	hits    atomic.Int64
	misses  atomic.Int64
}

type transcodeCacheEntry struct {
	json json.RawMessage
}

// NewTranscodeCache creates a bounded proto→JSON cache.
// maxSize controls the max number of cached entries. 0 uses default (1024).
func NewTranscodeCache(maxSize int) *TranscodeCache {
	if maxSize <= 0 {
		maxSize = defaultTranscodeCacheSize
	}
	return &TranscodeCache{
		entries: make(map[uint64]transcodeCacheEntry, maxSize),
		maxSize: maxSize,
	}
}

// TranscodeProtoToJSON returns cached JSON for a proto payload, or decodes
// and caches the result. The cache key is FNV-1a hash of
// (eventType, version, raw payload bytes).
func (c *TranscodeCache) TranscodeProtoToJSON(
	eventType string,
	version int,
	contentType string,
	payload []byte,
) (json.RawMessage, *problem.Problem) {
	key := c.computeKey(eventType, version, payload)

	// Fast path: read lock.
	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()
	if ok {
		c.hits.Add(1)
		return entry.json, nil
	}

	// Slow path: decode, marshal, store.
	c.misses.Add(1)
	decoded, p := codec.DecodePayload(eventType, version, contentType, payload)
	if p != nil {
		return nil, p
	}
	transcoded, err := json.Marshal(decoded)
	if err != nil {
		return nil, problem.Wrap(err, problem.Internal, "proto→json transcode failed")
	}

	result := json.RawMessage(transcoded)
	c.mu.Lock()
	if len(c.entries) >= c.maxSize {
		// Simple eviction: clear all. The working set of active event types
		// is small and refills within milliseconds. This avoids LRU bookkeeping
		// overhead on every hot-path access.
		c.entries = make(map[uint64]transcodeCacheEntry, c.maxSize)
	}
	c.entries[key] = transcodeCacheEntry{json: result}
	c.mu.Unlock()

	return result, nil
}

func (c *TranscodeCache) computeKey(eventType string, version int, payload []byte) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(eventType))
	_, _ = h.Write([]byte{byte(version >> 8), byte(version)})
	_, _ = h.Write(payload)
	return h.Sum64()
}

// Stats returns cache hit/miss counters for observability.
func (c *TranscodeCache) Stats() (hits, misses int64) {
	return c.hits.Load(), c.misses.Load()
}

// Len returns the current number of cached entries.
func (c *TranscodeCache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}
