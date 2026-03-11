# Stage S146 — Timeframe/Data Availability Semantics

**Date:** 2026-03-09
**Status:** COMPLETE

## Objective

Unify the semantics between timeframe, backfill, snapshot, and operational utility.
Formalize a single source of truth for per-TF expectations across all data kinds,
replacing scattered inline TF-conditional logic with a compile-time policy table.

## Problem Statement

The existing infrastructure (Composition_Stage, Artifact_Policy, Bootstrap_Expectation,
TF-adaptive health thresholds) was mature but lacked a unified contract that answers:
*"Given this timeframe and data kind, what should the operator expect?"*

Key gaps:
1. **No TF class abstraction** — all code used raw ms, no behavioral classification
2. **No per-TF backfill criticality** — all widgets treated live-only the same on 1s vs 15m
3. **Overlay hints didn't differentiate** — "Seeding" showed the same message on 1s and 15m
4. **No formal contract** for when partial/live-only is acceptable per TF per data kind

## Design

### TF Class Taxonomy

| Class | Range | Examples | Behavior |
|-------|-------|----------|----------|
| Tick | ≤10s | 1s, 5s | Live data arrives fast, live-only fully useful |
| Minute | ≤60s | 1m | Standard operational TF, backfill recommended |
| Multi_Minute | ≤15m | 5m, 15m | First live candle takes minutes, backfill critical |
| Hourly | ≤4h | 30m, 1h, 4h | First live candle takes 30min+, backfill essential |
| Daily | >4h | 1d | First live candle takes hours, backfill essential |

### Data Kind Categories

| Category | Data Kinds | TF-Sensitive | Backfill Source | Policy Function |
|----------|-----------|--------------|-----------------|-----------------|
| Historical Range | Candle | YES | GetRange (NATS) | `candle_tf_expectation()` |
| TF-Gated Analytics | CVD, Delta Vol, Bar Stats | YES | HTTP cold reader | `analytics_tf_gated_expectation()` |
| TF-Independent | Stats, Trades, Orderbook, OI | NO | Live only | `tf_independent_expectation()` |
| Accumulation | Heatmap, VPVR, Session_VPVR, TPO | YES | Live accumulation | `accumulation_tf_expectation()` |

### Backfill Criticality Matrix

| Data Kind / TF | Tick | Minute | Multi-Min | Hourly | Daily |
|----------------|------|--------|-----------|--------|-------|
| **Candle** | Optional | Recommended | Critical | Critical | Critical |
| **CVD/DeltaVol** | Optional | Recommended | Critical | Critical | Critical |
| **Stats/Trades/OB** | Optional | Optional | Optional | Optional | Optional |
| **Heatmap/VPVR** | Optional | Optional | Optional | Optional | Optional |

### Live-Only Utility Matrix

| Data Kind / TF | Tick | Minute | Multi-Min | Hourly | Daily |
|----------------|------|--------|-----------|--------|-------|
| **Candle** | Full | Degraded | Minimal | Minimal | Minimal |
| **CVD/DeltaVol** | Full | Degraded | Minimal | Minimal | Minimal |
| **Stats/Trades/OB** | Full | Full | Full | Full | Full |
| **Heatmap/VPVR** | Full | Degraded | Minimal | Minimal | Minimal |

### Overlay Patience (ms) — How Long Loading/Seeding Is Normal

| Data Kind / TF | Tick | Minute | Multi-Min | Hourly | Daily |
|----------------|------|--------|-----------|--------|-------|
| **Candle** | 10s | 2m | 10m | 1h | 48h |
| **Analytics** | 10s | 2m | 10m | 1h | 48h |
| **TF-Independent** | 10s | 10s | 10s | 10s | 10s |
| **Accumulation** | 15s | 1.5m | 5m | 15m | 30m |

## Implementation

### New Files

