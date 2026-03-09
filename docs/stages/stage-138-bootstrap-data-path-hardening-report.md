# Stage 138 — Bootstrap-to-Useful Data Path Hardening

**Date:** 2026-03-09
**Status:** COMPLETE

## Objective

Reduce time until widgets become operationally useful by hardening the full data path:
historical → snapshot → live accumulation → composed state.

## Changes

### 1. RANGE_CANDLE_PARSE_MAX: 32 → 256

**File:** `client/src/core/services/message_parser.odin`

The parser array in `Parsed_Range_Candles` now holds 256 entries instead of 32.
`adaptive_getrange_limit()` computes `min(750, 256) = 256`, so the initial GetRange
response fills the chart with up to 256 candles on first response. Stack cost ~24KB,
acceptable. Previously charts showed only 32 candles on first load, requiring lazy
scroll to see more.

**Impact:** Chart fills 8x faster on initial bootstrap.

### 2. GetRange Auto-Retry on Timeout

**Files:** `components.odin`, `layer_marketdata.odin`, `stream_views.odin`, `actions.odin`, `actions_cell_mutations.odin`

Added `retry_count: u8` to `GetRange_Component`, `Compare_Pane_GetRange`, and
`GetRange_Global_State`. On GetRange timeout (5s / 300 frames):

- **First timeout:** auto-retry (retry_count 0→1), re-dispatch GetRange request
- **Second timeout:** log error, give up (matches existing behavior)

Retry count is reset to 0 on: reconnect, TF change, stream switch, cell rebind,
layout preset change.

**Impact:** Transient network issues or server hiccups don't leave panes stuck in PEND.

### 3. Candle Chart Subplot Analytics Cold Reader Bootstrap

**File:** `client/src/core/app/analytics_range.odin`

New proc `request_active_subplot_analytics` iterates all candle cells and fetches
cold reader data (CVD, DeltaVol, OI) for cells with active subplots. Uses the same
cold reader APIs as the standalone Analytics widget (`/api/v1/cvd`, `/api/v1/delta_volume`,
`/api/v1/oi`).

**Call sites:**
- After reconnect (layer_marketdata.odin)
- After TF change (stream_views.odin `apply_set_timeframe_action`)
- After stream switch (stream_views.odin `apply_cycle_stream_action`)

**Impact:** Subplots on candle charts render immediately with historical data instead of
waiting for the first live TF-gated candle close (which on 1h TF could be up to 1 hour).

### 4. Bootstrap Timing Probe

**File:** `client/src/core/md_common/stream_apply_state.odin`

Added `first_event_ms: i64` to `Stream_Apply_State` — latched on the very first event
received on a stream. Survives reconnect but is cleared on full reset. Exposed via
`Apply_State_Telemetry` for HUD display.

**Impact:** Enables time-to-first-data measurement for debugging bootstrap latency.

### 5. Bound Cell Seeding on Reconnect

**File:** `client/src/core/app/layer_marketdata.odin`

On reconnect, now seeds ALL bound candle cells with GetRange requests, not just the
active stream. Also added lazy seeding in the per-frame slot repair loop for bound cells
that haven't been seeded yet.

**Impact:** Multi-cell layouts (bound to different markets) all bootstrap in parallel on
connect, instead of waiting for user to interact with each cell.

## Tests

5 new tests in `protocol_engine_test.odin`:
- `test_first_event_ms_latches_on_first_event` — first event sets, second doesn't overwrite
- `test_first_event_ms_survives_reconnect` — reconnect preserves latched value
- `test_first_event_ms_reset_clears` — full reset zeros
- `test_first_event_ms_in_telemetry` — telemetry snapshot includes value
- `test_first_event_ms_ignores_zero_timestamp` — zero timestamp doesn't latch

## File Summary

| File | Change |
|------|--------|
| `services/message_parser.odin` | RANGE_CANDLE_PARSE_MAX 32→256 |
| `app/components.odin` | retry_count on 3 GetRange structs |
| `app/layer_marketdata.odin` | Auto-retry, bound cell seeding, subplot bootstrap |
| `app/stream_views.odin` | retry_count resets, subplot bootstrap calls |
| `app/analytics_range.odin` | request_active_subplot_analytics + helper |
| `app/actions.odin` | retry_count reset on layout preset |
| `app/actions_cell_mutations.odin` | retry_count reset on cell rebind |
| `md_common/stream_apply_state.odin` | first_event_ms field + telemetry |
| `md_common/protocol_engine_test.odin` | 5 bootstrap timing tests |

## Acceptance Criteria

- [x] Widgets become useful earlier (256 candles on first load vs 32)
- [x] Bootstrap more predictable (auto-retry prevents stuck PEND)
- [x] Fewer panes stuck in transition (subplot analytics pre-populated, bound cells seeded)
- [x] Debugging support (first_event_ms telemetry probe)
- [x] Zero regressions — additive struct fields, backward-compatible
