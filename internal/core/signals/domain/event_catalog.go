// Package domain — Signal Composition Layer (Tier 2).
//
// # Conceptual Model
//
// The signal layer is a two-tier pipeline:
//
//	Evidence → [signal/] Detection → [signals/] Composition → [strategy/] Intent
//
// Tier 2 (this package): Deterministic signal composition that enriches
// microstructure evidence with regime context and cross-venue correlation.
// Receives EvidenceEvent + optional RegimeSignal, applies composition rules
// (confidence threshold, regime boost, cross-venue confirmation), and emits
// CompositeSignalV1 via the SignalPublisher port.
//
// Tier 2 operates in parallel with Tier 1 (signal/) — both consume
// EvidenceEvent directly. A downstream orchestrator merges outputs before
// handing off to strategy.
//
// # Ownership
//
//   - Input:   evidence.EvidenceEvent         (owned by evidence BC)
//   - Context: evidence.RegimeSignal           (owned by evidence BC)
//   - Output:  CompositeSignalV1               (owned by this package)
//   - Port:    ports.SignalPublisher            (owned by signals BC)
//
// # Handoff to Strategy
//
// Strategy consumes signals via IntentInput which requires SignalID and
// CorrelationID. CompositeSignalV1 carries both fields to enable direct
// lineage tracking through IntentProvenance.ParentSignalIDs.
//
// # Feature Type Note
//
// CompositeSignalV1 uses SignalFeature{Label, Value string} (transport-safe
// string pairs) while marketmodel.SignalEvent uses SignalFeature{Key string,
// Value float64} (numeric). This is intentional — composite signals carry
// pre-formatted evidence features for downstream consumers that may not
// need numeric precision. The strategy layer accesses confidence/severity
// directly, not through features.
package domain

// CompositionEventContract describes one signal composition event type.
type CompositionEventContract struct {
	Type       string // stable event type string
	Version    int    // schema version
	OwnerBC    string // bounded context that owns the schema
	ProducerBC string // bounded context that emits the event
}

// CompositionEventCatalog is the canonical registry of all signal composition event types.
//
// Governance rules:
//   - OwnerBC owns the schema; ProducerBC may differ when an actor wraps the composer.
//   - Version must match delivery envelope_policy registration.
//   - Adding a new entry here requires a matching delivery policy entry.
var CompositionEventCatalog = []CompositionEventContract{
	{Type: CompositeSignalType, Version: CompositeSignalVersion, OwnerBC: "signals", ProducerBC: "signals"},
}

// CompositionEventCatalogByType returns a map keyed by event type for O(1) lookup.
func CompositionEventCatalogByType() map[string]CompositionEventContract {
	m := make(map[string]CompositionEventContract, len(CompositionEventCatalog))
	for _, e := range CompositionEventCatalog {
		m[e.Type] = e
	}
	return m
}
