package observability

import (
	"sort"
	"sync"
	"sync/atomic"
)

const terminalWSMaxStreams = 1024

type TerminalWSStreamState struct {
	StreamID       string `json:"stream_id"`
	Venue          string `json:"venue"`
	Symbol         string `json:"symbol"`
	Channel        string `json:"channel"`
	LastSeq        int64  `json:"last_seq"`
	LastTsIngest   int64  `json:"last_ts_ingest"`
	LastTsServer   int64  `json:"last_ts_server"`
	LastLagMs      int64  `json:"last_lag_ms"`
	DeliveredTotal uint64 `json:"delivered_total"`
	DroppedTotal   uint64 `json:"dropped_total"`
	ResyncTotal    uint64 `json:"resync_total"`
	DropReason     string `json:"drop_reason,omitempty"`
}

type TerminalWSStateSnapshot struct {
	ConnectionsActive    int64                   `json:"connections_active"`
	SubscriptionsActive  int64                   `json:"subscriptions_active"`
	AuthFailTotal        uint64                  `json:"auth_fail_total"`
	SerializeErrorsTotal uint64                  `json:"serialize_errors_total"`
	ResyncTotal          uint64                  `json:"resync_total"`
	DropsTotal           uint64                  `json:"drops_total"`
	Streams              []TerminalWSStreamState `json:"streams"`
}

type terminalWSStore struct {
	mu      sync.Mutex
	streams map[string]*TerminalWSStreamState
	order   []string

	connectionsActive    int64
	subscriptionsActive  int64
	authFailTotal        uint64
	serializeErrorsTotal uint64
	resyncTotal          uint64
	dropsTotal           uint64
}

var globalTerminalWSStore = &terminalWSStore{
	streams: make(map[string]*TerminalWSStreamState),
	order:   make([]string, 0, terminalWSMaxStreams),
}

func SetTerminalWSConnectionsActive(active int64) {
	if active < 0 {
		active = 0
	}
	atomic.StoreInt64(&globalTerminalWSStore.connectionsActive, active)
}

func SetTerminalWSSubscriptionsActive(active int64) {
	if active < 0 {
		active = 0
	}
	atomic.StoreInt64(&globalTerminalWSStore.subscriptionsActive, active)
}

func IncTerminalWSAuthFail() {
	atomic.AddUint64(&globalTerminalWSStore.authFailTotal, 1)
}

func IncTerminalWSSerializeError() {
	atomic.AddUint64(&globalTerminalWSStore.serializeErrorsTotal, 1)
}

func IncTerminalWSResync(streamID string) {
	atomic.AddUint64(&globalTerminalWSStore.resyncTotal, 1)
	globalTerminalWSStore.mu.Lock()
	defer globalTerminalWSStore.mu.Unlock()
	state := globalTerminalWSStore.ensureStreamLocked(streamID)
	state.ResyncTotal++
}

func RecordTerminalWSDelivery(streamID, venue, symbol, channel string, seq, tsIngest, tsServer, lagMs int64) {
	globalTerminalWSStore.mu.Lock()
	defer globalTerminalWSStore.mu.Unlock()
	state := globalTerminalWSStore.ensureStreamLocked(streamID)
	state.Venue = venue
	state.Symbol = symbol
	state.Channel = channel
	if seq > state.LastSeq {
		state.LastSeq = seq
	}
	state.LastTsIngest = tsIngest
	state.LastTsServer = tsServer
	if lagMs < 0 {
		lagMs = 0
	}
	state.LastLagMs = lagMs
	state.DeliveredTotal++
}

func RecordTerminalWSDrop(streamID, venue, symbol, channel, reason string) {
	atomic.AddUint64(&globalTerminalWSStore.dropsTotal, 1)
	globalTerminalWSStore.mu.Lock()
	defer globalTerminalWSStore.mu.Unlock()
	state := globalTerminalWSStore.ensureStreamLocked(streamID)
	state.Venue = venue
	state.Symbol = symbol
	state.Channel = channel
	state.DropReason = reason
	state.DroppedTotal++
}

func SnapshotTerminalWSState(limit int) TerminalWSStateSnapshot {
	if limit <= 0 || limit > terminalWSMaxStreams {
		limit = terminalWSMaxStreams
	}
	snapshot := TerminalWSStateSnapshot{
		ConnectionsActive:    atomic.LoadInt64(&globalTerminalWSStore.connectionsActive),
		SubscriptionsActive:  atomic.LoadInt64(&globalTerminalWSStore.subscriptionsActive),
		AuthFailTotal:        atomic.LoadUint64(&globalTerminalWSStore.authFailTotal),
		SerializeErrorsTotal: atomic.LoadUint64(&globalTerminalWSStore.serializeErrorsTotal),
		ResyncTotal:          atomic.LoadUint64(&globalTerminalWSStore.resyncTotal),
		DropsTotal:           atomic.LoadUint64(&globalTerminalWSStore.dropsTotal),
		Streams:              make([]TerminalWSStreamState, 0, limit),
	}

	globalTerminalWSStore.mu.Lock()
	for _, streamID := range globalTerminalWSStore.order {
		if len(snapshot.Streams) >= limit {
			break
		}
		state, ok := globalTerminalWSStore.streams[streamID]
		if !ok || state == nil {
			continue
		}
		snapshot.Streams = append(snapshot.Streams, *state)
	}
	globalTerminalWSStore.mu.Unlock()

	sort.SliceStable(snapshot.Streams, func(i, j int) bool {
		if snapshot.Streams[i].LastTsServer != snapshot.Streams[j].LastTsServer {
			return snapshot.Streams[i].LastTsServer > snapshot.Streams[j].LastTsServer
		}
		if snapshot.Streams[i].LastSeq != snapshot.Streams[j].LastSeq {
			return snapshot.Streams[i].LastSeq > snapshot.Streams[j].LastSeq
		}
		return snapshot.Streams[i].StreamID < snapshot.Streams[j].StreamID
	})
	return snapshot
}

func (s *terminalWSStore) ensureStreamLocked(streamID string) *TerminalWSStreamState {
	if streamID == "" {
		streamID = "unknown"
	}
	if state, ok := s.streams[streamID]; ok && state != nil {
		return state
	}
	if len(s.streams) >= terminalWSMaxStreams && len(s.order) > 0 {
		evict := s.order[0]
		s.order = s.order[1:]
		delete(s.streams, evict)
	}
	state := &TerminalWSStreamState{StreamID: streamID}
	s.streams[streamID] = state
	s.order = append(s.order, streamID)
	return state
}
