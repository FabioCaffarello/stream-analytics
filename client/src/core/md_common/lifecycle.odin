package md_common

// Bootstrap lifecycle state enum for unified status tracking.
// Derived from existing app booleans — adds no new behavior, just names the implicit states.

Bootstrap_Lifecycle :: enum u8 {
	Init,              // App starting, no HTTP calls yet
	Session_Loaded,    // GET /api/v1/session returned
	Markets_Loaded,    // GET /api/v1/markets returned
	Ready,             // Bootstrap complete, ready for WS
	WS_Connected,      // WS open, hello received
	Subscribing,       // Subscribe ops sent, waiting for acks
	Live,              // Events flowing
	Degraded,          // Connected but health issues (desync)
	Offline,           // WS disconnected after having been connected
	Not_Ready,         // Backend not ready (session.ready=false)
}

// Pure derivation: computes lifecycle from current observable state.
// Called once per frame at the end of drain_marketdata.
derive_lifecycle :: proc(
	has_session: bool,
	session_ready: bool,
	has_markets: bool,
	ws_connected: bool,
	was_ever_connected: bool,
	has_subscribe_acks: bool,
	has_events: bool,
	has_desync: bool,
) -> Bootstrap_Lifecycle {
	if has_session && !session_ready do return .Not_Ready
	if !ws_connected {
		if was_ever_connected do return .Offline
		if session_ready && has_markets do return .Ready
		if has_markets do return .Markets_Loaded
		if has_session do return .Session_Loaded
		return .Init
	}
	if has_desync do return .Degraded
	if has_events do return .Live
	if has_subscribe_acks do return .Subscribing
	return .WS_Connected
}
