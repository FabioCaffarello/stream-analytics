package services

// S74: Portfolio data layer — JSON deserialization for portfolio read model endpoints.
// Three stores matching the backend S73 query APIs:
//   1. PortfolioStateV1    → GET /api/v1/portfolio/state/latest
//   2. AccountSnapshotV1   → GET /api/v1/portfolio/account-snapshot/latest
//   3. PortfolioSummaryV1  → GET /api/v1/portfolio/summary/latest
//
// All stores are value structs with bounded fixed-capacity arrays.
// No business logic — pure parse + store.

import "core:encoding/json"

// --- Capacity bounds ---

PORTFOLIO_BALANCE_CAP  :: 16
PORTFOLIO_POSITION_CAP :: 32
PORTFOLIO_EXPOSURE_CAP :: 16
PORTFOLIO_VENUE_CAP    :: 8
PORTFOLIO_ACCOUNT_CAP  :: 8

// Truncation flags — bitmask indicating which arrays were truncated during parse.
Truncation_Flags :: distinct bit_set[Truncation_Flag; u8]
Truncation_Flag :: enum u8 {
	Positions,
	Balances,
	Exposures,
	Venues,
	Accounts,
}

// --- Shared sub-types (parsed result) ---

Portfolio_Balance :: struct {
	asset:     string,
	total:     f64,
	available: f64,
	locked:    f64,
}

Portfolio_Position :: struct {
	venue:            string,
	symbol:           string,
	side:             string,
	quantity:         f64,
	avg_entry_price:  f64,
	notional_usd:     f64,
	realized_pnl:     f64,
	unrealized_pnl:   f64,
	trade_count:      i32,
	volume_traded_usd: f64,
	last_fill_ms:     i64,
}

Portfolio_Exposure :: struct {
	symbol:             string,
	net_qty:            f64,
	gross_notional_usd: f64,
	leverage:           f64,
}

Portfolio_Risk :: struct {
	margin_used_usd:        f64,
	margin_available_usd:   f64,
	maintenance_margin_usd: f64,
	var_95_usd:             f64,
}

Portfolio_Fill_Summary :: struct {
	total_trade_count:       i32,
	total_volume_traded_usd: f64,
	win_count:               i32,
	loss_count:              i32,
	largest_win_usd:         f64,
	largest_loss_usd:        f64,
	turnover_usd:            f64,
}

Portfolio_Provenance :: struct {
	source_execution_event_id: string,
	source_execution_seq:      i64,
	correlation_id:            string,
	trace_id:                  string,
	projector_version:         string,
}

// ---------------------------------------------------------------------------
// Store 1: Portfolio State (venue-scoped position & balance snapshot)
// ---------------------------------------------------------------------------

Portfolio_State_Result :: struct {
	state_id:          string,
	scope:             string,
	account_id:        string,
	venue:             string,
	projected_at_ms:   i64,
	balances:          [PORTFOLIO_BALANCE_CAP]Portfolio_Balance,
	balance_count:     int,
	positions:         [PORTFOLIO_POSITION_CAP]Portfolio_Position,
	position_count:    int,
	exposures:         [PORTFOLIO_EXPOSURE_CAP]Portfolio_Exposure,
	exposure_count:    int,
	equity_usd:        f64,
	realized_pnl_usd: f64,
	unrealized_pnl_usd: f64,
	risk:              Portfolio_Risk,
	fill_summary:      Portfolio_Fill_Summary,
	provenance:        Portfolio_Provenance,
}

// ---------------------------------------------------------------------------
// Store 2: Account Snapshot (account-level aggregation)
// ---------------------------------------------------------------------------

Portfolio_Venue_Position :: struct {
	venue:             string,
	positions:         [PORTFOLIO_POSITION_CAP]Portfolio_Position,
	position_count:    int,
	balances:          [PORTFOLIO_BALANCE_CAP]Portfolio_Balance,
	balance_count:     int,
	equity_usd:        f64,
	realized_pnl_usd: f64,
	unrealized_pnl_usd: f64,
	margin_used_usd:   f64,
}

