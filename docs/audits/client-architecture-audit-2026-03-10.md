# Client Architecture Audit — 2026-03-10

**Scope:** Full structural audit of `client/src/core/` (11 packages) + `client/src/platform/` + `client/web/`
**Method:** Deep read of all source files, dependency tracing, responsibility mapping
**Baseline:** 1,317 tests, ~30K LOC Odin + ~2K LOC JS, S158 guard rails

---

## 1. Capability Map

```
┌─────────────────────────────────────────────────────────────────────┐
│                    MARKET RACCOON CLIENT                            │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  ┌─────────────────────────────────────────────────────┐           │
│  │ APP — Dashboard Orchestration                       │           │
│  │  • Route/page management (6 routes)                 │           │
│  │  • Workspace tree (split/pane/stack, 16 panes max)  │           │
│  │  • 3-tier context resolution (Global→WS→Pane)       │           │
│  │  • Widget lifecycle contracts (13 widget kinds)      │           │
│  │  • UI action dispatch (50+ action kinds)            │           │
│  │  • Stream slot management (32 cap, LRU eviction)    │           │
│  │  • Compare mode (4-pane side-by-side)               │           │
│  │  • Health HUD + telemetry sampling                  │           │
│  │  • Legacy Entity_World (parallel ECS arrays)        │           │
│  │  • Layout persistence (schema V12)                  │           │
│  └──────────┬───────────┬──────────┬──────────────────┘           │
│             │           │          │                               │
│  ┌──────────▼───┐ ┌─────▼─────┐ ┌─▼───────────┐                  │
│  │ LAYERS       │ │ SERVICES  │ │ MD_COMMON   │                  │
│  │ Render+Store │ │ Data Infra│ │ Policy+State│                  │
│  └──────┬───────┘ └─────┬─────┘ └──────┬──────┘                  │
│         │               │              │                          │
│  ┌──────▼───────────────▼──────────────▼──────────┐              │
│  │ FOUNDATION: ports / streams / ui / widgets /   │              │
│  │             util / math                         │              │
│  └────────────────────────┬───────────────────────┘              │
│                           │                                       │
│  ┌────────────────────────▼───────────────────────┐              │
│  │ PLATFORM: native (GLFW+ImGui) | web (WASM+C2D) │              │
│  └────────────────────────────────────────────────┘              │
└─────────────────────────────────────────────────────────────────────┘
```

### Layer Ownership Summary

| Layer | Packages | LOC Est. | Role |
|-------|----------|----------|------|
| Foundation | ports, streams, ui, widgets, util, math | ~8K | Types, contracts, geometry, controls |
| Infrastructure | services, md_common | ~10K | Stores, parsers, state machines, policies |
| Aggregation | layers | ~4K | Market store, data source, render strategies |
| Orchestration | app | ~12K | Dashboard state, actions, rendering chrome |
| Platform | platform/native, platform/web, web/ | ~6K | GLFW/ImGui, WASM/Canvas2D, JS runtime |

---

## 2. Per-Area Responsibility Matrix

### 2.1 `ports/` — External Boundary

| File | Responsibility | Can Know | Must Not Know | Risk |
|------|---------------|----------|---------------|------|
| `input.odin` | Per-frame input (keys, mouse, scroll) | ui (Vec2, Rect) | services, layers, app | None |
| `marketdata.odin` | WS events, subscriptions, HTTP fetches | — | services, layers, app | MD_Runtime_Metrics (88 fields) may attract feature creep |
| `settings.odin` | KV persistence + clipboard | — | services, layers, app | None |
| `font.odin` | Font atlas lifecycle | ui (Font_Id) | services, layers, app | None |
| `text.odin` | Text measurement | ui (Vec2) | services, layers, app | None |

**Verdict:** Clean. 5 proc-pointer interfaces, zero upward imports.

---

### 2.2 `services/` — Data Infrastructure

