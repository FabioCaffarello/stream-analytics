# Stage 21 -- Client Transport & Protocol Unification

**Date:** 2026-03-06
**Branch:** codex/s9-legacy-removal-cutover
**Status:** COMPLETE
**Depends on:** S18 (protocol hardening), S19 (client-readiness surfaces), S20 (first vertical slice)

---

## Executive Summary

Stage 21 consolidates the client transport and protocol layer into a unified, deterministic consumption model. After S20 proved the first vertical slice (HTTP bootstrap + WS + freshness + timeline), we now audit the transport sprawl across platform implementations, identify duplicated protocol logic, and unify the bootstrap→handshake→snapshot→delta→reconnect→resync lifecycle into a clean layered architecture.

**Core finding:** The client transport layer works correctly but has accumulated organic complexity across 6 concerns that are interleaved rather than separated:

1. **HTTP bootstrap** (session/markets/freshness/timeline) — added in S20, clean
2. **WS lifecycle** (connect/hello/subscribe/reconnect) — works but duplicated across web+native (~2200 LOC each)
3. **Protocol integrity** (seq gaps, snapshot gates, prev_seq, desync) — split across platform layer AND app layer
4. **GetRange** (historical seed, lazy scroll, timeline bounds) — clean after S20
5. **Health/freshness** (candle health, stream health, backend freshness) — 3 separate systems
6. **Reconnect/resync** (backoff, re-subscribe, state clearing) — works but fragile

**Decision:** Protocol unification means consolidating WHERE each concern lives, not rewriting the transport. The implementation is a series of targeted extractions, not a rewrite.

---

## 1. Current-State Audit

### 1.1 File Inventory (Transport-Related)

| File | LOC (est) | Concern | Layer |
|------|-----------|---------|-------|
| `ports/marketdata.odin` | 351 | Port interface, event types, enums | Port |
| `md_common/transport_terminal_v1.odin` | ~38 | Fault action mapping | Shared |
| `md_common/md_common.odin` | ~600 | Rate tracking, backpressure assist, msg builders | Shared |
| `platform/web/marketdata_web.odin` | ~2290 | WS + HTTP, parse, stage, reconnect, subscribe | Platform |
| `platform/native/marketdata_native.odin` | ~2430 | WS + HTTP, parse, stage, reconnect, subscribe | Platform |
| `streams/stream_controller.odin` | 198 | Seq gap, ts regression, health update | Core |
| `streams/stream_registry.odin` | ~300 | Per-market stream handles, refcount, prune | Core |
| `streams/endpoints.odin` | ~200 | Venue→endpoint mapping, channel support | Core |
| `app/health.odin` | 566 | Candle health, metrics sampling, freshness poll, timeline fetch | App |
| `app/stream_views.odin` | 615 | GetRange, TF switching, binding helpers | App |
| `app/reconcile.odin` | 432 | Subscription reconciliation, diff-based sub/unsub | App |
| `app/marketdata.odin` | 911 | Event drain, per-type handlers, store apply | App |
| `app/top_bar.odin` | 514 | Readiness/freshness/health badge rendering | App (UI) |
| `services/market_discovery.odin` | ~400 | JSON parsers for session/freshness/timeline/markets | Service |

**Total transport-related:** ~9,855 LOC across 14 files, 5 layers.

### 1.2 Protocol Logic Distribution (Current)

| Protocol Concern | Where It Lives Today | Should Live |
|-----------------|---------------------|-------------|
| WS connect + hello handshake | Platform (web+native, duplicated) | Platform (correct) |
| Subscribe/unsubscribe wire format | Platform (md_common builders) | Platform (correct) |
| Reconnect backoff + timer | Platform (web+native, duplicated) | Platform (correct) |
| Re-subscribe on reconnect | App (drain_marketdata detects transition) | App (correct) |
| Seq gap detection (transport-level) | Platform (per-sub seq tracking) | Platform (correct) |
| Seq gap detection (stream-level) | Core (stream_controller.odin) | Core (correct) |
| Snapshot-before-delta gate | App (orderbook_snapshot_gate) | **Should be Core** |
| Desync forwarding + filtering | App (health.odin:refresh_active_stream_health) | App (acceptable) |
| Freshness polling | App (health.odin:poll_freshness) | App (correct) |
| Timeline fetch | App (health.odin:fetch_timeline_for_active) | App (correct) |
| GetRange request + timeout | App (stream_views.odin) | App (correct) |
| Candle health computation | App (health.odin) | App (correct) |
| Bootstrap (session) | App (app.odin:init) | App (correct) |
| HTTP fetch implementations | Platform (web+native) | Platform (correct) |

