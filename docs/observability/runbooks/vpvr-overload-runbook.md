# VPVR Overload Runbook

## Trigger
- `SLODataLossBurnRateFast`/`Slow`
- elevated `vpvr_overload_level`
- increasing `vpvr_drop_total`

## Severity
- `P1` if data-loss burn-rate alert is firing.
- `P2` if overload level is elevated with no drops.

## First 5 Minutes
```bash
curl -fsS http://localhost:8080/runtime/snapshot | jq .
curl -fsS http://localhost:8080/debug/pprof/goroutine?debug=1 | head -n 200
curl -fsS http://localhost:8080/metrics | rg 'vpvr_|heatmap_|slo:data_loss|ws_queue_depth|bus_consumer_lag'
```

## Diagnose
- Use recording rules first:
```promql
slo:data_loss:burn_rate_5m
slo:data_loss:burn_rate_1h
slo:data_loss:drop_rate_5m
slo:data_loss:cold_commit_errors_rate_5m
```
- Use bounded VPVR/Heatmap buckets (no raw instrument/timeframe):
```promql
max by (venue, instrument_bucket, timeframe_bucket) (vpvr_overload_level)
sum by (reason) (rate(vpvr_drop_total[5m]))
max by (venue, instrument_bucket, timeframe_bucket) (heatmap_queue_depth)
```

## Mitigate
- Apply overload policy and reduce per-window churn.
- Lower ingest pressure for affected venue stream.
- Preserve ack-on-commit and replay determinism.

## Escalate
- Escalate to insights owner when overload persists > 10m.
- Escalate to storage owner when `slo:data_loss:cold_commit_errors_rate_5m > 0`.

## Checklist
- [ ] `slo:data_loss:burn_rate_5m` returned below threshold.
- [ ] `vpvr_overload_level` returned to baseline.
- [ ] `vpvr_drop_total` no longer increasing.
- [ ] queue/backlog stabilized (`ws_queue_depth`, `bus_consumer_lag`).
