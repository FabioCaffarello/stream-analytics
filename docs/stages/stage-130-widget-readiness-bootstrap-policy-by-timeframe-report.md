# Stage S130 — Widget Readiness & Bootstrap Policy by Timeframe

## Objective
Formalize per-widget bootstrap expectations by timeframe so the terminal delivers useful feedback sooner and consistently across 1s, 5s, 1m, 5m, and 15m timeframes.

## Problem Statement
The readiness model (Pane_Visual_State) already had the correct states (Loading, Seeding, Snapshot_Pending, Active), but the overlay **hints were static** — a Stats widget on 1s showed the same "Fetching historical data..." message as one on 15m, even though their bootstrap behaviors are fundamentally different:
- **1s**: trades flood immediately, stats synthesize in <2s
- **5m**: first candle close takes 5 minutes, but OB/trades/stats arrive immediately
- **15m**: candle composition takes 15 minutes, but snapshot-gated widgets resolve in seconds

## Solution: Bootstrap Policy Table + TF-Aware Hints

### New Types (artifact_policy.odin)

```
Bootstrap_Source :: enum u8 {
    Live_Immediate,    // Trades, OB, Stats — arrive on subscribe, no TF dependency
    Live_TF_Gated,     // Delta Vol, CVD, Bar Stats — first close = TF duration
    Historical_Range,  // Candles — GetRange backfill
    Snapshot_Gate,     // Needs explicit snapshot (OB depth)
    Accumulation,      // Heatmap, VPVR, SVP, TPO — needs time to build
}

Bootstrap_Expectation :: struct {
    source:         Bootstrap_Source,
    min_seed_ms:    i64,
    partial_usable: bool,
}

Bootstrap_Hint :: struct {
    expected_ms: i64,
    partial_ok:  bool,
    hint_label:  string,
}
```

### Bootstrap Expectations Table

| Artifact | Source | Min Seed (ms) | Partial Usable |
|----------|--------|--------------|----------------|
| Trade | Live_Immediate | 500 | yes |
| Orderbook | Snapshot_Gate | 2,000 | no |
| Stats | Live_Immediate | 1,000 | yes |
| Candle | Historical_Range | 1,000 | no |
| Heatmap | Accumulation | 5,000 | no |
| VPVR | Accumulation | 5,000 | no |
| Evidence | Live_Immediate | 500 | yes |
| Signal | Live_Immediate | 500 | yes |
| Tape | Live_Immediate | 500 | yes |
| Range_Candle | Historical_Range | 1,000 | no |
| Open_Interest | Live_Immediate | 2,000 | yes |
| Delta_Volume | Live_TF_Gated | 1,000 | no |
| CVD | Live_TF_Gated | 1,000 | no |
| Bar_Stats | Live_TF_Gated | 1,000 | no |
| Session_VP | Accumulation | 5,000 | no |
| TPO_Profile | Accumulation | 10,000 | no |

### TF-Aware Hint Labels (by source)

| Source | TF Range | Hint Label |
|--------|----------|------------|
| Live_Immediate | any | "Data arrives within seconds" |
| Live_TF_Gated | ≤5s | "First close in seconds" |
| Live_TF_Gated | ≤1m | "Waiting for candle close" |
| Live_TF_Gated | ≤15m | "First close takes minutes" |
| Live_TF_Gated | >15m | "Long timeframe — first close may take a while" |
| Historical_Range | any | "Fetching historical data" |
| Snapshot_Gate | any | "Awaiting exchange snapshot" |
| Accumulation | ≤5s | "Accumulating data" |
| Accumulation | >5s | "Building over time" |

### Widget → Primary Artifact Mapping

| Widget Kind | Primary Artifact | Rationale |
|------------|-----------------|-----------|
| Candle | Candle | Historical range backfill |
| Stats | Stats | Live immediate from exchange |
| Counter | Candle | Derived from candle close count |
| Trades | Trade | Live immediate trade feed |
| Orderbook | Orderbook | Snapshot-gated from exchange |
| DOM | Orderbook | Same as Orderbook |
| Heatmap | Heatmap | Accumulation from OB snapshots |
| VPVR | VPVR | Accumulation from OB snapshots |
| Analytics | CVD | Representative TF-gated artifact |
| Session_VPVR | Session_VP | Session accumulation |
| TPO | TPO_Profile | Session accumulation |

## Files Modified

| File | Change |
|------|--------|
| `md_common/artifact_policy.odin` | +Bootstrap_Source, +Bootstrap_Expectation, +Bootstrap_Hint, +bootstrap_expectations table, +bootstrap_hint_for_artifact(), +artifact_bootstrap_expectation() |
| `app/shell_common.odin` | +_widget_primary_artifact(), +bootstrap_hint_for_widget(), tf_ms parameter on draw_pane_state_overlay, TF-aware hint text in Loading/Seeding/Snapshot_Pending overlays |
| `app/build_cell.odin` | Pass vm.tf_ms / ctx.tf_ms to draw_pane_state_overlay at 2 call sites |
| `app/marketdata_test.odin` | +13 S130 tests for widget-level bootstrap hints |
| `md_common/md_common_test.odin` | +7 S130 tests for artifact-level bootstrap policy |

## Design Principles

1. **All new functions are pure** — no mutation, no allocations, no side effects
2. **All hint_label strings are compile-time literals** — zero fmt.tprintf usage, no `{`/`}` gotcha
3. **@(rodata) for variable-indexed table** — not `::` constant (Odin restriction)
4. **Backward compatible** — tf_ms defaults to 60_000 (1m), no call site breaks
5. **Pane_Visual_State enum unchanged** — states are correct, only informational text changes
6. **WORKSPACE_SCHEMA_VERSION unchanged** — no persistence format change (stays at 12)

## Test Summary

- **20 new tests** (13 in app/marketdata_test.odin, 7 in md_common/md_common_test.odin)
- Coverage: all widget kinds, all TF tiers, all bootstrap sources, label correctness, partial usability flags, table completeness

## Acceptance Criteria

- [x] Widgets show TF-aware bootstrap hints in Loading/Seeding/Snapshot_Pending overlays
- [x] Hint labels differentiate between 1s, 5s, 1m, 5m, 15m+ timeframes for TF-gated artifacts
- [x] Non-TF-gated artifacts (Trades, Stats, OB) show consistent hints regardless of TF
- [x] Bootstrap policy table documents expected bootstrap time per artifact
- [x] All functions pure, all strings literal, zero allocations
- [x] 20 tests covering all widget × TF combinations
