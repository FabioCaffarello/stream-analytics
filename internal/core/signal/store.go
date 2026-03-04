package signal

import (
	"strings"

	evidencedomain "github.com/market-raccoon/internal/core/evidence/domain"
	marketmodel "github.com/market-raccoon/internal/core/marketmodel"
	"github.com/market-raccoon/internal/shared/problem"
)

type StateStoreConfig struct {
	PerStreamWindow    int
	PerTenantStreamCap int
	GlobalStreamCap    int
	TTLMillis          int64
	DedupWindowMillis  int64
}

func DefaultStateStoreConfig() StateStoreConfig {
	return StateStoreConfig{
		PerStreamWindow:    64,
		PerTenantStreamCap: 2048,
		GlobalStreamCap:    10000,
		TTLMillis:          15 * 60 * 1000,
		DedupWindowMillis:  5000,
	}
}

type MarketObservation struct {
	Key      marketmodel.StreamKey
	Tenant   string
	TsServer int64
	Seq      int64
	BestBid  float64
	BestAsk  float64
	BidDepth float64
	AskDepth float64
}

type dedupRecord struct {
	SignalType  string
	Fingerprint string
	TsServer    int64
}

type streamState struct {
	Key          marketmodel.StreamKey
	Tenant       string
	LastSeenTs   int64
	LastSeq      int64
	BestBid      float64
	BestAsk      float64
	BidDepth     float64
	AskDepth     float64
	SignalSeq    int64
	SeqRing      fixedRing[int64]
	EvidenceRing fixedRing[evidencedomain.EvidenceEvent]
	DedupRing    fixedRing[dedupRecord]
}

type StreamSnapshot struct {
	Key             marketmodel.StreamKey
	Tenant          string
	LastSeenTs      int64
	LastSeq         int64
	BestBid         float64
	BestAsk         float64
	BidDepth        float64
	AskDepth        float64
	EvidenceHistory []evidencedomain.EvidenceEvent
	WatermarkStart  int64
	WatermarkEnd    int64
}

type SignalStateStore struct {
	cfg      StateStoreConfig
	streams  map[string]*streamState
	byTenant map[string]map[string]struct{}
}

func NewSignalStateStore(cfg StateStoreConfig) *SignalStateStore {
	if cfg.PerStreamWindow <= 0 {
		cfg.PerStreamWindow = 1
	}
	if cfg.PerTenantStreamCap <= 0 {
		cfg.PerTenantStreamCap = 1
	}
	if cfg.GlobalStreamCap <= 0 {
		cfg.GlobalStreamCap = 1
	}
	if cfg.TTLMillis <= 0 {
		cfg.TTLMillis = 1
	}
	if cfg.DedupWindowMillis <= 0 {
		cfg.DedupWindowMillis = 1
	}
	return &SignalStateStore{
		cfg:      cfg,
		streams:  make(map[string]*streamState, cfg.GlobalStreamCap),
		byTenant: make(map[string]map[string]struct{}),
	}
}

type EvictionReason string

const (
	EvictionReasonTTL    EvictionReason = "ttl"
	EvictionReasonTenant EvictionReason = "tenant_cap"
	EvictionReasonGlobal EvictionReason = "global_cap"
)

func (s *SignalStateStore) StreamEntries() int {
	if s == nil {
		return 0
	}
	return len(s.streams)
}

func (s *SignalStateStore) ObserveMarket(obs MarketObservation) ([]EvictionReason, *problem.Problem) {
	if s == nil {
		return nil, problem.New(problem.ValidationFailed, "signal state store is nil")
	}
	if obs.Seq <= 0 || obs.TsServer <= 0 {
		return nil, problem.New(problem.ValidationFailed, "market observation must carry positive seq and ts_server")
	}
	state, evictions := s.getOrCreate(obs.Key, normalizedTenant(obs.Tenant), obs.TsServer)
	state.LastSeenTs = obs.TsServer
	state.LastSeq = obs.Seq
	state.BestBid = obs.BestBid
	state.BestAsk = obs.BestAsk
	state.BidDepth = obs.BidDepth
	state.AskDepth = obs.AskDepth
	state.SeqRing.Push(obs.Seq)
	return evictions, nil
}

func (s *SignalStateStore) ObserveEvidence(key marketmodel.StreamKey, tenant string, ev evidencedomain.EvidenceEvent) (StreamSnapshot, []EvictionReason, *problem.Problem) {
	if s == nil {
		return StreamSnapshot{}, nil, problem.New(problem.ValidationFailed, "signal state store is nil")
	}
	if p := ev.Validate(); p != nil {
		return StreamSnapshot{}, nil, p
	}
	state, evictions := s.getOrCreate(key, normalizedTenant(tenant), ev.TsServer)
	state.LastSeenTs = ev.TsServer
	state.LastSeq = ev.Seq
	state.SeqRing.Push(ev.Seq)
	state.EvidenceRing.Push(ev)
	return streamSnapshot(state), evictions, nil
}

func (s *SignalStateStore) NextSignalSeq(key marketmodel.StreamKey, tenant string) int64 {
	state, _ := s.getOrCreate(key, normalizedTenant(tenant), 1)
	state.SignalSeq++
	if state.SignalSeq <= 0 {
		state.SignalSeq = 1
	}
	return state.SignalSeq
}

