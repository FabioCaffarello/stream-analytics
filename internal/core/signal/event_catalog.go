// Package signal — Signal Detection Engine (Tier 1).
//
// # Conceptual Model
//
// The signal layer is a two-tier pipeline:
//
//	Evidence → [signal/] Detection → [signals/] Composition → [strategy/] Intent
//
// Tier 1 (this package): Deterministic rule-based detection of microstructure
// anomalies. Receives EvidenceEvent from the evidence bounded context, evaluates
// windowed detection rules (regime change, liquidity collapse, persistent
// imbalance, venue divergence), and emits SignalEvent via the SignalEmitter port.
//
// Each detection rule is a pure function: same input → same output.
// State is bounded (ring buffers, per-tenant caps, TTL eviction).
// Dedup and rate limiting are applied per-tenant before emission.
//
// # Ownership
//
//   - Input:  evidence.EvidenceEvent       (owned by evidence BC)
//   - Output: marketmodel.SignalEvent       (wire contract, owned by marketmodel)
//   - State:  SignalStateStore              (owned by this package)
//   - Port:   SignalEmitter                 (owned by this package)
//
// # Handoff to Tier 2
//
// Signal detection emissions (Tier 1) are independent of signal composition
// (Tier 2, signals/). Both tiers consume EvidenceEvent directly — they are
// parallel evaluation paths, not serial. A downstream orchestrator may feed
// both tiers from the same evidence stream and merge their outputs before
// handing off to strategy.
package signal

// DetectionEventContract describes one signal detection event type.
type DetectionEventContract struct {
	Type       string // stable event type string
	Version    int    // schema version
	OwnerBC    string // bounded context that owns the schema
	ProducerBC string // bounded context that emits the event
	RuleID     string // detection rule that produces this event
}

// DetectionEventCatalog is the canonical registry of all signal detection event types.
//
// Governance rules:
//   - OwnerBC owns the schema; ProducerBC may differ when an actor wraps the engine.
//   - Version must match delivery envelope_policy registration.
//   - Adding a new entry here requires a matching delivery policy entry.
var DetectionEventCatalog = []DetectionEventContract{
	{Type: "regime_change", Version: EventVersion, OwnerBC: "signal", ProducerBC: "signal", RuleID: "regime_change_rule"},
	{Type: "liquidity_collapse", Version: EventVersion, OwnerBC: "signal", ProducerBC: "signal", RuleID: "liquidity_collapse_rule"},
	{Type: "persistent_imbalance_signal", Version: EventVersion, OwnerBC: "signal", ProducerBC: "signal", RuleID: "persistent_imbalance_rule"},
	{Type: "venue_divergence_signal", Version: EventVersion, OwnerBC: "signal", ProducerBC: "signal", RuleID: "venue_divergence_rule"},
}

// DetectionEventCatalogByType returns a map keyed by event type for O(1) lookup.
func DetectionEventCatalogByType() map[string]DetectionEventContract {
	m := make(map[string]DetectionEventContract, len(DetectionEventCatalog))
	for _, e := range DetectionEventCatalog {
		m[e.Type] = e
	}
	return m
}
