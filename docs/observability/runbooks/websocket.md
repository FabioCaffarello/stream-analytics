# Websocket Runbook

## Trigger
- Alerts: `SLODeliveryLatencyBurnRateFast`, `SLODeliveryLatencyBurnRateSlow`

## Severity
- `P1` for fast burn-rate.
- `P2` for slow burn-rate only.

## First 5 Minutes
```bash
curl -fsS http://localhost:8080/runtime/snapshot | jq .
curl -fsS http://localhost:8080/metrics | rg 'slo:delivery_latency|ws_send_latency_ms|ws_queue_depth|ws_drops_total'
curl -fsS http://localhost:8080/debug/pprof/goroutine?debug=1 | head -n 120
```

## Diagnose
```promql
slo:delivery_latency:burn_rate_5m
slo:delivery_latency:burn_rate_1h
slo:delivery_latency:error_ratio
histogram_quantile(0.99, sum(rate(ws_send_latency_ms_bucket[5m])) by (le))
```

## Mitigate
- Reduce outbound batch size and per-client fanout.
- Isolate slow consumers.
- Keep deterministic ordering and replay compatibility.

## Escalate
- Escalate to delivery/runtime owner if p99 remains above target for 15m.
