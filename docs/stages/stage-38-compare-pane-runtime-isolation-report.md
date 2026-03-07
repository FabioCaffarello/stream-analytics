# Stage 38 — Compare Pane Runtime Isolation & Per-Pane Timeframe

**Date:** 2026-03-07
**Status:** COMPLETE
**Tests:** 271 (7 new S38), zero regressions
**Wire changes:** Zero
**New mutable state:** `Compare_State.tf_idx[4]int` (1 field)

---

## Executive Summary

S38 introduces per-pane timeframe (TF) override for compare mode, breaking the
prior constraint that all compare panes share the global TF. Each pane now has
an independent `tf_idx` (-1 = follow global, 0..8 = explicit TF override). The
surface view, subscription reconciliation, and rendering pipeline all resolve
through per-pane TF helpers, achieving pane-local health/staleness computation
and TF-aware slot resolution without duplicating protocol or apply_state logic.

Zero wire contract changes. Zero new protocol logic. Zero regression in 264
prior tests.

---

## Current-State Audit (Pre-S38)

### Compare Mode Architecture (S37)
- `Compare_State.slots[4]u64`: direct subject_id array (max 4 panes)
- All panes share `global_tf_ms(state)` → `state.active_tf_idx`
- `resolve_compare_surface_view(state, subject_id)` reads from slot's apply_state
- Reconcile subscribes all compare panes at global TF
- No per-pane TF override, no per-pane backfill, no per-pane composition

### Coupling Points Identified
1. **`global_tf_ms`** — hardcoded global TF for all compare pane health/staleness
2. **reconcile.odin:251-252** — `state.active_tf_idx` used for all pane subscriptions
3. **build_compare.odin** — no TF badge, no per-pane TF awareness
4. **resolve_compare_surface_view** — takes subject_id (no pane context for TF)

---

## S38 Architecture

### Per-Pane TF Model

```
Compare_State.tf_idx[4]int  // -1 = follow global, 0..8 = explicit override
                             // Mirrors world.timeframes[ci].tf_idx for cells
```

### Resolution Chain

```
compare_pane_effective_tf_idx(state, ci)    → int (resolves -1 to global)
compare_pane_effective_tf_ms(state, ci)     → i64 (ms duration)
compare_pane_effective_tf_string(state, ci) → string (label for subscriptions)
compare_pane_resolve_subject_id(state, ci)  → u64 (TF-aware slot lookup)
resolve_compare_surface_view(state, ci)     → Cell_Surface_View (per-pane TF)
```

### Data Flow

```
Pane TF Override
      │
      ├─ reconcile: subscribe each pane at its effective TF
      │   └─ Sub_Want{venue, symbol, channels, tf=per_pane_tf}
      │
      ├─ resolve: find slot matching market + pane TF
      │   └─ find_market_channel_slot(venue, symbol, .Candles, pane_tf)
      │
      ├─ surface: health/staleness use per-pane TF thresholds
      │   └─ stream_health_level(apply, now_ms, pane_tf_ms)
      │
      └─ render: layer_canvas receives TF-aware subject_id
          └─ render_subject_layer_canvas(state, resolved_sid, kind, rect)
```

---

## Per-Pane Isolation Plan

### What's Isolated (S38)
1. **TF selection** — each pane has independent tf_idx
2. **Subscription TF** — reconcile uses per-pane TF for subscription subjects
3. **Health computation** — staleness thresholds scale with per-pane TF
4. **Slot resolution** — rendering finds the correct TF-matched slot
5. **TF badge** — header shows per-pane TF (blue if overridden, yellow if global)

### What's Shared (by design)
1. **Apply state** — per-slot, shared across modes (canonical, no duplication)
2. **Stream slots** — shared 32-slot registry (no per-pane copies)
3. **Layer store** — centralized MarketStore (no per-pane stores)
4. **Protocol engine** — single canonical protocol (no per-pane forks)

