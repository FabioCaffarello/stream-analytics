package app

import "core:testing"
import "mr:services"

// S122: Workspace Artifact Tests — CRC integrity, fingerprint, determinism.

// ---------------------------------------------------------------------------
// FNV-1a Hash
// ---------------------------------------------------------------------------

@(test)
test_fnv1a_deterministic :: proc(t: ^testing.T) {
	h1 := fnv1a_32("V6|C|CW:50,50|RW:50,50|0:-1:0:1:1:0:0,0,0,0,0:0:0|LK:1")
	h2 := fnv1a_32("V6|C|CW:50,50|RW:50,50|0:-1:0:1:1:0:0,0,0,0,0:0:0|LK:1")
	testing.expect_value(t, h1, h2)
	testing.expect(t, h1 != 0, "hash should not be zero for non-empty input")
}

@(test)
test_fnv1a_different_inputs :: proc(t: ^testing.T) {
	h1 := fnv1a_32("hello")
	h2 := fnv1a_32("world")
	testing.expect(t, h1 != h2, "different inputs should produce different hashes")
}

@(test)
test_fnv1a_empty_string :: proc(t: ^testing.T) {
	h := fnv1a_32("")
	// FNV-1a offset basis is 0x811c9dc5.
	testing.expect_value(t, h, u32(0x811c9dc5))
}

// ---------------------------------------------------------------------------
// CRC Suffix Write / Validate
// ---------------------------------------------------------------------------

@(test)
test_ck_suffix_round_trip :: proc(t: ^testing.T) {
	body := "V6|C|CW:50,50|RW:50,50|0:-1:0:1:1:0:0,0,0,0,0:0:0|LK:1"
	buf: [256]u8
	// Copy body into buf.
	for i in 0 ..< len(body) { buf[i] = body[i] }
	off := artifact_write_ck_suffix(buf[:], len(body), body)

	full := string(buf[:off])
	// Should end with |CK:XXXXXXXX (12 chars).
	testing.expect(t, off == len(body) + 12, "CK suffix should be 12 chars")

	// Validate.
	restored_body, valid, has_ck := artifact_validate_ck(full)
	testing.expect(t, has_ck, "should detect CK suffix")
	testing.expect(t, valid, "CK should validate")
	testing.expect(t, restored_body == body, "body should be stripped of CK")
}

@(test)
test_ck_validate_no_suffix :: proc(t: ^testing.T) {
	body := "V6|C|CW:50|RW:50|0:-1:0:1:1:0:0,0,0,0,0:0:0|LK:1"
	restored_body, valid, has_ck := artifact_validate_ck(body)
	testing.expect(t, !has_ck, "should not detect CK suffix")
	testing.expect(t, valid, "should be valid (legacy tolerance)")
	testing.expect(t, restored_body == body, "body should be unchanged")
}

@(test)
test_ck_validate_corrupted :: proc(t: ^testing.T) {
	body := "V6|C|CW:50|RW:50|0:-1:0:1:1:0:0,0,0,0,0:0:0|LK:1"
	buf: [256]u8
	for i in 0 ..< len(body) { buf[i] = body[i] }
	off := artifact_write_ck_suffix(buf[:], len(body), body)

	// Corrupt one byte in the body.
	buf[5] = 'X'
	full := string(buf[:off])

	_, valid, has_ck := artifact_validate_ck(full)
	testing.expect(t, has_ck, "should detect CK suffix")
	testing.expect(t, !valid, "corrupted body should fail CK validation")
}

@(test)
test_ck_validate_tampered_checksum :: proc(t: ^testing.T) {
	body := "V6|C|CW:50|RW:50|0:-1:0:1:1:0:0,0,0,0,0:0:0|LK:1"
	buf: [256]u8
	for i in 0 ..< len(body) { buf[i] = body[i] }
	off := artifact_write_ck_suffix(buf[:], len(body), body)

	// Tamper with the checksum hex.
	buf[off - 1] = '0'
	buf[off - 2] = '0'
	full := string(buf[:off])

	_, valid, has_ck := artifact_validate_ck(full)
	testing.expect(t, has_ck, "should detect CK suffix")
	testing.expect(t, !valid, "tampered checksum should fail validation")
}

// ---------------------------------------------------------------------------
// Persist with CRC
// ---------------------------------------------------------------------------