1. **`md_common/tf_data_contract.odin`** — Canonical TF data contract
   - `TF_Class` enum with 5 behavioral classes
   - `tf_class_from_ms()` classifier
   - `Backfill_Criticality` + `Live_Only_Utility` enums
   - `TF_Data_Expectation` struct (criticality, utility, patience, min count)
   - Per-category expectation tables: `candle_tf_expectation()`, `analytics_tf_gated_expectation()`, `tf_independent_expectation()`, `accumulation_tf_expectation()`
   - Unified query: `tf_data_expectation(artifact, tf_ms)`
   - TF-aware overlay hints: `tf_overlay_hint(artifact, tf_ms, is_live_only)`

2. **`md_common/tf_data_contract_test.odin`** — 42 tests covering:
   - TF class classification (10 boundary tests)
   - Backfill criticality scaling (5 tests)
   - Overlay patience monotonicity (1 test)
   - Min useful count ordering (1 test)
   - TF-independent invariants (4 tests)
   - Analytics TF-gated scaling (3 tests)
   - Accumulation expectations (2 tests)
   - Unified query dispatch (3 tests)
   - Overlay hint correctness (8 tests)
   - TF class labels (1 test)
   - Edge cases (4 tests)

### Modified Files

3. **`app/widget_readiness.odin`** — Added:
   - `widget_tf_expectation(wk, tf_ms)` — delegates to TF data contract
   - `widget_backfill_critical(wk, tf_ms)` — convenience boolean query

4. **`app/shell_common.odin`** — Updated:
   - `tf_overlay_hint_for_widget()` — TF-aware overlay hint wrapper
   - Loading overlay: uses `tf_overlay_hint` instead of `bootstrap_hint`
   - Seeding overlay: uses `tf_overlay_hint` instead of `bootstrap_hint`
   - Snapshot_Pending overlay: uses `tf_overlay_hint`
   - No_History overlay: TF-aware urgency (was hardcoded "Ctrl+R to retry backfill")

5. **`app/widget_contract_test.odin`** — Added 11 S146 integration tests:
   - Widget TF expectation queries
   - Backfill criticality per widget kind × TF
   - Exhaustive coverage (all widget kinds return valid expectations)

## Key Behavioral Changes

### Overlay Messages Now TF-Aware

**Before (all TFs):**
- Loading: "Waiting for candle close" (generic)
- Live Only: "Ctrl+R to retry backfill" (same urgency on all TFs)

**After (TF-differentiated):**
- Loading @ 1s: "Fetching historical data"
- Loading @ 15m CVD: "Normal — first close takes minutes"
- Live Only @ 1s candle: "Live data building chart" (low urgency)
- Live Only @ 15m candle: "Live only — consider Ctrl+R for backfill" (medium urgency)
- Live Only @ 1h candle: "Backfill needed — Ctrl+R to fetch history" (high urgency)

### Backfill Criticality Formalized

The contract now explicitly classifies backfill as Optional/Recommended/Critical per
TF class. This provides a query API that future stages (orderflow, advanced indicators)
can use to decide whether to auto-trigger backfill or degrade gracefully.

## Test Results

- **md_common**: 482 tests, all pass (42 new S146 tests)
- **app**: 428 tests, all pass (11 new S146 tests)
- **Total new tests**: 53
- Zero regressions, zero wire-breaking changes

## Acceptance Criteria

| Criterion | Status |
|-----------|--------|
| Coherent policy per TF (1s, 5s, 1m, 5m, 15m) | DONE — TF_Class taxonomy with 5 classes |
| Less surprise for operator | DONE — overlay hints scale urgency with TF |
| Harmonized behavior across data kinds | DONE — 4 category tables, unified query |
| Formalized partial/live_only acceptability | DONE — Backfill_Criticality + Live_Only_Utility enums |
| Backfill absence ↔ utility relationship | DONE — per-TF criticality matrix |
| Aligned with overlays and readiness | DONE — overlays use tf_overlay_hint, readiness exposes widget_tf_expectation |
| Stable base for orderflow | DONE — query API ready for future stages |
