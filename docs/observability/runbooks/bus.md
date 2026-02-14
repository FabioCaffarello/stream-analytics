# Bus Runbook

## Trigger
- Alerts: `DataLossDropRateNonZero`, ingest/data-loss burn-rate alerts.

## Severity
- `P1` when drop rate is non-zero for 5m.
- `P2` for isolated retry/recovery events.

## First 5 Minutes
```bash
curl -fsS http://localhost:8080/runtime/snapshot | jq .
curl -fsS http://localhost:8080/metrics | rg 'bus_|slo:data_loss|ingest_drop_total|ws_drops_total'
curl -fsS http://localhost:8080/debug/pprof/goroutine?debug=1 | head -n 120
```

## Diagnose
```promql
slo:data_loss:drop_rate_5m
sum by (reason) (rate(ingest_drop_total[5m]))
sum by (reason) (rate(vpvr_drop_total[5m]))
sum by (reason) (rate(heatmap_drop_total[5m]))
sum by (reason) (rate(ws_drops_total[5m]))
sum by (subscriber_id) (rate(bus_dropped_total[5m]))
```

## Mitigate
- Reduce fanout pressure (throttle high-rate producers).
- Increase bounded queue headroom if configured.
- Restart only impacted consumer workers.

## Escalate
- Escalate to messaging owner if bus drops persist > 10m.
