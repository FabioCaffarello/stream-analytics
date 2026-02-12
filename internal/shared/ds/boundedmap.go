package ds

import (
	"container/list"
	"sync"
	"time"

	"github.com/market-raccoon/internal/shared/clock"
)

const (
	EvictReasonSize = "size"
	EvictReasonTTL  = "ttl"
)

type boundedEntry[K comparable, V any] struct {
	key       K
	value     V
	expiresAt time.Time
	element   *list.Element
}

// BoundedMap is a concurrency-safe LRU map with optional TTL eviction.
type BoundedMap[K comparable, V any] struct {
	mu      sync.RWMutex
	maxSize int
	ttl     time.Duration
	clock   clock.Clock

	items map[K]*boundedEntry[K, V]
	lru   *list.List

	onEvict func(key K, value V, reason string)
}

func NewBoundedMap[K comparable, V any](maxSize int, ttl time.Duration, clk clock.Clock) *BoundedMap[K, V] {
	if maxSize <= 0 {
		maxSize = 1
	}
	if clk == nil {
		clk = clock.NewSystemClock()
	}
	return &BoundedMap[K, V]{
		maxSize: maxSize,
		ttl:     ttl,
		clock:   clk,
		items:   make(map[K]*boundedEntry[K, V], maxSize),
		lru:     list.New(),
	}
}

func (m *BoundedMap[K, V]) SetOnEvict(fn func(key K, value V, reason string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onEvict = fn
}

func (m *BoundedMap[K, V]) Get(key K) (V, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var zero V
	entry, ok := m.items[key]
	if !ok {
		return zero, false
	}
	if m.isExpired(entry) {
		m.evictLocked(entry, EvictReasonTTL)
		return zero, false
	}
	m.lru.MoveToFront(entry.element)
	return entry.value, true
}

func (m *BoundedMap[K, V]) Put(key K, value V) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if entry, ok := m.items[key]; ok {
		entry.value = value
		entry.expiresAt = m.nextExpiry()
		m.lru.MoveToFront(entry.element)
		return
	}

	if len(m.items) >= m.maxSize {
		if tail := m.lru.Back(); tail != nil {
			tailKey := tail.Value.(K)
			if old, exists := m.items[tailKey]; exists {
				m.evictLocked(old, EvictReasonSize)
			}
		}
	}

	elem := m.lru.PushFront(key)
	m.items[key] = &boundedEntry[K, V]{
		key:       key,
		value:     value,
		expiresAt: m.nextExpiry(),
		element:   elem,
	}
}

func (m *BoundedMap[K, V]) Delete(key K) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if entry, ok := m.items[key]; ok {
		m.evictLocked(entry, "unknown")
	}
}

func (m *BoundedMap[K, V]) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.items)
}

// Sweep removes expired entries and returns the number evicted.
func (m *BoundedMap[K, V]) Sweep() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ttl <= 0 || len(m.items) == 0 {
		return 0
	}
	removed := 0
	for _, entry := range m.items {
		if m.isExpired(entry) {
			m.evictLocked(entry, EvictReasonTTL)
			removed++
		}
	}
	return removed
}

func (m *BoundedMap[K, V]) nextExpiry() time.Time {
	if m.ttl <= 0 {
		return time.Time{}
	}
	return m.clock.Now().Add(m.ttl)
}

func (m *BoundedMap[K, V]) isExpired(entry *boundedEntry[K, V]) bool {
	if m.ttl <= 0 {
		return false
	}
	return !entry.expiresAt.IsZero() && !entry.expiresAt.After(m.clock.Now())
}

func (m *BoundedMap[K, V]) evictLocked(entry *boundedEntry[K, V], reason string) {
	delete(m.items, entry.key)
	m.lru.Remove(entry.element)
	if m.onEvict != nil {
		m.onEvict(entry.key, entry.value, reason)
	}
}
