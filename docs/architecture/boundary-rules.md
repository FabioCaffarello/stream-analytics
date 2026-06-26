# Boundary Rules — Stream Analytics

> Canonical reference for layer isolation, dependency direction, and ownership.
> Enforced by `make invariants-check` (backend) and review policy (client).
> Last updated: 2026-03-10 (S159 boundary hardening).

---

## 1. Backend Layers

The backend follows hexagonal architecture with six layers.
Dependencies flow **strictly downward**; no layer may import a layer above it.

```
cmd/*            ← entry points (main.go) — bootstrap, wire, run
  ↓
interfaces/      ← HTTP/WS handlers — translate requests into actor messages
  ↓
actors/          ← Hollywood actor subsystems — orchestrate use cases
  ↓
adapters/        ← exchange connectors, storage, bus — implement ports
  ↓
core/*           ← domain + use cases + ports — business logic lives here
  ↓
shared/          ← foundation (zero internal imports)
```

### 1.1 shared/ — Foundation

**Purpose:** Cross-cutting building blocks with zero business logic.

| Allowed | Prohibited |
|---------|-----------|
| Error types (`problem`, `result`, `validation`) | Business rules, aggregates, entities |
| Value objects (`naming`, `ids`, `hash`, `clock`) | Exchange-specific logic |
| Serialization (`codec`, `envelope`, `contracts`) | Storage queries or connections |
| Observability (`metrics`, `observability`) | Actor framework types (`hollywood/*`) |
| Config schema, data structures (`ds`) | Imports from any `internal/*` package |
| Protobuf definitions (`proto/gen`) | HTTP handlers, NATS subjects |

**Litmus test:** If removing a type from `shared/` would break only one bounded context, it belongs in that context's `domain/` package, not in `shared/`.

**Real example — correct:** `naming.CanonicalVenue` normalises venue strings at every domain boundary. Used by marketdata, aggregation, delivery, insights. Lives in `shared/naming`.

**Real example — violation (hypothetical):** A `CandleAggregator` helper in `shared/` — this belongs in `core/aggregation/domain` because only aggregation owns candle construction.

### 1.2 core/* — Domain + Use Cases + Ports

Each bounded context (`core/marketdata`, `core/aggregation`, etc.) contains three sub-packages:

| Sub-package | Contains | May import |
|-------------|----------|-----------|
| `domain/` | Entities, value objects, aggregates, invariants | `shared/` only |
| `app/` | Use cases, orchestration, policies | Own `domain/`, own `ports/`, `shared/` |
| `ports/` | Interfaces for external deps (readers, writers, publishers) | `shared/`, own `domain/` |

**Prohibited for all `core/*` packages:**

| Import target | Reason |
|---------------|--------|
| `internal/actors` | Core must not know its runtime |
| `internal/adapters` | Core must not know implementations |
| `internal/interfaces` | Core must not know transport |
| `shared/policykit` | Policy kit is actor-runtime infrastructure |
| `time.Now()` directly | Use `clock.Clock` for determinism (INV-DET-01) |
| `fmt.Sprintf` in hot paths | Use `hash.FieldHasher` fluent API |

**Cross-context imports — permitted but minimised:**

Core modules may import another core module's `domain/` package when there is a genuine domain relationship. **Exhaustive list of allowed cross-deps:**

| From | May import | Reason |
|------|-----------|--------|
| `core/aggregation` | `core/marketdata/domain` | Aggregation consumes normalised market events |
| `core/aggregation` | `core/marketmodel/domain` | Tick size and instrument metadata for book building |
| `core/marketdata` | `core/marketmodel/domain` | Market model defines instrument metadata |

**Explicitly prohibited cross-deps:**

| From | Must NOT import | Why |
|------|----------------|-----|
| `core/delivery` | Any other `core/*` | Delivery is transport-only; receives pre-serialised envelopes |
| `core/workspace` | Any other `core/*` | Workspace is persistence-only; schema ↔ JSON, no domain logic |
| `core/insights` | `core/aggregation` | Insights receives via bus, not direct import |

**Rule:** A new cross-context import requires an ADR or explicit approval. If two contexts need to share a type, first consider whether that type belongs in `shared/contracts`.