| File | Responsibility | Can Know | Must Not Know | Risk |
|------|---------------|----------|---------------|------|
| `candle_store.odin` | 750-cap ring buffer | core:math | layers, app, ports | None |
| `trades_store.odin` | 256-cap trade ring | — | layers, app | None |
| `orderbook_store.odin` | 50-level ask/bid snapshot | core:math | layers, app | None |
| `dom_store.odin` | 512-level DOM fills | core:math | layers, app | None |
| `footprint_store.odin` | 200×50 candle×price bins | core:math | layers, app | None |
| `heatmap_store.odin` | 128-cap heatmap ring | core:math | layers, app | None |
| `vpvr_store.odin` | 200-bucket volume profile | core:math | layers, app | None |
| `stats_store.odin` | 64-cap stats ring | — | layers, app | None |
| `analytics_store.odin` | 64-cap kind-indexed ring | — | layers, app | None |
| `signal_store.odin` | 8-kind × 50-entry ring | — | layers, app | None |
| `message_parser.odin` | Two-pass JSON → Parsed_* | util, services | layers, app | **LARGE FILE** (~1K lines), parse telemetry grows with protocol |
| `settings_store.odin` | 128-slot KV + dirty tracking | ports (Settings_Port) | layers, app | Only file importing ports — acceptable |
| `profile_store.odin` | 12-profile connection manager | core:fmt | layers, app | None |
| `portfolio_store.odin` | HTTP JSON → portfolio types | core:encoding/json | layers, app | None |
| `session_health.odin` | HTTP JSON → health types | core:encoding/json | layers, app | None |
| `market_discovery.odin` | HTTP JSON → market types | core:encoding/json | layers, app | None |

**Verdict:** Excellent isolation. All stores zero-alloc, bounded, deterministic. No upward imports.

---

### 2.3 `layers/` — Render + Market Aggregation

| File | Responsibility | Can Know | Must Not Know | Risk |
|------|---------------|----------|---------------|------|
| `layer_api.odin` | Layer_Context (read-only contract), Layer_Strategy vtable | ports, services, ui | app | None |
| `market_store.odin` | 16-stream Market_Store with LRU | ports, services | app | **Market_Stream is large** (~20 embedded stores) |
| `market_store_reducers.odin` | 11 event reducers (trade→DOM+footprint+tape) | ports, services | app | Trade reducer fans out to 3 stores — coupling risk if more stores added |
| `data_source.odin` | Channel→market subject aggregation | ports, util | app, services | None |
| `layer_strategies.odin` | 9 stateless render strategies | services, ui | app | **1,698 lines in one file** — largest in layers/ |
| `render_primitives.odin` | 2048-cap primitive buffer | ui | app | Fixed cap may need tuning for dense charts |
| `layer_registry.odin` | Strategy registry + enable/disable + telemetry | ports, services | app | None |
| `time_axis.odin` | TF-aware time labels + grid lines | services, ui | app | None |
| `layer_canvas_renderer.odin` | Primitive → ui.Command_Buffer | ui | app | None |

**Verdict:** Strong. Read-only Layer_Context prevents mutation bugs. Z-ordered composition is deterministic. One concern: `layer_strategies.odin` is monolithic.

---

### 2.4 `md_common/` — Policy + State Machines

| File | Responsibility | Can Know | Must Not Know | Risk |
|------|---------------|----------|---------------|------|
| `artifact_policy.odin` | 17-kind @rodata policy tables | ports | services, layers, app | None — compile-time only |
| `stream_apply_state.odin` | Per-stream health/recovery FSM (140+ procs) | ports | services, layers | **978 lines, highest proc density** |
| `protocol_engine.odin` | Seq integrity FSM | ports | services, layers | None |
| `tf_data_contract.odin` | TF×availability tables | — | services, layers | None |
| `md_common.odin` | Protocol message builders + helpers | ports, services, util | layers, app | **870 lines**, imports services (for parse types only) |
| `runtime_snapshot.odin` | Deterministic state capture | — | services, layers | None |
| `diagnostics_view.odin` | Read-only health aggregation | — | services, layers | None |
| `fusion.odin` | Multi-venue evidence metadata | — | services, layers | None |

**Verdict:** Well-engineered shared library. Pure functions dominate. `stream_apply_state.odin` is the brain of the health pipeline — high value, high density.

---

### 2.5 `app/` — Dashboard Orchestration

| File | Responsibility | Can Know | Must Not Know | Risk |
|------|---------------|----------|---------------|------|
| `app.odin` | App_State god object + constants | everything in core | platform | **~1000+ fields**, monolithic |
| `actions.odin` | 50+ UI action dispatch | ports, services, ui | platform | **Dual-path** (ECS + pane) |
| `workspace.odin` | Split tree + pane pool | services, ui, widgets | platform | None — clean ADR-0025/26 |
| `operating_model.odin` | 3-tier context resolution | ports, services | platform | None — clean ADR-0031 |
| `widget_contract.odin` | 13-kind lifecycle vtable | layers, ports, services, ui | platform | None — clean ADR-0027 |
| `layer_marketdata.odin` | Drain MD → populate layers | layers, md_common, services | platform | **Breaks encapsulation** — directly accesses layer store |
| `build_cell.odin` | Per-cell rendering chrome | ports, services, ui | platform | Dual-path rendering |
| `build_dashboard.odin` | Dashboard grid + detail panel | layers, ports, services, ui | platform | None |
| `health.odin` | Candle health + backpressure | md_common, ports, services, streams, ui | platform | None |
| `components.odin` | Legacy ECS parallel arrays | md_common, ports, services, streams, ui, widgets | platform | **LEGACY — scheduled for removal** |

