# Stage 2 Boundary Hardening Report

**Status:** Implemented
**Date:** 2026-03-06
**Owner:** Architecture / Runtime Platform
**Relates to:** `docs/adrs/ADR-0023-frozen-semantic-model-feature-evidence-signal-intent-execution-portfolio.md`, `docs/architecture/semantic-hardening-stage1.md`

---

## Goal and Context

Execute boundary hardening after semantic freeze, removing embedded domain ownership from `processor` and `server` so runtime topology matches the target architecture.

Historical note:
- This report records what Stage 2 removed and what still remained transitional at that time.
- Later stages retired strategist legacy intake and introduced the canonical `strategy.intent -> execution.event -> portfolio.state` runtime.

Target alignment:

1. `processor`: aggregation / insights transforms only.
2. `server`: delivery + control-plane only.
3. `cmd/signals`: canonical `signal.event`.
4. `cmd/strategist`: still transitional composer path (`signal.composite`) until future `strategy.intent`.

---

## Embedded Paths That Existed

1. `processor` embedded signal runtime:
   - `cmd/processor/bootstrap.go` wired `internal/actors/signal/runtime`.
   - Gated by `processor.signals.enabled`.
2. `server` embedded evidence/composer runtime:
   - `cmd/server/bootstrap.go` wired `internal/actors/evidence/runtime` and `internal/actors/signals/runtime`.
   - Composer branch gated by `signals.use_composer`.

---

## Removed in Stage 2

1. Removed signal runtime wiring from `processor` guardian factories.
2. Removed envelope fan-out path dedicated to embedded signal runtime in `processor`.
3. Removed evidence runtime wiring from `server`.
4. Removed composer runtime wiring from `server`.
5. Removed deploy config knobs that implied embedded ownership in compose profiles:
   - `deploy/configs/processor.jsonc` no longer sets `processor.signals.enabled`.
   - `deploy/configs/server.jsonc` no longer carries `signals.use_composer` or server-side evidence section.

---

## What Remains Transitional

1. `signal.composite` still exists as compatibility stream via dedicated `cmd/strategist`.
2. Config schema still accepts legacy fields for compatibility (`processor.signals.enabled`, `signals.use_composer`), but they are now documented as deprecated/no-op.
3. `cmd/strategist` naming remains transitional until future `strategy.intent` BC.

---

## Legacy Risk Eliminated

1. Eliminated accidental dual-runtime ownership between dedicated services and embedded server/processor branches.
2. Eliminated hidden boundary violation where `server` hosted domain logic beyond delivery/control-plane.
3. Eliminated processor-side semantic leakage from aggregation into signal runtime ownership.

---

## Validation Executed

```bash
go test ./cmd/processor
go test ./cmd/server
go test ./internal/shared/config
make docs-check-fast
```