### 1.3 adapters/ — Infrastructure Implementations

**Purpose:** Implement `core/*/ports` interfaces with concrete technology.

| Allowed | Prohibited |
|---------|-----------|
| Import any `core/*/domain` and `core/*/ports` | Import `actors/` or `interfaces/` |
| Import `shared/*` | Contain business rules or invariants |
| Exchange client code (Binance, Bybit, etc.) | Expose exchange-specific types to core |
| Storage drivers (TimescaleDB, ClickHouse) | Own domain entities |

**Real example — correct:** `adapters/exchange/binance/` imports `core/marketdata/domain` to map Binance WebSocket messages into `marketdata.TradeTickV1`.

**Real example — violation (hypothetical):** An adapter that computes candle OHLCV — aggregation logic belongs in `core/aggregation/domain`.

### 1.4 actors/ — Runtime Orchestration

**Purpose:** Wire use cases into Hollywood actor subsystems with supervision.

| Allowed | Prohibited |
|---------|-----------|
| Import `core/*/app` and `core/*/domain` | Import `interfaces/` |
| Import `adapters/` (to inject into ports) | Contain domain logic (only orchestrate) |
| Import `shared/*` | Define new domain types |
| Hollywood framework types | Direct database/HTTP calls (delegate to adapters) |

**Real example — correct:** `actors/aggregation/runtime/processor.go` imports `aggapp` and `insightsapp` to wire the processing pipeline, delegating all computation to core use cases.

### 1.5 interfaces/ — External Transport

**Purpose:** HTTP/WS handlers that translate external requests into actor messages.

| Allowed | Prohibited |
|---------|-----------|
| Import `actors/` (to send messages) | Import `adapters/` directly |
| Import `core/*/domain` (for response DTOs) | Contain business logic |
| Import `shared/*` | Call core use cases directly (go through actors) |

### 1.6 cmd/* — Entry Points

**Purpose:** `main.go` for each binary. Bootstrap, wire, run.

| Allowed | Prohibited |
|---------|-----------|
| Import everything | Contain logic beyond bootstrap and wiring |

### 1.7 Binary Ownership Map

Each `cmd/*` binary owns exactly one deployment concern. No binary may host actors belonging to another binary's domain.

| Binary | Actor subsystems | Core domains consumed | Infrastructure |
|--------|-----------------|----------------------|----------------|
| `cmd/consumer` | `actors/marketdata` | marketdata, marketmodel | Exchange WS, NATS publish, Kafka publish (analytics path) |
| `cmd/processor` | `actors/aggregation`, `actors/insights`, `actors/evidence` | aggregation, insights, evidence, marketdata | NATS subscribe, NATS publish, ClickHouse write |
| `cmd/server` | `actors/delivery` | delivery | WS server, NATS subscribe, TimescaleDB/ClickHouse read |
| `cmd/store` | — (no actors) | — | TimescaleDB, ClickHouse, NATS subscribe |
| `cmd/migrate` | — (no actors) | — | Goose migrations (TimescaleDB + ClickHouse) |
| `cmd/emulator` | — (no actors) | — | Kafka write, NATS runtime store read |
| `cmd/validator` | — (no actors) | — | JetStream consume, NATS result publish, HTTP health |

**Rule:** If a new feature requires an actor subsystem not listed here, decide which binary owns it before writing code. Cross-binary actor hosting is prohibited.

---

## 2. Client Layers (Odin)

The client follows a strict DAG with six tiers.
Dependencies flow **strictly downward**; no package may import a package above it.

```
app/             ← orchestration, routing, state, frame loop
  ↓
layers/          ← visualization strategies (stateless, read-only)
  ↓
md_common/       ← protocol bridge (services ↔ layers)
  ↓
services/        ← domain stores, session health, message parsing
  ↓
streams/         ← stream registry, endpoint resolution
  ↓
ports/           ← adapter interfaces (input, fonts, marketdata, settings)
  ↓
ui/ + math/      ← foundation (zero internal imports)

util/            ← leaf utilities (no mr: imports, same tier as ui/)
widgets/         ← chart type definitions (imports ui/ only)
```

### 2.1 ui/ + math/ + util/ — Foundation

**Purpose:** Primitive types, layout helpers, math utilities, subject parsing.

