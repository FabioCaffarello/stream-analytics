package app

// adapterHealth tracks per-adapter consecutive failure counts for circuit breaker
// behavior. When an adapter accumulates failureThreshold consecutive failures the
// circuit trips. Subsequent dispatches to that adapter are rejected until the
// cooldown period elapses. After cooldown the circuit enters half-open state:
// the next call is allowed through, and a success fully resets the circuit while
// another failure re-trips it immediately.
type adapterHealth struct {
	failureThreshold int
	cooldownMs       int64
	adapters         map[string]*adapterCircuit
}

type adapterCircuit struct {
	consecutiveFailures int
	trippedAtMs         int64 // 0 = not tripped
}

func newAdapterHealth(failureThreshold int, cooldownMs int64) *adapterHealth {
	if failureThreshold <= 0 {
		failureThreshold = 5
	}
	if cooldownMs <= 0 {
		cooldownMs = 30_000
	}
	return &adapterHealth{
		failureThreshold: failureThreshold,
		cooldownMs:       cooldownMs,
		adapters:         make(map[string]*adapterCircuit),
	}
}

// isTripped returns true if the adapter circuit is open (too many consecutive
// failures and the cooldown has not yet elapsed). When the cooldown expires the
// circuit transitions to half-open: failure count resets and one probe call is
// allowed through.
func (h *adapterHealth) isTripped(adapterID string, nowMs int64) bool {
	circuit := h.adapters[adapterID]
	if circuit == nil || circuit.trippedAtMs == 0 {
		return false
	}
	if nowMs-circuit.trippedAtMs >= h.cooldownMs {
		// Cooldown expired -- transition to half-open, allow one probe.
		circuit.trippedAtMs = 0
		circuit.consecutiveFailures = 0
		return false
	}
	return true
}

// recordSuccess resets the failure counter for an adapter, closing the circuit.
func (h *adapterHealth) recordSuccess(adapterID string) {
	circuit := h.adapters[adapterID]
	if circuit == nil {
		return
	}
	circuit.consecutiveFailures = 0
	circuit.trippedAtMs = 0
}

// recordFailure increments the consecutive failure count for an adapter.
// Returns true if the circuit just tripped (transitioned from closed to open).
func (h *adapterHealth) recordFailure(adapterID string, nowMs int64) bool {
	circuit := h.adapters[adapterID]
	if circuit == nil {
		circuit = &adapterCircuit{}
		h.adapters[adapterID] = circuit
	}
	circuit.consecutiveFailures++
	if circuit.consecutiveFailures >= h.failureThreshold && circuit.trippedAtMs == 0 {
		circuit.trippedAtMs = nowMs
		return true
	}
	return false
}

// snapshot returns a read-only view of current health state for all tracked adapters.
func (h *adapterHealth) snapshot() map[string]AdapterHealthSnapshot {
	out := make(map[string]AdapterHealthSnapshot, len(h.adapters))
	for id, circuit := range h.adapters {
		out[id] = AdapterHealthSnapshot{
			ConsecutiveFailures: circuit.consecutiveFailures,
			TrippedAtMs:         circuit.trippedAtMs,
		}
	}
	return out
}

// AdapterHealthSnapshot is a read-only view of a single adapter's circuit state.
type AdapterHealthSnapshot struct {
	ConsecutiveFailures int   `json:"consecutive_failures"`
	TrippedAtMs         int64 `json:"tripped_at_ms"`
}
