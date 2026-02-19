# Ingest Runbook

## Trigger
- Alerts: `SLOIngestBurnRateFast`, `SLOIngestBurnRateSlow`

## Severity
- `P1` when fast burn-rate firing.
- `P2` when only slow burn-rate firing.

## First 5 Minutes
```bash
curl -fsS http://localhost:8080/runtime/snapshot | jq .
curl -fsS http://localhost:8080/metrics | rg 'slo:ingest|ingest_messages_total|ingest_drop_total|ingest_nak_total|ingest_term_total'
curl -fsS http://localhost:8080/debug/pprof/goroutine?debug=1 | head -n 120
```

## Diagnose
- Confirm affected `stream` and `venue` via:
```promql
slo:ingest:burn_rate_5m
slo:ingest:burn_rate_1h
slo:ingest:error_ratio
```
- Correlate ingest outcomes:
```promql
sum by (reason) (rate(ingest_drop_total[5m]))
sum by (reason) (rate(ingest_nak_total[5m]))
sum by (reason) (rate(ingest_term_total[5m]))
```

## Mitigate
- Reduce upstream load or pause high-volume stream input.
- Drain/restart consumer shard with persistent drops.
- Keep replay mode deterministic; do not bypass ack-on-commit.

## Escalate
- Escalate to on-call SRE if `SLOIngestBurnRateFast` persists for 10m.
- Escalate to data/platform owner if `slo:data_loss:drop_rate_5m > 0`.
