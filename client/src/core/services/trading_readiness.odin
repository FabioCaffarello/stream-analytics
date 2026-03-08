package services

// S78: Trading readiness surface — JSON deserialization for GET /api/v1/trading/readiness.
// Composed query-side view: control plane state + portfolio staleness.
// Portfolio does NOT own authorization logic — this is a reflection of execution state.

import "core:encoding/json"

// --- Capacity bounds ---

READINESS_VENUE_CAP     :: 8
READINESS_ACCOUNT_CAP   :: 8
READINESS_STRING_CAP    :: 16
READINESS_FLAG_CAP      :: 8

// --- Result types ---

Trading_Status :: enum u8 {
	Unknown,
	Enabled,
	Degraded,
	Disabled,
	Halted,
}

Venue_Readiness :: struct {
	venue:            string,
	trading_status:   Trading_Status,
	position_count:   i32,
	equity_usd:       f64,
	last_projected_ms: i64,
	stale:            bool,
	restricted:       bool,
}

Account_Readiness :: struct {
	account_id:     string,
	venues:         [READINESS_VENUE_CAP]Venue_Readiness,
	venue_count:    int,
	equity_usd:     f64,
	position_count: i32,
	stale:          bool,
}

Control_Plane_Readiness :: struct {
	state:                string,
	simulation_profile:   string,
	disabled_strategies:  [READINESS_STRING_CAP]string,
	disabled_strategy_count: int,
	disabled_adapters:    [READINESS_STRING_CAP]string,
	disabled_adapter_count: int,
	allowlist_restricted: bool,
	restricted_venues:    [READINESS_STRING_CAP]string,
	restricted_venue_count: int,
	restricted_symbols:   [READINESS_STRING_CAP]string,
	restricted_symbol_count: int,
	updated_at_ms:        i64,
}

Trading_Readiness_Result :: struct {
	control_plane:         Control_Plane_Readiness,
	accounts:              [READINESS_ACCOUNT_CAP]Account_Readiness,
	account_count:         int,
	safety_flags:          [READINESS_FLAG_CAP]string,
	flag_count:            int,
	evaluated_at_ms:       i64,
	staleness_threshold_ms: i64, // server-authoritative staleness threshold (0 = not provided)
}

// --- JSON schema (private) ---

@(private = "file")
Venue_Readiness_JSON :: struct {
	venue:            string `json:"venue"`,
	trading_status:   string `json:"trading_status"`,
	position_count:   i32    `json:"position_count"`,
	equity_usd:       f64    `json:"equity_usd"`,
	last_projected_ms: i64   `json:"last_projected_ms"`,
	stale:            bool   `json:"stale"`,
	restricted:       bool   `json:"restricted"`,
}

@(private = "file")
Account_Readiness_JSON :: struct {
	account_id:     string                 `json:"account_id"`,
	venues:         []Venue_Readiness_JSON `json:"venues"`,
	equity_usd:     f64                    `json:"equity_usd"`,
	position_count: i32                    `json:"position_count"`,
	stale:          bool                   `json:"stale"`,
}

@(private = "file")
Control_Plane_Readiness_JSON :: struct {
	state:                string   `json:"state"`,
	simulation_profile:   string   `json:"simulation_profile"`,
	disabled_strategies:  []string `json:"disabled_strategies"`,
	disabled_adapters:    []string `json:"disabled_adapters"`,
	allowlist_restricted: bool     `json:"allowlist_restricted"`,
	restricted_venues:    []string `json:"restricted_venues"`,
	restricted_symbols:   []string `json:"restricted_symbols"`,
	updated_at_ms:        i64      `json:"updated_at_ms"`,
}

@(private = "file")
Trading_Readiness_JSON :: struct {
	control_plane:         Control_Plane_Readiness_JSON `json:"control_plane"`,
	accounts:              []Account_Readiness_JSON     `json:"accounts"`,
	safety_flags:          []string                     `json:"safety_flags"`,
	evaluated_at_ms:       i64                          `json:"evaluated_at_ms"`,
	staleness_threshold_ms: i64                         `json:"staleness_threshold_ms"`,
}