Portfolio_Account_Snapshot_Result :: struct {
	snapshot_id:        string,
	account_id:         string,
	projected_at_ms:    i64,
	venues:             [PORTFOLIO_VENUE_CAP]Portfolio_Venue_Position,
	venue_count:        int,
	total_equity_usd:   f64,
	total_realized_usd: f64,
	total_unrealized_usd: f64,
	total_margin_used_usd: f64,
	total_leverage:     f64,
	fill_summary:       Portfolio_Fill_Summary,
}

// ---------------------------------------------------------------------------
// Store 3: Portfolio Summary (global aggregation)
// ---------------------------------------------------------------------------

Portfolio_Account_Summary :: struct {
	account_id:         string,
	venue_count:        i32,
	position_count:     i32,
	equity_usd:         f64,
	realized_pnl_usd:  f64,
	unrealized_pnl_usd: f64,
}

Portfolio_Summary_Result :: struct {
	summary_id:           string,
	projected_at_ms:      i64,
	accounts:             [PORTFOLIO_ACCOUNT_CAP]Portfolio_Account_Summary,
	account_count:        int,
	global_equity_usd:    f64,
	global_realized_usd:  f64,
	global_unrealized_usd: f64,
	global_margin_used_usd: f64,
	global_leverage:      f64,
	total_position_count: i32,
	total_open_orders:    i32,
	fill_summary:         Portfolio_Fill_Summary,
}

// ===========================================================================
// JSON schemas (private, match backend Go JSON tags)
// ===========================================================================

@(private = "file")
Balance_JSON :: struct {
	asset:     string `json:"asset"`,
	total:     f64    `json:"total"`,
	available: f64    `json:"available"`,
	locked:    f64    `json:"locked"`,
}

@(private = "file")
Position_JSON :: struct {
	venue:            string `json:"venue"`,
	symbol:           string `json:"symbol"`,
	side:             string `json:"side"`,
	quantity:         f64    `json:"quantity"`,
	avg_entry_price:  f64    `json:"avg_entry_price"`,
	notional_usd:     f64    `json:"notional_usd"`,
	realized_pnl:     f64    `json:"realized_pnl"`,
	unrealized_pnl:   f64    `json:"unrealized_pnl"`,
	trade_count:      i32    `json:"trade_count"`,
	volume_traded_usd: f64   `json:"volume_traded_usd"`,
	last_fill_ms:     i64    `json:"last_fill_ms"`,
}

@(private = "file")
Exposure_JSON :: struct {
	symbol:             string `json:"symbol"`,
	net_qty:            f64    `json:"net_qty"`,
	gross_notional_usd: f64    `json:"gross_notional_usd"`,
	leverage:           f64    `json:"leverage"`,
}

@(private = "file")
Risk_JSON :: struct {
	margin_used_usd:        f64 `json:"margin_used_usd"`,
	margin_available_usd:   f64 `json:"margin_available_usd"`,
	maintenance_margin_usd: f64 `json:"maintenance_margin_usd"`,
	var_95_usd:             f64 `json:"var_95_usd"`,
}

@(private = "file")
Fill_Summary_JSON :: struct {
	total_trade_count:       i32 `json:"total_trade_count"`,
	total_volume_traded_usd: f64 `json:"total_volume_traded_usd"`,
	win_count:               i32 `json:"win_count"`,
	loss_count:              i32 `json:"loss_count"`,
	largest_win_usd:         f64 `json:"largest_win_usd"`,
	largest_loss_usd:        f64 `json:"largest_loss_usd"`,
	turnover_usd:            f64 `json:"turnover_usd"`,
}

@(private = "file")
Provenance_JSON :: struct {
	source_execution_event_id: string `json:"source_execution_event_id"`,
	source_execution_seq:      i64    `json:"source_execution_seq"`,
	correlation_id:            string `json:"correlation_id"`,
	trace_id:                  string `json:"trace_id"`,
	projector_version:         string `json:"projector_version"`,
}

