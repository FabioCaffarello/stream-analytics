# ADR-0021 - Signals/Strategist Dedicated Topology Cutover

**Status:** Accepted
**Implementation status:** Implemented (Stage 2 boundary hardening)
**Partial marker:** Status: Implemented (Stage 2 boundary hardening)
**Owner:** Runtime Platform
**Last updated:** 2026-03-06
**Date:** 2026-03-05
**Deciders:** Chief Architect
**Relates to:** ADR-0018, ADR-0014, ADR-0015, `.context/plans/signals-strategist-entrypoint-hardening.md`

---

## Context

Signals and strategist behavior has existed in more than one runtime topology:

1. Embedded subsystem wiring inside existing entrypoints (`cmd/processor`, `cmd/server`).
2. Dedicated services (`cmd/signals`, `cmd/strategist`) running in compose.

This dual topology created ambiguity for rollout and increased risk of duplicated processing in dev/runtime cutovers.
Additionally, subject filter mismatch (`evidence.>`) in dedicated services caused startup failure because JetStream subject roots allow `insights.*` and `liquidity.*` rather than `evidence.*`.

The system needs an explicit, auditable cutover rule that preserves ownership/monotonic guarantees while avoiding legacy fallback by accident.

## Decision

1. Adopt dedicated services as primary runtime topology for signals and strategist.
2. Remove embedded runtime branches from `cmd/server` and `cmd/processor`; dedicated services are the only runtime topology.
3. Canonical input contracts for dedicated services:
- `cmd/signals` consumes from roots: `marketdata.>`, `aggregation.>`, `insights.>`, `liquidity.>`.
- `cmd/strategist` consumes from root: `insights.>`.
- `evidence.>` is not a valid JetStream root and must not be used.

## Consequences

- Positive:
- Clear ownership of topology and cutover intent in config.
- Eliminates risk of accidental dual-runtime behavior in compose.
- Startup contract failure on invalid subjects is resolved with explicit allowed roots.

- Negative:
- Legacy config keys remain for backward-compatible parsing (`processor.signals.enabled`, `signals.use_composer`) but are no-op.

## Implementation Matrix

| Capability | Status | Reference |
|---|---|---|
| Dedicated `cmd/signals` bootstrapped and healthy | Implemented | `cmd/signals/main.go`, `cmd/signals/bootstrap.go` |
| Dedicated `cmd/strategist` bootstrapped and healthy | Implemented | `cmd/strategist/main.go`, `cmd/strategist/bootstrap.go` |
| Subject filter contract corrected (`insights/liquidity` roots) | Implemented | `deploy/configs/signals.jsonc`, `deploy/configs/strategist.jsonc` |
| Embedded processor signal path removed from runtime wiring | Implemented | `cmd/processor/bootstrap.go` |
| Embedded server evidence/strategist path removed from runtime wiring | Implemented | `cmd/server/bootstrap.go` |
| Runbook alignment to dedicated-only topology | Implemented | `docs/operations/signals-strategist-cutover.md` |

## Evidence

- `.context/evidence/m3-runtime-validation-2026-03-05.md`
- `.context/evidence/m4-cutover-hardening-2026-03-05.md`
- `scripts/test/util/smoke-compose.sh` restart gate

## Changelog

- 2026-03-05:
- ADR created to fix topology ambiguity and define dedicated cutover contract.
- Added processor embedded-signal feature flag (`processor.signals.enabled`) for safe cutover.
- 2026-03-06:
- Stage 2 boundary hardening removed embedded runtime branches from `cmd/processor` and `cmd/server`.
- Dedicated services became the only supported runtime topology for signals/strategist.
