package marketmodel

import (
	"sync"
	"time"

	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/problem"
)

type StateStoreConfig struct {
	MaxEntries int
	TTL        time.Duration
	Now        func() time.Time
}

type StreamState struct {
	Book    Book
	LastSeq Seq
}

type stateEntry struct {
	key      StreamKey
	value    StreamState
	lastSeen time.Time
}

type StateStore struct {
	mu         sync.Mutex
	maxEntries int
	ttl        time.Duration
	now        func() time.Time
	entries    map[string]stateEntry
	order      []string
}

func NewStateStore(cfg StateStoreConfig) *StateStore {
	if cfg.MaxEntries <= 0 {
		cfg.MaxEntries = 10_000
	}
	if cfg.TTL <= 0 {
		cfg.TTL = time.Hour
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	s := &StateStore{
		maxEntries: cfg.MaxEntries,
		ttl:        cfg.TTL,
		now:        cfg.Now,
		entries:    make(map[string]stateEntry, cfg.MaxEntries),
		order:      make([]string, 0, cfg.MaxEntries),
	}
	metrics.SetCanonicalStateEntries(0)
	return s
}

func (s *StateStore) Entries() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evictExpiredLocked(s.now())
	return len(s.entries)
}

func (s *StateStore) UpsertSnapshot(key StreamKey, seq Seq, snapshot BookSnapshot) *problem.Problem {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.evictExpiredLocked(now)

	entry, ok := s.entries[key.String()]
	if ok && seq <= entry.value.LastSeq {
		return problem.Newf(problem.OutOfOrder, "seq %d must be > last seq %d for stream %s", seq, entry.value.LastSeq, key)
	}
	book := NewBook()
	if p := book.ApplySnapshot(snapshot); p != nil {
		return p
	}

	s.putLocked(key, stateEntry{
		key:      key,
		lastSeen: now,
		value: StreamState{
			Book:    book,
			LastSeq: seq,
		},
	})
	return nil
}

func (s *StateStore) ApplyDelta(key StreamKey, seq Seq, delta BookDelta, snapshotTS int64) (BookSnapshot, *problem.Problem) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.evictExpiredLocked(now)

	entry, ok := s.entries[key.String()]
	if !ok {
		return BookSnapshot{}, problem.Newf(problem.NotFound, "missing stream state for %s", key)
	}
	if seq <= entry.value.LastSeq {
		return BookSnapshot{}, problem.Newf(problem.OutOfOrder, "seq %d must be > last seq %d for stream %s", seq, entry.value.LastSeq, key)
	}
	if p := entry.value.Book.ApplyDelta(delta); p != nil {
		return BookSnapshot{}, p
	}
	entry.value.LastSeq = seq
	entry.lastSeen = now
	s.entries[key.String()] = entry
	metrics.SetCanonicalStateEntries(len(s.entries))
	return entry.value.Book.Snapshot(snapshotTS), nil
}

func (s *StateStore) putLocked(key StreamKey, entry stateEntry) {
	k := key.String()
	if _, ok := s.entries[k]; !ok {
		s.order = append(s.order, k)
	}
	s.entries[k] = entry
	s.evictCapacityLocked()
	metrics.SetCanonicalStateEntries(len(s.entries))
}

func (s *StateStore) evictExpiredLocked(now time.Time) {
	if len(s.entries) == 0 {
		return
	}
	for len(s.order) > 0 {
		k := s.order[0]
		entry, ok := s.entries[k]
		if !ok {
			s.order = s.order[1:]
			continue
		}
		if now.Sub(entry.lastSeen) <= s.ttl {
			break
		}
		delete(s.entries, k)
		s.order = s.order[1:]
		metrics.IncCanonicalStateEvicted("ttl")
	}
	metrics.SetCanonicalStateEntries(len(s.entries))
}

func (s *StateStore) evictCapacityLocked() {
	for len(s.entries) > s.maxEntries && len(s.order) > 0 {
		k := s.order[0]
		s.order = s.order[1:]
		if _, ok := s.entries[k]; !ok {
			continue
		}
		delete(s.entries, k)
		metrics.IncCanonicalStateEvicted("capacity")
	}
}
