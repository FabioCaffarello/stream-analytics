# Stage 152 — Snapshot & Backfill Semantics Hardening

**Date:** 2026-03-09
**Status:** COMPLETE
**Objective:** Formalize snapshot, backfill, live-only and no-history semantics to reduce inconsistencies and make the terminal predictable across multiple timeframes.

---

## Problem Statement

Before S152, backfill and snapshot semantics had several inconsistencies:

1. **Fixed GetRange timeout** (300 frames / 5s) regardless of TF — a 1h TF backfill may legitimately take longer on the cold reader
2. **Fixed retry budget** (max 1) across all TF classes — high TFs pay a higher cost for failed backfill (empty chart for 30min+)
3. **`Live_Only_Utility` defined but unused** — `widget_data_readiness` mapped all `Live_Only` to `Partial_Usable` identically regardless of TF class
4. **No formal `Backfill_Outcome`** — when GetRange completed, the result wasn't classified (success/partial/empty/timeout)
5. **No `Backfill_Expectation` on `Cell_Surface_View`** — UI couldn't communicate *why* a pane was empty or what the operator should expect per TF

---

## Solution

### 1. `Backfill_Policy` — TF-Adaptive Retry & Timeout (`tf_data_contract.odin`)

New struct + lookup table centralizing retry/timeout policy per TF class:

| TF Class | Timeout (frames @ 60fps) | Timeout (seconds) | Max Retries | Live_Only Fallback |
|----------|--------------------------|-------------------|-------------|-------------------|
| Tick | 300 | 5s | 1 | Yes |
| Minute | 480 | 8s | 1 | Yes |
| Multi_Minute | 600 | 10s | 2 | No |
| Hourly | 900 | 15s | 2 | No |
| Daily | 1200 | 20s | 2 | No |

**Key insight:** High TFs get longer timeouts (server scans more cold storage) and more retries (cost of failure is an empty chart for minutes/hours). Low TFs accept Live_Only as graceful fallback.

### 2. `Backfill_Outcome` — GetRange Result Classification

New enum classifying GetRange results:
- **Success** — Got candles, store populated, chart renders history
- **Partial** — Got some candles but fewer than `min_useful_count` for this TF
- **Empty** — GetRange returned zero candles (no history on server)
- **Timeout** — GetRange timed out after retry budget exhausted
- **Not_Attempted** — No GetRange sent (live-only or not connected yet)

### 3. `Backfill_Expectation` — Per-Cell Read Model

New struct on `Cell_Surface_View` combining TF data contract policy with current backfill state:
- `criticality` — How important is backfill? (Optional/Recommended/Critical)
- `live_only_util` — How useful is live-only? (Full/Degraded/Minimal)
- `outcome` — Current backfill result classification
- `patience_ms` — How long to wait before operator concern
- `tf_class` — Behavioral TF class

### 4. TF-Adaptive GetRange Timeout (layer_marketdata.odin)

Replaced the hardcoded `GETRANGE_TIMEOUT_FRAMES :: 300` with policy-driven lookup:
- **Active stream:** Uses `backfill_policy_for_tf_ms(active_tf_ms)`
- **Per-cell:** Same as active stream policy
- **Compare panes:** Uses per-pane TF via `compare_pane_effective_tf_ms()`
- Retry budget now uses `bf_policy.max_retries` instead of hardcoded `1`

### 5. `widget_backfill_concern` + `widget_backfill_hint` (widget_readiness.odin)

New UI-facing functions:
- `widget_backfill_concern(sv)` — Returns true when operator should be alerted (Critical TF + no backfill, or Timeout at any TF)
- `widget_backfill_hint(sv)` — TF-aware hint string extending the overlay system with outcome-specific messaging

### 6. `resolve_cell_getrange_retry_count` (stream_slots.odin)

New helper reading the retry count from the correct context (global for follow-active, per-cell for bound cells), used by `derive_backfill_expectation`.

---

## Files Changed

