package deliveryruntime

import (
	"sort"
	"strings"

	deliveryv1 "github.com/market-raccoon/internal/shared/proto/gen/delivery/v1"
)

const (
	defaultBackpressureElevatedRatio = 0.50
	defaultBackpressureHighRatio     = 0.75
	defaultBackpressureCriticalRatio = 0.95

	defaultBackpressureSampleTopN       = 5
	defaultBackpressureSampleMaxUnique  = 64
	defaultBackpressureSampleFlushEvery = 64
)

const (
	backpressureActionNone                = "none"
	backpressureActionReduceSubscriptions = "reduce_subscriptions"
	backpressureActionReconnect           = "reconnect"
)

const (
	backpressureDropReasonQueueFull            = "queue_full"
	backpressureDropReasonDropOldest           = "drop_oldest"
	backpressureDropReasonPriorityDrop         = "priority_drop"
	backpressureDropReasonPriorityDropSelf     = "priority_drop_self"
	backpressureDropReasonFrameTooLarge        = "frame_too_large"
	backpressureDropReasonSlowClientDisconnect = "slow_client_disconnect"
)

type backpressureStrategy struct {
	elevatedRatio float64
	highRatio     float64
	criticalRatio float64

	sampleTopN       int
	sampleMaxUnique  int
	sampleFlushEvery int
}

type backpressureDropSampleKey struct {
	reason   string
	channel  string
	priority string
}

type backpressureDropSampleEntry struct {
	key   backpressureDropSampleKey
	count int
}

func defaultBackpressureStrategy() backpressureStrategy {
	return backpressureStrategy{
		elevatedRatio:    defaultBackpressureElevatedRatio,
		highRatio:        defaultBackpressureHighRatio,
		criticalRatio:    defaultBackpressureCriticalRatio,
		sampleTopN:       defaultBackpressureSampleTopN,
		sampleMaxUnique:  defaultBackpressureSampleMaxUnique,
		sampleFlushEvery: defaultBackpressureSampleFlushEvery,
	}
}

func (s backpressureStrategy) normalizeDropReason(reason string) string {
	r := strings.ToLower(strings.TrimSpace(reason))
	switch r {
	case backpressureDropReasonQueueFull,
		backpressureDropReasonDropOldest,
		backpressureDropReasonPriorityDrop,
		backpressureDropReasonPriorityDropSelf,
		backpressureDropReasonFrameTooLarge,
		backpressureDropReasonSlowClientDisconnect:
		return r
	default:
		return backpressureDropReasonQueueFull
	}
}

func (s backpressureStrategy) actionHintForDrop(reason string) deliveryv1.ActionHint {
	switch s.normalizeDropReason(reason) {
	case backpressureDropReasonQueueFull,
		backpressureDropReasonPriorityDropSelf,
		backpressureDropReasonSlowClientDisconnect:
		return deliveryv1.ActionHint_ACTION_HINT_RECONNECT
	case backpressureDropReasonFrameTooLarge:
		return deliveryv1.ActionHint_ACTION_HINT_NONE
	default:
		return deliveryv1.ActionHint_ACTION_HINT_NONE
	}
}

func (s backpressureStrategy) queueLevel(queueLen, queueCap int) (level int, action string) {
	if queueCap <= 0 {
		return 0, backpressureActionNone
	}
	ratio := float64(max(queueLen, 0)) / float64(queueCap)
	switch {
	case ratio >= s.criticalRatio:
		return 3, backpressureActionReconnect
	case ratio >= s.highRatio:
		return 2, backpressureActionReduceSubscriptions
	case ratio >= s.elevatedRatio:
		return 1, backpressureActionNone
	default:
		return 0, backpressureActionNone
	}
}

func (s *SessionActor) recordBackpressureDropSample(reason, channel, priority string) {
	reason = s.bpStrategy.normalizeDropReason(reason)
	channel = strings.ToLower(strings.TrimSpace(channel))
	if channel == "" {
		channel = "unknown"
	}
	priority = strings.ToLower(strings.TrimSpace(priority))
	if priority == "" {
		priority = "standard"
	}

	if s.bpDropSamples == nil {
		s.bpDropSamples = make(map[backpressureDropSampleKey]int)
	}
	key := backpressureDropSampleKey{reason: reason, channel: channel, priority: priority}
	if _, exists := s.bpDropSamples[key]; exists {
		s.bpDropSamples[key]++
	} else if len(s.bpDropSamples) < s.bpStrategy.sampleMaxUnique {
		s.bpDropSamples[key] = 1
	} else {
		s.bpDropSampleDrops++
	}
	s.bpDropSampleWindow++
	if s.bpDropSampleWindow >= s.bpStrategy.sampleFlushEvery {
		s.flushBackpressureDropSamples(false)
	}
}

func (s *SessionActor) flushBackpressureDropSamples(force bool) {
	if len(s.bpDropSamples) == 0 {
		s.bpDropSampleWindow = 0
		if force {
			s.bpDropSampleDrops = 0
		}
		return
	}
	if !force && s.bpDropSampleWindow < s.bpStrategy.sampleFlushEvery {
		return
	}

	entries := make([]backpressureDropSampleEntry, 0, len(s.bpDropSamples))
	for key, count := range s.bpDropSamples {
		entries = append(entries, backpressureDropSampleEntry{key: key, count: count})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].count != entries[j].count {
			return entries[i].count > entries[j].count
		}
		if entries[i].key.reason != entries[j].key.reason {
			return entries[i].key.reason < entries[j].key.reason
		}
		if entries[i].key.channel != entries[j].key.channel {
			return entries[i].key.channel < entries[j].key.channel
		}
		return entries[i].key.priority < entries[j].key.priority
	})

	limit := s.bpStrategy.sampleTopN
	if limit <= 0 || limit > len(entries) {
		limit = len(entries)
	}
	for i := 0; i < limit; i++ {
		entry := entries[i]
		s.logger.Warn("delivery session: sampled backpressure drops",
			"reason", entry.key.reason,
			"channel", entry.key.channel,
			"priority", entry.key.priority,
			"count", entry.count,
			"sampled_unique", len(entries),
			"sample_dropped", s.bpDropSampleDrops,
		)
	}

	clear(s.bpDropSamples)
	s.bpDropSampleDrops = 0
	s.bpDropSampleWindow = 0
}
