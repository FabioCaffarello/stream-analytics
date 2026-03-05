## M4 Cutover Hardening: Dedicated Signals/Strategist Topology
Date: 2026-03-05

### Goal
- Evitar topologia dupla no runtime e consolidar cutover para `cmd/signals` + `cmd/strategist`.
- Preservar ownership/monotonicidade com regressao focada.

### Changes Applied
- Feature flag de cutover no processor:
  - `internal/shared/config/schema.go`
  - novo `processor.signals.enabled` (`nil => true` para backward compatibility)
  - helper `ProcessorSignalsConfig.IsEnabled()`.
- Wiring condicional no processor:
  - `cmd/processor/bootstrap.go`
  - fan-out agora recebe `includeSignal` para evitar envio para canal sem consumidor.
  - guardian inclui `SubsystemSignals` somente quando `processor.signals.enabled=true`.
  - log explicito quando desabilitado.
- Config de compose para modo dedicado:
  - `deploy/configs/processor.jsonc`
  - `processor.signals.enabled=false`.
- Governanca de arquitetura:
  - `docs/adrs/ADR-0021-signals-strategist-dedicated-topology-cutover.md`
  - `docs/architecture/decisions.md` atualizado com ADR-0021.
  - `docs/architecture/subsystems.md` alinhado para `insights.*`/`liquidity.*` e topologia de cutover.
  - `docs/operations/signals-strategist-cutover.md` com procedimento de rollout/rollback.

### Test and Validation
- Unit/config:
  - `go test ./internal/shared/config` -> `ok`
  - `go test ./cmd/processor -run FanOutEnvelopeStream -count=1` -> `ok`
- Ownership/monotonic targeted regression:
  - `go test ./internal/actors/signal/runtime -run 'TestSignalSubsystem_OwnerOnlyEmitsAcrossReplicas_WithReplayDuplicates|TestSignalSubsystem_WatermarkRegressionDropsAsOutOfOrder' -count=1` -> `ok`
  - `go test ./internal/actors/signals/runtime -run 'TestSignalsSubsystem_ReplicaCount2_NoDoubleEmit_WithDuplicateInput|TestSignalsSubsystem_WatermarkOutOfOrderDropped|TestSignalsSubsystem_ReplayDeterministic' -count=1` -> `ok`
- Runtime:
  - `make up PROCESSOR_REPLICAS=2` -> `ok`
  - `make smoke` -> `all endpoints are ready`
  - `make ps` -> core services `Up ... (healthy)` incluindo `signals` e `strategist`.
- Quality gates:
  - `make fmt-check` -> `ok`
  - `make lint` -> `ok` (0 issues)
  - `make test-short` -> `ok`
  - `make docs-check-full` -> `ok`
- Processor logs:
  - `processor: embedded signals subsystem disabled` confirmado em `compose-processor-1` e `compose-processor-2`.
- Client smoke via Playwright (`http://127.0.0.1:8090`):
  - `WASM loaded. ws=ws://127.0.0.1:8090/ws`
  - console sem erros/warnings
  - `GET /api/v1/markets` -> `200 OK`.

### Residual Risks
- Propagacao do novo flag para ambientes fora do compose local ainda depende de rollout operacional.
- Runbooks operacionais de cutover/reversao ainda precisam consolidacao final.
