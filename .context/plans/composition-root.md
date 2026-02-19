---
status: draft
generated: 2026-02-17
agents:
  - type: "refactoring-specialist"
    role: "Extract shared bootstrap primitives and restructure cmd/ binaries"
  - type: "test-writer"
    role: "Validate bootstrap with fake configs and ensure E2E parity"
  - type: "code-reviewer"
    role: "Review dependency graph, API surface, and rollback safety"
docs:
  - "project-overview.md"
  - "development-workflow.md"
  - "testing-strategy.md"
phases:
  - id: "phase-1"
    name: "Extract shared bootstrap primitives"
    prevc: "E"
    agent: "refactoring-specialist"
  - id: "phase-2"
    name: "Per-binary Run() + thin main.go"
    prevc: "E"
    agent: "refactoring-specialist"
  - id: "phase-3"
    name: "E2E test-hooks consolidation"
    prevc: "E"
    agent: "refactoring-specialist"
  - id: "phase-4"
    name: "Validation & green CI"
    prevc: "V"
    agent: "test-writer"
---

# Unified Composition Root — cmd/ ultra-thin

> Extract all wiring, adapter instantiation, and bootstrap logic from cmd/ binaries into dedicated bootstrap packages. Each cmd/*/main.go becomes 20-40 lines: load config, validate, bootstrap, run. Eliminates cross-binary duplication and centralizes dependency wiring per binary.

## Task Snapshot

- **Primary goal:** Every `cmd/*/main.go` <= 40 lines; zero adapter instantiation outside bootstrap; all wiring testable in isolation.
- **Success signal:** `make ci` green, all E2E tests pass, each main.go is load-config -> validate -> `Run(ctx, cfg)`.
- **Key constraint:** No new modules — bootstrap packages live within existing `internal/shared` and `internal/actors` modules to avoid go.work/go.mod churn.

## Current State (from codebase analysis)

| Binary | Lines today | + E2E hook | Direct instantiations | Duplicated helpers |
|---|---|---|---|---|
| cmd/consumer | 762 | 328 | 13 | buildLogger, applyShardOverrides, publisher, guardian spawn/shutdown |
| cmd/processor | 752 | 226 | 14 | buildLogger, applyShardOverrides, publisher, guardian spawn/shutdown |
| cmd/server | 185 | — | 10 | buildLogger, guardian spawn/shutdown |
| cmd/store | 325 | — | 10 | buildLogger, guardian spawn/shutdown |

**Duplicated logic:** ~400 lines across 4 binaries — 4x `buildLogger`, 2x `applyShardOverrides`, 4x guardian spawn, 4x shutdown sequence, 2x near-identical publisher builders, 2x overlapping E2E probe servers.

## Target Package Layout

```
internal/shared/bootstrap/              <-- new pkg in existing shared module
  logger.go                             BuildLogger(cfg) *slog.Logger
  config.go                             LoadAndValidate(path, ...Override) (AppConfig, error)
  shard.go                              ApplyShardOverrides(cfg, flagIdx, flagCnt)
  signal.go                             WaitForSignal(ctx) os.Signal

internal/actors/runtime/                <-- extend existing pkg
  engine_helpers.go                     NewDefaultEngine(), SpawnGuardian(), ShutdownGuardian()

internal/adapters/bus/                  <-- extend existing pkg
  publisher_factory.go                  NewPublisherFromConfig(ctx, cfg, logger) (EventPublisher, func())

internal/shared/testprobe/              <-- new pkg in existing shared module
  probe.go                              Probe HTTP mini-server
  posture.go                            HasE2EPosture(), port resolution

cmd/<binary>/
  main.go                              <= 40 lines: flags -> Load -> Run
  bootstrap.go                          Run(ctx, cfg) error — binary-specific wiring
  e2e_testhook.go                       uses testprobe, keeps only unique injection logic
```

### Why this layout (not `internal/bootstrap/<binary>`)

Each `cmd/<binary>` is its own Go module. Creating `internal/bootstrap/<binary>` would add 4 new modules (go.mod + replace + go.work each). Instead:
- **Shared primitives** -> `internal/shared/bootstrap/` (zero new modules, shared already has the deps)
- **Actor helpers** -> `internal/actors/runtime/` (already imported by all cmd/)
- **Publisher factory** -> `internal/adapters/bus/` (already imported by consumer/processor)
- **Binary-specific wiring** -> `cmd/<binary>/bootstrap.go` (same `package main`, same module)

Module count unchanged. Dependency graph unchanged.

## Key Design Decision: Dependency Graph

```
internal/shared/bootstrap
  imports: config, slog (stdlib only + shared internals)