### 1.3 Duplication Analysis

**HIGH duplication (web vs native):**
- WS state machine: `MD_Web_State` (~200 fields) vs `MD_Native_State` (~200 fields) — near-identical
- Message parse loop: `web_poll_impl` vs `native_poll_impl` — same pattern, ~400 LOC each
- Subscribe/unsubscribe: `web_subscribe` vs `native_subscribe` — identical logic
- Reconnect: `web_poll_reconnect` vs `native_poll_reconnect` — same backoff/timer
- HTTP fetch: `web_http_get` vs `native_http_get` — only transport differs
- Metrics collection: identical field copying in both

**LOW duplication (app layer):**
- `drain_marketdata` is correctly centralized (one copy)
- `reconcile_subscriptions` is correctly centralized (one copy)
- Health/freshness/timeline is correctly centralized in `health.odin`

### 1.4 Integrity Gaps

| Gap | Severity | Location |
|-----|----------|----------|
| **G1:** `orderbook_snapshot_gate` is in `app/marketdata.odin` but enforces a transport-level invariant (snapshot-before-delta) | P2 | `app/marketdata.odin:314-328` |
| **G2:** No client-side `prev_seq` chain validation — fully delegated to platform transport | P3 | `platform/web:1290-1300` (only resync on gap, no formal chain check) |
| **G3:** Three separate "health" systems with no unification (candle health, stream controller health, freshness) | P2 | `health.odin`, `stream_controller.odin`, `Freshness_State` |
| **G4:** `reconnect_transport` re-enters from app layer but clears state in platform layer without coordination | P3 | `platform/web:547`, `app/stream_views.odin` |
| **G5:** No formal state machine for bootstrap→connect→subscribe→live lifecycle | P2 | Implicit in `app.odin:init` + `drain_marketdata` transition detection |

### 1.5 What Works Well (No Change Needed)

| Component | Assessment |
|-----------|-----------|
| `Marketdata_Port` interface | Clean, minimal, correctly abstracts platform |
| `MD_Event` / `MD_Event_Data` union | Zero-copy, fixed-size, efficient |
| `drain_marketdata` event loop | Correct routing, per-type handlers, slot resolution |
| `reconcile_subscriptions` | Diff-based, TF-aware, backpressure-aware |
| `stream_controller` seq/ts tolerance | Correct ±10/5s thresholds for multi-replica |
| S20 HTTP bootstrap flow | Clean session→markets→timeline→WS sequence |
| S20 freshness polling | Correct cadence, channel mapping, UI integration |
| S20 timeline-bounded GetRange | Correct first_ts/last_ts bounds |

---

## 2. Stage 21 Architecture

### 2.1 Design Principles

1. **Consolidate, don't rewrite.** The transport works. Extract and align, don't rebuild.
2. **Protocol rules in `md_common`, not in platform.** Shared protocol logic (snapshot gate, prev_seq validation, desync policy) belongs in `md_common` so both web and native share the same rules.
3. **Platform = I/O only.** Web and native should differ ONLY in how bytes move (JS bridge vs TCP). All protocol interpretation should be shared.
4. **App layer = orchestration.** The app layer orchestrates lifecycle (bootstrap→connect→subscribe→drain→health) but delegates protocol rules downward.
5. **One health model.** Unify candle health, stream health, and freshness into a single `Stream_Health` that the UI can query.

### 2.2 Target Layer Boundaries

```
┌─────────────────────────────────────────────────┐
│  App Layer (app/)                                │
│  - Bootstrap orchestration (init)                │
│  - Event drain + store apply (marketdata.odin)   │
│  - Subscription reconciliation (reconcile.odin)  │
│  - Health observation (health.odin)              │
│  - UI badges/display (top_bar.odin)              │
│  - GetRange / lazy loading (stream_views.odin)   │
├─────────────────────────────────────────────────┤
│  Core Layer (streams/)                           │
│  - Stream_Controller (seq/ts/health)             │
│  - Stream_Registry (per-market handles)          │
│  - Endpoint mapping                              │
├─────────────────────────────────────────────────┤
│  Protocol Layer (md_common/) ← S21 consolidation │
│  - Snapshot gate logic (shared)                  │
│  - prev_seq chain validation (shared)            │
│  - Fault action mapping (shared)                 │
│  - Message builders (subscribe/resync/getrange)  │
│  - Rate tracking + backpressure assist           │
│  - Bootstrap lifecycle state enum                │
├─────────────────────────────────────────────────┤
│  Port Interface (ports/)                         │
│  - Marketdata_Port (proc pointers)               │
│  - Event types, enums                            │
├─────────────────────────────────────────────────┤
│  Platform Layer (platform/web, platform/native)  │
│  - I/O: WS bytes, HTTP bytes                     │
│  - Parse: JSON → MD_Event staging                │
│  - Transport state: connect/backoff/timer        │
│  - Calls md_common for protocol decisions        │
└─────────────────────────────────────────────────┘
```