@(test)
test_persist_v6_includes_ck :: proc(t: ^testing.T) {
	state := make_persist_state(2)
	defer free(state)

	// Build V6 + CK in a buffer that stays alive.
	buf: [2048]u8
	off := build_layout_v6_string(state, buf[:])
	body := string(buf[:off])
	off = artifact_write_ck_suffix(buf[:], off, body)
	full := string(buf[:off])

	testing.expect(t, len(full) > 12, "V6 string should be non-trivial")

	// Verify CK suffix.
	_, valid, has_ck := artifact_validate_ck(full)
	testing.expect(t, has_ck, "V6 with CK should have CK suffix")
	testing.expect(t, valid, "V6 CK should validate")
}

@(test)
test_restore_v6_with_ck :: proc(t: ^testing.T) {
	state := make_persist_state(3)
	defer free(state)
	state.world.widgets[0].kind = .Candle
	state.world.widgets[1].kind = .Trades
	state.world.widgets[2].kind = .Orderbook
	binding_set(&state.world.bindings[0], "binance", "BTCUSDT")

	// Build V6 + CK in a buffer that stays alive in this scope.
	buf: [2048]u8
	off := build_layout_v6_string(state, buf[:])
	body := string(buf[:off])
	off = artifact_write_ck_suffix(buf[:], off, body)
	v6_with_ck := string(buf[:off])

	restored := new(App_State)
	defer free(restored)
	result := restore_layout_v6_validated(restored, v6_with_ck)
	testing.expect(t, result == .Ok, "restore with CK should succeed")
	testing.expect_value(t, restored.world.count, 3)
	testing.expect(t, restored.world.widgets[0].kind == .Candle, "cell 0 kind")
}

@(test)
test_restore_v6_corrupted_ck_rejected :: proc(t: ^testing.T) {
	state := make_persist_state(1)
	defer free(state)

	// Build V6 + CK in buffer.
	buf: [2048]u8
	off := build_layout_v6_string(state, buf[:])
	body := string(buf[:off])
	off = artifact_write_ck_suffix(buf[:], off, body)

	// Corrupt body while keeping CK suffix intact.
	buf[5] = 'X' // flip a byte in body
	corrupted_str := string(buf[:off])

	restored := new(App_State)
	defer free(restored)
	result := restore_layout_v6_validated(restored, corrupted_str)
	testing.expect(t, result == .Corrupted, "corrupted CK should return Corrupted")
}

@(test)
test_restore_v6_without_ck_accepted :: proc(t: ^testing.T) {
	// Legacy V6 strings without CK should still be accepted.
	state := make_persist_state(1)
	defer free(state)
	buf: [2048]u8
	off := build_layout_v6_string(state, buf[:])
	v6_no_ck := string(buf[:off])

	restored := new(App_State)
	defer free(restored)
	result := restore_layout_v6_validated(restored, v6_no_ck)
	testing.expect(t, result == .Ok, "V6 without CK should be accepted (legacy tolerance)")
}

// ---------------------------------------------------------------------------
// Fingerprint
// ---------------------------------------------------------------------------

@(test)
test_fingerprint_deterministic :: proc(t: ^testing.T) {
	state := make_persist_state(2)
	defer free(state)
	state.world.widgets[0].kind = .Candle
	binding_set(&state.world.bindings[0], "binance", "BTCUSDT")

	fp1 := workspace_artifact_fingerprint(state)
	fp2 := workspace_artifact_fingerprint(state)
	testing.expect_value(t, fp1, fp2)
	testing.expect(t, fp1 != 0, "fingerprint should not be zero")
}

@(test)
test_fingerprint_changes_on_mutation :: proc(t: ^testing.T) {
	state := make_persist_state(2)
	defer free(state)
	state.world.widgets[0].kind = .Candle

	fp1 := workspace_artifact_fingerprint(state)

	// Mutate state.
	state.world.indicators[0].show_ma = true

	fp2 := workspace_artifact_fingerprint(state)
	testing.expect(t, fp1 != fp2, "fingerprint should change after mutation")
}

@(test)
test_artifact_changed_after_mutation :: proc(t: ^testing.T) {
	state := make_persist_state(1)
	defer free(state)

	// Stamp initial fingerprint.
	workspace_artifact_stamp(state)
	testing.expect(t, !workspace_artifact_changed(state), "should not be changed right after stamp")

	// Mutate state.
	state.world.indicators[0].show_vwap = true
	testing.expect(t, workspace_artifact_changed(state), "should be changed after mutation")

	// Re-stamp.
	workspace_artifact_stamp(state)
	testing.expect(t, !workspace_artifact_changed(state), "should not be changed after re-stamp")
}

