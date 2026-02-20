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
	onSweep func(removed int)

	opCount          uint64
	sweepEveryOps    uint64
	sweepMinInterval time.Duration
	lastSweep        time.Time
}

type evictionEvent[K comparable, V any] struct {
	key    K
	value  V
	reason string
}

func NewBoundedMap[K comparable, V any](maxSize int, ttl time.Duration, clk clock.Clock) *BoundedMap[K, V] {
	if maxSize <= 0 {
		maxSize = 1
	}
	if clk == nil {
		clk = clock.NewSystemClock()
	}
	return &BoundedMap[K, V]{
		maxSize:   maxSize,
		ttl:       ttl,
		clock:     clk,
		items:     make(map[K]*boundedEntry[K, V], maxSize),
		lru:       list.New(),
		lastSweep: clk.Now(),
	}
}

func (m *BoundedMap[K, V]) SetOnEvict(fn func(key K, value V, reason string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onEvict = fn
}

// SetOnSweep registers a callback that fires after each sweep with the number of
// entries removed. The callback is invoked outside the lock.
func (m *BoundedMap[K, V]) SetOnSweep(fn func(removed int)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onSweep = fn
}

// SetSweepEveryOps configures opportunistic sweep cadence by operation count.
//
// n=0 disables op-count based sweeping. TTL correctness does not depend on this
// cadence because Get() always checks per-entry expiration.
func (m *BoundedMap[K, V]) SetSweepEveryOps(n uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sweepEveryOps = n
}

// SetSweepMinInterval configures opportunistic sweep cadence by elapsed time.
//
// d<=0 disables time-based sweeping. TTL correctness does not depend on this
// cadence because Get() always checks per-entry expiration.
func (m *BoundedMap[K, V]) SetSweepMinInterval(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sweepMinInterval = d
}

func (m *BoundedMap[K, V]) Get(key K) (V, bool) {
	m.mu.Lock()
	var zero V
	onEvict := m.onEvict
	now := m.clock.Now()
	m.opCount++
	evicted := make([]evictionEvent[K, V], 0, 1)

	entry, ok := m.items[key]
	if !ok {
		if sweepEvicted := m.maybeSweepLocked(now); len(sweepEvicted) > 0 {
			evicted = append(evicted, sweepEvicted...)
		}
		m.mu.Unlock()
		m.fireEvictions(onEvict, evicted)
		return zero, false
	}
	if m.isExpiredAt(entry, now) {
		ev := m.evictLocked(entry, EvictReasonTTL)
		if ev != nil {
			evicted = append(evicted, *ev)
		}
		if sweepEvicted := m.maybeSweepLocked(now); len(sweepEvicted) > 0 {
			evicted = append(evicted, sweepEvicted...)
		}
		m.mu.Unlock()
		m.fireEvictions(onEvict, evicted)
		return zero, false
	}
	m.lru.MoveToFront(entry.element)
	val := entry.value
	if sweepEvicted := m.maybeSweepLocked(now); len(sweepEvicted) > 0 {
		evicted = append(evicted, sweepEvicted...)
	}
	m.mu.Unlock()
	m.fireEvictions(onEvict, evicted)
	return val, true
}

func (m *BoundedMap[K, V]) Put(key K, value V) {
	m.mu.Lock()
	onEvict := m.onEvict
	now := m.clock.Now()
	m.opCount++
	var evicted []evictionEvent[K, V]

	if entry, ok := m.items[key]; ok {
		entry.value = value
		entry.expiresAt = m.nextExpiryAt(now)
		m.lru.MoveToFront(entry.element)
		if sweepEvicted := m.maybeSweepLocked(now); len(sweepEvicted) > 0 {
			evicted = append(evicted, sweepEvicted...)
		}
		m.mu.Unlock()
		m.fireEvictions(onEvict, evicted)
		return
	}

	if len(m.items) >= m.maxSize {
		if tail := m.lru.Back(); tail != nil {
			tailKey := tail.Value.(K)
			if old, exists := m.items[tailKey]; exists {
				if ev := m.evictLocked(old, EvictReasonSize); ev != nil {
					evicted = append(evicted, *ev)
				}
			}
		}
	}

	elem := m.lru.PushFront(key)
	m.items[key] = &boundedEntry[K, V]{
		key:       key,
		value:     value,
		expiresAt: m.nextExpiryAt(now),
		element:   elem,
	}
	if sweepEvicted := m.maybeSweepLocked(now); len(sweepEvicted) > 0 {
		evicted = append(evicted, sweepEvicted...)
	}
	m.mu.Unlock()
	m.fireEvictions(onEvict, evicted)
}

func (m *BoundedMap[K, V]) Delete(key K) {
	m.mu.Lock()
	onEvict := m.onEvict
	now := m.clock.Now()
	m.opCount++
	var evicted []evictionEvent[K, V]

	if entry, ok := m.items[key]; ok {
		if ev := m.evictLocked(entry, "unknown"); ev != nil {
			evicted = append(evicted, *ev)
		}
	}
	if sweepEvicted := m.maybeSweepLocked(now); len(sweepEvicted) > 0 {
		evicted = append(evicted, sweepEvicted...)
	}
	m.mu.Unlock()
	m.fireEvictions(onEvict, evicted)
}

func (m *BoundedMap[K, V]) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.items)
}

