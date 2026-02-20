package deliveryruntime

import (
	"container/list"
	"encoding/json"
	"sync"
	"sync/atomic"

	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/problem"
)

const (
	defaultTranscodeCacheSize  = 1024
	defaultTranscodeShardCount = 16
)

// TranscodeCache is a sharded LRU cache for proto→JSON transcode results.
// The cache is sharded to reduce lock contention under high concurrency.
type TranscodeCache struct {
	shards     []*transcodeShard
	shardCount uint64
	maxSize    int // global max across all shards
	hits       atomic.Int64
	misses     atomic.Int64
}

type transcodeShard struct {
	mu      sync.RWMutex
	entries map[uint64]*list.Element
	lru     *list.List
	maxSize int
}

type transcodeCacheEntry struct {
	json json.RawMessage
}

type lruEntry struct {
	key   uint64
	entry transcodeCacheEntry
}

const maxIntUint64 = uint64(^uint(0) >> 1)

// uint64ToIntSafe converts a small uint64 to int with overflow guard.
func uint64ToIntSafe(u uint64) int {
	if u > maxIntUint64 {
		// This should never occur for shard indexes; fall back to shard 0.
		return 0
	}
	return int(u)
}

// NewTranscodeCache creates a bounded proto→JSON cache.
// maxSize controls the overall max number of cached entries (across shards). 0 uses default.
func NewTranscodeCache(maxSize int) *TranscodeCache {
	if maxSize <= 0 {
		maxSize = defaultTranscodeCacheSize
	}
	shardCount := uint64(defaultTranscodeShardCount)
	perShard := (maxSize + int(shardCount) - 1) / int(shardCount)
	shards := make([]*transcodeShard, int(shardCount))
	for i := 0; i < int(shardCount); i++ {
		shards[i] = &transcodeShard{
			entries: make(map[uint64]*list.Element, perShard),
			lru:     list.New(),
			maxSize: perShard,
		}
	}
	return &TranscodeCache{
		shards:     shards,
		shardCount: shardCount,
		maxSize:    maxSize,
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
	var shard *transcodeShard
	// If shardCount is a power-of-two we can use a mask which avoids modulo
	// and keeps the index conversion safe on all architectures.
	if (c.shardCount & (c.shardCount - 1)) == 0 {
		idx := uint64ToIntSafe(key & (c.shardCount - 1))
		shard = c.shards[idx]
	} else {
		idx := uint64ToIntSafe(key % c.shardCount)
		shard = c.shards[idx]
	}

	// Fast path: read lock on shard.
	shard.mu.RLock()
	elem, ok := shard.entries[key]
	shard.mu.RUnlock()
	if ok {
		c.hits.Add(1)
		// Move to front under write lock.
		shard.mu.Lock()
		if e, still := shard.entries[key]; still {
			shard.lru.MoveToFront(e)
		}
		shard.mu.Unlock()
		return elem.Value.(lruEntry).entry.json, nil
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

	// Insert into shard under write lock; evict least-recently-used if needed.
	shard.mu.Lock()
	// If key already inserted by another goroutine, update and move to front.
	if existingElem, exists := shard.entries[key]; exists {
		existingElem.Value = lruEntry{key: key, entry: transcodeCacheEntry{json: result}}
		shard.lru.MoveToFront(existingElem)
		shard.mu.Unlock()
		return result, nil
	}
	// Evict oldest if at capacity.
	if shard.lru.Len() >= shard.maxSize {
		back := shard.lru.Back()
		if back != nil {
			be := back.Value.(lruEntry)
			delete(shard.entries, be.key)
			shard.lru.Remove(back)
		}
	}
	elem = shard.lru.PushFront(lruEntry{key: key, entry: transcodeCacheEntry{json: result}})
	shard.entries[key] = elem
	shard.mu.Unlock()
	return result, nil
}

func (c *TranscodeCache) computeKey(eventType string, version int, payload []byte) uint64 {
	// Inline FNV-1a-64 to avoid fnv.New64a() alloc + []byte(eventType) alloc.
	const (
		offset64 = 14695981039346656037
		prime64  = 1099511628211
	)
	h := uint64(offset64)
	for i := 0; i < len(eventType); i++ {
		h ^= uint64(eventType[i])
		h *= prime64
	}
	h ^= uint64(byte(version >> 8))
	h *= prime64
	h ^= uint64(byte(version))
	h *= prime64
	for _, b := range payload {
		h ^= uint64(b)
		h *= prime64
	}
	return h
}

// Stats returns cache hit/miss counters for observability.
func (c *TranscodeCache) Stats() (hits, misses int64) {
	return c.hits.Load(), c.misses.Load()
}

// Len returns the current number of cached entries.
func (c *TranscodeCache) Len() int {
	total := 0
	for _, s := range c.shards {
		s.mu.RLock()
		total += s.lru.Len()
		s.mu.RUnlock()
	}
	return total
}
