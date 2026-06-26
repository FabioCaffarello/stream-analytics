package main

// Web settings port backed by localStorage via JS storage bridge (modules/storage.js).

import "core:strings"
import "mr:ports"

foreign import odin_env "odin_env"

@(default_calling_convention = "contextless")
foreign odin_env {
	web_settings_load :: proc(key_ptr: [^]u8, key_len: i32, out_ptr: [^]u8, out_cap: i32) -> i32 ---
	web_settings_save :: proc(key_ptr: [^]u8, key_len: i32, value_ptr: [^]u8, value_len: i32) -> bool ---
	web_clipboard_write :: proc(text_ptr: [^]u8, text_len: i32) -> bool ---
	// S126: Workspace backend persistence.
	web_workspace_load :: proc() -> i32 ---  // Load workspace from backend into localStorage. Returns 1 on success.
	web_workspace_sync :: proc() -> i32 ---  // Sync localStorage workspace to backend. Returns 1 on success.
}

WEB_SETTINGS_VAL_CAP :: 8192

stub_settings_port :: proc() -> ports.Settings_Port {
	return {
		load            = web_settings_load_value,
		save            = web_settings_save_value,
		flush           = web_settings_flush,
		clipboard_write = web_clipboard_write_text,
		backend_load    = web_workspace_backend_load,
		backend_sync    = web_workspace_backend_sync,
	}
}

// Read a persisted setting directly from web localStorage bridge.
// Used by platform services that are not wired through core Settings_Store.
web_settings_lookup :: proc(key: string) -> (value: string, ok: bool) {
	return web_settings_load_value(key)
}

@(private = "file")
web_settings_load_value :: proc(key: string) -> (value: string, ok: bool) {
	if len(key) == 0 do return "", false
	buf: [WEB_SETTINGS_VAL_CAP]u8
	n := web_settings_load(
		raw_data(transmute([]u8)key), i32(len(key)),
		raw_data(buf[:]), i32(len(buf)),
	)
	if n <= 0 do return "", false
	if n > i32(len(buf)) do n = i32(len(buf))
	return strings.clone(string(buf[:int(n)])), true
}

@(private = "file")
web_settings_save_value :: proc(key: string, value: string) -> bool {
	if len(key) == 0 do return false
	return web_settings_save(
		raw_data(transmute([]u8)key), i32(len(key)),
		raw_data(transmute([]u8)value), i32(len(value)),
	)
}

@(private = "file")
web_clipboard_write_text :: proc(text: string) -> bool {
	if len(text) == 0 do return false
	return web_clipboard_write(
		raw_data(transmute([]u8)text), i32(len(text)),
	)
}

@(private = "file")
web_settings_flush :: proc() {
	// localStorage writes are immediate.
}

// S126: Load workspace state from backend into localStorage before settings_init.
// Returns true if backend had saved state that was applied.
web_workspace_backend_load :: proc() -> bool {
	return web_workspace_load() > 0
}

// S126: Sync current localStorage workspace state to backend.
// Returns true if backend accepted the save.
web_workspace_backend_sync :: proc() -> bool {
	return web_workspace_sync() > 0
}