### 2.3 Protocol Decisions (Explicit)

**D1: Bootstrap → WS handoff protocol**

The official client lifecycle is:

```
1. HTTP: GET /api/v1/session → Bootstrap_State (ready, server_time, markets)
2. HTTP: GET /api/v1/markets → enrich market details (tick_size, market_type)
3. GATE: if !bootstrap.ready → show "NOT READY", skip WS
4. WS: connect to /ws
5. WS: server sends `hello` (mandatory first frame)
6. WS: client optionally sends `hello` with requested_features
7. For each cell binding: reconcile_subscriptions → subscribe ops
8. Per subscribe: server sends snapshot → ack → events
9. HTTP: GET /api/v1/freshness (600-frame cadence)
10. HTTP: GET /api/v1/timeline (once per market+TF change)
11. WS: GetRange for candle backfill (timeline-bounded)
12. WS: live event stream
```

This is already implemented by S20. S21 preserves it.

**D2: Snapshot-before-delta enforcement**

Currently in `app/marketdata.odin:orderbook_snapshot_gate`. This is a protocol-level invariant, not an app-level decision.

S21 decision: **Keep in app layer.** Moving it to `md_common` would require passing orderbook state through the port, which breaks the port's role as a thin I/O boundary. The gate is only 15 lines and is called from one place. The cost of moving outweighs the architectural benefit.

**D3: prev_seq chain validation**

The backend sends `prev_seq` on every event frame (S18 contract). The client platform layers track per-subscription `last_seq` and detect gaps. The stream controller also independently detects gaps with ±10 tolerance.

S21 decision: **Two-tier detection is correct.** Transport-level (platform) catches wire-level gaps fast. Stream-level (controller) provides cross-event-type tolerance. No unification needed.

**D4: Reconnect/resync protocol**

Current flow on disconnect:
1. Platform detects WS close → enters backoff
2. Platform reconnects → sends hello
3. App detects `Connected` transition in `drain_marketdata` → clears `prev_subs_count` → triggers `reconcile_subscriptions`
4. Reconcile re-subscribes all wanted channels (fresh subscribe = fresh snapshot + ack)
5. GetRange state cleared (server has no memory)

S21 decision: **This is correct.** Each reconnect is a fresh session (S18 D5). No session resumption. The app-layer transition detection in `drain_marketdata` is the right place because only the app layer knows what cells need.

**D5: Health unification**

Three health signals exist:
- **Candle health** (`Candle_Health`): TF-adaptive, per-store
- **Stream health** (`Stream_State`): seq/ts/snapshot-based, per-stream
- **Backend freshness** (`Freshness_State`): HTTP-polled, per-channel

S21 decision: **Do NOT merge into one.** They measure different things:
- Candle health = "is the candle store receiving timely data?" (render concern)
- Stream health = "is the WS connection delivering coherent data?" (transport concern)
- Freshness = "is the backend pipeline flowing?" (backend concern)

Merging would lose semantic precision. Instead, S21 adds a **unified status derivation** in the top bar that combines all three into a single display priority.

**D6: Duplicate platform logic**

~3000 LOC duplicated between web and native. This is the biggest technical debt in the transport layer.

S21 decision: **Defer full extraction.** The duplication is stable (both implementations work identically). Extracting shared logic into `md_common` requires refactoring the state structs, which is high-risk for low immediate value. Instead, S21 will extract only the **protocol decision functions** (snapshot gate, fault mapping, backoff calculation) that are currently inline in both platforms, leaving I/O and state management platform-specific.

### 2.4 What Changes vs What Stays

**STAYS UNCHANGED:**
- All WS message parsing (platform layer)
- All store push/apply logic (app/marketdata.odin)
- All widget rendering
- Reconciliation logic (reconcile.odin)
- GetRange + lazy loading (stream_views.odin)
- S20 bootstrap flow (session → markets → gate → WS → freshness → timeline)
- Port interface (Marketdata_Port)

**CHANGES:**
- Extract `md_common.compute_backoff_ms` (currently inline in both platforms)
- Extract `md_common.bootstrap_lifecycle_state` enum + derivation
- Add unified status derivation in `top_bar.odin` combining 3 health signals
- Formalize the connect transition detection as a named state in `App_State`
- Document the protocol lifecycle as a state diagram in this report

