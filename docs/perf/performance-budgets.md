# Performance Budgets

## Pipeline Throughput

- Target: >= 83,000 events/sec (10M events in < 120s)
- Measured: Pending soak run evidence in `.context/evidence/c4-pipeline-soak.txt`

## Memory Budgets

| Component | Budget | Soak Test |
|-----------|--------|-----------|
| 1M pipeline (single exchange) | <= 512 MB heap delta | TestSoak_FullPipeline_1M_Messages |
| 10M pipeline (4 exchanges) | <= 1 GB heap delta | TestSoak_MultiExchange_10M_Messages |
| Pipeline + delivery (100k) | <= 256 MB heap delta | TestSoak_PipelineWithDelivery_100k |

## Goroutine Budgets

| Component | Max Drift | Soak Test |
|-----------|-----------|-----------|
| Pipeline (no delivery) | <= 32 | TestSoak_FullPipeline_1M_Messages |
| Pipeline (4 exchanges) | <= 48 | TestSoak_MultiExchange_10M_Messages |
| Pipeline + delivery | <= 48 | TestSoak_PipelineWithDelivery_100k |
| WS delivery (50 clients) | <= 96 | TestSoak_WSDelivery_SlowClients |

## Latency Budgets

| Path | p95 | p99 | Source |
|------|-----|-----|--------|
| Ingest (parse->envelope) | <= 500 us | < 10 ms | PRD-0001 (p99 < 10ms is the authoritative product target) |
| E2E (ingest->orderbook snapshot) | <= 15 us/op | - | BenchmarkE2E_IngestToOrderbookSnapshot |
| E2E (trade->candle) | <= 20 us/op | - | BenchmarkE2E_TradeToCandle |
| Cold-path commit | <= 10 ms | <= 25 ms | TestStoreSoak_ColdPathLatencyBudgets |
| VPVR policy decision | <= 2 ms | <= 5 ms | TestVPVROverloadSoakBurstDeterministicBudgets |

## Cardinality Budgets

| Resource | Max | Enforced By |
|----------|-----|-------------|
| Active orderbooks | 4,096 | BoundedMap eviction |
| Active candles | 50,000 | BoundedMap eviction |
| Active stats windows | 50,000 | BoundedMap eviction |
| Active instrument streams | 4,096 | BoundedMap eviction |
