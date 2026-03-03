package md_common

// Keep Terminal_V1 as default path; legacy fallback is opt-in.
ALLOW_LEGACY_WS_DEFAULT :: #config(ALLOW_LEGACY_WS, false)

legacy_switch_from_text :: proc(raw: string) -> bool {
	if len(raw) == 0 do return ALLOW_LEGACY_WS_DEFAULT
	if raw == "0" || raw == "off" || raw == "OFF" || raw == "false" || raw == "FALSE" {
		return false
	}
	if raw == "1" || raw == "on" || raw == "ON" || raw == "true" || raw == "TRUE" {
		return true
	}
	return ALLOW_LEGACY_WS_DEFAULT
}
