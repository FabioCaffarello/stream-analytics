package services

import "core:fmt"
import "core:strconv"
import "core:strings"

PROFILE_CAP :: 12
PROFILE_KEY_CAP :: 32
PROFILE_COUNT_KEY :: SETTING_CONNECTION_PROFILE_COUNT
PROFILE_ACTIVE_KEY :: SETTING_CONNECTION_PROFILE_ACTIVE
PROFILE_ENTRY_PREFIX :: "connection_profile_"

Connection_Profile :: struct {
	name:        [32]u8,
	name_len:    u8,
	ws_url:      [256]u8,
	ws_url_len:  u16,
	venue:       [32]u8,
	venue_len:   u8,
	symbol:      [48]u8,
	symbol_len:  u8,
	market_type: [24]u8,
	market_type_len: u8,
	api_key_ref: [64]u8,
	api_key_ref_len: u8,
	session_only: bool,
	jwt_token:   [256]u8,
	jwt_token_len: u16,
}

Profile_Store :: struct {
	profiles:            [PROFILE_CAP]Connection_Profile,
	count:               int,
	active_idx:          int,
	session_api_key:     [128]u8,
	session_api_key_len: u8,
}

@(private = "file")
set_fixed_string :: proc(dst: []u8, dst_len: ^u8, value: string) {
	n := min(len(dst), len(value))
	for i in 0 ..< n {
		dst[i] = value[i]
	}
	if dst_len != nil do dst_len^ = u8(n)
}

@(private = "file")
set_fixed_string_u16 :: proc(dst: []u8, dst_len: ^u16, value: string) {
	n := min(len(dst), len(value))
	for i in 0 ..< n {
		dst[i] = value[i]
	}
	if dst_len != nil do dst_len^ = u16(n)
}

@(private = "file")
fixed_string :: proc(buf: []u8, n: int) -> string {
	m := n
	if m <= 0 do return ""
	if m > len(buf) do m = len(buf)
	return string(buf[:m])
}

profile_name :: proc(p: ^Connection_Profile) -> string {
	if p == nil do return ""
	return fixed_string(p.name[:], int(p.name_len))
}

profile_ws_url :: proc(p: ^Connection_Profile) -> string {
	if p == nil do return ""
	return fixed_string(p.ws_url[:], int(p.ws_url_len))
}

profile_venue :: proc(p: ^Connection_Profile) -> string {
	if p == nil do return ""
	return fixed_string(p.venue[:], int(p.venue_len))
}

profile_symbol :: proc(p: ^Connection_Profile) -> string {
	if p == nil do return ""
	return fixed_string(p.symbol[:], int(p.symbol_len))
}

profile_market_type :: proc(p: ^Connection_Profile) -> string {
	if p == nil do return ""
	return fixed_string(p.market_type[:], int(p.market_type_len))
}

profile_api_key_ref :: proc(p: ^Connection_Profile) -> string {
	if p == nil do return ""
	return fixed_string(p.api_key_ref[:], int(p.api_key_ref_len))
}

profile_jwt_token :: proc(p: ^Connection_Profile) -> string {
	if p == nil do return ""
	return fixed_string(p.jwt_token[:], int(p.jwt_token_len))
}

profile_make :: proc(name: string, ws_url: string, venue: string, symbol: string, market_type: string = "", api_key_ref: string = "", session_only: bool = true, jwt_token: string = "") -> Connection_Profile {
	p: Connection_Profile
	set_fixed_string(p.name[:], &p.name_len, name)
	set_fixed_string_u16(p.ws_url[:], &p.ws_url_len, ws_url)
	set_fixed_string(p.venue[:], &p.venue_len, venue)
	set_fixed_string(p.symbol[:], &p.symbol_len, symbol)
	set_fixed_string(p.market_type[:], &p.market_type_len, market_type)
	set_fixed_string(p.api_key_ref[:], &p.api_key_ref_len, api_key_ref)
	p.session_only = session_only
	set_fixed_string_u16(p.jwt_token[:], &p.jwt_token_len, jwt_token)
	return p
}

profile_store_active :: proc(store: ^Profile_Store) -> ^Connection_Profile {
	if store == nil do return nil
	if store.count <= 0 do return nil
	idx := clamp(store.active_idx, 0, store.count - 1)
	return &store.profiles[idx]
}

profile_store_set_active :: proc(store: ^Profile_Store, idx: int) -> bool {
	if store == nil do return false
	if idx < 0 || idx >= store.count do return false
	store.active_idx = idx
	return true
}

profile_store_upsert :: proc(store: ^Profile_Store, profile: Connection_Profile) -> bool {
	if store == nil do return false
	p := profile
	name := profile_name(&p)
	if len(name) == 0 do return false
	for i in 0 ..< store.count {
		if profile_name(&store.profiles[i]) == name {
			store.profiles[i] = p
			return true
		}
	}
	if store.count >= PROFILE_CAP do return false
	store.profiles[store.count] = p
	store.count += 1
	if store.count == 1 do store.active_idx = 0
	return true
}

