package deliveryruntime

import (
	"container/list"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/market-raccoon/internal/core/delivery/domain"
	"github.com/market-raccoon/internal/shared/metrics"
)

const (
	defaultSnapshotWireCacheCapacity = 256
	defaultSnapshotWireCacheTTL      = 2 * time.Second
)

type snapshotWireCacheEntry struct {
	key       string
	payload   []byte
	expiresAt time.Time
}

// SnapshotWireCache stores pre-built snapshot wire payloads for short-lived
// resync bursts across sessions.
type SnapshotWireCache struct {
	mu       sync.Mutex
	capacity int
	ttl      time.Duration
	items    map[string]*list.Element
	lru      *list.List
}

func NewSnapshotWireCache(capacity int, ttl time.Duration) *SnapshotWireCache {
	if capacity <= 0 {
		capacity = defaultSnapshotWireCacheCapacity
	}
	if ttl <= 0 {
		ttl = defaultSnapshotWireCacheTTL
	}
	return &SnapshotWireCache{
		capacity: capacity,
		ttl:      ttl,
		items:    make(map[string]*list.Element, capacity),
		lru:      list.New(),
	}
}

func snapshotWireCacheKey(subject domain.Subject, depth uint32) string {
	return fmt.Sprintf(
		"%s|%s|%s|%d",
		strings.ToLower(strings.TrimSpace(subject.Venue)),
		strings.ToUpper(strings.TrimSpace(subject.Symbol)),
		strings.ToLower(strings.TrimSpace(subject.StreamType)),
		depth,
	)
}

func (c *SnapshotWireCache) Get(key string, now time.Time) ([]byte, bool) {
	if c == nil || strings.TrimSpace(key) == "" {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	elem, ok := c.items[key]
	if !ok || elem == nil {
		metrics.IncDeliveryWSSnapshotCacheMiss()
		return nil, false
	}
	entry, ok := elem.Value.(*snapshotWireCacheEntry)
	if !ok || entry == nil {
		c.deleteElemLocked(elem)
		metrics.IncDeliveryWSSnapshotCacheMiss()
		return nil, false
	}
	if !entry.expiresAt.IsZero() && now.After(entry.expiresAt) {
		c.deleteElemLocked(elem)
		metrics.IncDeliveryWSSnapshotCacheMiss()
		return nil, false
	}
	c.lru.MoveToFront(elem)
	metrics.IncDeliveryWSSnapshotCacheHit()
	return append([]byte(nil), entry.payload...), true
}

func (c *SnapshotWireCache) Set(key string, payload []byte, now time.Time) {
	if c == nil || strings.TrimSpace(key) == "" || len(payload) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.items[key]; ok && elem != nil {
		if entry, ok := elem.Value.(*snapshotWireCacheEntry); ok && entry != nil {
			entry.payload = append(entry.payload[:0], payload...)
			entry.expiresAt = now.Add(c.ttl)
			c.lru.MoveToFront(elem)
			metrics.SetDeliveryWSSnapshotCacheEntries(len(c.items))
			return
		}
		c.deleteElemLocked(elem)
	}

	entry := &snapshotWireCacheEntry{
		key:       key,
		payload:   append([]byte(nil), payload...),
		expiresAt: now.Add(c.ttl),
	}
	elem := c.lru.PushFront(entry)
	c.items[key] = elem

	for len(c.items) > c.capacity {
		c.deleteElemLocked(c.lru.Back())
	}
	metrics.SetDeliveryWSSnapshotCacheEntries(len(c.items))
}

func (c *SnapshotWireCache) deleteElemLocked(elem *list.Element) {
	if c == nil || elem == nil {
		return
	}
	entry, _ := elem.Value.(*snapshotWireCacheEntry)
	if entry != nil {
		delete(c.items, entry.key)
	}
	c.lru.Remove(elem)
	metrics.SetDeliveryWSSnapshotCacheEntries(len(c.items))
}