internal/actors/runtime (existing)
  imports: hollywood/actor, shared/config — already there
  NEW funcs: NewDefaultEngine, SpawnGuardian, ShutdownGuardian

internal/adapters/bus (existing)
  imports: nats, shared/config — already there
  NEW func: NewPublisherFromConfig

cmd/<binary>/bootstrap.go
  imports: shared/bootstrap, actors/runtime, adapters/bus, core/*
  This is the ONLY place that assembles the full dependency graph
```

No circular deps. No new cross-module imports. Each helper lives where its dependencies already exist.

## Risk Assessment

| Risk | Prob | Impact | Mitigation |
|---|---|---|---|
| E2E tests break during refactor | Medium | High | Refactor one binary at a time (Phase 2); run E2E after each |
| Subtle behavior drift in extracted funcs | Medium | Medium | Diff old vs new code line-by-line; test coverage on new helpers |
| Guardian API instability during refactor | Low | High | No concurrent actor runtime changes on this branch |
| Binary-specific quirks missed | Medium | Medium | Each binary keeps own `bootstrap.go`; unique logic stays local |
| `internal/shared` grows too much | Low | Low | bootstrap pkg is small (~150 lines); testprobe only if E2E exists |

## Working Phases

### Phase 1 — Extract shared bootstrap primitives
> **Agent:** `refactoring-specialist`

**Objective:** Create helper functions in existing packages. All cross-binary duplicates eliminated at the library level. Each helper independently tested.

| # | Task | Status | Deliverable |
|---|------|--------|-------------|
| 1.1 | Create `internal/shared/bootstrap/logger.go` — extract identical `buildLogger` from all 4 binaries | pending | `BuildLogger(cfg config.LogConfig) *slog.Logger` |
| 1.2 | Create `internal/shared/bootstrap/config.go` — `LoadAndValidate(path string, overrides ...func(*config.AppConfig)) (config.AppConfig, error)` | pending | Replaces 4 `loadXxxConfig` funcs |
| 1.3 | Create `internal/shared/bootstrap/shard.go` — `ApplyShardOverrides(cfg *config.AppConfig, flagIdx, flagCnt int)` | pending | Eliminates 2 identical copies |
| 1.4 | Create `internal/shared/bootstrap/signal.go` — `WaitForSignal(ctx) os.Signal` + `ContextWithSignal(parent) (ctx, cancel)` | pending | Replaces 4 signal.Notify blocks |
| 1.5 | Add `internal/actors/runtime/engine_helpers.go` — `NewDefaultEngine()`, `SpawnGuardian(e, cfg)`, `ShutdownGuardian(ctx, e, pid, logger, timeout)` | pending | Replaces 4 identical spawn+shutdown sequences |
| 1.6 | Add `internal/adapters/bus/publisher_factory.go` — `NewPublisherFromConfig(ctx, cfg, logger)` | pending | Unifies consumer `buildPublisher` + processor `buildEnvelopePublisher` |
| 1.7 | Write unit tests for all new functions | pending | `bootstrap_test.go`, `engine_helpers_test.go`, `publisher_factory_test.go` |

**Commit:** `feat(bootstrap): extract shared bootstrap primitives`

---

### Phase 2 — Per-binary Run() + thin main.go
> **Agent:** `refactoring-specialist`

**Objective:** For each binary, create `bootstrap.go` with `Run(ctx context.Context, cfg config.AppConfig) error` and reduce `main.go` to <= 40 lines. Order: simplest first.

**Order:** server (185 lines) -> store (325) -> consumer (762) -> processor (752)

| # | Task | Status | Deliverable |
|---|------|--------|-------------|
| 2.1 | **cmd/server** — extract `Run(ctx, cfg)` into `bootstrap.go`; thin main.go | pending | main.go <= 30 lines |
| 2.2 | `make test MODULE=./cmd/server` — verify green | pending | Tests pass |
| 2.3 | **cmd/store** — extract `Run(ctx, cfg)` into `bootstrap.go`; `schema_check.go` stays | pending | main.go <= 30 lines |
| 2.4 | `make test MODULE=./cmd/store` — verify green | pending | Tests pass |
| 2.5 | **cmd/consumer** — extract `Run(ctx, cfg)` into `bootstrap.go`; exchange builders into `exchanges.go`; replay into `replay.go` | pending | main.go <= 40 lines |
| 2.6 | `make test MODULE=./cmd/consumer` — verify green | pending | Tests pass |
| 2.7 | **cmd/processor** — extract `Run(ctx, cfg)` into `bootstrap.go`; sources into `sources.go`; insights wiring into `insights.go` | pending | main.go <= 40 lines |
| 2.8 | `make test MODULE=./cmd/processor` — verify green | pending | Tests pass |

**main.go target template:**

```go
func main() {
    cfgPath := flag.String("config", "", "config.jsonc path")
    // binary-specific flags...
    flag.Parse()

    cfg, err := bootstrap.LoadAndValidate(*cfgPath, withFlagOverrides(*addr, *logLevel))
    if err != nil {
        fmt.Fprintf(os.Stderr, "config: %v\n", err)
        os.Exit(1)
    }

    ctx := bootstrap.ContextWithSignal(context.Background())
    if err := Run(ctx, cfg); err != nil {
        fmt.Fprintf(os.Stderr, "run: %v\n", err)
        os.Exit(1)
    }
}
```

**What remains unique per bootstrap.go:**

| Binary | Unique in bootstrap.go |
|---|---|
| server | delivery subsystem factory, WS route setup, HTTP + WS server creation |
| store | ClickHouse writer + batch writer, schema validation, store JetStream consumer, observer Guardian |
| consumer | exchange runtime builders (binance/bybit), sequencer, recorder publisher, replay path |
| processor | envelope source init (JetStream/file/inmem), aggregation+insights services, codec registry |

**Commit per binary:** `refactor(cmd/server): extract composition root` etc.

---

### Phase 3 — E2E test-hooks consolidation
> **Agent:** `refactoring-specialist`

**Objective:** Extract duplicated E2E probe infrastructure into `internal/shared/testprobe/`.

| # | Task | Status | Deliverable |
|---|------|--------|-------------|
| 3.1 | Create `internal/shared/testprobe/probe.go` — shared Probe struct, `/healthz`, `/readyz`, `/metrics` routes | pending | ~80 lines |
| 3.2 | Create `internal/shared/testprobe/posture.go` — `HasE2EPosture()`, `LoopbackAddr()`, port helpers | pending | ~40 lines |
| 3.3 | Refactor `cmd/consumer/e2e_testhook.go` to use testprobe; keep only feed injection | pending | ~50% reduction |
| 3.4 | Refactor `cmd/processor/e2e_testhook.go` to use testprobe; keep only failure injection | pending | ~50% reduction |
| 3.5 | Run full E2E suite to confirm parity | pending | All E2E green |

**Commit:** `refactor(e2e): consolidate probe server into testprobe package`

---

### Phase 4 — Validation & green CI
> **Agent:** `test-writer` + `code-reviewer`

**Objective:** Full CI green, DoD verified, no regressions.

| # | Task | Status | Deliverable |
|---|------|--------|-------------|
| 4.1 | `make ci` full pipeline | pending | Green output |
| 4.2 | Grep audit: no adapter instantiation in `cmd/*/main.go` | pending | `grep -r "New.*Publisher\|NewEngine\|NewWriter" cmd/*/main.go` = empty |
| 4.3 | Line count: `wc -l cmd/*/main.go` — all <= 40 | pending | Output |
| 4.4 | Add `bootstrap_test.go` per binary: `Run(ctx, config.Load(""))` smoke test | pending | 4 test files |
| 4.5 | Update MEMORY.md with bootstrap patterns | pending | Memory updated |

**Commit:** `test(bootstrap): add composition root validation`

## Definition of Done

- [ ] No `cmd/*/main.go` instantiates adapters directly (only `bootstrap.*` or `Run()`)
- [ ] Dependency wiring in exactly 1 place per binary (`bootstrap.go`)
- [ ] Each `cmd/*/main.go` <= 40 lines
- [ ] `make ci` green (lint + test + registry-check + docs-check)
- [ ] All E2E tests pass — zero behavior change
- [ ] Tests can call `Run(ctx, fakeConfig)` for each binary's bootstrap
- [ ] Zero new Go modules (no go.mod/go.work changes)

## Rollback

Pure internal restructuring — no config schema changes, no protocol changes, no DB migrations. Every phase produces isolated commits. Rollback = `git revert` the phase commit(s).

## Evidence & Follow-up

| Artifact | When |
|---|---|
| `wc -l cmd/*/main.go` before/after | Phase 4 |
| `grep` adapter audit in cmd/ | Phase 4 |
| CI run link | Phase 4 |

| Follow-up | When |
|---|---|
| New binaries use `bootstrap.LoadAndValidate` + `Run()` pattern | Next binary added |
| Evaluate testprobe as separate module | If E2E grows |
