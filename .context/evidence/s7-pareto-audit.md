# S7 Pareto Audit — Escalabilidade, Performance, Observabilidade & Manutenção

**Data:** 2026-02-17
**Branch:** `feat/w4-w5-extreme-runtime`
**Auditor:** Claude Opus 4.6 (SRE/Staff)

---

## A) Pareto Board (12 itens, ordenados por ROI)

| # | Item | Impacto | Prob. | Custo | ROI | Blast Radius | Evidência | Fix mínimo |
|---|------|---------|-------|-------|-----|--------------|-----------|------------|
| 1 | **Bench pipeline incompleto**: ingest, orderbook, hash não estão no `make bench-hotpath` / `bench-check` | 5 | 5 | S | 25 | Baixo (aditivo) | `Makefile:266` só roda codec+policykit; `ingest_bench_test.go`, `orderbook_bench_test.go`, `hash_test.go` existem mas sem baseline | Estender `bench-hotpath`/`bench-baseline`/`bench-check` para incluir todos os hot-paths |
| 2 | **Telemetry maps sem cap**: `byTicker`, `depthGapsBySymbol`, `lastDepthFinalBySymbol` crescem sem limite | 5 | 4 | S | 20 | Baixo (localizado em `telemetry.go`) | `telemetry.go:20-22` — maps unbounded; 10k+ tickers possíveis | Cap maps a N (e.g., 2048); evict LRU ou oldest |
| 3 | **wsQueue drop_oldest O(n)**: `items = items[1:]` realoca slice a cada drop | 4 | 5 | S | 20 | Baixo (localizado em `backpressure_queue.go`) | `backpressure_queue.go:71,94` — O(n) copy a cada eviction | Ring buffer com head/tail pointers |
| 4 | **PolicyKit policyLevels map unbounded**: `partition = type|venue|instrument` cresce sem limites | 4 | 4 | S | 16 | Baixo (localizado em `processor.go`) | `processor.go:277-278` — map key é `type|venue|instrument` | Cap a 4096 entries com eviction |
| 5 | **BusDroppedTotal subscriber_id cardinality**: se subscribers churnam, cardinality explode | 4 | 3 | S | 12 | Baixo (label fix) | `metrics.go:69-75` — `subscriber_id` label pode crescer | Já usa bucket (`bucketSubscriberID`) — OK, mas validar sob churn |
| 6 | **Backpressure `isDepthWSMessage` usa `bytes.Contains` no hot path** | 3 | 4 | S | 12 | Baixo (localizado) | `backpressure_queue.go:121-126` — O(n) string scan cada enqueue sob pressão | Pré-classificar na parse; ou usar type tag no WsMessage |
| 7 | **Processor consumeLoop → mailbox unbounded**: envelopes da channel vão direto para mailbox do actor | 4 | 3 | M | 6 | Médio (requer mudança em actor protocol) | `processor.go:214-227` — `engine.Send()` unbounded se processing lento | Backpressure via bounded send ou batch drain |
| 8 | **WS Consumer retryTimestamps unbounded slice** | 3 | 3 | S | 9 | Baixo | `ws/consumer.go` — retryTimestamps slice sem cap | Bounded ring buffer (similar a retry_budget.go) |
| 9 | **Ausência de WS backpressure test determinístico** | 4 | 4 | M | 8 | Baixo (aditivo) | Nenhum test para queue saturation + concurrent write safety | Criar `TestWSQueue_BackpressureBurst` + `TestWSQueue_ConcurrentEnqueuePop` |
| 10 | **JetStream per-message heartbeat goroutine** | 4 | 3 | L | 4 | Alto (requer refactor no consumer) | `jetstream/consumer.go:277-310` — 1 goroutine/msg × 1000msg/s = explosion | Single heartbeat manager com refcount |
| 11 | **Store batcher `estimatePayloadSize` json.Marshal per enqueue** | 3 | 3 | M | 4.5 | Médio | `cmd/store/batcher.go` — JSON marshal para estimar tamanho | Usar `len(payload)` ou heurística baseada em tipo |
| 12 | **interfaces/ws sem connection limits** | 3 | 2 | M | 3 | Médio | `interfaces/ws/server.go` — CheckOrigin: true, no max sessions | Adicionar max_sessions config + rate limit no upgrade |

---

## B) Plano de Execução

### Sprint S7-A — Guardrails de Performance (3 PRs)

| PR | Tema | DoD | Risco | Rollback |
|----|------|-----|-------|----------|
| **S7-PR1** | Bench pipeline completo | `make bench-hotpath` cobre codec+policykit+ingest+orderbook+hash; `make bench-baseline` gera baseline para todos; `make bench-check` detecta regressão ≥15% em qualquer hot-path | Baixo — aditivo | Revert commit |
| **S7-PR2** | wsQueue ring buffer + benchmark | `BenchmarkWSQueue_*` mostra 0 allocs em steady-state; Enqueue drop_oldest é O(1); race test passa | Baixo — mudança localizada | Revert commit |
| **S7-PR3** | Telemetry map caps + cardinality test | Maps capped a 2048 entries; `TestTelemetry_BoundedCardinality` valida cap; nenhuma mudança em métricas Prometheus | Baixo — mudança localizada | Revert commit |

### Sprint S7-B — WS + JetStream Confidence (3 PRs)

| PR | Tema | DoD | Risco | Rollback |
|----|------|-----|-------|----------|
| **S7-PR4** | WS backpressure deterministic tests | `TestWSQueue_BackpressureBurst_1000` + `TestWSQueue_ConcurrentEnqueuePop_Race` passam com -race | Baixo — aditivo | Revert commit |
| **S7-PR5** | Processor policyLevels bounded + test | Map capped; test valida eviction sob carga | Baixo — localizado | Revert commit |
| **S7-PR6** | isDepthWSMessage otimização | Benchmarks antes/depois; mover classificação para parse phase | Médio — toca hot path | Revert commit; fallback para bytes.Contains |

---

## C) Gates obrigatórios por PR

```bash
make registry-check
make invariants-check
make test-workspace
make test-workspace-race
make test-replay-golden
make lint
```

---

## Invariantes preservadas

- ack-on-commit: nenhuma mudança no JetStream consumer flow
- idempotência: hash não é modificado, só benchmarkado
- determinismo codec/replay: codec path intocado
- domain isolation protobuf-free: nenhum import de proto em domain/app
- registry inviolável: nenhuma mudança no subject-registry.yaml
- cardinalidade de métricas: nenhum label novo adicionado; caps adicionados
