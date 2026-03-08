package app

import "mr:services"

// S74: Portfolio data layer — fetch and poll logic for three portfolio read model stores.
// Pure data layer — no UI rendering. Fetch procs are called by the poll loop and can be
// triggered from any page that needs portfolio data.

PORTFOLIO_POLL_INTERVAL :: u64(600) // ~10s at 60fps
PORTFOLIO_BUF_SIZE      :: 16384   // 16KB — portfolio payloads can be large

// --- Fetch: Portfolio State (venue-scoped) ---

// Fetch latest portfolio state for the configured account+venue+symbol.
@(private = "package")
fetch_portfolio_state :: proc(state: ^App_State) {
	if state.marketdata.fetch_portfolio_state == nil {
		state.portfolio.state_status = .Error
		return
	}
	pf := &state.portfolio
	account_id := string(pf.account_id[:int(pf.account_id_len)])
	venue := string(pf.venue[:int(pf.venue_len)])
	symbol := string(pf.symbol[:int(pf.symbol_len)])
	if len(account_id) == 0 || len(venue) == 0 || len(symbol) == 0 {
		pf.state_status = .Error
		return
	}

	buf: [PORTFOLIO_BUF_SIZE]u8
	n := state.marketdata.fetch_portfolio_state(raw_data(buf[:]), i32(len(buf)), account_id, venue, symbol)
	if n <= 0 {
		pf.state_status = .Error
		return
	}

	result: services.Portfolio_State_Result
	ok, truncated := services.portfolio_state_parse_json(buf[:int(n)], &result)
	if !ok {
		pf.state_status = .Error
		return
	}
	pf.state = result
	pf.state_status = .Success
	pf.state_frame = state.frame
	pf.truncation_flags += truncated
}

// --- Fetch: Account Snapshot ---

// Fetch latest account snapshot for the configured account.
@(private = "package")
fetch_account_snapshot :: proc(state: ^App_State) {
	if state.marketdata.fetch_account_snapshot == nil {
		state.portfolio.snapshot_status = .Error
		return
	}
	pf := &state.portfolio
	account_id := string(pf.account_id[:int(pf.account_id_len)])
	if len(account_id) == 0 {
		pf.snapshot_status = .Error
		return
	}

	buf: [PORTFOLIO_BUF_SIZE]u8
	n := state.marketdata.fetch_account_snapshot(raw_data(buf[:]), i32(len(buf)), account_id)
	if n <= 0 {
		pf.snapshot_status = .Error
		return
	}

	result: services.Portfolio_Account_Snapshot_Result
	ok, truncated := services.portfolio_account_snapshot_parse_json(buf[:int(n)], &result)
	if !ok {
		pf.snapshot_status = .Error
		return
	}
	pf.snapshot = result
	pf.snapshot_status = .Success
	pf.snapshot_frame = state.frame
	pf.truncation_flags += truncated
}

// --- Fetch: Portfolio Summary (global) ---

// Fetch latest global portfolio summary.
@(private = "package")
fetch_portfolio_summary :: proc(state: ^App_State) {
	if state.marketdata.fetch_portfolio_summary == nil {
		state.portfolio.summary_status = .Error
		return
	}

	buf: [PORTFOLIO_BUF_SIZE]u8
	n := state.marketdata.fetch_portfolio_summary(raw_data(buf[:]), i32(len(buf)))
	if n <= 0 {
		state.portfolio.summary_status = .Error
		return
	}

	result: services.Portfolio_Summary_Result
	ok, truncated := services.portfolio_summary_parse_json(buf[:int(n)], &result)
	if !ok {
		state.portfolio.summary_status = .Error
		return
	}
	state.portfolio.summary = result
	state.portfolio.summary_status = .Success
	state.portfolio.summary_frame = state.frame
	state.portfolio.truncation_flags += truncated
}

// --- Fetch: Trading Readiness (composed: control plane + portfolio) ---

// Fetch trading readiness surface from backend.
@(private = "package")
fetch_trading_readiness :: proc(state: ^App_State) {
	if state.marketdata.fetch_trading_readiness == nil {
		state.portfolio.readiness_status = .Error
		return
	}

	buf: [PORTFOLIO_BUF_SIZE]u8
	n := state.marketdata.fetch_trading_readiness(raw_data(buf[:]), i32(len(buf)))
	if n <= 0 {
		state.portfolio.readiness_status = .Error
		return
	}

	result: services.Trading_Readiness_Result
	if !services.trading_readiness_parse_json(buf[:int(n)], &result) {
		state.portfolio.readiness_status = .Error
		return
	}
	state.portfolio.readiness = result
	state.portfolio.readiness_status = .Success
	state.portfolio.readiness_frame = state.frame
}

// --- Poll: periodic refresh ---

// Poll all portfolio stores that have targets configured. Safe to call every frame.
@(private = "package")
poll_portfolio :: proc(state: ^App_State) {
	if current_conn_status(state) != .Connected do return

	pf := &state.portfolio

	// Portfolio state — requires account_id + venue + symbol.
	if pf.account_id_len > 0 && pf.venue_len > 0 && pf.symbol_len > 0 {
		if state.frame % PORTFOLIO_POLL_INTERVAL == 0 || pf.state_status == .Idle {
			fetch_portfolio_state(state)
		}
	}

	// Account snapshot — requires account_id.
	if pf.account_id_len > 0 {
		if state.frame % PORTFOLIO_POLL_INTERVAL == 0 || pf.snapshot_status == .Idle {
			fetch_account_snapshot(state)
		}
	}

	// Portfolio summary — no target required (global).
	if state.frame % PORTFOLIO_POLL_INTERVAL == 0 || pf.summary_status == .Idle {
		fetch_portfolio_summary(state)
	}

	// S78: Trading readiness — no target required (composed query).
	if state.frame % PORTFOLIO_POLL_INTERVAL == 0 || pf.readiness_status == .Idle {
		fetch_trading_readiness(state)
	}
}

// --- Target setters ---

// Set the target account for portfolio queries. Resets fetch status to trigger immediate fetch.
@(private = "package")
portfolio_set_account :: proc(pf: ^Portfolio_Data_State, account_id: string) {
	n := min(len(account_id), len(pf.account_id))
	for i in 0 ..< n {
		pf.account_id[i] = account_id[i]
	}
	pf.account_id_len = u8(n)
	// Reset fetch status so next poll triggers immediately.
	pf.snapshot_status = .Idle
	pf.state_status = .Idle
}

// Set the target venue+symbol for portfolio state queries. Resets state fetch status.
@(private = "package")
portfolio_set_target :: proc(pf: ^Portfolio_Data_State, account_id: string, venue: string, symbol: string) {
	portfolio_set_account(pf, account_id)
	vn := min(len(venue), len(pf.venue))
	for i in 0 ..< vn {
		pf.venue[i] = venue[i]
	}
	pf.venue_len = u8(vn)
	sn := min(len(symbol), len(pf.symbol))
	for i in 0 ..< sn {
		pf.symbol[i] = symbol[i]
	}
	pf.symbol_len = u8(sn)
	pf.state_status = .Idle
}

// Clear all portfolio state (e.g., on page leave or disconnect).
@(private = "package")
portfolio_clear :: proc(pf: ^Portfolio_Data_State) {
	pf^ = {}
}
