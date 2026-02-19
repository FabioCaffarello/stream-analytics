# Guardian Runbook

## Trigger
- Alerts: `SLODataLossBurnRateFast`, `SLODataLossBurnRateSlow`, `ColdPathCommitErrorsNonZero`

## Severity
- `P1` when commit errors are non-zero.
- `P2` when slow burn-rate only.

## First 5 Minutes
```bash
curl -fsS http://localhost:8080/runtime/snapshot | jq .
curl -fsS http://localhost:8080/metrics | rg 'slo:data_loss|vpvr_writer_write_fail_total|bus_publish_errors_total|guardian_'
curl -fsS http://localhost:8080/debug/pprof/goroutine?debug=1 | head -n 120
```

## Diagnose
```promql
slo:data_loss:burn_rate_5m
slo:data_loss:burn_rate_1h
slo:data_loss:cold_commit_errors_rate_5m
sum(rate(vpvr_writer_write_fail_total[5m]))
sum(rate(bus_publish_errors_total[5m]))
```

## Mitigate
- Stop non-essential writers causing commit errors.
- Keep ack-on-commit contract; do not ACK before durable write.
- Trigger bounded replay for impacted stream partition.

## Escalate
- Escalate to storage and SRE owners when cold commit errors persist > 5m.
