# Seq Coherence RCA (2026-03-04)

## Scope
- Run: `artifacts/iq/20260304T181424Z`
- Topline objective metric: `delivery_router_coherence_violations_total{type="seq_non_monotonic"}`
- Replicas: `PROCESSOR_REPLICAS=2`

## Baseline before reason instrumentation
- Latest prior snapshot: `artifacts/iq/20260304T175435Z/logs/server.metrics.prom`
- `delivery_router_coherence_violations_total{type="seq_non_monotonic"} = 291`
- Per-stream attribution unavailable in that run (no sampled stream-key logs yet).

## Reason distribution (instrumented run)
From `artifacts/iq/20260304T181424Z/logs/server.metrics.prom`:

- `out_of_order_input`: **163**
- `replay_duplicate`: **6**
- `owner_change`: **0**
- `resync_overlap`: **0**
- `stale_event`: **0**
- `unknown`: **0**

Total `seq_non_monotonic`: **169**

## Path and stream evidence
### Router path
Sampled violation logs in `artifacts/iq/20260304T181424Z/logs/compose.server.log` show:
- origin: `router`
- processor_instance_id: `unknown` (not propagated in incoming envelope metadata)
- dominant stream key: `marketdata.trade/binance/BTCUSDT/raw`
- sampled reason: `out_of_order_input`

Observed sampled top-N set in this run:
- `marketdata.trade/binance/BTCUSDT/raw,out_of_order_input`: 15 sampled entries

### Session/gateway path
- No session-side seq coherence violation signatures found in `compose.server.log` for this run.
- No gateway-side seq rejection signatures found in this run.

### Processor-side context
From `artifacts/iq/20260304T181424Z/logs/compose.processor.log`:
- both `processor-1` and `processor-2` active and processing.
- periodic "deferring periodic snapshot tick while processor catches up" messages observed.
- no explicit owner-handoff markers in current logs.

## RCA conclusion
1. The dominant failure mode is **router-observed out-of-order trade input** on `marketdata.trade/binance/BTCUSDT/raw`.
2. Secondary mode is **replay duplicates**.
3. In this run, **owner_change** and **resync_overlap** were not dominant contributors.

## Fix focus selected
Implement smallest robust policy changes for top 1-2 reasons only:
- `out_of_order_input` (dominant)
- `replay_duplicate` (secondary)

Policy shape:
- single router-enforced `SeqPolicy` decision point
- early stale/duplicate drop without delivery emission
- owner/resync gating primitives remain present but are only activated when their signals are present