---

## 3. Protocol Unification Plan (By Layer)

### Layer 1: Protocol Shared Logic Extraction (md_common)

**Goal:** Move pure-function protocol decisions from platform to `md_common`.

**Changes:**

1. **`md_common/backoff.odin`** (new, ~30 LOC)
   - `compute_backoff_ms(current_ms, initial_ms, max_ms, multiplier) -> next_ms`
   - `compute_jittered_backoff_ms(base_ms) -> jittered_ms`
   - Currently duplicated in web (`web_poll_reconnect`) and native (`native_poll_reconnect`)

2. **`md_common/lifecycle.odin`** (new, ~20 LOC)
   - `Bootstrap_Lifecycle :: enum { Init, Session_Loaded, Markets_Loaded, Ready, WS_Connected, Subscribing, Live, Degraded, Offline }`
   - Pure derivation function: `derive_lifecycle_state(has_session, ready, ws_connected, has_acks, has_events) -> Bootstrap_Lifecycle`
   - Used by app layer for unified status display

**Validation:** Both platforms call shared backoff; app layer uses lifecycle enum.

### Layer 2: App-Layer Lifecycle Formalization

**Goal:** Replace the implicit lifecycle detection in `drain_marketdata` with named state.

**Changes:**

1. Add `lifecycle: md_common.Bootstrap_Lifecycle` to `App_State`
2. Update `init()` to set lifecycle states as bootstrap progresses
3. Update `drain_marketdata` connect-transition detection to advance lifecycle
4. Use lifecycle state in `top_bar.odin` for unified status badge

**Impact:** ~20 lines changed in `app.odin`, ~10 lines in `top_bar.odin`.

### Layer 3: Unified Status Display

**Goal:** Top bar shows ONE canonical status derived from all health signals.

**Priority hierarchy (highest wins):**
1. `bootstrap.has_session && !bootstrap.ready` → "NOT READY" (red)
2. `conn_status == Offline/Reconnecting` → "OFFLINE"/"RECONNECTING" (grey/yellow)
3. `stream_health == Desync` → "DESYNC" (red)
4. `!freshness.active` → "STALE" (yellow)
5. `candle_health == Stale` → "LAG" (yellow)
6. `candle_health == OK && freshness.active` → "LIVE" (green)

**Current state:** Top bar already shows `LIVE`/`OFFLINE`/`NOT READY`/`FLOWING`/`STALE` as separate badges (correct). S21 does NOT collapse them — multiple badges is better UX than one overloaded badge. Instead, S21 ensures the badges are ordered by severity (left-to-right: most critical → least critical).

**Decision change:** After analysis, the current multi-badge approach is superior. No unified badge needed. S21 will only **reorder badges by severity** if they aren't already.

### Layer 4: Reconnect Protocol Hardening

**Goal:** Ensure reconnect clears ALL stale state deterministically.

**Current reconnect clearing (in `drain_marketdata`):**
- `prev_subs_count = 0` (force full re-subscribe)
- `getrange.pending = false` (clear in-flight getrange)
- Per-cell `getranges[ci].pending = false`
- Per-slot `orderbook_snapshot_seen = false`

**Missing from reconnect clear (S21 fix):**
- `freshness.loaded = false` (stale freshness should be re-fetched)
- `timeline.loaded = false` (timeline bounds may have changed)

**Impact:** 2 lines added to `drain_marketdata` reconnect block.

### Layer 5: Store Isolation Verification

**Goal:** Verify no store leaks data across stream switches or reconnects.

After audit, stores are correctly isolated:
- Per-slot stores (in `Stream_View_Slot`) hold per-market data
- Global stores mirror active slot via `sync_active_stream_view_to_global_stores`
- TF switch clears candle/heatmap/vpvr in both slot and global
- Reconnect clears orderbook snapshot gate per-slot

**No changes needed.** Store isolation is correct.

---

## 4. Code Changes (Delivered)

### 4.1 Backoff: already shared (no new file needed)

`md_common.backoff_with_jitter` (in `md_common/md_common.odin:643`) was already called by both web and native platforms. No new backoff extraction needed — the shared function pre-dates S21.

### 4.2 New file: `md_common/lifecycle.odin` (44 LOC)

