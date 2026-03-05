package deliveryruntime

import (
	"time"

	"github.com/gorilla/websocket"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/observability"
)

// ── Periodic tick handlers ──────────────────────────────────────────────────

func (s *SessionActor) handleKeepaliveTick() {
	if s.closed || s.cfg.Conn == nil {
		return
	}
	if err := s.cfg.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
		s.engine.Send(s.self, sessionDisconnected{})
	}
}

func (s *SessionActor) handleMetricsTick() {
	if s.closed || s.cfg.Conn == nil || s.session == nil {
		return
	}
	snapshot := observability.SnapshotTerminalWSState(1)
	serializeErrors := saturatingUint64ToInt64(snapshot.SerializeErrorsTotal)
	if serializeErrors < 0 {
		serializeErrors = 0
	}
	resyncTotal := saturatingUint64ToInt64(snapshot.ResyncTotal)
	if resyncTotal < 0 {
		resyncTotal = 0
	}
	msgOut := s.messagesOut
	if msgOut < 0 {
		msgOut = 0
	}
	lag := s.lastLagMs
	if lag < 0 {
		lag = 0
	}
	if s.cfg.TranscodeCache != nil {
		metrics.SetTranscodeCacheEntries(s.cfg.TranscodeCache.Len())
		hits, misses := s.cfg.TranscodeCache.Stats()
		metrics.SetTranscodeCacheHits(hits)
		metrics.SetTranscodeCacheMisses(misses)
	}
	bpLevel, bpAction := s.computeBackpressureLevel()
	hwm := s.queueHighWatermark
	s.queueHighWatermark = 0
	metrics.SetWSBackpressureLevel(bpLevel)
	metrics.SetWSQueueHighWatermark(hwm)
	metrics.SetWSQueueCapacity(s.limits.OutboundQueueSize)
	metrics.IncWSControlFrame("metrics")
	s.writeJSON(wsMetricsFrame{
		Type: "metrics",
		Payload: wsMetricsPayload{
			WSDroppedTotal:            int64(s.dropCount),
			WSQueueLen:                s.outbound.Len(),
			WSLagMs:                   lag,
			PublishToDeliverLatencyMs: lag,
			SerializeErrorsTotal:      serializeErrors,
			ResyncTotal:               resyncTotal,
			ActiveSubscriptions:       len(s.session.Subscriptions()),
			MessagesOutTotal:          msgOut,
			BackpressureLevel:         bpLevel,
			RecommendedAction:         bpAction,
			QueueCapacity:             s.limits.OutboundQueueSize,
			QueueHighWatermark:        hwm,
		},
	})
}

// ── Numeric helpers ─────────────────────────────────────────────────────────

func saturatingUint64ToInt64(v uint64) int64 {
	max := uint64(^uint64(0) >> 1)
	if v > max {
		return int64(max)
	}
	return int64(v)
}

func (s *SessionActor) normalizeServerTS(ts int64) int64 {
	if ts > 0 {
		return ts
	}
	metrics.IncWSContractViolation("missing_ts_server")
	nowMs := time.Now().UnixMilli()
	if s != nil && s.cfg.Clock != nil {
		nowMs = s.cfg.Clock.Now().UnixMilli()
	}
	if nowMs <= 0 {
		nowMs = 1
	}
	return nowMs
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// clockNowMs returns current time in milliseconds using session clock.
func (s *SessionActor) clockNowMs() int64 {
	nowMs := time.Now().UnixMilli()
	if s.cfg.Clock != nil {
		nowMs = s.cfg.Clock.Now().UnixMilli()
	}
	return nowMs
}
