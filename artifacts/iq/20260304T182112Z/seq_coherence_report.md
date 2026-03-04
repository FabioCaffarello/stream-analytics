# Seq Coherence IQ Report (2026-03-04)

## Objective
Drive `delivery_router_coherence_violations_total{type="seq_non_monotonic"}` to near-zero under `PROCESSOR_REPLICAS=2` with deterministic guardrails.

## Evidence snapshots

| Snapshot | Source file | seq_non_monotonic | Notes |
|---|---|---:|---|
| Baseline (pre-reason instrumentation) | `artifacts/iq/20260304T175435Z/logs/server.metrics.prom` | 291 | No per-stream reason breakdown available. |
| Instrumented RCA run (pre-fix) | `artifacts/iq/20260304T181424Z/logs/server.metrics.prom` | 169 | Reasons: out_of_order_input=163, replay_duplicate=6. |
| Interim policy run | `artifacts/iq/20260304T181919Z/logs/server.metrics.prom` | 336 | Stale window too narrow; many remained out_of_order_input. |
| Final policy run | `artifacts/iq/20260304T182112Z/logs/server.metrics.prom` | **0** | Target met. Rejections now classified as stale_event (269). |

## Final reason/rejection distribution (latest)
From `artifacts/iq/20260304T182112Z/logs/server.metrics.prom`:

- `delivery_router_coherence_violations_total{type="seq_non_monotonic",reason="*"}`: **0 across all reasons**
- `delivery_router_events_rejected_total{reason="stale_event"}`: **269**
- `delivery_router_events_rejected_total{reason="seq_non_monotonic"}`: **0**
- `delivery_router_events_rejected_total{reason="replay_duplicate"}`: **0**

Sampled streams in latest run (`compose.server.log`):
- `marketdata.trade/binance/BTCUSDT/raw` (stale_event)
- `insights.microstructure_evidence/binance/BTCUSDT/raw` (stale_event)

## Outcome
- Objective metric (`seq_non_monotonic`) is eliminated in the latest run under 2 replicas.
- Residual non-accept traffic is explicitly explained as deterministic stale drops with bounded reason labels.
- Guardrail is centralized in one router SeqPolicy enforcement point.
