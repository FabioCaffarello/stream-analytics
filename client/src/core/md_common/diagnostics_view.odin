package md_common

// S51: Diagnostics View — unified read model consolidating per-cell health,
// per-slot store integrity, stream latency, and staleness alerts.
//
// Pure structs + resolve function. No allocations, no mutable state.

Store_Integrity_Flag :: enum u8 {
	Ok,
	Gap,       // detected gaps in candle windows
	Stale,     // newest candle older than threshold
	Empty,     // no candles in store
}

Cell_Diagnostic :: struct {
	widget_kind:     u8,                     // Widget_Kind ordinal
	composition:     Composition_Stage,
	health_level:    System_Health_Level,
	stale_artifacts: int,
	aging_artifacts: int,
	event_count:     u64,
}

Store_Health :: struct {
	candle_count:     int,
	newest_age_ms:    i64,
	integrity:        Store_Integrity_Flag,
}

Stream_Latency :: struct {
	recv_age_ms:    [Artifact_Kind]i64,
	worst_age_ms:   i64,
	worst_artifact: Artifact_Kind,
}

Stale_Alert :: struct {
	slot_idx:      int,
	artifact_kind: Artifact_Kind,
	age_ms:        i64,
	threshold_ms:  i64,
}

DIAGNOSTICS_MAX_ALERTS :: 32

Diagnostics_View :: struct {
	// Per-cell diagnostics.
	cell_health:    [SNAPSHOT_MAX_CELLS]Cell_Diagnostic,
	cell_count:     int,

	// Per-slot store health.
	store_health:   [SNAPSHOT_MAX_SLOTS]Store_Health,

	// Per-slot stream latency.
	stream_latency: [SNAPSHOT_MAX_SLOTS]Stream_Latency,

	// Staleness alerts (sorted by severity, worst first).
	stale_alerts:   [DIAGNOSTICS_MAX_ALERTS]Stale_Alert,
	stale_count:    int,

	// Slot count.
	slot_count:     int,

	// Aggregate summary.
	aggregate:      Aggregate_Health_Summary,
}

// diagnostics_stream_latency computes per-artifact recv age for a slot.
// Pure function — reads from apply state and current time.
diagnostics_stream_latency :: proc(
	s: Stream_Apply_State,
	now_ms: i64,
) -> Stream_Latency {
	lat: Stream_Latency
	lat.worst_age_ms = -1

	for kind in Artifact_Kind {
		recv := s.last_recv_ms[kind]
		if recv <= 0 {
			lat.recv_age_ms[kind] = -1
			continue
		}
		age := now_ms - recv
		if age < 0 do age = 0
		lat.recv_age_ms[kind] = age

		if age > lat.worst_age_ms {
			lat.worst_age_ms = age
			lat.worst_artifact = kind
		}
	}
	return lat
}

// diagnostics_store_health computes store integrity from candle metrics.
diagnostics_store_health :: proc(
	candle_count: int,
	newest_ts: i64,
	now_ms: i64,
	tf_ms: i64,
) -> Store_Health {
	h: Store_Health
	h.candle_count = candle_count

	if candle_count <= 0 {
		h.integrity = .Empty
		return h
	}

	if newest_ts > 0 && now_ms > 0 {
		h.newest_age_ms = now_ms - newest_ts
		if h.newest_age_ms < 0 do h.newest_age_ms = 0

		// Stale if newest candle is older than 3x TF.
		stale_threshold := tf_ms * 3
		if stale_threshold > 0 && h.newest_age_ms > stale_threshold {
			h.integrity = .Stale
			return h
		}
	}

	h.integrity = .Ok
	return h
}

// diagnostics_count_stale_aging counts stale and aging artifacts for a slot.
diagnostics_count_stale_aging :: proc(
	s: Stream_Apply_State,
	now_ms: i64,
	tf_ms: i64,
) -> (stale: int, aging: int) {
	for kind in Artifact_Kind {
		staleness := apply_state_artifact_staleness(s, kind, now_ms, tf_ms)
		switch staleness {
		case .Stale:   stale += 1
		case .Aging:   aging += 1
		case .Fresh, .Unknown:
		}
	}
	return
}

// diagnostics_cell_health computes a Cell_Diagnostic for one cell.
diagnostics_cell_health :: proc(
	widget_kind: u8,
	s: Stream_Apply_State,
	now_ms: i64,
	tf_ms: i64,
) -> Cell_Diagnostic {
	stale, aging := diagnostics_count_stale_aging(s, now_ms, tf_ms)
	comp := apply_state_composition_stage(s)
	health := stream_health_level(s, now_ms, tf_ms)

	return Cell_Diagnostic{
		widget_kind     = widget_kind,
		composition     = comp,
		health_level    = health,
		stale_artifacts = stale,
		aging_artifacts = aging,
		event_count     = s.event_count,
	}
}