**Verdict:** Functional but strained. App_State is a god object. Dual-path rendering (ECS + pane) creates maintenance burden. New abstractions (workspace, operating model, widget contracts) are clean.

---

### 2.6 Foundation Packages

| Package | Files | Responsibility | Imports | Risk |
|---------|-------|---------------|---------|------|
| `streams/` | 6 | Stream pool, health FSM, endpoints | ports, util | None |
| `ui/` | 10 | RCL, layout, controls, colors | core:* only | None |
| `widgets/` | 2 | Chart config, draw tools | ui only | None |
| `util/` | 4 | Protocol types, subject hashing | ports only | None |
| `math/` | 1 | Pure numeric utilities | core:math only | None |

**Verdict:** Exemplary. Zero upward imports. Pure functions. Deterministic.

---

### 2.7 `platform/` — Native + Web

| Area | Responsibility | Can Know | Must Not Know | Risk |
|------|---------------|----------|---------------|------|
| `platform/native/` | GLFW/ImGui, WS threading, settings file | ports, app, services, ui | — | None |
| `platform/web/` | WASM exports, Canvas2D foreign procs | ports, app, services, ui | — | **1128 lines of probe exports** in main.odin |
| `web/modules/` | JS runtime (WS, input, canvas, storage) | — | — | Sync XMLHttpRequest in storage.js |

**Verdict:** Clean port implementations. Probe export volume in web/main.odin is a maintenance concern but necessary for Playwright testing.

---

## 3. Dependency Graph (Verified)

```
                         ┌─────────┐
                         │   APP   │
                         └────┬────┘
                  ┌───────────┼───────────┐
                  ▼           ▼           ▼
            ┌─────────┐ ┌─────────┐ ┌──────────┐
            │ LAYERS  │ │SERVICES │ │MD_COMMON │
            └────┬────┘ └────┬────┘ └─────┬────┘
                 │           │            │
                 └─────┬─────┘            │
                       │     ┌────────────┘
                       ▼     ▼
            ┌──────────────────────────────┐
            │ FOUNDATION                    │
            │ ports / streams / ui /        │
            │ widgets / util / math         │
            └──────────────────────────────┘
```

**Circular dependency check: NONE FOUND.**

All arrows point downward. Guard rails from S158 hold:
- `services/` never imports `layers/` or `app/`
- `layers/` never imports `app/`
- `ui/` imports zero `mr:*` packages
- `ports/` imports only `ui` (for Vec2/Rect types)

---

## 4. Architectural Conflicts & Naming Drift

### 4.1 Conflicts

| # | Conflict | Location | Severity |
|---|---------|----------|----------|
| C1 | **God Object: App_State** (~1000+ fields across sub-states) | `app/app.odin` | HIGH |
| C2 | **Dual-path rendering** (legacy Entity_World + pane-based) | `app/actions.odin`, `app/build_cell.odin` | MEDIUM |
| C3 | **layer_marketdata breaks encapsulation** — app/ directly accesses Market_Store internals | `app/layer_marketdata.odin` | MEDIUM |
| C4 | **layer_strategies.odin monolith** (1,698 lines, 9 strategies in one file) | `layers/layer_strategies.odin` | LOW |
| C5 | **stream_apply_state.odin density** (978 lines, 140+ procs) | `md_common/stream_apply_state.odin` | LOW |
| C6 | **Market_Stream embeds ~20 stores** — single struct owns all data domains | `layers/market_store.odin` | LOW |

### 4.2 Naming Drift

| # | Issue | Location | Note |
|---|-------|----------|------|
| N1 | `layer_marketdata.odin` lives in `app/` not `layers/` | `app/layer_marketdata.odin` | Name suggests layers/ ownership |
| N2 | `components.odin` uses ECS terminology in pane-based architecture | `app/components.odin` | Legacy artifact |
| N3 | `stream_views.odin` vs `stream_slots.odin` — unclear naming boundary | `app/stream_views.odin`, `app/stream_slots.odin` | Both manage stream-to-view mapping |
| N4 | `shell_common.odin` — vague name for shared rendering helpers | `app/shell_common.odin` | Could be `render_helpers.odin` |
| N5 | `market_store.odin` lives in `layers/` but is a data container, not a render layer | `layers/market_store.odin` | Structural — move would break imports |