| Allowed | Prohibited |
|---------|-----------|
| Standard library only | Any `mr:` import |

`widgets/` is a leaf package containing chart type definitions. It may import `ui/` but nothing else.

### 2.2 ports/ — Adapter Interfaces

**Purpose:** Define how the app talks to the platform (input, fonts, settings, marketdata).

| Allowed | Prohibited |
|---------|-----------|
| Import `ui/` (for `Vec2`, `Font_Id`) | Import `services/`, `layers/`, `app/`, `streams/` |

**Real example — correct:** `ports/input.odin` defines `Input_State` and keyboard/mouse abstractions consumed by `app/`.

Platform implementations (`platform/web/`, `platform/native/`) implement these interfaces. They may import `app/`, `ports/`, `services/` — they sit outside the core DAG.

### 2.3 streams/ — Stream Registry

**Purpose:** Stream types, endpoint resolution, stream lifecycle.

| Allowed | Prohibited |
|---------|-----------|
| Import `ports/`, `util/` | Import `services/`, `layers/`, `app/`, `md_common/` |

### 2.4 services/ — Domain Stores

**Purpose:** Per-stream data stores (DOM, Footprint, Trades, Orderbook, Analytics), session health, snapshot lifecycle, message parsing.

| Allowed | Prohibited |
|---------|-----------|
| Import `ports/`, `util/`, `streams/` | Import `layers/`, `app/`, `md_common/` |
| Own per-stream mutable state | Render logic or visualization |

**Stores live on `Market_Stream` and are isolated per-stream:**

| Store | Owner | Cap |
|-------|-------|-----|
| `Trades_Store` | services | 256 ticks |
| `Orderbook_Store` | services | 50/side |
| `DOM_Store` | services | 512 levels |
| `Footprint_Store` | services | 200x50 grid |
| `Analytics_Store` | services | ring buffer |

**Rule:** Adding a new store requires justification that no existing store can serve the data need. New stores must declare a cap on creation.

### 2.5 md_common/ — Protocol Bridge

**Purpose:** Shared protocol utilities that both `services/` and `layers/` need. Intentional bridge to avoid duplication.

| Allowed | Prohibited |
|---------|-----------|
| Import `ports/`, `services/`, `util/`, `streams/` | Import `layers/`, `app/` |

**Real example — correct:** `md_common/stream_apply_state.odin` defines the 5-layer health pipeline consumed by both `services/` (for state tracking) and `layers/` (for render decisions).

**Why a bridge?** Without `md_common/`, types like `Stream_Apply_State` and `Timeframe_Data_Contract` would either be duplicated across `services/` and `layers/`, or one package would have to import the other (breaking the DAG). The bridge is a controlled exception with a narrow scope.

### 2.6 layers/ — Visualization Strategies

**Purpose:** Stateless render strategies that read from services and produce draw commands.

| Allowed | Prohibited |
|---------|-----------|
| Import `ports/`, `services/`, `md_common/`, `ui/`, `util/` | Import `app/` |
| Read `Layer_Context` (read-only) | Write to any store |
| Stateless strategy functions | Cached/mutable state |

**Real example — correct:** `layers/layer_strategies.odin` implements candle, line, heatmap rendering by reading from `Market_Store` and outputting draw lists.

**Real example — violation (hypothetical):** A strategy that modifies `DOM_Store` entries — stores are owned by `services/`, strategies only read.

### 2.7 app/ — Orchestration

**Purpose:** Top-level application state, routing, frame loop, widget layout, workspace persistence.

| Allowed | Prohibited |
|---------|-----------|
| Import all lower tiers | Be imported by any other core package |
| Own `Cell_Surface_View`, `Data_Readiness` | Bypass services to write stores directly |
| Frame loop side effects (`health.odin`) | Cached health/reliability state |

### 2.8 Client Import Matrix

Complete reference. `Y` = allowed, `—` = prohibited, `B` = bridge (md_common only).

