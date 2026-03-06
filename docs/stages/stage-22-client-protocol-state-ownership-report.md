# Stage 22 — Client Protocol State Ownership

**Date:** 2026-03-06
**Status:** COMPLETE
**Scope:** ~550 LOC new (3 files + 1 test file), zero breaking changes

## Objective

Close the architecture initiated in S21 by creating canonical protocol/stream session contracts in the client. Transform scattered implicit state into explicit, testable state machines with per-artifact policy tables.

## Problem Statement

After S21, protocol-level concerns remained scattered across 5+ files:

| Concern | Where (Before S22) |
|---|---|
| Snapshot gate | Inline in `marketdata.odin:316-329`, orderbook-only |
| Seq gap detection | Platform adapters + `md_common.seq_gap_transition` |
| Stale detection | `health.odin`, 3 mechanisms inline |
| Per-artifact behavior | Ad-hoc in `drain_marketdata` handler code |
| Apply state tracking | `active_metrics.*` booleans + `Stream_View_Slot` fields |
| Reconnect clearing | Inline in `drain_marketdata` |
| GetRange state | Inline in `App_State.getrange` |

No single source of truth for "what does this artifact need?" or "what state is this stream in?"

## Deliverables

### 1. `artifact_policy.odin` (~195 LOC)

Canonical policy table defining per-artifact behavior contracts:

```
Artifact_Kind: Trade, Orderbook, Stats, Candle, Heatmap, VPVR, Evidence, Signal, Tape, Range_Candle
```

Each artifact has an `Artifact_Policy` struct with:

| Field | Meaning |
|---|---|
| `needs_snapshot_gate` | Must see snapshot before accepting deltas |
| `accepts_range_seed` | Can be seeded via GetRange |
| `accepts_delta_without_snapshot` | Process deltas without prior snapshot |
| `snapshot_semantics` | None / Latest_Wins / Ring_Append / Window_Dedup |
| `reset_on_reconnect` | Clear snapshot gate on reconnect |
| `reset_on_tf_change` | Clear store on timeframe change |
| `is_tf_sensitive` | Subject includes timeframe component |
| `has_synthetic_fallback` | Can be synthesized from other data |
| `backpressure_priority` | Critical / Degradable / Low |
| `stale_detection` | None / TF_Adaptive / Dual_Silence |

**Key functions:**
- `artifact_policy(kind)` — lookup by artifact kind
- `artifact_policy_for_event(event_kind)` — lookup by event kind
- `should_skip_by_bp_policy(...)` — pure backpressure decision
- `snapshot_gate_check(policy, ...)` — generic snapshot gate (replaces inline orderbook gate)

### 2. `protocol_engine.odin` (~220 LOC)

Canonical per-stream protocol state machine with 8 states:

```
Idle → Bootstrap_Pending → Seeded → Live
                                  ↓
                              Degraded → Resyncing → Seeded
                                  ↓
                                Stale → Live (on event)

Live → Reconnecting → Bootstrap_Pending (on re-subscribe)
```

**State transitions** (all pure functions, no side effects):

| Function | Trigger | Effect |
|---|---|---|
| `protocol_on_subscribe` | Subscribe sent | → Bootstrap_Pending, reset all |
| `protocol_on_snapshot` | Snapshot received | → Seeded (from pending), → Live (from degraded) |
| `protocol_on_event` | Any event | Seq gap detection, state advancement |
| `protocol_on_range_complete` | GetRange is_last=true | Mark seeded, → Seeded if pending |
| `protocol_on_reconnect` | Transport reconnects | → Reconnecting, clear snapshot/seq |
| `protocol_on_resync_sent` | Resync message sent | → Resyncing |
| `protocol_on_resync_ack` | Server ACKs resync | → Seeded, reset |
| `protocol_on_snapshot_gap` | Snapshot gate gap | → Degraded |
| `protocol_on_tf_change` | Timeframe changes | → Bootstrap_Pending, clear getrange |
| `protocol_on_stale_timeout` | No events within threshold | → Stale |

**Query functions:**
- `protocol_check_stale(p, now_ms, threshold_ms)` — pure stale check
- `protocol_is_accepting_events(state)` — can this stream receive data?
- `protocol_needs_resync(p)` — does this stream need resync?

### 3. `stream_apply_state.odin` (~140 LOC)

Per-stream apply state tracking with per-artifact arrays:

```odin
Stream_Apply_State :: struct {
    snapshot_seen:      [Artifact_Kind]bool,    // per-artifact snapshot gate
    has_live:           [Artifact_Kind]bool,    // live data received?
    using_synthetic:    [Artifact_Kind]bool,    // synthetic fallback active?
    last_recv_ms:       [Artifact_Kind]i64,    // last event per artifact
    getrange_seeded:    bool,
    getrange_pending:   bool,
    getrange_oldest_ts: i64,
    ...
}
```

**Key functions:**
- `apply_state_reset(s)` — full reset
- `apply_state_on_reconnect(s)` — policy-driven reconnect clearing
- `apply_state_on_tf_change(s)` — policy-driven TF change clearing
- `apply_state_mark_event(s, kind, now_ms, is_snapshot)` — track event
- `apply_state_mark_synthetic(s, kind, now_ms)` — track synthetic (displaced by live)
- `apply_state_should_use_synthetic(s, kind)` — query synthetic fallback
- `apply_state_needs_snapshot(s, kind)` — query snapshot gate
- `apply_state_summary(s)` — compatibility bridge to existing `active_metrics` booleans

### 4. `protocol_engine_test.odin` (~52 tests, ~480 LOC)

Comprehensive test coverage across all three files:

| Category | Tests | Coverage |
|---|---|---|
| Artifact policy table | 9 | Completeness, per-artifact contracts |
| Backpressure policy | 3 | Critical/Degradable/Low priorities |
| Snapshot gate | 6 | Gate/no-gate, all orderbook paths |
| Protocol state machine | 15 | All transitions, seq gaps, stale, resync |
| Apply state | 12 | Reset, reconnect, TF change, synthetic, getrange |
| Full lifecycle integration | 3 | End-to-end, resync recovery, coordinated |

## Validation

- `make check-core`: **10/10 packages pass** (all core packages compile clean)
- `odin test md_common`: **127/127 tests pass** (75 existing + 52 new)
- Zero changes to existing files — pure additive
- No new imports or dependencies

## Architecture Decisions

1. **Pure functions, no side effects.** All protocol transitions return a `Protocol_Transition` struct. Caller decides what to do (send resync, clear store, etc.). This enables testing without mocking.

2. **Per-artifact arrays, not per-artifact structs.** Using `[Artifact_Kind]bool` arrays in `Stream_Apply_State` enables policy-driven iteration (`for kind in Artifact_Kind`) rather than ad-hoc field-by-field code.

3. **`@(rodata)` global instead of `::` constant.** Odin constants cannot be indexed with variable indices. The policy table uses `@(rodata)` annotation for read-only data segment placement.

4. **Additive, not invasive.** S22 creates the canonical abstractions. Existing `drain_marketdata`, `health.odin`, and platform adapters continue working unchanged. Migration to use these new contracts can happen incrementally.

5. **Snapshot gate generalized.** The inline `orderbook_snapshot_gate` function in `marketdata.odin` is policy-specific (hardcoded to orderbook). The new `snapshot_gate_check` is policy-driven — works for any artifact with `needs_snapshot_gate=true`.

## What S22 Enables

With these contracts in place, future stages can:
- Replace scattered `active_metrics.has_live_*` booleans with `Stream_Apply_State`
- Replace inline snapshot gate with `snapshot_gate_check(artifact_policy(.Orderbook), ...)`
- Replace ad-hoc reconnect clearing with `apply_state_on_reconnect`
- Replace ad-hoc TF change clearing with `apply_state_on_tf_change`
- Use `Stream_Protocol` per market for canonical state tracking
- Use `protocol_check_stale` for unified stale detection
- Use `should_skip_by_bp_policy` instead of inline backpressure switch

## File Inventory

| File | LOC | Purpose |
|---|---|---|
| `md_common/artifact_policy.odin` | ~195 | Policy table + lookup + BP + snapshot gate |
| `md_common/protocol_engine.odin` | ~220 | State machine + transitions |
| `md_common/stream_apply_state.odin` | ~140 | Apply state tracking |
| `md_common/protocol_engine_test.odin` | ~480 | 52 tests |
| **Total** | **~1,035** | |

## Risk Assessment

- **Low:** Pure additive — no existing behavior changes
- **Low:** All functions are pure (no I/O, no side effects, no allocations)
- **Low:** Comprehensive test coverage with integration tests
- **None:** Zero platform-specific code (shared in md_common)
