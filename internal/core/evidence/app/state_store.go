package app

import (
	"sort"
	"strings"
)

// EvidenceStateStoreConfig controls bounded state behavior for LEL.
type EvidenceStateStoreConfig struct {
	MaxEntries int
	TTLMillis  int64
}

// StateEviction reports one deterministic eviction decision.
type StateEviction struct {
	StreamID string
	Reason   string
}

// StateObserveResult summarizes state actions for one input event.
type StateObserveResult struct {
	Accepted  bool
	Reason    string
	PrevTs    int64
	Evictions []StateEviction
}

type streamState struct {
	LastTs  int64
	LastSeq int64
}

// EvidenceStateStore tracks stream activity with deterministic bounded eviction.
type EvidenceStateStore struct {
	maxEntries int
	ttlMillis  int64
	entries    map[string]streamState
}

func NewEvidenceStateStore(cfg EvidenceStateStoreConfig) *EvidenceStateStore {
	if cfg.MaxEntries <= 0 {
		cfg.MaxEntries = 1024
	}
	if cfg.TTLMillis <= 0 {
		cfg.TTLMillis = 10 * 60 * 1000
	}
	return &EvidenceStateStore{
		maxEntries: cfg.MaxEntries,
		ttlMillis:  cfg.TTLMillis,
		entries:    make(map[string]streamState, cfg.MaxEntries),
	}
}

func (s *EvidenceStateStore) Len() int {
	if s == nil {
		return 0
	}
	return len(s.entries)
}

func (s *EvidenceStateStore) Observe(streamID string, seq, tsServer int64) StateObserveResult {
	if s == nil {
		return StateObserveResult{Accepted: false, Reason: "nil_store"}
	}
	streamID = strings.TrimSpace(streamID)
	if streamID == "" {
		return StateObserveResult{Accepted: false, Reason: "empty_stream_id"}
	}
	if seq <= 0 {
		return StateObserveResult{Accepted: false, Reason: "invalid_seq"}
	}
	if tsServer <= 0 {
		return StateObserveResult{Accepted: false, Reason: "invalid_ts_server"}
	}

	result := StateObserveResult{Accepted: true}
	result.Evictions = append(result.Evictions, s.evictTTL(tsServer)...)

	prev, exists := s.entries[streamID]
	if exists {
		result.PrevTs = prev.LastTs
		if seq <= prev.LastSeq {
			return StateObserveResult{
				Accepted:  false,
				Reason:    "non_monotonic_seq",
				PrevTs:    prev.LastTs,
				Evictions: result.Evictions,
			}
		}
	}

	s.entries[streamID] = streamState{LastTs: tsServer, LastSeq: seq}
	if len(s.entries) > s.maxEntries {
		victim := s.oldestStream()
		if victim != "" {
			delete(s.entries, victim)
			result.Evictions = append(result.Evictions, StateEviction{
				StreamID: victim,
				Reason:   "capacity",
			})
		}
	}
	return result
}

func (s *EvidenceStateStore) evictTTL(currentTs int64) []StateEviction {
	if len(s.entries) == 0 || s.ttlMillis <= 0 {
		return nil
	}
	keys := make([]string, 0, len(s.entries))
	for k := range s.entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	evicted := make([]StateEviction, 0, 4)
	for _, k := range keys {
		v := s.entries[k]
		if currentTs-v.LastTs <= s.ttlMillis {
			continue
		}
		delete(s.entries, k)
		evicted = append(evicted, StateEviction{
			StreamID: k,
			Reason:   "ttl",
		})
	}
	return evicted
}

func (s *EvidenceStateStore) oldestStream() string {
	if len(s.entries) == 0 {
		return ""
	}

	keys := make([]string, 0, len(s.entries))
	for k := range s.entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	victim := keys[0]
	victimState := s.entries[victim]
	for i := 1; i < len(keys); i++ {
		k := keys[i]
		v := s.entries[k]
		if v.LastTs < victimState.LastTs {
			victim = k
			victimState = v
		}
	}
	return victim
}
