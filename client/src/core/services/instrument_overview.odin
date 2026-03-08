package services

// S58: Instrument Overview parser — JSON deserialization for GET /api/v1/instrument/overview.
// Backend-owned composed read model. Client consumes this as a canonical view-model
// without reconstructing instrument state from scattered sources.

import "core:encoding/json"

OVERVIEW_CHANNEL_CAP  :: 12
OVERVIEW_ARTIFACT_CAP :: 8
OVERVIEW_TF_CAP       :: 12

// --- Parsed result from backend JSON ---

Overview_Channel :: struct {
	name:     string,
	flowing:  bool,
	lag_ms:   i64,
}

Overview_Artifact_Timeline :: struct {
	timeframe: string,
	first_ts:  i64,
	last_ts:   i64,
	status:    string,
}

Overview_Artifact :: struct {
	name:       string,
	endpoint:   string,
	timeframes: [OVERVIEW_TF_CAP]string,
	tf_count:   int,
	timeline:   Overview_Artifact_Timeline,
}

Instrument_Overview_Result :: struct {
	venue:      string,
	instrument: string,
	status:     string,
	checked_at: i64,
	// Readiness.
	readiness_status: string,
	// Freshness.
	freshness_status: string,
	freshness_active: bool,
	channels:         [OVERVIEW_CHANNEL_CAP]Overview_Channel,
	channel_count:    int,
	// Resync diagnostics.
	resync_status: string,
	resync_total:  u64,
	drops_total:   u64,
	streams:       int,
	max_lag_ms:    i64,
	// Artifacts.
	artifacts:      [OVERVIEW_ARTIFACT_CAP]Overview_Artifact,
	artifact_count: int,
}

// --- JSON schema (matches backend InstrumentOverviewResponse) ---

@(private = "file")
Overview_Channel_JSON :: struct {
	last_event_ts: i64  `json:"last_event_ts"`,
	lag_ms:        i64  `json:"lag_ms"`,
	flowing:       bool `json:"flowing"`,
}

@(private = "file")
Overview_Readiness_JSON :: struct {
	status: string `json:"status"`,
}

@(private = "file")
Overview_Freshness_JSON :: struct {
	status:   string                             `json:"status"`,
	active:   bool                               `json:"active"`,
	channels: map[string]Overview_Channel_JSON   `json:"channels"`,
}

@(private = "file")
Overview_Resync_JSON :: struct {
	status:       string `json:"status"`,
	resync_total: u64    `json:"resync_total"`,
	drops_total:  u64    `json:"drops_total"`,
	streams:      int    `json:"streams"`,
	max_lag_ms:   i64    `json:"max_lag_ms"`,
}

@(private = "file")
Overview_Artifact_Timeline_JSON :: struct {
	timeframe: string `json:"timeframe"`,
	first_ts:  i64    `json:"first_ts"`,
	last_ts:   i64    `json:"last_ts"`,
	status:    string `json:"status"`,
}

@(private = "file")
Overview_Artifact_JSON :: struct {
	name:       string                          `json:"name"`,
	endpoint:   string                          `json:"endpoint"`,
	timeframes: []string                        `json:"timeframes"`,
	timeline:   Overview_Artifact_Timeline_JSON `json:"timeline"`,
}

@(private = "file")
Overview_JSON :: struct {
	venue:      string                   `json:"venue"`,
	instrument: string                   `json:"instrument"`,
	status:     string                   `json:"status"`,
	checked_at: i64                      `json:"checked_at"`,
	readiness:  Overview_Readiness_JSON  `json:"readiness"`,
	freshness:  Overview_Freshness_JSON  `json:"freshness"`,
	resync:     Overview_Resync_JSON     `json:"resync"`,
	artifacts:  []Overview_Artifact_JSON `json:"artifacts"`,
}

// Parse GET /api/v1/instrument/overview response. Returns true on success.
instrument_overview_parse_json :: proc(data: []u8, out: ^Instrument_Overview_Result) -> bool {
	if len(data) == 0 || out == nil do return false

	root: Overview_JSON
	if json.unmarshal(data, &root) != nil do return false

	out^ = {}
	out.venue = root.venue
	out.instrument = root.instrument
	out.status = root.status
	out.checked_at = root.checked_at

	// Readiness.
	out.readiness_status = root.readiness.status

	// Freshness.
	out.freshness_status = root.freshness.status
	out.freshness_active = root.freshness.active
	out.channel_count = 0
	for name, ch in root.freshness.channels {
		if out.channel_count >= OVERVIEW_CHANNEL_CAP do break
		out.channels[out.channel_count] = Overview_Channel{
			name    = name,
			flowing = ch.flowing,
			lag_ms  = ch.lag_ms,
		}
		out.channel_count += 1
	}

	// Resync.
	out.resync_status = root.resync.status
	out.resync_total = root.resync.resync_total
	out.drops_total = root.resync.drops_total
	out.streams = root.resync.streams
	out.max_lag_ms = root.resync.max_lag_ms

	// Artifacts.
	out.artifact_count = 0
	for art in root.artifacts {
		if out.artifact_count >= OVERVIEW_ARTIFACT_CAP do break
		a := &out.artifacts[out.artifact_count]
		a.name = art.name
		a.endpoint = art.endpoint
		a.timeline = Overview_Artifact_Timeline{
			timeframe = art.timeline.timeframe,
			first_ts  = art.timeline.first_ts,
			last_ts   = art.timeline.last_ts,
			status    = art.timeline.status,
		}
		a.tf_count = 0
		for tf in art.timeframes {
			if a.tf_count >= OVERVIEW_TF_CAP do break
			a.timeframes[a.tf_count] = tf
			a.tf_count += 1
		}
		out.artifact_count += 1
	}
	return true
}
