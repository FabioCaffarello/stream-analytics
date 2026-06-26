package main

// Native settings port — JSON file-backed persistence.
// Config path: ~/.stream-analytics.json

import "core:encoding/json"
import "core:fmt"
import "core:os"
import "core:strings"
import "mr:ports"

SETTINGS_FILE :: ".stream-analytics.json"

// --- File-private state ---

@(private = "file")
g_settings_map: map[string]string

@(private = "file")
g_settings_loaded: bool

// --- Public API ---

make_settings_port :: proc() -> ports.Settings_Port {
	native_load_all()
	return ports.Settings_Port{
		load  = native_settings_load,
		save  = native_settings_save,
		flush = native_settings_flush,
	}
}

// Read a persisted setting directly from native storage.
// Used by platform services that are not wired through core Settings_Store.
native_settings_lookup :: proc(key: string) -> (value: string, ok: bool) {
	return native_settings_load(key)
}

// --- Port implementation ---

@(private = "file")
native_settings_load :: proc(key: string) -> (value: string, ok: bool) {
	if !g_settings_loaded do native_load_all()
	v, found := g_settings_map[key]
	return v, found
}

@(private = "file")
native_settings_save :: proc(key: string, value: string) -> bool {
	if g_settings_map == nil {
		g_settings_map = make(map[string]string)
	}
	g_settings_map[key] = value
	return true
}

@(private = "file")
native_settings_flush :: proc() {
	if g_settings_map == nil do return
	path := settings_file_path()
	if path == "" do return

	data, err := json.marshal(g_settings_map)
	if err != nil {
		fmt.printf("[settings] Marshal error: %v\n", err)
		return
	}
	defer delete(data)

	ok := os.write_entire_file(path, data)
	if !ok {
		fmt.println("[settings] Failed to write", path)
	}
}

// --- Internal ---

@(private = "file")
native_load_all :: proc() {
	g_settings_loaded = true
	path := settings_file_path()
	if path == "" do return

	data, ok := os.read_entire_file(path)
	if !ok do return
	defer delete(data)

	loaded: map[string]string
	if json.unmarshal(data, &loaded) == nil {
		g_settings_map = loaded
	}
}

@(private = "file")
settings_file_path :: proc() -> string {
	home := os.get_env("HOME", context.temp_allocator)
	if home == "" do return ""
	return strings.concatenate({home, "/", SETTINGS_FILE}, context.temp_allocator)
}