| File | Change |
|------|--------|
| `md_common/tf_data_contract.odin` | +`Backfill_Policy`, `backfill_policy_for_tf`, `backfill_policy_for_tf_ms`, `Backfill_Outcome`, `classify_backfill_outcome`, `Backfill_Expectation`, `derive_backfill_expectation`, `backfill_outcome_label` |
| `md_common/stream_apply_state.odin` | Updated `GETRANGE_TIMEOUT_FRAMES` comment to note it's the base/default for Tick TFs |
| `md_common/tf_data_contract_test.odin` | +20 tests for backfill policy, outcome classification, expectation derivation |
| `app/stream_slots.odin` | +`backfill_expectation` field on `Cell_Surface_View`, wired in `resolve_cell_surface_view_with_stores` and `resolve_compare_surface_view`, +`resolve_cell_getrange_retry_count` |
| `app/layer_marketdata.odin` | TF-adaptive timeout + retry budget from `Backfill_Policy` (was hardcoded 300/1) |
| `app/widget_readiness.odin` | +`widget_backfill_concern`, +`widget_backfill_hint` |
| `app/widget_contract_test.odin` | +12 tests for backfill concern and hint functions |

---

## Test Summary

**New tests:** 32 (20 md_common + 12 app)

### md_common tests (20):
- Backfill policy per TF class (5): tick, minute, multi_minute, hourly, daily
- Timeout scaling (1): tick < minute < multi < hourly < daily
- Policy delegation from tf_ms (1)
- Backfill outcome classification (7): success, partial, empty, timeout, not_attempted, pending, seeded_zero
- Backfill expectation derivation (4): tick_success, 15m_not_attempted, 1h_timeout, patience_scales
- Outcome labels (1)

### app tests (12):
- Backfill concern (5): optional_no_concern, critical_not_attempted, critical_success, critical_timeout, recommended_timeout
- Backfill hint (7): success, critical_timeout, not_attempted_minimal, not_attempted_full, empty_critical, partial_critical

---

## Verification

```
make check-core          → all packages OK
make check-wasm-compile  → OK
```

---

## Per-TF Semantics Summary (Formalized)

### 1s (Tick)
- Backfill: Optional — chart fills in seconds from live data
- GetRange: 5s timeout, 1 retry
- Live_Only: Fully useful — no operator concern
- Overlay: "Live data building chart"

### 5s (Tick)
- Same as 1s

### 1m (Minute)
- Backfill: Recommended — live-only builds over a minute
- GetRange: 8s timeout, 1 retry
- Live_Only: Degraded but usable
- Overlay: "Live only — backfill improves view"

### 5m (Multi_Minute)
- Backfill: Critical — 5 minutes for first live close
- GetRange: 10s timeout, 2 retries
- Live_Only: Minimal utility — operator should fetch history
- Overlay: "Live only — consider Ctrl+R for backfill"

### 15m (Multi_Minute)
- Same as 5m

### 30m–4h (Hourly)
- Backfill: Critical — 30min+ for first live close
- GetRange: 15s timeout, 2 retries
- Live_Only: Minimal — chart essentially empty without history
- Overlay: "Backfill needed — Ctrl+R to fetch history"

### 1d (Daily)
- Backfill: Critical — hours for first live close
- GetRange: 20s timeout, 2 retries
- Live_Only: Minimal — unusable without history
- Overlay: "Backfill needed — Ctrl+R to fetch history"

---

## Design Principles

1. **Policy-first, not patches** — All backfill semantics centralized in `tf_data_contract.odin`
2. **High TFs as first-class citizens** — Longer timeouts, more retries, explicit fallback classification
3. **No conflation** — Pending, stale, absent, and degraded are distinct states with clear derivation
4. **Operator trust** — Every ambiguous state has a TF-aware hint message explaining what to expect
5. **Pure functions** — All new logic is pure, deterministic, testable with zero mutation

---

## Deferred

- **Adaptive timeout from server latency** — Currently timeouts are fixed per TF class; could adapt from observed cold reader response times
- **Per-venue backfill expectations** — Some exchanges have deeper history than others
- **Backfill progress indicator** — Show completion % during long GetRange responses
