// Package domain events.go centralizes the insights event catalog.
//
// Every event type emitted by the insights bounded context is registered here
// with its stable type string, schema version, owner BC, and producer BC.
// Individual domain files (volume_profile.go, heatmap_bucket.go, etc.) retain
// their original constants for backward compatibility — this file is the
// canonical cross-reference.
package domain

// EventContract describes one insights event type for delivery governance.
type EventContract struct {
	Type       string // stable event type string
	Version    int    // schema version
	OwnerBC    string // bounded context that owns the schema
	ProducerBC string // bounded context that emits the event
}

// EventCatalog is the canonical registry of all insights event types.
//
// Governance rules:
//   - OwnerBC owns the schema; ProducerBC may differ (e.g. aggregation produces VPVR).
//   - Version must match delivery envelope_policy registration.
//   - Adding a new entry here requires a matching delivery policy entry.
var EventCatalog = []EventContract{
	// ── Hot-path aggregates ──────────────────────────────────────────────
	{Type: VolumeProfileSnapshotType, Version: VolumeProfileSnapshotVersion, OwnerBC: "insights", ProducerBC: "aggregation"},
	{Type: VolumeProfileDeltaType, Version: VolumeProfileDeltaVersion, OwnerBC: "insights", ProducerBC: "aggregation"},
	{Type: HeatmapSnapshotType, Version: HeatmapSnapshotVersion, OwnerBC: "insights", ProducerBC: "aggregation"},
	{Type: HeatmapDeltaType, Version: HeatmapDeltaVersion, OwnerBC: "insights", ProducerBC: "aggregation"},

	// ── Cross-venue derived ─────────────────────────────────────────────
	{Type: CrossVenueTradeSnapshotType, Version: CrossVenueTradeSnapshotVersion, OwnerBC: "insights", ProducerBC: "aggregation"},
	{Type: CrossVenueSpreadSignalType, Version: CrossVenueSpreadSignalVersion, OwnerBC: "insights", ProducerBC: "aggregation"},

	// ── Session-scoped profiles ─────────────────────────────────────────
	{Type: SessionVolumeProfileType, Version: SessionVolumeProfileVersion, OwnerBC: "insights", ProducerBC: "aggregation"},
	{Type: TPOProfileType, Version: TPOProfileVersion, OwnerBC: "insights", ProducerBC: "aggregation"},

	// ── Multi-venue fusion ──────────────────────────────────────────────
	{Type: FusedVolumeProfileSnapshotType, Version: FusedVolumeProfileSnapshotVersion, OwnerBC: "insights", ProducerBC: "aggregation"},
	{Type: FusedHeatmapSnapshotType, Version: FusedHeatmapSnapshotVersion, OwnerBC: "insights", ProducerBC: "aggregation"},
}

// EventCatalogByType returns a map keyed by event type for O(1) lookup.
func EventCatalogByType() map[string]EventContract {
	m := make(map[string]EventContract, len(EventCatalog))
	for _, e := range EventCatalog {
		m[e.Type] = e
	}
	return m
}
