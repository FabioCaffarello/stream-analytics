# SLO / SLI (Market-Raccoon)

## SLO-1: Ingest Success Ratio
- Objective (30d): `99.9%`
- SLI: successful ingest ratio (`ok / total`) from ingest pipeline.
- PromQL (reference):
```promql
sum(rate(ingest_messages_total{status="ok"}[5m]))
/
clamp_min(sum(rate(ingest_messages_total[5m])), 1e-9)
```
- Error budget (30d): `0.1%`
- Burn-rate thresholds:
  - Fast: `5m/1h > 14.4`
  - Slow: `30m/6h > 6`

## SLO-2: Delivery Latency p99 (WS/HTTP)
- Objective (30d): `99%` of deliveries under `250ms` (p99 guardrail).
- SLI: good latency ratio from delivery histogram.
- PromQL (reference):
```promql
sum(rate(ws_send_latency_ms_bucket{le="250"}[5m]))
/
clamp_min(sum(rate(ws_send_latency_ms_count[5m])), 1e-9)
```
- Error budget (30d): `1%`
- Burn-rate thresholds:
  - Fast: `5m/1h > 14.4`
  - Slow: `30m/6h > 6`

## SLO-3: Data Loss Guard
- Objective (30d): `99.99%` (guardrail for `drop_total == 0` and `cold-path commit errors == 0`).
- SLI: no data-loss symptoms and no cold-path commit errors.
- PromQL (reference):
```promql
(
  sum(rate(ingest_drop_total[5m]))
+ sum(rate(ingest_nak_total[5m]))
+ sum(rate(ingest_term_total[5m]))
+ sum(rate(vpvr_drop_total[5m]))
+ sum(rate(heatmap_drop_total[5m]))
+ sum(rate(ws_drops_total[5m]))
+ sum(rate(backpressure_drops_total[5m]))
+ sum(rate(bus_dropped_total[5m]))
+ sum(rate(policykit_drop_total[5m]))
+ sum(rate(vpvr_writer_write_fail_total[5m]))
+ sum(rate(bus_publish_errors_total[5m]))
)
/
clamp_min(sum(rate(ingest_messages_total[5m])), 1e-9)
```
- Error budget (30d): `0.01%`
- Burn-rate thresholds:
  - Fast: `5m/1h > 14.4`
  - Slow: `30m/6h > 6`
