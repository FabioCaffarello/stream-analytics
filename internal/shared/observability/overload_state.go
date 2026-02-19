package observability

import "sync"

const policyKitOverloadStateMaxEntries = 64

type PolicyKitThreshold struct {
	QueueRatio   float64
	BacklogRatio float64
	MapRatio     float64
	LatencyMs    float64
}

type PolicyKitThresholdPair struct {
	Enter   PolicyKitThreshold
	Recover PolicyKitThreshold
}

type PolicyKitOverloadEntry struct {
	Stream        string
	Venue         string
	OverloadLevel int
	Stride        int
	Thresholds    PolicyKitThresholdPair
}

type policyKitOverloadStore struct {
	mu      sync.Mutex
	order   []string
	entries map[string]PolicyKitOverloadEntry
}

var globalPolicyKitOverloadStore = newPolicyKitOverloadStore()

func newPolicyKitOverloadStore() *policyKitOverloadStore {
	return &policyKitOverloadStore{
		order:   make([]string, 0, policyKitOverloadStateMaxEntries),
		entries: make(map[string]PolicyKitOverloadEntry, policyKitOverloadStateMaxEntries),
	}
}

func UpdatePolicyKitOverload(entry PolicyKitOverloadEntry) {
	globalPolicyKitOverloadStore.update(entry)
}

func SnapshotPolicyKitOverload() []PolicyKitOverloadEntry {
	return globalPolicyKitOverloadStore.snapshot()
}

func (s *policyKitOverloadStore) update(entry PolicyKitOverloadEntry) {
	stream := sanitizeOverloadPart(entry.Stream)
	venue := sanitizeOverloadPart(entry.Venue)
	key := stream + "|" + venue

	s.mu.Lock()
	defer s.mu.Unlock()

	entry.Stream = stream
	entry.Venue = venue
	if _, exists := s.entries[key]; !exists {
		if len(s.order) >= policyKitOverloadStateMaxEntries {
			evict := s.order[0]
			s.order = s.order[1:]
			delete(s.entries, evict)
		}
		s.order = append(s.order, key)
	}
	s.entries[key] = entry
}

func (s *policyKitOverloadStore) snapshot() []PolicyKitOverloadEntry {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]PolicyKitOverloadEntry, 0, len(s.order))
	for _, key := range s.order {
		entry, ok := s.entries[key]
		if !ok {
			continue
		}
		out = append(out, entry)
	}
	return out
}

func sanitizeOverloadPart(v string) string {
	if v == "" {
		return "unknown"
	}
	return v
}