### What's Deferred (S39)
1. **Per-pane GetRange/backfill** — requires pane-local getrange state
2. **TF cycling keybind** — pane focus concept + number key routing
3. **Per-pane invalidation** — reseed one pane without affecting others

---

## Code Changes

### 1. `components.odin` — Data Model
- Added `tf_idx: [4]int` to `Compare_State` (per-pane TF override)

### 2. `stream_views.odin` — TF Resolvers
- Added `compare_pane_effective_tf_idx(state, ci) → int`
- Added `compare_pane_effective_tf_string(state, ci) → string`
- Added `compare_pane_effective_tf_ms(state, ci) → i64`
- Mirrors `cell_effective_tf_idx/string/ms` pattern

### 3. `stream_slots.odin` — Surface View + Subject Resolution
- Added `compare_pane_resolve_subject_id(state, pane_idx) → u64`
  - Uses seed subject_id to find market identity (venue/symbol)
  - Finds best slot at pane's effective TF via `find_market_channel_slot`
  - Falls back to seed if TF-matched slot not yet available
- Changed `resolve_compare_surface_view` signature from `(state, subject_id)` to `(state, pane_idx)`
  - Uses `compare_pane_resolve_subject_id` for slot resolution
  - Uses `compare_pane_effective_tf_ms` for health/staleness (was `global_tf_ms`)
- Retained `global_tf_ms` as utility for non-pane contexts

### 4. `build_compare.odin` — Rendering
- Uses `compare_pane_resolve_subject_id(state, ci)` for rendering (was `state.compare.slots[ci]`)
- Passes `pane_idx` to `resolve_compare_surface_view` (was `subject_id`)
- Added per-pane TF badge in header (blue=override, yellow=global)

### 5. `reconcile.odin` — Subscriptions
- Compare pane subscriptions use `compare_pane_effective_tf_string(state, csi)` (was `state.active_tf_idx`)

### 6. `actions.odin` — Initialization
- `apply_enter_compare`: initializes `tf_idx[i] = -1` for all panes
- `apply_add_compare_stream`: initializes `tf_idx[si] = -1` for new pane

---

## Tests (7 new, 271 total)

| Test | What It Validates |
|------|-------------------|
| `test_s38_per_pane_tf_health_isolation` | Same Candle event → Degraded at 1m, Healthy at 1h |
| `test_s38_per_pane_tf_staleness_isolation` | Same Candle event → stale at 1m, fresh at 1h |
| `test_s38_per_pane_tf_composition_unaffected` | Composition stage is TF-independent |
| `test_s38_per_pane_tf_two_panes_different_health` | Two panes, same data, different TF → different health |
| `test_s38_per_pane_tf_default_follows_global` | tf_idx=-1 produces same result as global TF |
| `test_s38_per_pane_tf_critical_isolation` | Dual_Silence stale + exhausted → Critical (TF irrelevant) |
| `test_s38_per_pane_tf_candle_stale_not_critical` | Single candle stale + exhausted → Unhealthy (not Critical) |

---

## Risks

| Risk | Mitigation |
|------|------------|
| Per-pane TF subscription creates additional wire subjects | Bounded: max 4 panes × 1 TF each. Reconcile deduplicates same market+TF |
| Slot for new TF not yet available after TF change | Graceful fallback: `compare_pane_resolve_subject_id` returns seed subject_id |
| No per-pane GetRange yet → panes at new TF show Live_Only | Acceptable: composition badge shows "LIVE" until S39 adds per-pane backfill |
| `find_market_channel_slot` searches .Candles only | Correct for TF-sensitive resolution; non-TF channels (OB/Trades) use seed |

---

## Recommended S39: Per-Pane Interaction & Backfill

1. **Pane focus concept** — `focused_pane: int` in Compare_State
2. **TF cycling keybind** — number keys (1-9) set focused pane's TF
3. **Per-pane GetRange** — pane-local getrange state (pending/seeded/oldest_ts)
4. **Per-pane invalidation** — reseed one pane without affecting others
5. **Per-pane recovery** — isolated auto-recovery per compare pane
