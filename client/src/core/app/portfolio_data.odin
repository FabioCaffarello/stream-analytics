package app

// Portfolio data fetch layer — stubbed out.
// The portfolio service no longer exists in stream-analytics; all procs are no-ops.
// The Portfolio_Data_State struct in app.odin is kept to avoid cascade changes.

PORTFOLIO_POLL_INTERVAL  :: u64(600)
PORTFOLIO_RETRY_INTERVAL :: u64(300)
PORTFOLIO_BUF_SIZE       :: 16384

@(private = "package")
fetch_portfolio_state :: proc(state: ^App_State) {
	state.portfolio.state_status = .Error
}

@(private = "package")
fetch_account_snapshot :: proc(state: ^App_State) {
	state.portfolio.snapshot_status = .Error
}

@(private = "package")
fetch_portfolio_summary :: proc(state: ^App_State) {
	state.portfolio.summary_status = .Error
}

@(private = "package")
fetch_trading_readiness :: proc(state: ^App_State) {
	state.portfolio.readiness_status = .Error
}

@(private = "package")
poll_portfolio :: proc(state: ^App_State) {
	// No-op: portfolio service removed.
}

@(private = "package")
portfolio_set_account :: proc(pf: ^Portfolio_Data_State, account_id: string) {}

@(private = "package")
portfolio_set_target :: proc(pf: ^Portfolio_Data_State, account_id: string, venue: string, symbol: string) {}

@(private = "package")
portfolio_clear :: proc(pf: ^Portfolio_Data_State) {
	pf^ = {}
}