@(private = "file")
Portfolio_State_JSON :: struct {
	state_id:          string          `json:"state_id"`,
	scope:             string          `json:"scope"`,
	account_id:        string          `json:"account_id"`,
	venue:             string          `json:"venue"`,
	projected_at_ms:   i64             `json:"projected_at_ms"`,
	balances:          []Balance_JSON  `json:"balances"`,
	positions:         []Position_JSON `json:"positions"`,
	exposures:         []Exposure_JSON `json:"exposures"`,
	equity_usd:        f64             `json:"equity_usd"`,
	realized_pnl_usd: f64             `json:"realized_pnl_usd"`,
	unrealized_pnl_usd: f64           `json:"unrealized_pnl_usd"`,
	risk:              Risk_JSON       `json:"risk"`,
	fill_summary:      Fill_Summary_JSON `json:"fill_summary"`,
	provenance:        Provenance_JSON `json:"provenance"`,
}

@(private = "file")
Venue_Position_JSON :: struct {
	venue:             string          `json:"venue"`,
	positions:         []Position_JSON `json:"positions"`,
	balances:          []Balance_JSON  `json:"balances"`,
	equity_usd:        f64             `json:"equity_usd"`,
	realized_pnl_usd: f64             `json:"realized_pnl_usd"`,
	unrealized_pnl_usd: f64           `json:"unrealized_pnl_usd"`,
	margin_used_usd:   f64             `json:"margin_used_usd"`,
}

@(private = "file")
Account_Snapshot_JSON :: struct {
	snapshot_id:        string                `json:"snapshot_id"`,
	account_id:         string                `json:"account_id"`,
	projected_at_ms:    i64                   `json:"projected_at_ms"`,
	venues:             []Venue_Position_JSON `json:"venues"`,
	total_equity_usd:   f64                   `json:"total_equity_usd"`,
	total_realized_usd: f64                   `json:"total_realized_usd"`,
	total_unrealized_usd: f64                 `json:"total_unrealized_usd"`,
	total_margin_used_usd: f64                `json:"total_margin_used_usd"`,
	total_leverage:     f64                   `json:"total_leverage"`,
	fill_summary:       Fill_Summary_JSON     `json:"fill_summary"`,
}

@(private = "file")
Account_Summary_JSON :: struct {
	account_id:         string `json:"account_id"`,
	venue_count:        i32    `json:"venue_count"`,
	position_count:     i32    `json:"position_count"`,
	equity_usd:         f64    `json:"equity_usd"`,
	realized_pnl_usd:  f64    `json:"realized_pnl_usd"`,
	unrealized_pnl_usd: f64   `json:"unrealized_pnl_usd"`,
}

@(private = "file")
Portfolio_Summary_JSON :: struct {
	summary_id:           string                `json:"summary_id"`,
	projected_at_ms:      i64                   `json:"projected_at_ms"`,
	accounts:             []Account_Summary_JSON `json:"accounts"`,
	global_equity_usd:    f64                   `json:"global_equity_usd"`,
	global_realized_usd:  f64                   `json:"global_realized_usd"`,
	global_unrealized_usd: f64                  `json:"global_unrealized_usd"`,
	global_margin_used_usd: f64                 `json:"global_margin_used_usd"`,
	global_leverage:      f64                   `json:"global_leverage"`,
	total_position_count: i32                   `json:"total_position_count"`,
	total_open_orders:    i32                   `json:"total_open_orders"`,
	fill_summary:         Fill_Summary_JSON     `json:"fill_summary"`,
}

// ===========================================================================
// Parsers
// ===========================================================================

