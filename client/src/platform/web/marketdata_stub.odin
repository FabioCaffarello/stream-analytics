package main

// Stub marketdata port for WASM/offline mode.

import "mr:ports"

stub_marketdata_port :: proc() -> ports.Marketdata_Port {
		return {
			subscribe   = stub_subscribe,
			unsubscribe = stub_unsubscribe,
			poll        = stub_poll,
			now_ms      = stub_now_ms,
			conn_status = stub_conn_status,
			metrics     = stub_metrics,
			describe_stream = stub_describe_stream,
			shutdown    = stub_shutdown,
		}
	}

@(private = "file")
stub_subscribe :: proc(venue: string, symbol: string, channel: ports.MD_Channel) -> bool {
	return false
}

@(private = "file")
stub_unsubscribe :: proc(venue: string, symbol: string, channel: ports.MD_Channel) {}

@(private = "file")
stub_poll :: proc(events_buf: []ports.MD_Event) -> int {
	return 0
}

@(private = "file")
stub_now_ms :: proc() -> i64 {
	return 0
}

@(private = "file")
stub_conn_status :: proc() -> ports.MD_Conn_Status {
	return .Offline
}

@(private = "file")
stub_metrics :: proc(out: ^ports.MD_Runtime_Metrics) -> bool {
	return false
}

@(private = "file")
stub_describe_stream :: proc(subject_id: u64, out: ^ports.MD_Stream_Info) -> bool {
	return false
}

@(private = "file")
stub_shutdown :: proc() {}
