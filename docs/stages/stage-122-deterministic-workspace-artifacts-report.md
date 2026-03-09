# Stage 122 — Deterministic Workspace Artifacts

**Date:** 2026-03-09
**Status:** COMPLETE
**Schema Version:** 12 (was 11)

## Objective

Elevate workspace persistence to institutional-grade: deterministic, reproducible, integrity-verified.

## Changes

### 1. Legacy V1-V5 Removal (Dead Code Cleanup)

Removed all legacy persistence functions and settings keys that have been dead since S111:

**Removed functions (layout_persist.odin):**
- `persist_layout()` (V1)
- `restore_layout()` / `restore_layout_from_string()` (V1)
- `persist_layout_v2()` / `restore_layout_v2()` (V2)
- `persist_layout_v3()` / `restore_layout_v3()` (V3)
- `persist_layout_v4()` / `restore_layout_v4()` / `restore_layout_v4_from_string()` (V4)
- `persist_layout_v5_from_v4()` / `restore_layout_v5()` / `restore_layout_v5_from_string()` (V5)

**Removed settings keys (settings_store.odin):**
- `SETTING_LAYOUT` (V1), `SETTING_LAYOUT_V2`, `SETTING_LAYOUT_V3`, `SETTING_LAYOUT_V4`, `SETTING_LAYOUT_V5`
- Corresponding entries removed from `known_keys` init array

**Impact:** ~800 lines of dead code removed. `layout_persist.odin` went from 1650 to 430 lines.

### 2. CRC Integrity Footer (workspace_artifact.odin)

Added FNV-1a integrity checksum to all persisted workspace strings:

- **Format:** `|CK:XXXXXXXX` suffix (8 hex digits, FNV-1a hash of body before `|CK:`)
- **Write path:** `persist_layout_v6()` and `save_custom_preset()` append CK automatically
- **Read path:** `restore_layout_v6_validated()` validates CK if present, rejects on mismatch
- **Legacy tolerance:** V6 strings without `|CK:` are accepted (graceful degradation)
- **Zero allocation:** FNV-1a computed inline, hex written to stack buffer

### 3. Artifact Fingerprint

Added workspace state fingerprinting for idempotent persist gating:

- `workspace_artifact_fingerprint(state)` — FNV-1a of V6 body string
- `workspace_artifact_changed(state)` — compares current vs last-persisted fingerprint
- `workspace_artifact_stamp(state)` — records fingerprint after persist
- `last_persist_fingerprint` field added to `App_State`

### 4. Import/Export Hardening

- `layout_import_from_string()` now V6-only — rejects V5/V4 input
- `load_custom_preset()` now V6-only — legacy V4/V1 presets no longer loaded
- Both validate CRC if present before applying

### 5. Caller Cleanup

- `app.odin` version detection simplified: V6-only (was V1-V5 detection chain)
- `persistence_test.odin` removed V5/V4 first-run fallback tests

## Files Changed

| File | Change |
|------|--------|
| `app/workspace_artifact.odin` | NEW — FNV-1a, CK suffix, fingerprint |
| `app/workspace_artifact_test.odin` | NEW — 22 tests |
| `app/layout_persist.odin` | Rewritten — V6-only, ~800 lines removed |
| `app/workspace_schema.odin` | V12, updated schema doc |
| `app/persistence_test.odin` | Removed V5/V4 refs, updated version checks |
| `app/app.odin` | V6-only version detection, fingerprint field |
| `services/settings_store.odin` | Removed V1-V5 setting keys |

## Test Results

- **301 tests pass** (app package — 22 new S122 tests)
- **186 tests pass** (services)
- **401 tests pass** (md_common)
- **22 tests pass** (layers)
- **All core packages compile clean** (check-core)

### New S122 Tests (22)

**FNV-1a (3):** deterministic, different inputs, empty string
**CK Suffix (4):** round-trip, no-suffix tolerance, corrupted body, tampered checksum
**Persist with CK (4):** includes CK, restore with CK, corrupted rejected, legacy accepted
**Fingerprint (3):** deterministic, changes on mutation, changed/stamp lifecycle
**Import (2):** V6 with CK, rejects non-V6
**Custom Preset (2):** saves with CK, loads with CK
**Schema (2):** version 12, version constant

## Acceptance Criteria

| Criterion | Status |
|-----------|--------|
| Workspace deterministic | PASS — persist→restore→persist produces identical V6 strings |
| Restore predictable | PASS — V6-only path, structured Persist_Result, CRC validation |
| Base for snapshots/bundles | PASS — fingerprint enables diff-aware persist, CRC enables tamper detection |
| Schema versioning hardened | PASS — V12 stamped, future version detection, legacy keys removed |
| Serialization stable | PASS — V6 format unchanged, CRC appended as backward-compatible suffix |
| Restore idempotent | PASS — fingerprint comparison gates unnecessary re-persist |

## Architecture Notes

- V6 format remains the wire format — no breaking changes
- CK suffix is purely additive (12 chars appended)
- FNV-1a matches project convention (shared/hash uses FNV-1a)
- Fingerprint stored in-memory only (not persisted) — recomputed on restore
- Legacy V1-V5 data in user settings is harmlessly ignored (keys no longer loaded)
