package services

// In-memory settings store with dirty tracking.
// Fixed capacity, zero allocation. Platform port handles persistence.

import "mr:ports"

SETTINGS_CAP :: 64

Settings_Entry :: struct {
	key:   string,
	value: string,
	used:  bool,
}

Settings_Store :: struct {
	entries: [SETTINGS_CAP]Settings_Entry,
	count:   int,
	dirty:   bool,
	port:    ports.Settings_Port,
}

// Known settings keys.
SETTING_SYMBOL       :: "symbol"
SETTING_THEME        :: "theme"
SETTING_OB_PRICE_GRP :: "ob_price_group"

// Initialize store, loading known keys from port.
settings_init :: proc(store: ^Settings_Store, port: ports.Settings_Port) {
	store.port = port
	if port.load == nil do return

	// Pre-load known keys.
	known_keys := [?]string{SETTING_SYMBOL, SETTING_THEME, SETTING_OB_PRICE_GRP}
	for key in known_keys {
		value, ok := port.load(key)
		if ok {
			settings_set_internal(store, key, value)
		}
	}
	store.dirty = false // loading doesn't count as dirty
}

settings_get :: proc(store: ^Settings_Store, key: string) -> (string, bool) {
	for i in 0 ..< store.count {
		if store.entries[i].used && store.entries[i].key == key {
			return store.entries[i].value, true
		}
	}
	return "", false
}

settings_set :: proc(store: ^Settings_Store, key: string, value: string) {
	settings_set_internal(store, key, value)
	store.dirty = true
}

settings_flush :: proc(store: ^Settings_Store) {
	if !store.dirty do return
	if store.port.save == nil do return

	for i in 0 ..< store.count {
		if store.entries[i].used {
			store.port.save(store.entries[i].key, store.entries[i].value)
		}
	}

	if store.port.flush != nil {
		store.port.flush()
	}
	store.dirty = false
}

// --- Internal ---

@(private = "file")
settings_set_internal :: proc(store: ^Settings_Store, key: string, value: string) {
	// Update existing.
	for i in 0 ..< store.count {
		if store.entries[i].used && store.entries[i].key == key {
			store.entries[i].value = value
			return
		}
	}
	// Insert new.
	if store.count < SETTINGS_CAP {
		store.entries[store.count] = Settings_Entry{
			key   = key,
			value = value,
			used  = true,
		}
		store.count += 1
	}
}
