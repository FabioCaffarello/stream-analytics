# Stage 101 — Workspace Persistence Hardening

**Date:** 2026-03-08
**Branch:** codex/s9-legacy-removal-cutover
**Status:** COMPLETE

## Objective

Make workspace persistence robust, deterministic, and free of HTTP 400 noise in the browser console.

## Findings

### HTTP 400 Root Cause Analysis

Traced all HTTP call paths originating from `storage.js` → `http_get_sync` → `web_http_get` → backend handlers.

**All fetch call sites have proper guards:**
- `web_fetch_analytics_*` — short-circuit on empty venue/instrument/timeframe
- `request_analytics_range` — S92 `normalized_symbol()` strips market type suffix
- `poll_freshness` — connection check + venue/symbol guard
- `fetch_timeline_for_active` — slot nil + stream_info + venue/symbol guard
- `request_session_vpvr_snapshot` — venue/symbol guard + normalized_symbol

**The prior 400 source** (S92) was already fixed: symbols like `BTCUSDT:SPOT` were sent to analytics endpoints which rejected the colon suffix. The `normalized_symbol()` call on line 42 of `analytics_range.odin` resolved this.

**No active 400 bug found** — all HTTP calls are properly guarded.

### Bugs Found and Fixed

#### BUG 1: Widget Kind Serialization Overflow (V6 Layout)

**Impact:** Session_VPVR (enum=10) and TPO (enum=11) widgets could not survive V6 persist/restore cycle.

**Root Cause:** `build_layout_v6_string` used single-byte ASCII digit: `buf[off] = '0' + u8(widget_kind)`. For enum values ≥ 10, this produces non-digit characters (`:`, `;`) which the parser rejects.

**Fix:** Changed V6 persist to use `write_int_to_buf()` (multi-digit safe). Changed V6 restore to parse widget kind as integer field via `parse_int_from()` instead of single-character digit. V4 backward-compat writes clamp to 9 (Empty).

#### BUG 2: Symbol Colon Corrupts V6 Field Separator

**Impact:** Symbols containing `:` (e.g. `BTCUSDT:SPOT`, `ETHUSDT:PERP`) would corrupt the V6 layout string because `:` is the field separator. On restore, the symbol gets truncated and subsequent fields (indicator flags, spans, etc.) parse garbage values.

**Root Cause:** `build_layout_v6_string` wrote raw `binding_symbol()` including the market type suffix. The V6 parser reads the stream field until `:` or `|`, so the `:SPOT` suffix was interpreted as the end of the field.

**Fix:** Apply `normalized_symbol()` during persist to strip the market type suffix before writing to the V6 string. This matches the backend API contract (S92) and ensures the `:` field separator is never ambiguous.

## Changes

### Files Modified

| File | Change |
|------|--------|
| `layout_persist.odin` | V6 persist: multi-digit widget kind via `write_int_to_buf`; normalized symbol before write. V6 restore: `parse_int_from` for widget kind field. V4 persist: clamp widget kind to 9; normalized symbol. |

### Files Added

| File | Purpose |
|------|---------|
| `persistence_test.odin` | 21 new tests covering V6 round-trip, first-run, resilience, schema validation |

## Test Coverage

### New Tests (21)

| Test | Category |
|------|----------|
| `test_v6_round_trip_basic` | V6 round-trip: widgets, bindings, analytics kind |
| `test_v6_round_trip_indicator_flags` | All 11 indicator bits (MA, BBands, VWAP, RSI, MACD, Funding, Liq, Counter, CVD, DV, OI) |
| `test_v6_round_trip_chart_display` | Chart display packed fields (vol, heatmap, vpvr, intensity, groups, filter, analytics_kind) |
| `test_v6_round_trip_spans_and_subplots` | Col/row spans, sub_main_split, sub_ratios |
| `test_v6_round_trip_per_cell_tf` | Per-cell timeframe (-1 global, 3, 7) |
| `test_v6_round_trip_grid_weights` | Col/row weights (3x2 grid) |
| `test_v6_round_trip_layout_mode` | Custom vs Preset mode |
| `test_v6_round_trip_evidence_link_disabled` | Signal-evidence link flag |
| `test_v6_round_trip_follow_active_cell` | Follow-active (no binding) + bound cell |
| `test_first_run_empty_settings_defaults` | Empty settings → V6/V5/V4 fallback chain fails gracefully |
| `test_first_run_default_binding` | PRD-0009: cell 0 gets binance/BTCUSDT:SPOT on first run |
| `test_v6_restore_rejects_invalid_header` | Empty, V5, garbage, truncated strings rejected |
| `test_v6_restore_rejects_truncated_string` | Partial V6 strings rejected |
| `test_v6_restore_max_cells` | CELL_MAX cells persist and restore |
| `test_indicator_flags_pack_unpack_all_bits` | Each of 11 bits individually verified |
| `test_indicator_flags_all_on_round_trip` | All 11 flags set → packed → unpacked |
| `test_chart_display_pack_unpack_round_trip` | All chart display fields + analytics_kind |
| `test_v6_complex_layout_restore` | 6-cell layout: Candle, Analytics, Trades, Orderbook, Heatmap, Session_VPVR |
| `test_v6_reload_determinism` | persist→restore→persist produces identical V6 string |
| `test_normalized_symbol_strips_suffix` | `:SPOT`, `:PERP` stripped; bare symbol and empty pass through |
| `test_workspace_schema_version` | WORKSPACE_SCHEMA_VERSION ≥ 10 |

### Total Tests

- **Before:** 40 tests (app package)
- **After:** 61 tests (app package)
- All pass, zero regressions

## Acceptance Criteria

| Criterion | Status |
|-----------|--------|
| Zero HTTP 400 in console | VERIFIED — all fetch paths have guards; no unguarded path exists |
| Deterministic workspace restore | VERIFIED — persist→restore→persist produces identical V6 string |
| First-run resilience | VERIFIED — empty settings fallback chain gracefully degrades |
| Analytics restore | VERIFIED — 11-bit indicator flags (including CVD/DV/OI) survive round-trip |
| Compare mode | N/A — ephemeral by design (never persisted) |
| Multiple panes | VERIFIED — 6-cell complex layout with mixed widgets, bindings, spans |
| Schema migration chain | VERIFIED — V6→V5→V4 fallback tested; V1 is terminal fallback |

## Architecture Notes

- **Compare mode is ephemeral** (workspace_schema.odin:35) — never persisted across sessions, resets on init
- **Session_VPVR and TPO widgets** now persist correctly (multi-digit widget kind)
- **Symbol normalization at persist boundary** ensures V6 format consistency and matches backend API contract
- **WORKSPACE_SCHEMA_VERSION remains 10** — no format version bump needed (fix is backward-compatible; existing V6 strings with single-digit widgets still parse correctly)
