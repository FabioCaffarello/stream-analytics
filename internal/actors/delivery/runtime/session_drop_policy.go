package deliveryruntime

import (
	"strings"

	"github.com/market-raccoon/internal/core/delivery/domain"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/observability"
	"github.com/market-raccoon/internal/shared/problem"
	deliveryv1 "github.com/market-raccoon/internal/shared/proto/gen/delivery/v1"
)

// enqueueDelivery is the hot-path backpressure policy executor.
// Kept in a dedicated unit to isolate drop-policy decisions from wire writing.
func (s *SessionActor) enqueueDelivery(evt DeliveryEvent) {
	if s.outbound.IsFull() {
		switch s.policy {
		case domain.BackpressureDropNewest:
			if s.onDrop("queue_full", &evt) {
				return
			}
			return
		case domain.BackpressureDropOldest:
			s.outbound.DropFront()
			if s.onDrop("drop_oldest", &evt) {
				return
			}
		case domain.BackpressurePriorityDrop:
			if !s.priorityDrop(evt) {
				if s.onDrop("priority_drop_self", &evt) {
					return
				}
				return
			}
			if s.onDrop("priority_drop", &evt) {
				return
			}
			metrics.SetWSQueueDepth(s.outbound.Len())
			if s.flushing {
				return
			}
			s.flushing = true
			s.engine.Send(s.self, sessionFlushOutbound{})
			return
		default:
			if s.onDrop("queue_full", &evt) {
				return
			}
			return
		}
	}
	s.outbound.PushBack(evt)
	qLen := s.outbound.Len()
	metrics.SetWSQueueDepth(qLen)
	metrics.SetWSTenantQueueDepth(s.cfg.TenantID, qLen)
	if qLen > s.queueHighWatermark {
		s.queueHighWatermark = qLen
	}
	if s.flushing {
		return
	}
	s.flushing = true
	s.engine.Send(s.self, sessionFlushOutbound{})
}

func (s *SessionActor) priorityDrop(evt DeliveryEvent) bool {
	if !s.outbound.IsFull() {
		s.outbound.PushBack(evt)
		return true
	}
	incomingScore := s.dropScore(evt.Env.Type)
	lowestIdx := -1
	lowestScore := incomingScore
	for i := 0; i < s.outbound.Len(); i++ {
		score := s.dropScore(s.outbound.At(i).Env.Type)
		if score < lowestScore {
			lowestScore = score
			lowestIdx = i
		}
	}
	if lowestIdx < 0 {
		return false
	}
	s.outbound.RemoveAt(lowestIdx)
	s.outbound.PushBack(evt)
	return true
}

func (s *SessionActor) onDrop(reason string, evt *DeliveryEvent) bool {
	switch reason {
	case "queue_full", "priority_drop_self":
		s.writeProblem("delivery", "",
			withWSLimitProblemDetails(
				problem.New(problem.Unavailable, "outbound queue limit reached"),
				wsLimitTypeOutboundQueue,
				deliveryv1.ActionHint_ACTION_HINT_RECONNECT,
			),
		)
	case "frame_too_large":
		s.writeProblem("delivery", "",
			withWSLimitProblemDetails(
				problem.Newf(problem.ValidationFailed, "event exceeds max_frame_bytes (%d)", s.limits.MaxFrameBytes),
				wsLimitTypeMaxFrameBytes,
				deliveryv1.ActionHint_ACTION_HINT_NONE,
			),
		)
	}

	metrics.IncWSDrops(reason)
	metrics.IncWSTenantDrop(s.cfg.TenantID, reason)
	channel := "unknown"
	streamID := "unknown"
	venue := "unknown"
	symbol := "unknown"
	if evt != nil {
		streamID = evt.Subject.String()
		venue = evt.Subject.Venue
		symbol = evt.Subject.Symbol
		channel = channelName(channelEnumFromStreamType(evt.Subject.StreamType), evt.Subject.StreamType)
	}
	metrics.IncWSDropped(reason, channel, s.dropPriorityLabel(evt))
	observability.RecordTerminalWSDrop(streamID, venue, symbol, channel, reason)
	s.dropCount++
	threshold := s.cfg.SlowClientDropThreshold
	if threshold <= 0 || s.dropCount < threshold {
		return false
	}

	metrics.IncWSDrops("slow_client_disconnect")
	s.writeProblem("delivery", "",
		withWSLimitProblemDetails(
			problem.New(problem.Unavailable, "outbound queue limit reached; disconnecting slow client"),
			wsLimitTypeOutboundQueue,
			deliveryv1.ActionHint_ACTION_HINT_RECONNECT,
		),
	)
	s.logger.Warn(
		"delivery session: slow client disconnected after drop threshold breach",
		"client_id", s.cfg.ClientID,
		"session_id", s.session.ID(),
		"drops", s.dropCount,
		"threshold", threshold,
		"reason", reason,
	)
	s.closeSession()
	return true
}

func (s *SessionActor) eventPriority(eventType string) int {
	if s.priorities == nil {
		return 0
	}
	return s.priorities[eventType]
}

func (s *SessionActor) dropScore(eventType string) int {
	score := s.eventPriority(eventType) * 10
	if s.isHighVolumeEventType(eventType) {
		score -= 5
	}
	return score
}

func (s *SessionActor) isHighVolumeEventType(eventType string) bool {
	return strings.EqualFold(strings.TrimSpace(eventType), "marketdata.bookdelta")
}

func (s *SessionActor) dropPriorityLabel(evt *DeliveryEvent) string {
	if evt == nil {
		return "control"
	}
	if s.isHighVolumeEventType(evt.Env.Type) {
		return "high_volume"
	}
	if s.eventPriority(evt.Env.Type) >= 80 {
		return "critical"
	}
	return "standard"
}