profile_store_remove :: proc(store: ^Profile_Store, idx: int) -> bool {
	if store == nil do return false
	if idx < 0 || idx >= store.count do return false
	for i in idx ..< store.count - 1 {
		store.profiles[i] = store.profiles[i + 1]
	}
	store.profiles[store.count - 1] = {}
	store.count -= 1
	if store.count <= 0 {
		store.active_idx = 0
		return true
	}
	if store.active_idx >= store.count do store.active_idx = store.count - 1
	return true
}

@(private = "file")
profile_store_key_for_index :: proc(idx: int, buf: []u8) -> string {
	prefix := PROFILE_ENTRY_PREFIX
	n := 0
	for c in prefix {
		if n >= len(buf) do break
		buf[n] = u8(c)
		n += 1
	}
	suf := fmt.bprintf(buf[n:], "%d", idx)
	_ = suf
	end := n
	for end < len(buf) && buf[end] != 0 {
		end += 1
	}
	return string(buf[:end])
}

@(private = "file")
profile_serialize_into :: proc(buf: []u8, p: ^Connection_Profile) -> string {
	if p == nil do return ""
	session_only := p.session_only ? "1" : "0"
	out := strings.concatenate({
		profile_name(p), "|",
		profile_ws_url(p), "|",
		profile_venue(p), "|",
		profile_symbol(p), "|",
		profile_market_type(p), "|",
		profile_api_key_ref(p), "|",
		session_only, "|",
		profile_jwt_token(p),
	})
	defer delete(out)
	n := min(len(buf), len(out))
	for i in 0 ..< n {
		buf[i] = out[i]
	}
	return string(buf[:n])
}

@(private = "file")
profile_deserialize :: proc(raw: string) -> (Connection_Profile, bool) {
	parts: [8]string
	part_idx := 0
	start := 0
	for i in 0 ..< len(raw) {
		if raw[i] != '|' do continue
		if part_idx < len(parts) {
			parts[part_idx] = raw[start:i]
			part_idx += 1
		}
		start = i + 1
	}
	if part_idx < len(parts) {
		parts[part_idx] = raw[start:]
		part_idx += 1
	}
	if part_idx < 7 do return {}, false
	jwt := parts[7] if part_idx >= 8 else ""
	profile := profile_make(parts[0], parts[1], parts[2], parts[3], parts[4], parts[5], parts[6] != "0", jwt)
	return profile, len(profile_name(&profile)) > 0
}

profile_store_save :: proc(store: ^Profile_Store, settings: ^Settings_Store) {
	if store == nil || settings == nil do return
	count_buf: [8]u8
	settings_set(settings, PROFILE_COUNT_KEY, fmt.bprintf(count_buf[:], "%d", store.count))
	active_buf: [8]u8
	settings_set(settings, PROFILE_ACTIVE_KEY, fmt.bprintf(active_buf[:], "%d", store.active_idx))
	for i in 0 ..< PROFILE_CAP {
		key_buf: [PROFILE_KEY_CAP]u8
		key := profile_store_key_for_index(i, key_buf[:])
		if i < store.count {
			entry_buf: [512]u8
			settings_set(settings, key, profile_serialize_into(entry_buf[:], &store.profiles[i]))
		} else {
			settings_set(settings, key, "")
		}
	}
}

profile_store_load :: proc(store: ^Profile_Store, settings: ^Settings_Store) {
	if store == nil || settings == nil do return
	store^ = {}
	count := 0
	if v, ok := settings_get(settings, PROFILE_COUNT_KEY); ok {
		count = parse_int_clamped(v, 0, PROFILE_CAP, 0)
	}
	for i in 0 ..< count {
		key_buf: [PROFILE_KEY_CAP]u8
		key := profile_store_key_for_index(i, key_buf[:])
		raw, ok := settings_get(settings, key)
		if !ok || len(raw) == 0 do continue
		if p, ok := profile_deserialize(raw); ok {
			store.profiles[store.count] = p
			store.count += 1
		}
	}
	if v, ok := settings_get(settings, PROFILE_ACTIVE_KEY); ok {
		store.active_idx = parse_int_clamped(v, 0, max(store.count - 1, 0), 0)
	}
}

profile_store_ensure_default :: proc(store: ^Profile_Store, settings: ^Settings_Store,
	name: string, ws_url: string, venue: string, symbol: string, market_type: string = "") {
	if store == nil || settings == nil do return
	profile_store_load(store, settings)
	if store.count > 0 do return
	_ = profile_store_upsert(store, profile_make(name, ws_url, venue, symbol, market_type))
	store.active_idx = 0
	profile_store_save(store, settings)
	settings_flush(settings)
}

@(private = "file")
parse_int_clamped :: proc(raw: string, lo: int, hi: int, fallback: int) -> int {
	v64, ok := strconv.parse_int(strings.trim_space(raw))
	if !ok do return fallback
	v := int(v64)
	if v < lo do return lo
	if v > hi do return hi
	return v
}