Bootstrap lifecycle state enum + pure derivation function:
- `Bootstrap_Lifecycle` enum: Init, Session_Loaded, Markets_Loaded, Ready, WS_Connected, Subscribing, Live, Degraded, Offline, Not_Ready
- `derive_lifecycle(has_session, session_ready, has_markets, ws_connected, was_ever_connected, has_subscribe_acks, has_events, has_desync)` — stateless derivation covering both bootstrap and runtime phases
- Called in `app.odin` init (twice: post-session, post-markets) and once per frame in `drain_marketdata`

### 4.3 Modified: `app/app.odin`

- Added `import "mr:md_common"`, `lifecycle: md_common.Bootstrap_Lifecycle`, `was_ever_connected: bool`
- Bootstrap init calls `derive_lifecycle` after session parse and again after markets fetch (replaces manual `.Session_Loaded`/`.Markets_Loaded`/`.Ready` assignments)

### 4.4 Modified: `app/marketdata.odin`

- Added `import "mr:md_common"`
- **Reconnect block:** Clear `freshness.loaded = false` and `timeline = {}` on WS reconnect; set `was_ever_connected = true`
- **End of drain_marketdata:** Single `derive_lifecycle` call replaces 3 manual transition sites (WS_Connected, Offline, Live). Lifecycle is now derived from observable state every frame.

### Delivered files summary

| File | Change | LOC |
|------|--------|-----|
| `md_common/backoff.odin` | DELETED — was dead code (backoff_with_jitter already in md_common.odin) | -24 |
| `md_common/lifecycle.odin` | NEW — lifecycle enum + complete derivation function | 44 |
| `app/app.odin` | +import, +lifecycle field, +was_ever_connected, +derive_lifecycle calls in init | +10 |
| `app/marketdata.odin` | +import, +reconnect clearing, +single derive_lifecycle call (replaces 3 manual sites) | +12 |
| **Net new/changed** | | **~42 LOC** |

---

## 5. Validation

### 5.1 Compilation

All changes compile clean (verified 2026-03-06):

```
client/make check-core          → all packages OK (10/10 packages)
client/make check-wasm-compile  → OK
go build ./cmd/server/           → clean
```

### 5.2 Protocol Contract Preservation

| S18 Contract | S21 Impact | Status |
|--------------|-----------|--------|
| WS-8 Hello gate | No change | PRESERVED |
| WS-9 snapshot_seq monotonicity | No change | PRESERVED |
| WS-10 prev_seq chain | No change | PRESERVED |
| WS-11 snapshot-before-ack | No change | PRESERVED |
| HTTP consumption policy | No change | PRESERVED |
| GetRange in-memory only | No change | PRESERVED |

### 5.3 S20 Surface Preservation

| S20 Surface | S21 Impact | Status |
|-------------|-----------|--------|
| Session bootstrap | lifecycle enum added, flow unchanged | PRESERVED |
| Freshness polling | `loaded` cleared on reconnect (improvement) | PRESERVED |
| Timeline fetch | `Timeline_State` cleared on reconnect (improvement) | PRESERVED |
| NOT READY badge | Now also driven by lifecycle enum | PRESERVED |
| FLOWING/STALE badge | Unchanged | PRESERVED |

---

## 6. Risks

| Risk | Severity | Mitigation |
|------|----------|------------|
| Backoff extraction changes reconnect timing | Low | Pure extraction, same algorithm, unit-testable |
| Lifecycle enum adds state to track | Low | Derived from existing booleans, not new behavior |
| Freshness/timeline clear on reconnect causes brief badge flicker | Low | Re-fetch happens within 1-2 frames; acceptable |
| Platform duplication not fully resolved | Medium | Deferred by design; tracked as tech debt for S22+ |

---

## 7. Constraints (Held)

- No new wire contracts created
- No trading surface expansion
- No exchange integration changes
- No client-side federation/readiness/freshness logic duplication
- No behavioral changes to existing WS protocol handling
- All S15-S20 frozen contracts preserved
- No dashboard/workspace expansion

---

## 8. Recommended Next Slice (Post-S21)

All S21 sub-items complete:
- **S21.1:** Backoff already shared via `md_common.backoff_with_jitter` — dead `backoff.odin` deleted
- **S21.2:** `derive_lifecycle` wired as single source of truth in init + drain_marketdata
- **S21.3:** Reconnect state clearing done (freshness + timeline)
- **S21.4:** Badge ordering verified correct (right-to-left: LIVE → NOT READY → STALE/FLOWING)

**S22 candidates:**
1. Platform layer shared state extraction (de-duplicate web/native state machine ~3000 LOC)
2. Catalog-driven widget enablement (`GET /api/v1/catalog`)
3. Instrument Overview in stream picker (`GET /api/v1/instrument/overview`)