// Sweep removes expired entries and returns the number evicted.
func (m *BoundedMap[K, V]) Sweep() int {
	m.mu.Lock()
	onEvict := m.onEvict
	onSweep := m.onSweep
	now := m.clock.Now()
	removed, evicted := m.sweepLocked(now)
	m.lastSweep = now
	m.mu.Unlock()
	m.fireEvictions(onEvict, evicted)
	if onSweep != nil {
		onSweep(removed)
	}
	return removed
}

func (m *BoundedMap[K, V]) nextExpiryAt(now time.Time) time.Time {
	if m.ttl <= 0 {
		return time.Time{}
	}
	return now.Add(m.ttl)
}

func (m *BoundedMap[K, V]) isExpiredAt(entry *boundedEntry[K, V], now time.Time) bool {
	if m.ttl <= 0 {
		return false
	}
	return !entry.expiresAt.IsZero() && !entry.expiresAt.After(now)
}

func (m *BoundedMap[K, V]) evictLocked(entry *boundedEntry[K, V], reason string) *evictionEvent[K, V] {
	delete(m.items, entry.key)
	m.lru.Remove(entry.element)
	if m.onEvict == nil {
		return nil
	}
	return &evictionEvent[K, V]{key: entry.key, value: entry.value, reason: reason}
}

func (m *BoundedMap[K, V]) fireEvictions(fn func(key K, value V, reason string), evs []evictionEvent[K, V]) {
	if fn == nil || len(evs) == 0 {
		return
	}
	for i := range evs {
		ev := evs[i]
		fn(ev.key, ev.value, ev.reason)
	}
}

func (m *BoundedMap[K, V]) maybeSweepLocked(now time.Time) []evictionEvent[K, V] {
	if m.ttl <= 0 {
		return nil
	}

	dueByOps := m.sweepEveryOps > 0 && m.opCount%m.sweepEveryOps == 0
	dueByInterval := m.sweepMinInterval > 0 && now.Sub(m.lastSweep) >= m.sweepMinInterval
	if !dueByOps && !dueByInterval {
		return nil
	}

	_, evicted := m.sweepLocked(now)
	m.lastSweep = now
	return evicted
}

func (m *BoundedMap[K, V]) sweepLocked(now time.Time) (int, []evictionEvent[K, V]) {
	if m.ttl <= 0 || len(m.items) == 0 {
		return 0, nil
	}
	removed := 0
	evicted := make([]evictionEvent[K, V], 0)
	for _, entry := range m.items {
		if m.isExpiredAt(entry, now) {
			if ev := m.evictLocked(entry, EvictReasonTTL); ev != nil {
				evicted = append(evicted, *ev)
			}
			removed++
		}
	}
	return removed, evicted
}
