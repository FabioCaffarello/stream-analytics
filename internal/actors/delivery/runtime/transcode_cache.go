package deliveryruntime

import (
	"container/list"
	"encoding/json"
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
	entries map[uint64]*list.Element
	lru     *list.List // list of lruEntry, front = most recent
	maxSize int
	hits    atomic.Int64
	misses  atomic.Int64
}

type transcodeCacheEntry struct {
	json json.RawMessage
}

type lruEntry struct {
	key   uint64
	entry transcodeCacheEntry
}

// NewTranscodeCache creates a bounded proto→JSON cache.
// maxSize controls the max number of cached entries. 0 uses default (1024).
func NewTranscodeCache(maxSize int) *TranscodeCache {
	if maxSize <= 0 {
		maxSize = defaultTranscodeCacheSize
	}
	return &TranscodeCache{
		entries: make(map[uint64]*list.Element, maxSize),
		lru:     list.New(),
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
	// Fast path: read lock to check presence.
	c.mu.RLock()
	elem, ok := c.entries[key]
	c.mu.RUnlock()
	if ok {
		c.hits.Add(1)
		// Move to front under write lock.
		c.mu.Lock()
		if e, still := c.entries[key]; still {
			c.lru.MoveToFront(e)
		}
		c.mu.Unlock()
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
	// Insert into LRU under write lock; evict least-recently-used if needed.
	c.mu.Lock()
	// If key already inserted by another goroutine, update and move to front.
	if existingElem, exists := c.entries[key]; exists {
		existingElem.Value = lruEntry{key: key, entry: transcodeCacheEntry{json: result}}
		c.lru.MoveToFront(existingElem)
		c.mu.Unlock()
		return result, nil
	}
	// Evict oldest if at capacity.
	if c.lru.Len() >= c.maxSize {
		back := c.lru.Back()
		if back != nil {
			be := back.Value.(lruEntry)
			delete(c.entries, be.key)
			c.lru.Remove(back)
		}
	}
	elem = c.lru.PushFront(lruEntry{key: key, entry: transcodeCacheEntry{json: result}})
	c.entries[key] = elem
	c.mu.Unlock()
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
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lru.Len()
}