func (s *SignalStateStore) IsDuplicate(key marketmodel.StreamKey, tenant, signalType, fingerprint string, tsServer int64) bool {
	state, _ := s.getOrCreate(key, normalizedTenant(tenant), tsServer)
	records := state.DedupRing.Values()
	for i := range records {
		delta := tsServer - records[i].TsServer
		if delta < 0 {
			delta = -delta
		}
		if delta <= s.cfg.DedupWindowMillis && records[i].SignalType == signalType && records[i].Fingerprint == fingerprint {
			return true
		}
	}
	state.DedupRing.Push(dedupRecord{SignalType: signalType, Fingerprint: fingerprint, TsServer: tsServer})
	return false
}

func (s *SignalStateStore) getOrCreate(key marketmodel.StreamKey, tenant string, tsServer int64) (*streamState, []EvictionReason) {
	evictions := make([]EvictionReason, 0, 4)
	evictions = append(evictions, s.evictExpired(tsServer)...)

	streamID := keyedStreamID(tenant, key)
	if existing, ok := s.streams[streamID]; ok {
		if tsServer > 0 {
			existing.LastSeenTs = max64(existing.LastSeenTs, tsServer)
		}
		return existing, evictions
	}

	tenants := s.byTenant[tenant]
	if tenants == nil {
		tenants = make(map[string]struct{})
		s.byTenant[tenant] = tenants
	}
	if len(tenants) >= s.cfg.PerTenantStreamCap {
		victim := s.oldestStream(func(st *streamState) bool { return st != nil && st.Tenant == tenant })
		if victim != "" {
			s.deleteStream(victim)
			evictions = append(evictions, EvictionReasonTenant)
		}
	}
	if len(s.streams) >= s.cfg.GlobalStreamCap {
		victim := s.oldestStream(func(st *streamState) bool { return st != nil })
		if victim != "" {
			s.deleteStream(victim)
			evictions = append(evictions, EvictionReasonGlobal)
		}
	}

	created := &streamState{
		Key:          key,
		Tenant:       tenant,
		LastSeenTs:   tsServer,
		SeqRing:      newFixedRing[int64](s.cfg.PerStreamWindow),
		EvidenceRing: newFixedRing[evidencedomain.EvidenceEvent](s.cfg.PerStreamWindow),
		DedupRing:    newFixedRing[dedupRecord](s.cfg.PerStreamWindow),
	}
	s.streams[streamID] = created
	tenants[streamID] = struct{}{}
	return created, evictions
}

func (s *SignalStateStore) evictExpired(tsServer int64) []EvictionReason {
	if tsServer <= 0 {
		return nil
	}
	out := make([]EvictionReason, 0)
	for id, st := range s.streams {
		if st == nil {
			s.deleteStream(id)
			out = append(out, EvictionReasonTTL)
			continue
		}
		if tsServer-st.LastSeenTs > s.cfg.TTLMillis {
			s.deleteStream(id)
			out = append(out, EvictionReasonTTL)
		}
	}
	return out
}

func (s *SignalStateStore) oldestStream(filter func(*streamState) bool) string {
	victim := ""
	oldest := int64(0)
	for id, st := range s.streams {
		if st == nil || !filter(st) {
			continue
		}
		if victim == "" || st.LastSeenTs < oldest || (st.LastSeenTs == oldest && strings.Compare(id, victim) < 0) {
			victim = id
			oldest = st.LastSeenTs
		}
	}
	return victim
}

func (s *SignalStateStore) deleteStream(streamID string) {
	st, ok := s.streams[streamID]
	if !ok {
		return
	}
	delete(s.streams, streamID)
	if tenantStreams, ok := s.byTenant[st.Tenant]; ok {
		delete(tenantStreams, streamID)
		if len(tenantStreams) == 0 {
			delete(s.byTenant, st.Tenant)
		}
	}
}

func streamSnapshot(st *streamState) StreamSnapshot {
	seqStart := int64(0)
	if oldest, ok := st.SeqRing.Oldest(); ok {
		seqStart = oldest
	}
	seqEnd := st.LastSeq
	if newest, ok := st.SeqRing.Newest(); ok && newest > seqEnd {
		seqEnd = newest
	}
	return StreamSnapshot{
		Key:             st.Key,
		Tenant:          st.Tenant,
		LastSeenTs:      st.LastSeenTs,
		LastSeq:         st.LastSeq,
		BestBid:         st.BestBid,
		BestAsk:         st.BestAsk,
		BidDepth:        st.BidDepth,
		AskDepth:        st.AskDepth,
		EvidenceHistory: st.EvidenceRing.Values(),
		WatermarkStart:  seqStart,
		WatermarkEnd:    seqEnd,
	}
}

func keyedStreamID(tenant string, key marketmodel.StreamKey) string {
	return tenant + "|" + key.String()
}

func normalizedTenant(v string) string {
	v = strings.TrimSpace(strings.ToLower(v))
	if v == "" {
		return "default"
	}
	return v
}

func max64(a, b int64) int64 {
	if a >= b {
		return a
	}
	return b
}
