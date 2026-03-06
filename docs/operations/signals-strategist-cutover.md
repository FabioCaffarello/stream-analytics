# Signals/Strategy/Execution/Portfolio Topology Runbook

**Status:** Active
**Last updated:** 2026-03-06

## Purpose

Operate `signals`, `strategist`, `executor`, and `portfolio` as dedicated services only, aligned with Stage 2 boundary hardening plus Stage 4 runtime bootstrap.
Stage 6 retires `signal.composite` from strategist runtime intake and keeps the canonical chain only.
Stage 7 adds an opt-in safe real-adapter cut-in for executor behind the same boundary.
Stage 8 expands safe pilot lifecycle/reconciliation in the same boundary without changing contracts.
Stage 9A inserts explicit execution governance before adapter use.

## Topology Contract

Dedicated topology:
- `cmd/signals` (`compose-signals-1`)
- `cmd/strategist` (`compose-strategist-1`)
- `cmd/executor` (`compose-executor-1`)
- `cmd/portfolio` (`compose-portfolio-1`)

Embedded processor/server fallback paths were removed from runtime wiring in Stage 2.

Authoritative docs:
- `docs/adrs/ADR-0023-frozen-semantic-model-feature-evidence-signal-intent-execution-portfolio.md`
- `docs/architecture/semantic-hardening-stage1.md`
- `docs/architecture/boundary-hardening-stage2.md`
- `docs/architecture/runtime-bootstrap-stage4.md`
- `docs/architecture/legacy-retirement-stage6.md`
- `docs/architecture/real-adapter-integration-stage7.md`
- `docs/architecture/real-execution-lifecycle-stage8.md`
- `docs/architecture/execution-governance-stage9a.md`

## Runtime Controls

Dedicated services input contracts:
- `signals` filters: `marketdata.>`, `aggregation.>`, `insights.>`, `liquidity.>`
- `strategist` filters (default): `signal.event.>`
- `executor` filters: `strategy.intent.>`
- `portfolio` filters: `execution.event.>`

Do not use `evidence.>` as filter root.
Do not use `signal.>` broad filter for strategist.

### Executor Mode Controls (Stage 8)

Default mode:
- `execution.mode=bootstrap_simulated`
- `execution.adapter=bootstrap.simulated`

Opt-in safe real adapter:
- `execution.mode=real_adapter_safe`
- `execution.adapter=binance.spot`
- `execution.real.enabled=true`
- `execution.real.binance.trade_api.endpoint_mode=test_order` (validation-only)
- `execution.real.binance.trade_api.endpoint_mode=safe_order_lifecycle` (lifecycle pilot)
- `execution.allowed_venues` and `execution.allowed_symbols` must be non-empty allowlists
- `execution.trade_only=true` and `execution.safe_mode=true`
- Stage 9A governance evaluates explicit grant scope before adapter selection:
  - no grant -> deny
  - adapter outside selected route -> deny
  - missing credential availability -> deny
  - venue/symbol/account outside scope -> deny
- lifecycle mode requires deterministic bounded reconciliation:
  - `execution.real.binance.trade_api.reconcile_enabled=true`
  - `execution.real.binance.trade_api.reconcile_poll_interval`
  - `execution.real.binance.trade_api.reconcile_max_polls`

Credentials boundary:
- `execution.real.binance.trade_api.api_key_env`
- `execution.real.binance.trade_api.api_secret_env`

Credentials are loaded only from env vars and are trade-only by contract.
Stage 9A resolves them through a dedicated credential boundary before execution.
Withdraw/custody semantics are not part of Stage 7.

### Retired Legacy Signal (`signal.composite`)

`signal.composite` is retired from strategist operational intake in Stage 6.
Any residual `signal.composite` handling is compatibility-only for historical replay/read paths and must not be used as decision input.

## Validation Gate

Runtime:

```bash
make up PROCESSOR_REPLICAS=2
make ps
make smoke
```

Expectations:
- No core service in `Restarting`.
- `compose-signals-1`, `compose-strategist-1`, `compose-executor-1`, and `compose-portfolio-1` are `Up ... (healthy)`.

Logs:

```bash
docker logs --tail 120 compose-signals-1
docker logs --tail 120 compose-strategist-1
docker logs --tail 120 compose-executor-1
docker logs --tail 120 compose-portfolio-1
docker logs --tail 120 compose-processor-1
docker logs --tail 120 compose-processor-2
```

Cutover monitoring checks:
- strategist publishes `strategy.intent` only from `signal.event` input semantic;
- execution rejects are visible via `meta.execution_status=rejected` and deterministic `meta.execution_reason`;
- execution rejects expose `meta.execution_reason_category` to distinguish governance, credentials, adapter selection, execution-policy, and venue/runtime failures;
- execution events expose boundary markers `meta.execution_boundary`, `meta.execution_adapter`, and `meta.execution_mode`;
- bootstrap mode events expose `meta.execution_mode=bootstrap_simulated`;
- real safe mode events expose `meta.execution_mode=real_adapter_safe`;
- lifecycle mode emits explicit `execution_status` progression (`placed`, `partially_filled`, terminal state);
- portfolio projections include `meta.source_execution_status`.

Client check:

```bash
curl -fsS http://127.0.0.1:8090/api/v1/markets
```

## Regression Suite (Topology Focus)

```bash
go test ./internal/actors/signal/runtime -run 'TestSignalSubsystem_OwnerOnlyEmitsAcrossReplicas_WithReplayDuplicates|TestSignalSubsystem_WatermarkRegressionDropsAsOutOfOrder' -count=1
go test ./internal/actors/strategy/runtime -count=1
go test ./internal/actors/execution/runtime -count=1
go test ./internal/actors/portfolio/runtime -count=1
go test ./internal/actors/runtime -run 'TestBootstrapFlow_SignalToStrategyToExecutionToPortfolio|TestBootstrapFlow_ExpiredIntentProjectsRejectedPortfolioState|TestBootstrapFlow_RealAdapterSafeAcceptedProjectsPendingPortfolioState' -count=1
go test ./internal/actors/runtime -run 'TestBootstrapFlow_RealAdapterSafeLifecycleReconcilesToFilledPortfolioState' -count=1
go test ./internal/adapters/execution/... ./cmd/executor ./internal/shared/config -count=1
go test ./cmd/processor -count=1
go test ./cmd/server -count=1
```

Quality gates:

```bash
make fmt-check
make lint
make test-short
```

## Rollback

Rollback no longer uses embedded processor/server domain branches.

If dedicated `signals`/`strategist`/`executor`/`portfolio` become unstable:
1. Roll back service images/config revisions.
2. Keep one active dedicated topology version per service.
3. Re-run `make ps` and `make smoke`.

## Evidence

- `.context/evidence/m3-runtime-validation-2026-03-05.md`
- `.context/evidence/m4-cutover-hardening-2026-03-05.md`