---

## 5. Cockpit vs Cosmetic Assessment

This is NOT a cosmetic UI. Evidence of operational cockpit design:

| Cockpit Indicator | Evidence | Files |
|---|---|---|
| **Multi-venue stream health** | 7-state Stream_Reliability enum, 5-layer health pipeline | `md_common/stream_apply_state.odin` |
| **TF-adaptive staleness** | Backfill criticality scales Tick→Daily, per-artifact thresholds | `md_common/tf_data_contract.odin` |
| **Recovery orchestration** | Exponential backoff, 3-attempt cap, gate resets, remediation decisions | `md_common/stream_apply_state.odin` |
| **Backpressure response** | 4-level BP state, auto-degrade heatmap/VPVR, server-reported congestion | `app/health.odin`, `md_common/md_common.odin` |
| **Render budget enforcement** | Per-layer microsecond budgets (400µs–1500µs), over-budget tracking | `layers/layer_registry.odin` |
| **Deterministic snapshots** | Lossless state capture for incident reproduction | `md_common/runtime_snapshot.odin` |
| **Replay scrubber** | 256-entry ring with integrity flags (gap/reorder/duplicate) | `md_common/replay_scrubber.odin` |
| **Composition stage pipeline** | Empty → Range_Pending → Backfilled → Live_Only → Composed | `md_common/stream_apply_state.odin` |
| **Widget data contracts** | Immutable context, formal lifecycle, 13 widget kinds | `app/widget_contract.odin` |
| **Probe instrumentation** | 1000+ exported probes for automated testing | `platform/web/main.odin` |
| **Evidence-signal linkage** | Automatic temporal linking for audit trail | `layers/market_store_reducers.odin` |

**Assessment: The UI is architected as an operational cockpit.** Every data path has health derivation, every render path has budget enforcement, every state transition has diagnostics. This is not a chart viewer with widgets bolted on — it's a monitoring dashboard with professional-grade observability.

---

## 6. Growth Risk Assessment

### Healthy Growth Vectors
- Adding new Widget_Kind (13→N): Widget contract system handles this cleanly
- Adding new layer strategies: Layer_Strategy vtable + registry pattern scales
- Adding new artifact kinds: @rodata policy tables + reducer pattern scales
- Adding new exchanges: Endpoint_Capabilities bitmask + venue normalization handles this

### Risky Growth Vectors

| Risk | Trigger | Consequence | Mitigation |
|------|---------|-------------|------------|
| **App_State bloat** | Every new feature adds fields | Context window pollution, init complexity | Decompose into subsystem structs |
| **action dispatch sprawl** | New UI actions (currently 50+) | apply_ui_actions becomes unreadable | Group by domain (stream, layout, widget) |
| **Market_Stream store count** | New orderflow data types | Market_Stream struct grows unbounded | Consider store registry pattern |
| **Dual-path maintenance** | Bug fixes must touch both ECS + pane code | Divergent behavior, double testing | Complete ECS removal |
| **Probe export volume** | New widgets need new probes | web/main.odin becomes unmaintainable | Generate probes from widget contracts |
| **layer_strategies.odin growth** | New layer render implementations | Single file becomes unmanageable | Split into per-strategy files |

---

## 7. Refactoring Priorities

### P0 — Structural Debt (blocks reliable evolution)

| # | Refactoring | Rationale | Files |
|---|-------------|-----------|-------|
| P0-1 | **Complete Entity_World removal** | Dual-path rendering doubles maintenance for every UI change. Pane-based architecture is the target. All new code already uses pane path. | `app/components.odin`, `app/build_cell.odin`, `app/actions.odin` |

### P1 — Architecture Hygiene (prevents drift)

| # | Refactoring | Rationale | Files |
|---|-------------|-----------|-------|
| P1-1 | **Decompose App_State** into subsystem contexts (Connection, Telemetry, Evidence, Compare, Overlay, etc.) | God object makes reasoning about state ownership impossible. Operating model (ADR-0031) already defines the tier split — extend it to sub-states. | `app/app.odin`, `app/components.odin` |
| P1-2 | **Rename `layer_marketdata.odin`** → `drain_marketdata.odin` or move to dedicated drain/ concern | Current name implies layers/ ownership. It's actually an app-level drain orchestrator. | `app/layer_marketdata.odin` |
| P1-3 | **Group action handlers** by domain (stream_actions, layout_actions, widget_actions) | 50+ action kinds in one dispatch switch is hard to navigate. | `app/actions.odin` |

