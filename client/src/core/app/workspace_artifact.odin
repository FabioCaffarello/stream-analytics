package app

// S122: Workspace Artifact — Deterministic integrity + fingerprint for workspace persistence.
//
// Adds:
//   - FNV-1a checksum (|CK: suffix) for corruption detection on V6 strings.
//   - Workspace fingerprint for idempotent restore comparison.
//   - Artifact metadata for future bundle/versioning support.
//
// CK suffix format:  |CK:XXXXXXXX  (8 hex digits, FNV-1a of body before |CK:)
// Backward-compatible: V6 strings without |CK: are accepted (legacy tolerance).

// FNV-1a 32-bit hash — zero-alloc, deterministic, same algorithm as shared/hash.
fnv1a_32 :: proc(data: string) -> u32 {
	h: u32 = 0x811c9dc5
	for b in data {
		h ~= u32(b)
		h *= 0x01000193
	}
	return h
}

// Compute CK suffix string for a V6 body. Writes "|CK:XXXXXXXX" into buf at off.
// Returns new offset.
artifact_write_ck_suffix :: proc(buf: []u8, off: int, body: string) -> int {
	o := off
	hash := fnv1a_32(body)

	buf[o] = '|'; o += 1
	buf[o] = 'C'; o += 1
	buf[o] = 'K'; o += 1
	buf[o] = ':'; o += 1

	// Write 8 hex digits (uppercase).
	@(static, rodata)
	hex := [16]u8{'0','1','2','3','4','5','6','7','8','9','A','B','C','D','E','F'}
	for i := 7; i >= 0; i -= 1 {
		nibble := (hash >> uint(i * 4)) & 0xF
		buf[o] = hex[nibble]
		o += 1
	}

	return o
}

// Validate CK suffix on a V6 string. Returns (body, valid, has_ck).
// body = string without |CK: suffix.
// valid = true if CK matches or no CK present.
// has_ck = true if CK suffix was found.
artifact_validate_ck :: proc(v: string) -> (body: string, valid: bool, has_ck: bool) {
	// Scan backward for |CK: suffix (always last 12 chars: |CK:XXXXXXXX).
	if len(v) >= 12 {
		ck_start := len(v) - 12
		if v[ck_start] == '|' && v[ck_start + 1] == 'C' && v[ck_start + 2] == 'K' && v[ck_start + 3] == ':' {
			body = v[:ck_start]
			hex_str := v[ck_start + 4:]
			expected := fnv1a_32(body)
			parsed := parse_hex_u32(hex_str)
			return body, parsed == expected, true
		}
	}
	// No CK suffix — accept as-is (legacy tolerance).
	return v, true, false
}

// Parse 8-char hex string to u32.
@(private = "file")
parse_hex_u32 :: proc(s: string) -> u32 {
	if len(s) != 8 do return 0
	v: u32 = 0
	for c in s {
		v <<= 4
		switch c {
		case '0'..='9': v |= u32(c - '0')
		case 'A'..='F': v |= u32(c - 'A' + 10)
		case 'a'..='f': v |= u32(c - 'a' + 10)
		case: return 0
		}
	}
	return v
}

// Workspace artifact fingerprint — FNV-1a hash of the persisted workspace state.
// Used for idempotent comparison: if fingerprint hasn't changed, skip re-persist.
workspace_artifact_fingerprint :: proc(state: ^App_State) -> u32 {
	buf: [2048]u8
	off := build_layout_v6_string(state, buf[:])
	return fnv1a_32(string(buf[:off]))
}

// Check if workspace state has changed since last persist.
workspace_artifact_changed :: proc(state: ^App_State) -> bool {
	return workspace_artifact_fingerprint(state) != state.last_persist_fingerprint
}

// Stamp the current fingerprint after a successful persist.
workspace_artifact_stamp :: proc(state: ^App_State) {
	state.last_persist_fingerprint = workspace_artifact_fingerprint(state)
}
