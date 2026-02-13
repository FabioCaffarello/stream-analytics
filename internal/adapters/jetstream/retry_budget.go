package jetstream

import (
	"log/slog"
	"strconv"
	"strings"
	"sync"

	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/nats-io/nats.go"
)

const (
	defaultTransientRetryBudgetDeliveries = 3
	defaultRetryBudgetFallbackCapacity    = 1024

	jsHeaderNumDelivered = "Nats-Num-Delivered"
	jsHeaderMsgDelivery  = "Nats-Msg-Redelivery"
)

type retryBudgetEntry struct {
	attempts int
	slot     int
}

// retryBudgetTracker is bounded by a fixed-size ring and keyed by partition.
// It is used only when JetStream delivery metadata/headers are unavailable.
type retryBudgetTracker struct {
	mu      sync.Mutex
	maxSize int
	next    int
	ring    []string
	entries map[string]retryBudgetEntry
}

func newRetryBudgetTracker(maxSize int) *retryBudgetTracker {
	if maxSize <= 0 {
		maxSize = defaultRetryBudgetFallbackCapacity
	}
	return &retryBudgetTracker{
		maxSize: maxSize,
		ring:    make([]string, maxSize),
		entries: make(map[string]retryBudgetEntry, maxSize),
	}
}

func (t *retryBudgetTracker) increment(key string) (attempts int, evicted bool) {
	if t == nil || strings.TrimSpace(key) == "" {
		return 0, false
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if entry, ok := t.entries[key]; ok {
		entry.attempts++
		t.entries[key] = entry
		return entry.attempts, false
	}

	slot := t.next
	victim := t.ring[slot]
	if victim != "" {
		delete(t.entries, victim)
		evicted = true
	}

	t.ring[slot] = key
	t.entries[key] = retryBudgetEntry{attempts: 1, slot: slot}
	t.next = (slot + 1) % t.maxSize
	return 1, evicted
}

func (t *retryBudgetTracker) reset(key string) {
	if t == nil || strings.TrimSpace(key) == "" {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	entry, ok := t.entries[key]
	if !ok {
		return
	}
	if entry.slot >= 0 && entry.slot < len(t.ring) {
		t.ring[entry.slot] = ""
	}
	delete(t.entries, key)
}

func (t *retryBudgetTracker) size() int {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.entries)
}

func withTransientRetryBudget(maxDeliver int) int {
	limit := defaultTransientRetryBudgetDeliveries
	if maxDeliver > 0 && maxDeliver < limit {
		limit = maxDeliver
	}
	if limit <= 0 {
		return 1
	}
	return limit
}

func (c *Consumer) applyTransientRetryBudget(msg *nats.Msg, env envelope.Envelope, meta *nats.MsgMetadata, decision ingestDecision) ingestDecision {
	if c == nil || msg == nil {
		return decision
	}
	if decision.Disposition != DispositionNak {
		c.resetRetryBudgetPartition(msg, env)
		return decision
	}
	if !isRetryBudgetReason(decision.ReasonCode) {
		return decision
	}
	if c.transientRetryBudget <= 0 {
		return decision
	}

	if delivered, ok := deliveredCount(meta, msg.Header); ok {
		if delivered >= c.transientRetryBudget {
			c.resetRetryBudgetPartition(msg, env)
			return exhaustedTransientDecision()
		}
		return decision
	}

	partition := retryBudgetPartition(msg, env)
	attempts, evicted := c.retryBudget.increment(partition)
	if evicted {
		metrics.IncIngestDrop(ingestReasonBufferFullDrop)
		slog.Warn(
			"jetstream: retry budget fallback buffer full; evicting partition state",
			"reason_code", ingestReasonBufferFullDrop,
		)
	}
	if attempts >= c.transientRetryBudget {
		c.retryBudget.reset(partition)
		return exhaustedTransientDecision()
	}
	return decision
}

func (c *Consumer) resetRetryBudgetPartition(msg *nats.Msg, env envelope.Envelope) {
	if c == nil || c.retryBudget == nil || msg == nil {
		return
	}
	c.retryBudget.reset(retryBudgetPartition(msg, env))
}

func deliveredCount(meta *nats.MsgMetadata, header nats.Header) (int, bool) {
	if meta != nil && meta.NumDelivered > 0 {
		const maxInt = int(^uint(0) >> 1)
		if meta.NumDelivered > uint64(maxInt) {
			return maxInt, true
		}
		return int(meta.NumDelivered), true
	}
	if header == nil {
		return 0, false
	}
	if delivered, ok := parsePositiveInt(strings.TrimSpace(header.Get(jsHeaderNumDelivered))); ok {
		return delivered, true
	}
	if redelivered, ok := parseNonNegativeInt(strings.TrimSpace(header.Get(jsHeaderMsgDelivery))); ok {
		return redelivered + 1, true
	}
	return 0, false
}

func parsePositiveInt(raw string) (int, bool) {
	value, ok := parseNonNegativeInt(raw)
	if !ok || value <= 0 {
		return 0, false
	}
	return value, true
}

func parseNonNegativeInt(raw string) (int, bool) {
	if raw == "" {
		return 0, false
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0, false
	}
	return value, true
}

func retryBudgetPartition(msg *nats.Msg, env envelope.Envelope) string {
	venue := strings.ToLower(strings.TrimSpace(env.Venue))
	instrument := strings.ToUpper(strings.TrimSpace(env.Instrument))
	if venue == "" || instrument == "" {
		_, _, subjectVenue, subjectInstrument := parseSubjectMeta(msg.Subject)
		if venue == "" {
			venue = strings.ToLower(strings.TrimSpace(subjectVenue))
		}
		if instrument == "" {
			instrument = strings.ToUpper(strings.TrimSpace(subjectInstrument))
		}
	}
	if venue == "" {
		venue = "unknown"
	}
	if instrument == "" {
		instrument = "unknown"
	}
	return venue + "|" + instrument
}

func isRetryBudgetReason(reason string) bool {
	switch normalizeIngestReason(reason) {
	case ingestReasonTransientFailure, ingestReasonQuarantinePublishError:
		return true
	default:
		return false
	}
}

func exhaustedTransientDecision() ingestDecision {
	return ingestDecision{
		Disposition: DispositionTerm,
		Status:      "term",
		ReasonCode:  ingestReasonTransientExhausted,
	}
}
