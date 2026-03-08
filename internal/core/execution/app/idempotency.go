package app

// idempotencyCache is a bounded LRU-like cache for intent deduplication.
// It tracks recently seen intentIDs and prevents duplicate execution.
// Not thread-safe — caller must synchronize if needed.
type idempotencyCache struct {
	maxSize int
	entries map[string]int64 // intentID → observedAtMs
	order   []string         // insertion order for eviction
}

func newIdempotencyCache(maxSize int) *idempotencyCache {
	if maxSize <= 0 {
		maxSize = 1024
	}
	return &idempotencyCache{
		maxSize: maxSize,
		entries: make(map[string]int64, maxSize),
		order:   make([]string, 0, maxSize),
	}
}

// seen returns true if intentID was already processed. If not seen, records it
// with the given observedAtMs timestamp and returns false.
func (c *idempotencyCache) seen(intentID string, observedAtMs int64) bool {
	if intentID == "" {
		return false
	}
	if _, exists := c.entries[intentID]; exists {
		return true
	}
	// Evict oldest if at capacity.
	if len(c.order) >= c.maxSize {
		oldest := c.order[0]
		c.order = c.order[1:]
		delete(c.entries, oldest)
	}
	c.entries[intentID] = observedAtMs
	c.order = append(c.order, intentID)
	return false
}

// size returns current cache size.
func (c *idempotencyCache) size() int {
	return len(c.entries)
}
