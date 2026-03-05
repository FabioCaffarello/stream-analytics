# Signals/Strategist Cutover Runbook

**Status:** Active
**Last updated:** 2026-03-05

## Purpose

Operate a safe topology cutover for `signals` and `strategist` with explicit
fallback controls, without relying on legacy embedded runtime paths by default.

## Topology Contract

Primary topology (dedicated services):
- `cmd/signals` (`compose-signals-1`)
- `cmd/strategist` (`compose-strategist-1`)

Fallback topology (embedded paths, controlled by flags):
- processor embedded signal subsystem: `processor.signals.enabled`
- server embedded strategist/composer path: `signals.use_composer`

Authoritative decision: `docs/adrs/ADR-0021-signals-strategist-dedicated-topology-cutover.md`

## Runtime Controls

Dedicated mode (recommended):

```jsonc
// deploy/configs/processor.jsonc
"processor": {
  "signals": { "enabled": false }
}

// deploy/configs/server.jsonc
"signals": {
  "use_composer": false
}
```

Dedicated services input contracts:
- `signals` filters: `marketdata.>`, `aggregation.>`, `insights.>`, `liquidity.>`
- `strategist` filters: `insights.>`

Do not use `evidence.>` as filter root.

## Rollout Stages

1. Stage A: dual-ready baseline
- Dedicated services healthy in compose.
- Embedded processor/server paths still available as fallback flags.

2. Stage B: dedicated active
- `processor.signals.enabled=false`
- `signals.use_composer=false`
- Dedicated `signals/strategist` stay healthy and process expected subjects.

3. Stage C: operational hardening
- Quality gates green.
- Runbook and evidence updated.

## Validation Gate

Runtime:

```bash
make up PROCESSOR_REPLICAS=2
make ps
make smoke
```

Expectations:
- No core service in `Restarting`.
- `compose-signals-1` and `compose-strategist-1` are `Up ... (healthy)`.
- `compose-processor-*` logs include:
  - `processor: embedded signals subsystem disabled`

Logs:

```bash
docker logs --tail 120 compose-signals-1
docker logs --tail 120 compose-strategist-1
docker logs --tail 120 compose-processor-1
docker logs --tail 120 compose-processor-2
```

Client check:

```bash
# UI should load WASM and API discovery should be healthy
curl -fsS http://127.0.0.1:8090/api/v1/markets
```

## Regression Suite (Cutover Focus)

```bash
go test ./internal/actors/signal/runtime -run 'TestSignalSubsystem_OwnerOnlyEmitsAcrossReplicas_WithReplayDuplicates|TestSignalSubsystem_WatermarkRegressionDropsAsOutOfOrder' -count=1
go test ./internal/actors/signals/runtime -run 'TestSignalsSubsystem_ReplicaCount2_NoDoubleEmit_WithDuplicateInput|TestSignalsSubsystem_WatermarkOutOfOrderDropped|TestSignalsSubsystem_ReplayDeterministic' -count=1
go test ./internal/shared/config
go test ./cmd/processor -run FanOutEnvelopeStream -count=1
```

Quality gates:

```bash
make fmt-check
make lint
make test-short
```

## Rollback

Fast rollback to embedded path (if dedicated services become unstable):

1. Set flags:
- `processor.signals.enabled=true`
- `signals.use_composer=true`

2. Re-deploy/restart services.

3. Re-run:
- `make ps`
- `make smoke`

4. Confirm no dual path conflict in logs:
- keep only one active topology per subsystem during incident mitigation.

## Evidence

- `.context/evidence/m3-runtime-validation-2026-03-05.md`
- `.context/evidence/m4-cutover-hardening-2026-03-05.md`