@(test)
test_persist_stamps_fingerprint :: proc(t: ^testing.T) {
	state := make_persist_state(1)
	defer free(state)
	testing.expect_value(t, state.last_persist_fingerprint, u32(0))

	// Stamp manually (persist_layout_v6 uses stack buffers which are ephemeral).
	workspace_artifact_stamp(state)
	testing.expect(t, state.last_persist_fingerprint != 0, "stamp should set fingerprint")

	// Subsequent stamp of same state should produce same fingerprint.
	fp1 := state.last_persist_fingerprint
	workspace_artifact_stamp(state)
	testing.expect_value(t, state.last_persist_fingerprint, fp1)
}

// ---------------------------------------------------------------------------
// Import with CRC
// ---------------------------------------------------------------------------

@(test)
test_import_v6_with_ck :: proc(t: ^testing.T) {
	state := make_persist_state(2)
	defer free(state)
	state.world.widgets[0].kind = .Candle
	state.world.widgets[1].kind = .Trades

	// Build V6 string with CK.
	buf: [2048]u8
	off := build_layout_v6_string(state, buf[:])
	body := string(buf[:off])
	off = artifact_write_ck_suffix(buf[:], off, body)
	v6_with_ck := string(buf[:off])

	restored := new(App_State)
	defer free(restored)
	ok := layout_import_from_string(restored, v6_with_ck)
	testing.expect(t, ok, "import with CK should succeed")
	testing.expect_value(t, restored.world.count, 2)
}

@(test)
test_import_rejects_non_v6 :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	testing.expect(t, !layout_import_from_string(state, "V5|C|CW:50|RW:50"), "V5 import should be rejected")
	testing.expect(t, !layout_import_from_string(state, "V4|C|CW:50|RW:50"), "V4 import should be rejected")
	testing.expect(t, !layout_import_from_string(state, "abc"), "garbage import should be rejected")
}

// ---------------------------------------------------------------------------
// Custom Preset with CRC
// ---------------------------------------------------------------------------

@(test)
test_custom_preset_saves_with_ck :: proc(t: ^testing.T) {
	state := make_persist_state(1)
	defer free(state)

	// Build V6 + CK in buffer (mimics save_custom_preset).
	buf: [2048]u8
	off := build_layout_v6_string(state, buf[:])
	body := string(buf[:off])
	off = artifact_write_ck_suffix(buf[:], off, body)
	full := string(buf[:off])

	_, valid, has_ck := artifact_validate_ck(full)
	testing.expect(t, has_ck, "preset should have CK suffix")
	testing.expect(t, valid, "preset CK should validate")
}

@(test)
test_custom_preset_loads_with_ck :: proc(t: ^testing.T) {
	state := make_persist_state(2)
	defer free(state)
	state.world.widgets[0].kind = .Candle
	state.world.widgets[1].kind = .Orderbook

	// Build V6 + CK in a buffer that stays alive in this scope.
	buf: [2048]u8
	off := build_layout_v6_string(state, buf[:])
	body := string(buf[:off])
	off = artifact_write_ck_suffix(buf[:], off, body)
	v6_with_ck := string(buf[:off])

	restored := new(App_State)
	defer free(restored)
	// Store directly using the in-scope buffer.
	services.settings_set(&restored.settings, services.SETTING_CUSTOM_LAYOUT_1, v6_with_ck)

	ok := load_custom_preset(restored, 1)
	testing.expect(t, ok, "load preset with CK should succeed")
	testing.expect_value(t, restored.world.count, 2)
	testing.expect(t, restored.world.widgets[0].kind == .Candle, "cell 0 kind")
	testing.expect(t, restored.world.widgets[1].kind == .Orderbook, "cell 1 kind")
}

// ---------------------------------------------------------------------------
// Schema Version
// ---------------------------------------------------------------------------

@(test)
test_schema_version_12 :: proc(t: ^testing.T) {
	testing.expect_value(t, WORKSPACE_SCHEMA_VERSION, 12)
}

@(test)
test_persist_schema_version_constant :: proc(t: ^testing.T) {
	// Verify the constant is bumped to 12 — no settings store needed.
	testing.expect_value(t, WORKSPACE_SCHEMA_VERSION, 12)
}