// Parse GET /api/v1/portfolio/state/latest response.
// Returns (success, truncation_flags). Truncation flags indicate which arrays exceeded capacity.
portfolio_state_parse_json :: proc(data: []u8, out: ^Portfolio_State_Result) -> (ok: bool, truncated: Truncation_Flags) {
	if len(data) == 0 || out == nil do return false, {}

	root: Portfolio_State_JSON
	if json.unmarshal(data, &root) != nil do return false, {}

	out^ = {}
	out.state_id = root.state_id
	out.scope = root.scope
	out.account_id = root.account_id
	out.venue = root.venue
	out.projected_at_ms = root.projected_at_ms
	out.equity_usd = root.equity_usd
	out.realized_pnl_usd = root.realized_pnl_usd
	out.unrealized_pnl_usd = root.unrealized_pnl_usd

	flags: Truncation_Flags

	// Balances.
	out.balance_count = 0
	for b in root.balances {
		if out.balance_count >= PORTFOLIO_BALANCE_CAP {
			flags += {.Balances}
			break
		}
		out.balances[out.balance_count] = Portfolio_Balance{
			asset     = b.asset,
			total     = b.total,
			available = b.available,
			locked    = b.locked,
		}
		out.balance_count += 1
	}

	// Positions.
	out.position_count = 0
	for p in root.positions {
		if out.position_count >= PORTFOLIO_POSITION_CAP {
			flags += {.Positions}
			break
		}
		out.positions[out.position_count] = Portfolio_Position{
			venue            = p.venue,
			symbol           = p.symbol,
			side             = p.side,
			quantity          = p.quantity,
			avg_entry_price  = p.avg_entry_price,
			notional_usd     = p.notional_usd,
			realized_pnl     = p.realized_pnl,
			unrealized_pnl   = p.unrealized_pnl,
			trade_count      = p.trade_count,
			volume_traded_usd = p.volume_traded_usd,
			last_fill_ms     = p.last_fill_ms,
		}
		out.position_count += 1
	}

	// Exposures.
	out.exposure_count = 0
	for e in root.exposures {
		if out.exposure_count >= PORTFOLIO_EXPOSURE_CAP {
			flags += {.Exposures}
			break
		}
		out.exposures[out.exposure_count] = Portfolio_Exposure{
			symbol             = e.symbol,
			net_qty            = e.net_qty,
			gross_notional_usd = e.gross_notional_usd,
			leverage           = e.leverage,
		}
		out.exposure_count += 1
	}

	// Risk.
	out.risk = Portfolio_Risk{
		margin_used_usd        = root.risk.margin_used_usd,
		margin_available_usd   = root.risk.margin_available_usd,
		maintenance_margin_usd = root.risk.maintenance_margin_usd,
		var_95_usd             = root.risk.var_95_usd,
	}

	// Fill summary.
	out.fill_summary = convert_fill_summary(root.fill_summary)

	// Provenance.
	out.provenance = Portfolio_Provenance{
		source_execution_event_id = root.provenance.source_execution_event_id,
		source_execution_seq      = root.provenance.source_execution_seq,
		correlation_id            = root.provenance.correlation_id,
		trace_id                  = root.provenance.trace_id,
		projector_version         = root.provenance.projector_version,
	}

	return true, flags
}

