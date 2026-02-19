# Consumer Stall / No-Progress Runbook

## Trigger
- Alerts: `ConsumerLagGrowingNoProgress`, `ProcessGoroutinesHigh`, `ProcessHeapAllocHigh`

## Severity
- `P1` when `ConsumerLagGrowingNoProgress` firing (data pipeline stalled).
- `P2` when only resource alerts (`ProcessGoroutinesHigh`, `ProcessHeapAllocHigh`).

## First 5 Minutes
```bash
curl -fsS http://localhost:8081/runtime/snapshot | jq .
curl -fsS http://localhost:8081/metrics | rg 'bus_consumer_lag|store_commit_total|process_goroutines|process_heap_alloc_bytes'
curl -fsS http://localhost:8081/debug/pprof/goroutine?debug=1 | head -n 200
```

## Diagnose
- Confirm lag is growing while commits are zero:
```promql
slo:consumer_lag:deriv_5m
sum(rate(store_commit_total{status="ok"}[5m]))
bus_consumer_lag{bus_type="jetstream"}
```
- Check if store process is alive and ready:
```bash
curl -fsS http://localhost:8083/readyz
curl -fsS http://localhost:8083/metrics | rg 'store_commit_total|store_flush_total'
```
- Check ClickHouse connectivity from store:
```bash
docker exec market-raccoon-clickhouse clickhouse-client --query "SELECT 1"
```
- Check goroutine profile for blocked operations:
```bash
curl -fsS http://localhost:8081/debug/pprof/goroutine?debug=1 | rg -A5 'Fetch|Pull|Wait|chan send|chan receive'
```
- Check NATS JetStream consumer info:
```bash
docker exec market-raccoon-nats nats consumer info market-raccoon market-raccoon-processor
docker exec market-raccoon-nats nats consumer info market-raccoon market-raccoon-store
```

## Mitigate
- If ClickHouse is down: restart ClickHouse, then verify store commits resume.
- If store is stuck: restart store process; durable consumer will resume from last ACK.
- If processor goroutines are blocked: restart processor; check result channel deadlock.
- If NATS is unreachable: check NATS container health, restart if needed.
- Do not force-ACK messages; always preserve ack-on-commit semantics.

## Escalate
- Escalate to on-call SRE if `ConsumerLagGrowingNoProgress` persists for 10m after mitigation.
- Escalate to platform owner if goroutine count exceeds 50 000 or heap exceeds 1 GB.