| From ↓ \ To → | ui | math | util | widgets | ports | streams | services | md_common | layers | app |
|---|---|---|---|---|---|---|---|---|---|---|
| **app** | Y | Y | Y | Y | Y | Y | Y | Y | Y | — |
| **layers** | Y | Y | Y | — | Y | — | Y | Y | — | — |
| **md_common** | — | — | Y | — | Y | Y | Y | — | — | — |
| **services** | — | — | Y | — | Y | Y | — | — | — | — |
| **streams** | — | — | Y | — | Y | — | — | — | — | — |
| **ports** | Y | — | — | — | — | — | — | — | — | — |
| **widgets** | Y | — | — | — | — | — | — | — | — | — |
| **ui** | — | — | — | — | — | — | — | — | — | — |
| **math** | — | — | — | — | — | — | — | — | — | — |
| **util** | — | — | — | — | — | — | — | — | — | — |

---

## 3. State Pipeline Rules (Client)

### 3.1 Cell_Surface_View — Unified Read Model

**Ceiling: 10 fields.** Adding a field requires justification and ceiling review.

Current fields: `composition`, `health_level`, `recovery_status`, `reliability`, `snapshot_lifecycle`, `is_transport_lagging`, `candle_health`, `stale_count`, `aging_count`, `data_quality`.

### 3.2 Data_Readiness — Render Gate

**Ceiling: 6 variants.** Adding a variant requires ADR.

Current variants: `Absent`, `Pending`, `Degraded`, `Stale`, `Live`, `Recovering`.

### 3.3 Pure Derivation Constraint

All health, reliability, readiness, and visual state values are **derived every frame** from protocol state. No intermediate caching.

**Rule:** If you find yourself writing `cached_reliability := ...` and reading it later, you are violating this constraint. Derive it from source state at the point of use.

---

## 4. Bounded Context Boundaries (Backend)

### 4.1 Boundary Summary

Each bounded context owns a slice of the domain. Communication between contexts happens through **events on the bus** (NATS JetStream), never through direct function calls at runtime.

```
shared ──────────────────────────────────────────────────────
  │
  ├─ marketdata ─── marketmodel
  ├─ evidence
  ├─ insights
  │
  ├─ aggregation ──→ marketdata/domain (compile-time only)
  │
  ├─ delivery (isolated — transport only)
  └─ workspace (isolated — persistence only)
```

### 4.2 Isolation Rules Per Context

| Context | Runtime input | Runtime output | Compile-time deps |
|---------|--------------|----------------|-------------------|
| **marketdata** | Exchange WS feeds | `TradeTickV1`, `BookDeltaV1` → NATS | `marketmodel/domain`, `shared` |
| **aggregation** | NATS events from marketdata | `CandleV2`, `OrderBookSnapshotV2` → NATS | `marketdata/domain`, `shared` |
| **insights** | NATS events from aggregation | `VolumeProfileV1`, `FootprintCandleV1` → NATS | `shared` |
| **evidence** | NATS events from insights | `LiquidityEvidence` → NATS | `shared` |
| **delivery** | NATS (all artifact streams) | WS frames to clients | `shared` only |
| **workspace** | HTTP requests | JSON persistence | `shared` only |

**Rule:** If you need context A to call context B's function at runtime, you are breaking the boundary. Use the bus.

### 4.3 delivery/ — The Isolation Exception

Delivery is deliberately the most isolated core module. It receives pre-serialised envelopes from the bus and forwards them to WS clients. It has **zero knowledge** of what's inside the envelopes.

**Why this matters:** Delivery must never be a bottleneck for domain changes. Adding a new artifact type to aggregation must not require a delivery code change.

### 4.4 workspace/ — Persistence Boundary

Workspace owns schema-versioned persistence for dashboard layouts. It converts between runtime state and JSON documents. It contains **zero domain logic** about market data, health, or indicators.

**Workspace schema version** (`WORKSPACE_SCHEMA_VERSION`) bumps only when the persistence format changes — never for runtime-only state changes.

---

## 5. Orderflow Ownership

Orderflow is a **cross-cutting concern**, NOT a separate bounded context (ADR-0033).

| Data tier | Owner BC | Examples |
|-----------|----------|----------|
| T0 Raw feeds | `marketdata` | TradeTickV1, BookDeltaV1 |
| T1 Aggregates | `aggregation` | TapeWindowV1, OrderBookSnapshotV2 |
| T2 Derived artifacts | `insights` | VolumeProfileV1, FootprintCandleV1 |
| T3 Evidence | `evidence` | LiquidityEvidence rules |
| Client-local state | `client` | DOM accumulation, footprint grid |