### P2 — Code Organization (improves maintainability)

| # | Refactoring | Rationale | Files |
|---|-------------|-----------|-------|
| P2-1 | **Split `layer_strategies.odin`** into per-strategy files (e.g., `strategy_price_candles.odin`) | 1,698 lines in one file. Each strategy is independent — natural split boundary. | `layers/layer_strategies.odin` |
| P2-2 | **Merge `stream_views.odin` + `stream_slots.odin`** or clarify naming | Overlapping responsibility (both manage stream↔view mapping). Naming is confusing. | `app/stream_views.odin`, `app/stream_slots.odin` |
| P2-3 | **Rename `shell_common.odin`** → `render_helpers.odin` | "Shell" is vague. These are shared rendering utility procs. | `app/shell_common.odin` |

### P3 — Future-Proofing (nice to have)

| # | Refactoring | Rationale | Files |
|---|-------------|-----------|-------|
| P3-1 | **Auto-generate probe exports** from widget contract table | 1000+ hand-written probes are fragile. Widget_Contract already declares capabilities. | `platform/web/main.odin` |
| P3-2 | **Market_Stream store registry** instead of embedded fields | Each new data type adds a field to Market_Stream. A registry would decouple. | `layers/market_store.odin` |
| P3-3 | **Formalize drain interface** between app and layers | `layer_marketdata.odin` directly accesses Market_Store internals. A drain port would restore encapsulation. | `app/layer_marketdata.odin`, `layers/market_store.odin` |

---

## 8. Verified Guard Rails (S158)

| Guard Rail | Status | Evidence |
|------------|--------|----------|
| Cell_Surface_View ≤ 10 fields | HOLDS | `app/widget_readiness.odin` |
| Data_Readiness ≤ 6 variants | HOLDS | `app/widget_readiness.odin` |
| Pure derivation only (no cached health) | HOLDS | All health procs are pure in `md_common/` |
| Per-stream store isolation | HOLDS | DOM + Footprint on Market_Stream (S148) |
| Layer_Context read-only | HOLDS | `layers/layer_api.odin` — const pointer |
| Strategies stateless | HOLDS | `layers/layer_strategies.odin` — no mutable state |
| services/ never imports layers/ or app/ | HOLDS | Verified across all 26 service files |
| layers/ never imports app/ | HOLDS | Verified across all 11 layer files |
| Workspace schema bump only on persistence change | HOLDS | WORKSPACE_SCHEMA_VERSION=12 |

---

## 9. Architecture Score

| Dimension | Score | Notes |
|-----------|-------|-------|
| **Dependency Direction** | 9/10 | All arrows point down. Zero circular deps. |
| **Separation of Concerns** | 7/10 | Clean at package level; App_State god object and dual-path rendering lower the score. |
| **Domain Modeling** | 9/10 | 17 artifact policies, 7-state reliability, 5-stage composition — all first-class. |
| **Operational Readiness** | 10/10 | Health pipeline, render budgets, replay scrubber, probes, snapshots. |
| **Testability** | 9/10 | 1,317 tests, pure functions dominate, deterministic state capture. |
| **Scalability** | 8/10 | Widget contracts + layer strategies scale well. App_State doesn't. |
| **Code Organization** | 7/10 | layer_strategies monolith, action dispatch sprawl, naming drift. |
| **Overall** | **8.4/10** | Professional-grade operational cockpit with manageable structural debt. |

---

## 10. Summary

The Market Raccoon client is architecturally sound at the macro level. The dependency hierarchy is strict, guard rails hold, and the operational cockpit capabilities (health pipelines, render budgets, deterministic snapshots, replay scrubber) are production-grade.

The primary debt is concentrated in `app/`:
1. **App_State god object** — decomposes poorly, makes onboarding hard
2. **Dual-path rendering** (ECS + pane) — doubles maintenance cost
3. **layer_marketdata encapsulation breach** — app directly mutates layer store

These are migration artifacts, not design failures. The new abstractions (workspace tree, operating model, widget contracts) are clean and should replace the legacy paths.

**Recommendation:** Prioritize P0-1 (Entity_World removal) as the single highest-ROI refactoring. It eliminates dual-path rendering, removes components.odin, simplifies actions.odin, and enables P1-1 (App_State decomposition) as a follow-up.