// --- Parser ---

trading_readiness_parse_json :: proc(data: []u8, out: ^Trading_Readiness_Result) -> bool {
	if len(data) == 0 || out == nil do return false

	root: Trading_Readiness_JSON
	if json.unmarshal(data, &root) != nil do return false

	out^ = {}
	out.evaluated_at_ms = root.evaluated_at_ms
	out.staleness_threshold_ms = root.staleness_threshold_ms

	// Control plane.
	cp := &out.control_plane
	cp.state = root.control_plane.state
	cp.simulation_profile = root.control_plane.simulation_profile
	cp.allowlist_restricted = root.control_plane.allowlist_restricted
	cp.updated_at_ms = root.control_plane.updated_at_ms

	cp.disabled_strategy_count = 0
	for s in root.control_plane.disabled_strategies {
		if cp.disabled_strategy_count >= READINESS_STRING_CAP do break
		cp.disabled_strategies[cp.disabled_strategy_count] = s
		cp.disabled_strategy_count += 1
	}
	cp.disabled_adapter_count = 0
	for s in root.control_plane.disabled_adapters {
		if cp.disabled_adapter_count >= READINESS_STRING_CAP do break
		cp.disabled_adapters[cp.disabled_adapter_count] = s
		cp.disabled_adapter_count += 1
	}
	cp.restricted_venue_count = 0
	for s in root.control_plane.restricted_venues {
		if cp.restricted_venue_count >= READINESS_STRING_CAP do break
		cp.restricted_venues[cp.restricted_venue_count] = s
		cp.restricted_venue_count += 1
	}
	cp.restricted_symbol_count = 0
	for s in root.control_plane.restricted_symbols {
		if cp.restricted_symbol_count >= READINESS_STRING_CAP do break
		cp.restricted_symbols[cp.restricted_symbol_count] = s
		cp.restricted_symbol_count += 1
	}

	// Accounts.
	out.account_count = 0
	for a in root.accounts {
		if out.account_count >= READINESS_ACCOUNT_CAP do break
		ar := &out.accounts[out.account_count]
		ar.account_id = a.account_id
		ar.equity_usd = a.equity_usd
		ar.position_count = a.position_count
		ar.stale = a.stale

		ar.venue_count = 0
		for v in a.venues {
			if ar.venue_count >= READINESS_VENUE_CAP do break
			ar.venues[ar.venue_count] = Venue_Readiness{
				venue            = v.venue,
				trading_status   = parse_trading_status(v.trading_status),
				position_count   = v.position_count,
				equity_usd       = v.equity_usd,
				last_projected_ms = v.last_projected_ms,
				stale            = v.stale,
				restricted       = v.restricted,
			}
			ar.venue_count += 1
		}

		out.account_count += 1
	}

	// Safety flags.
	out.flag_count = 0
	for f in root.safety_flags {
		if out.flag_count >= READINESS_FLAG_CAP do break
		out.safety_flags[out.flag_count] = f
		out.flag_count += 1
	}

	return true
}

// --- Helpers ---

@(private = "file")
parse_trading_status :: proc(s: string) -> Trading_Status {
	if s == "enabled"  do return .Enabled
	if s == "degraded" do return .Degraded
	if s == "disabled" do return .Disabled
	if s == "halted"   do return .Halted
	return .Unknown
}

// Returns a display string for a trading status.
trading_status_label :: proc(status: Trading_Status) -> string {
	switch status {
	case .Enabled:  return "ENABLED"
	case .Degraded: return "DEGRADED"
	case .Disabled: return "DISABLED"
	case .Halted:   return "HALTED"
	case .Unknown:  return "UNKNOWN"
	}
	return "UNKNOWN"
}

// Check if any safety flag matches the given name.
readiness_has_flag :: proc(result: ^Trading_Readiness_Result, flag: string) -> bool {
	if result == nil do return false
	for i in 0 ..< result.flag_count {
		if result.safety_flags[i] == flag do return true
	}
	return false
}