// Parse GET /api/v1/portfolio/account-snapshot/latest response.
// Returns (success, truncation_flags).
portfolio_account_snapshot_parse_json :: proc(data: []u8, out: ^Portfolio_Account_Snapshot_Result) -> (ok: bool, truncated: Truncation_Flags) {
	if len(data) == 0 || out == nil do return false, {}

	root: Account_Snapshot_JSON
	if json.unmarshal(data, &root) != nil do return false, {}

	out^ = {}
	out.snapshot_id = root.snapshot_id
	out.account_id = root.account_id
	out.projected_at_ms = root.projected_at_ms
	out.total_equity_usd = root.total_equity_usd
	out.total_realized_usd = root.total_realized_usd
	out.total_unrealized_usd = root.total_unrealized_usd
	out.total_margin_used_usd = root.total_margin_used_usd
	out.total_leverage = root.total_leverage
	out.fill_summary = convert_fill_summary(root.fill_summary)

	flags: Truncation_Flags

	// Venues.
	out.venue_count = 0
	for v in root.venues {
		if out.venue_count >= PORTFOLIO_VENUE_CAP {
			flags += {.Venues}
			break
		}
		vp := &out.venues[out.venue_count]
		vp.venue = v.venue
		vp.equity_usd = v.equity_usd
		vp.realized_pnl_usd = v.realized_pnl_usd
		vp.unrealized_pnl_usd = v.unrealized_pnl_usd
		vp.margin_used_usd = v.margin_used_usd

		// Positions within venue.
		vp.position_count = 0
		for p in v.positions {
			if vp.position_count >= PORTFOLIO_POSITION_CAP {
				flags += {.Positions}
				break
			}
			vp.positions[vp.position_count] = Portfolio_Position{
				venue            = p.venue,
				symbol           = p.symbol,
				side             = p.side,
				quantity          = p.quantity,
				avg_entry_price  = p.avg_entry_price,
				notional_usd     = p.notional_usd,
				realized_pnl     = p.realized_pnl,
				unrealized_pnl   = p.unrealized_pnl,
				trade_count      = p.trade_count,
				volume_traded_usd = p.volume_traded_usd,
				last_fill_ms     = p.last_fill_ms,
			}
			vp.position_count += 1
		}

		// Balances within venue.
		vp.balance_count = 0
		for b in v.balances {
			if vp.balance_count >= PORTFOLIO_BALANCE_CAP {
				flags += {.Balances}
				break
			}
			vp.balances[vp.balance_count] = Portfolio_Balance{
				asset     = b.asset,
				total     = b.total,
				available = b.available,
				locked    = b.locked,
			}
			vp.balance_count += 1
		}

		out.venue_count += 1
	}

	// Sort venues deterministically by name.
	sort_venues(out.venues[:out.venue_count])

	return true, flags
}

// Parse GET /api/v1/portfolio/summary/latest response.
// Returns (success, truncation_flags).
portfolio_summary_parse_json :: proc(data: []u8, out: ^Portfolio_Summary_Result) -> (ok: bool, truncated: Truncation_Flags) {
	if len(data) == 0 || out == nil do return false, {}

	root: Portfolio_Summary_JSON
	if json.unmarshal(data, &root) != nil do return false, {}

	out^ = {}
	out.summary_id = root.summary_id
	out.projected_at_ms = root.projected_at_ms
	out.global_equity_usd = root.global_equity_usd
	out.global_realized_usd = root.global_realized_usd
	out.global_unrealized_usd = root.global_unrealized_usd
	out.global_margin_used_usd = root.global_margin_used_usd
	out.global_leverage = root.global_leverage
	out.total_position_count = root.total_position_count
	out.total_open_orders = root.total_open_orders
	out.fill_summary = convert_fill_summary(root.fill_summary)

	flags: Truncation_Flags

	// Accounts.
	out.account_count = 0
	for a in root.accounts {
		if out.account_count >= PORTFOLIO_ACCOUNT_CAP {
			flags += {.Accounts}
			break
		}
		out.accounts[out.account_count] = Portfolio_Account_Summary{
			account_id         = a.account_id,
			venue_count        = a.venue_count,
			position_count     = a.position_count,
			equity_usd         = a.equity_usd,
			realized_pnl_usd  = a.realized_pnl_usd,
			unrealized_pnl_usd = a.unrealized_pnl_usd,
		}
		out.account_count += 1
	}

	return true, flags
}

// ===========================================================================
// S76: Computed views for exposure drilldown and stale detection
// ===========================================================================

PORTFOLIO_SYMBOL_EXPOSURE_CAP :: 32

// Aggregated exposure per symbol across all venues.
Portfolio_Symbol_Exposure :: struct {
	symbol:             string,
	net_qty:            f64,
	gross_notional_usd: f64,
	venue_count:        i32,
}

