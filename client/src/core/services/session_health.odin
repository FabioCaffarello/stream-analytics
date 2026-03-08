package services

// S59: Session Health parser — JSON deserialization for GET /api/v1/session/dashboard.
// Backend-owned composed read model for session-level health and diagnostics.
// The client augments this with local transport metrics (MD_Runtime_Metrics)
// that only the client can observe (RTT, parse timings, protocol state).

import "core:encoding/json"

HEALTH_ARTIFACT_CAP :: 8
HEALTH_TF_CAP       :: 12

// --- Parsed result from backend JSON ---

Session_Health_Freshness :: struct {
	status:             string,
	active_instruments: int,
	stale_instruments:  int,
	flowing_channels:   int,
	stale_channels:     int,
	checked_at:         i64,
}

Session_Health_Resync :: struct {
	status:             string,
	connections_active: i64,
	streams:            int,
	resync_total:       u64,
	drops_total:        u64,
	max_lag_ms:         i64,
}

Session_Health_Artifact_Coverage :: struct {
	status:                  string,
	total_instruments:       int,
	available_instruments:   int,
	empty_instruments:       int,
	unavailable_instruments: int,
}

Session_Health_Artifact :: struct {
	name:              string,
	endpoint:          string,
	default_timeframe: string,
	timeframes:        [HEALTH_TF_CAP]string,
	tf_count:          int,
	coverage:          Session_Health_Artifact_Coverage,
}

Session_Health_Summary :: struct {
	venues:      int,
	instruments: int,
}

Session_Health_Result :: struct {
	server_time_ms:   i64,
	status:           string,
	// Readiness.
	readiness_status: string,
	// Freshness.
	freshness:        Session_Health_Freshness,
	// Resync / delivery.
	resync:           Session_Health_Resync,
	// Artifacts coverage.
	artifacts:      [HEALTH_ARTIFACT_CAP]Session_Health_Artifact,
	artifact_count: int,
	// Summary.
	summary:        Session_Health_Summary,
}

// --- JSON schema (matches backend SessionDashboardResponse) ---

@(private = "file")
Health_Readiness_JSON :: struct {
	status: string `json:"status"`,
}

@(private = "file")
Health_Freshness_JSON :: struct {
	status:             string `json:"status"`,
	active_instruments: int    `json:"active_instruments"`,
	stale_instruments:  int    `json:"stale_instruments"`,
	flowing_channels:   int    `json:"flowing_channels"`,
	stale_channels:     int    `json:"stale_channels"`,
	checked_at:         i64    `json:"checked_at"`,
}

@(private = "file")
Health_Resync_JSON :: struct {
	status:             string `json:"status"`,
	connections_active: i64    `json:"connections_active"`,
	streams:            int    `json:"streams"`,
	resync_total:       u64    `json:"resync_total"`,
	drops_total:        u64    `json:"drops_total"`,
	max_lag_ms:         i64    `json:"max_lag_ms"`,
}

@(private = "file")
Health_Coverage_JSON :: struct {
	status:                  string `json:"status"`,
	total_instruments:       int    `json:"total_instruments"`,
	available_instruments:   int    `json:"available_instruments"`,
	empty_instruments:       int    `json:"empty_instruments"`,
	unavailable_instruments: int    `json:"unavailable_instruments"`,
}

@(private = "file")
Health_Artifact_JSON :: struct {
	name:              string             `json:"name"`,
	endpoint:          string             `json:"endpoint"`,
	default_timeframe: string             `json:"default_timeframe"`,
	timeframes:        []string           `json:"timeframes"`,
	coverage:          Health_Coverage_JSON `json:"coverage"`,
}

@(private = "file")
Health_Summary_JSON :: struct {
	venues:      int `json:"venues"`,
	instruments: int `json:"instruments"`,
}

@(private = "file")
Health_Dashboard_JSON :: struct {
	server_time_ms: i64                   `json:"server_time_ms"`,
	status:         string                `json:"status"`,
	readiness:      Health_Readiness_JSON  `json:"readiness"`,
	freshness:      Health_Freshness_JSON  `json:"freshness"`,
	resync:         Health_Resync_JSON     `json:"resync"`,
	artifacts:      []Health_Artifact_JSON `json:"artifacts"`,
	summary:        Health_Summary_JSON    `json:"summary"`,
}

// Parse GET /api/v1/session/dashboard response. Returns true on success.
session_health_parse_json :: proc(data: []u8, out: ^Session_Health_Result) -> bool {
	if len(data) == 0 || out == nil do return false

	root: Health_Dashboard_JSON
	if json.unmarshal(data, &root) != nil do return false

	out^ = {}
	out.server_time_ms = root.server_time_ms
	out.status = root.status

	// Readiness.
	out.readiness_status = root.readiness.status

	// Freshness.
	out.freshness = Session_Health_Freshness{
		status             = root.freshness.status,
		active_instruments = root.freshness.active_instruments,
		stale_instruments  = root.freshness.stale_instruments,
		flowing_channels   = root.freshness.flowing_channels,
		stale_channels     = root.freshness.stale_channels,
		checked_at         = root.freshness.checked_at,
	}

	// Resync.
	out.resync = Session_Health_Resync{
		status             = root.resync.status,
		connections_active = root.resync.connections_active,
		streams            = root.resync.streams,
		resync_total       = root.resync.resync_total,
		drops_total        = root.resync.drops_total,
		max_lag_ms         = root.resync.max_lag_ms,
	}

	// Artifacts.
	out.artifact_count = 0
	for art in root.artifacts {
		if out.artifact_count >= HEALTH_ARTIFACT_CAP do break
		a := &out.artifacts[out.artifact_count]
		a.name = art.name
		a.endpoint = art.endpoint
		a.default_timeframe = art.default_timeframe
		a.coverage = Session_Health_Artifact_Coverage{
			status                  = art.coverage.status,
			total_instruments       = art.coverage.total_instruments,
			available_instruments   = art.coverage.available_instruments,
			empty_instruments       = art.coverage.empty_instruments,
			unavailable_instruments = art.coverage.unavailable_instruments,
		}
		a.tf_count = 0
		for tf in art.timeframes {
			if a.tf_count >= HEALTH_TF_CAP do break
			a.timeframes[a.tf_count] = tf
			a.tf_count += 1
		}
		out.artifact_count += 1
	}

	// Summary.
	out.summary = Session_Health_Summary{
		venues      = root.summary.venues,
		instruments = root.summary.instruments,
	}
	return true
}