**Rule:** New orderflow types must be assigned to exactly one tier and one owner. If ownership is ambiguous, write an ADR.

---

## 6. Naming Boundaries

Full rules in `docs/architecture/naming-rules.md`. Critical subset:

| Rule | Prohibition | Correct usage |
|------|------------|---------------|
| N1 | Packages differing only by singular/plural | e.g. `insight/` vs `insights/` — pick one and be consistent |
| N3 | Bare `State` in type names | Always qualify: `Stream_State`, `Transport_State` |
| N4 | Swapping `Health` and `Readiness` | Health = monitoring; Readiness = operational gate |
| N5 | Swapping `Snapshot` and `Summary` | Snapshot = point-in-time; Summary = rolled-up |
| N6 | Swapping `Event` and `Action` | Event = immutable fact (backend); Action = user input (client) |
| N7 | Cross-stack semantic drift | Same term must mean the same thing backend ↔ client |

---

## 7. Domain Invariants

These invariants are **non-negotiable**. Violations are build-breaking.

| ID | Rule | Enforced by |
|----|------|-------------|
| INV-DOM-01 | Core and actors must be protobuf-free | `make invariants-check` |
| INV-DET-01 | Core cannot call `time.Now()` | `make invariants-check` |
| INV-REP-01 | Replay module must remain offline (no NATS import) | `make invariants-check` |
| INV-BUS-01 | Subject taxonomy: valid family + version | `make invariants-check` |
| INV-ACK-01 | JetStream ingest: ACK/NAK/TERM semantics | Review policy |
| INV-TOPO-01 | Guardian: readiness = all 10 subsystems + restart budget | Test suite |
| INV-MEX-01 | Stream identity = `venue + instrument + market_type` | Test suite |

---

## 8. Review Checklist for New Changes

Use this checklist when reviewing PRs or planning new features.

### Backend

- [ ] **Layer direction:** Does the change import only from layers below it?
- [ ] **Shared boundary:** Does the new type in `shared/` serve 2+ bounded contexts? If not, move it to the owning context.
- [ ] **Core purity:** Does `core/*/domain` remain free of `actors`, `adapters`, `interfaces`, `time.Now()`, `fmt.Sprintf`?
- [ ] **Cross-context import:** Does the change add a new `core/X → core/Y` import? If yes, is it in the §1.2 allowed list? If not, write an ADR.
- [ ] **Adapter isolation:** Does the adapter only implement ports interfaces, without leaking exchange-specific types upward?
- [ ] **Actor responsibility:** Does the actor only orchestrate? No domain logic inside actor message handlers.
- [ ] **Binary ownership:** Does the new actor subsystem belong to the correct `cmd/*` binary per §1.7?
- [ ] **Bus boundary:** Are cross-context communications via NATS events, not direct function calls?
- [ ] **Naming:** Does every new type follow naming-rules.md? No bare `State`, no `Health`/`Readiness` swap.

### Client

- [ ] **DAG direction:** Does the change respect the import matrix in §2.8?
- [ ] **Store ownership:** Do writes to stores happen only in `services/`?
- [ ] **Strategy purity:** Are layer strategies stateless and side-effect free?
- [ ] **Pure derivation:** Is health/reliability/readiness derived per-frame, not cached?
- [ ] **Cell_Surface_View ceiling:** If adding a field, is the 10-field ceiling justified?
- [ ] **Data_Readiness ceiling:** If adding a variant, is an ADR attached?
- [ ] **Widget isolation:** Does the widget receive all data via `Widget_Data_Context`, not globals?
- [ ] **md_common scope:** If adding to `md_common/`, is it genuinely needed by both `services/` and `layers/`? If only one consumer, put it in that package.
- [ ] **Odin gotchas:** No `fmt.tprintf` for persistent strings, no `{`/`}` in format strings for JSON, no variable indexing into constants.

---

## 9. Enforcement Mechanisms

