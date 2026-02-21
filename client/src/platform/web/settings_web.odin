package main

// Stub settings port for WASM/web.
// Future: bridge to localStorage via foreign procs in odin.js.

import "mr:ports"

stub_settings_port :: proc() -> ports.Settings_Port {
	return {
		load  = stub_settings_load,
		save  = stub_settings_save,
		flush = stub_settings_flush,
	}
}

@(private = "file")
stub_settings_load :: proc(key: string) -> (value: string, ok: bool) {
	return "", false
}

@(private = "file")
stub_settings_save :: proc(key: string, value: string) -> bool {
	return true
}

@(private = "file")
stub_settings_flush :: proc() {}