// Compute per-symbol exposure from account snapshot (aggregates across venues).
portfolio_compute_symbol_exposures :: proc(
	snap: ^Portfolio_Account_Snapshot_Result,
	out: ^[PORTFOLIO_SYMBOL_EXPOSURE_CAP]Portfolio_Symbol_Exposure,
) -> int {
	if snap == nil || out == nil do return 0
	count := 0

	for vi in 0 ..< snap.venue_count {
		venue := &snap.venues[vi]
		for pi in 0 ..< venue.position_count {
			pos := &venue.positions[pi]
			if len(pos.symbol) == 0 do continue

			// Find existing entry.
			found := false
			for ei in 0 ..< count {
				if out[ei].symbol == pos.symbol {
					out[ei].net_qty += pos.side == "short" ? -pos.quantity : pos.quantity
					out[ei].gross_notional_usd += pos.notional_usd
					out[ei].venue_count += 1
					found = true
					break
				}
			}
			if !found && count < PORTFOLIO_SYMBOL_EXPOSURE_CAP {
				out[count] = Portfolio_Symbol_Exposure{
					symbol             = pos.symbol,
					net_qty            = pos.side == "short" ? -pos.quantity : pos.quantity,
					gross_notional_usd = pos.notional_usd,
					venue_count        = 1,
				}
				count += 1
			}
		}
	}
	return count
}

// Returns true if a position's last fill is older than threshold_ms.
portfolio_position_is_stale :: proc(pos: ^Portfolio_Position, now_ms: i64, threshold_ms: i64) -> bool {
	if pos == nil || pos.last_fill_ms <= 0 do return false
	return (now_ms - pos.last_fill_ms) > threshold_ms
}

// Check for exposure divergence: same symbol traded on multiple venues with opposing sides.
portfolio_has_exposure_divergence :: proc(snap: ^Portfolio_Account_Snapshot_Result) -> bool {
	if snap == nil do return false

	// Track symbols seen with their side across venues.
	MAX_TRACK :: 32
	Symbol_Track :: struct { symbol: string, has_long: bool, has_short: bool }
	tracks: [MAX_TRACK]Symbol_Track
	track_count := 0

	for vi in 0 ..< snap.venue_count {
		venue := &snap.venues[vi]
		for pi in 0 ..< venue.position_count {
			pos := &venue.positions[pi]
			if len(pos.symbol) == 0 do continue

			found := false
			for ti in 0 ..< track_count {
				if tracks[ti].symbol == pos.symbol {
					if pos.side == "long" do tracks[ti].has_long = true
					if pos.side == "short" do tracks[ti].has_short = true
					if tracks[ti].has_long && tracks[ti].has_short do return true
					found = true
					break
				}
			}
			if !found && track_count < MAX_TRACK {
				tracks[track_count] = Symbol_Track{
					symbol    = pos.symbol,
					has_long  = pos.side == "long",
					has_short = pos.side == "short",
				}
				track_count += 1
			}
		}
	}
	return false
}

// --- Deterministic sorting ---

// Sort venues by name (insertion sort — bounded N≤8).
@(private = "file")
sort_venues :: proc(venues: []Portfolio_Venue_Position) {
	n := len(venues)
	for i in 1 ..< n {
		key := venues[i]
		j := i - 1
		for j >= 0 && str_less(key.venue, venues[j].venue) {
			venues[j + 1] = venues[j]
			j -= 1
		}
		venues[j + 1] = key
	}
}

// Lexicographic string comparison.
@(private = "file")
str_less :: proc(a, b: string) -> bool {
	la := len(a)
	lb := len(b)
	n := la if la < lb else lb
	for i in 0 ..< n {
		if a[i] < b[i] do return true
		if a[i] > b[i] do return false
	}
	return la < lb
}

// --- Helpers ---

@(private = "file")
convert_fill_summary :: proc(fs: Fill_Summary_JSON) -> Portfolio_Fill_Summary {
	return Portfolio_Fill_Summary{
		total_trade_count       = fs.total_trade_count,
		total_volume_traded_usd = fs.total_volume_traded_usd,
		win_count               = fs.win_count,
		loss_count              = fs.loss_count,
		largest_win_usd         = fs.largest_win_usd,
		largest_loss_usd        = fs.largest_loss_usd,
		turnover_usd            = fs.turnover_usd,
	}
}