| What | How | When |
|------|-----|------|
| INV-DOM-01, INV-DET-01, INV-REP-01, INV-BUS-01 | `make invariants-check` (grep-based static analysis) | CI on every push |
| INV-TOPO-01, INV-MEX-01 | Test suite assertions | CI on every push |
| Backend layer direction | Go compiler (separate `go.mod` per module; cross-module imports are explicit) | Compile time |
| Backend cross-context imports | `go.mod` `require` directives — adding a new dep is visible in diff | PR review |
| Client DAG direction | Manual review (Odin has no module system enforcing this) | PR review |
| Client store write ownership | Manual review + test coverage in `services/*_test.odin` | PR review |
| Naming rules | Manual review against `naming-rules.md` | PR review |
| Cell_Surface_View / Data_Readiness ceilings | Manual review | PR review |

**Gap:** Client DAG enforcement is manual. A future stage should add a CI script that greps for prohibited `import "mr:..."` patterns per package.

---

## 10. Violation Examples

### 10.1 Backend — Adapter Leaking Domain Logic

```go
// BAD: adapters/exchange/binance/ws.go
func (c *Client) handleTrade(msg []byte) aggregation.Candle {
    // Adapter should NOT build candles — that's core/aggregation's job
    return aggregation.BuildCandle(...)
}

// GOOD: adapters/exchange/binance/ws.go
func (c *Client) handleTrade(msg []byte) marketdata.TradeTickV1 {
    // Adapter maps raw bytes to a domain event, nothing more
    return marketdata.TradeTickV1{...}
}
```

### 10.2 Backend — Core Importing Actor Framework

```go
// BAD: core/delivery/app/session.go
import "github.com/anthdm/hollywood/actor"  // Core must not know its runtime

// GOOD: core/delivery/ports/publisher.go
type EventPublisher interface {
    Publish(ctx context.Context, env envelope.Envelope) *problem.Problem
}
// The actor layer injects an implementation of this port
```

### 10.3 Backend — Cross-Context Direct Call

```go
// BAD: core/insights/app/builder.go
import "github.com/stream-analytics/internal/core/aggregation/app"
func (b *Builder) Build() {
    candles := aggapp.GetLatestCandles(...)  // Direct call across contexts
}

// GOOD: actors/insights/runtime/consumer.go
func (a *InsightsConsumer) Receive(ctx actor.Context) {
    // Receives CandleV2 events via NATS subscription — no direct import
    switch msg := ctx.Message().(type) {
    case *envelope.Envelope:
        a.builder.Build(msg.Payload)
    }
}
```

### 10.4 Client — Layer Writing to Store

```odin
// BAD: layers/layer_strategies.odin
layer_draw_dom :: proc(ctx: ^Layer_Context) {
    ctx.stream.dom_store.levels[0].accumulated += 1  // Layers must not write stores
}

// GOOD: services/dom_store.odin
dom_store_reduce_trade :: proc(store: ^DOM_Store, tick: Trade_Tick) {
    store.levels[price_to_level(tick.price)].accumulated += tick.qty
}
```

### 10.5 Client — Cached Reliability

```odin
// BAD: app/health.odin
stream.cached_reliability = derive_reliability(stream)  // Never cache
// ... later ...
if stream.cached_reliability == .Reliable { ... }        // Stale read

// GOOD: app/health.odin
reliability := derive_reliability(stream)                // Derive at point of use
if reliability == .Reliable { ... }
```

### 10.6 Backend — Shared Containing Single-Context Logic

```go
// BAD: shared/candle_helpers.go
func MergeCandles(a, b Candle) Candle { ... }  // Only aggregation uses this

// GOOD: core/aggregation/domain/candle_merge.go
func MergeCandles(a, b Candle) Candle { ... }  // Lives where it's owned
```

### 10.7 Client — md_common Scope Creep

```odin
// BAD: md_common/widget_helpers.odin
// Only app/ uses widget helpers — this belongs in app/, not in the bridge
build_widget_context :: proc(pane: ^Pane) -> Widget_Data_Context { ... }

// GOOD: app/widget_contract.odin
build_widget_context :: proc(pane: ^Pane) -> Widget_Data_Context { ... }
```

### 10.8 Client — Upward Import

```odin
// BAD: services/candle_store.odin
import "mr:layers"  // services must NEVER import layers

// GOOD: services/candle_store.odin
import "mr:ports"   // services imports ports (downward only)
```
