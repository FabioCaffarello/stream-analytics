## M3 Runtime Validation: Signals/Strategist Entrypoints
Date: 2026-03-05

### Goal and Context
- Concluir M3 com bootstrap funcional para `cmd/signals` e `cmd/strategist`, removendo crash loop em compose.
- Validar runtime com `PROCESSOR_REPLICAS=2`, smoke gate e navegação no client (`:8090`).

### Implemented Changes
- `cmd/strategist/bootstrap.go`
  - `effectiveStrategistFilters`: troca de `evidence.>` para `insights.>` (subject root permitido).
- `cmd/signals/bootstrap.go`
  - `effectiveSignalFilters`: inclui `insights.>` e `liquidity.>` em vez de depender de `evidence.>`.
- `deploy/configs/signals.jsonc`
  - `jetstream.filter_subjects`: `["marketdata.>", "aggregation.>", "insights.>", "liquidity.>"]`.
- `deploy/configs/strategist.jsonc`
  - `jetstream.filter_subjects`: `["insights.>"]`.

### Validation Commands
- `gofmt -w cmd/signals/bootstrap.go cmd/strategist/bootstrap.go`
- `go test ./cmd/signals/...` -> `ok` (`[no test files]`)
- `go test ./cmd/strategist/...` -> `ok` (`[no test files]`)
- `make up PROCESSOR_REPLICAS=2` -> build/recreate concluído com sucesso
- `make ps` -> `signals` e `strategist` em `Up ... (healthy)` (sem `Restarting`)
- `make smoke` -> `all endpoints are ready`
- `make fmt-check` -> `ok`
- `make lint` -> `ok` (0 issues nos módulos)
- `make test-short` -> `ok` (incluindo `cmd/signals` e `cmd/strategist`)

### Runtime Evidence
- `docker logs compose-signals-1`:
  - subscribed durable `signals-v1-signals`
  - filters `["marketdata.>","aggregation.>","insights.>","liquidity.>"]`
  - HTTP listening `:8084`
- `docker logs compose-strategist-1`:
  - subscribed durable `strategist-v1-strategist`
  - filters `["insights.>"]`
  - HTTP listening `:8085`

### Client/Playwright Evidence (`http://127.0.0.1:8090`)
- Snapshot inicial: `Loading WASM...`
- Após carregamento: `WASM loaded. ws=ws://127.0.0.1:8090/ws`
- Console: `0` erros / `0` warnings
- Network: `GET /api/v1/markets` -> `200 OK`

### Risks and Notes
- ADR de topologia final (`signals/strategist` dedicado vs híbrido) permanece pendente em M2.
- Suite curta passou, mas testes específicos de regressão de ownership/monotonicidade para o cutover final de M4 ainda faltam.
