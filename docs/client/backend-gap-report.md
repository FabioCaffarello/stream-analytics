# Backend Gaps Detected (Client-Side)

## Scope
This report is generated from Odin client-only telemetry and diagnostics. No backend behavior was changed.

## Detection Signals
The client records and exposes these counters in runtime metrics and `Copy Diagnostics`:
- `backend_gap_no_metrics`: backend stopped sending `METRICS` for longer than the stale window.
- `backend_gap_pong_timeout`: no `PONG` received inside timeout window.
- `backend_gap_resync_ack_timeout`: `RESYNC` was sent but no snapshot/ack completion before timeout.
- `backend_gap_missing_ts_server`: data frames arrived without `ts_server`.
- `backend_gap_seq_gap_recurring`: recurring sequence gaps on the same stream.
- `backend_gap_frequent_drops`: frequent client drop pressure (`drop_count`) in a short window.

Related evidence fields:
- `transport_state`, `ws_error_category`, `ws_error_action`
- `seq_gap_count`, `resync_count`
- `drop_trade_ring`, `drop_candle_ring`, `drop_ws_queue`, `drop_payload_oversize`
- `server_ws_queue_len`, `server_ws_dropped`, `server_ws_lag_ms`, `server_resync_total`
- `server_instance_id`, `protocol_version`, `last_server_ts_ms`

## Symptoms, Evidence, Recommendation

### 1. Missing METRICS frames
- Symptom: telemetry blind spot during live session.
- Evidence: `backend_gap_no_metrics > 0`, stale `last_metrics_ts_ms`.
- Recommendation (backend): guarantee periodic `METRICS` cadence and include jitter-tolerant SLA in runbook.

### 2. PONG timeout / heartbeat instability
- Symptom: false reconnects/desync while socket still open.
- Evidence: `backend_gap_pong_timeout > 0`, elevated `ws_error_category=Timeout`.
- Recommendation (backend): ensure `PING/PONG` path priority and bounded response latency.

### 3. RESYNC acknowledgment missing or delayed
- Symptom: client escalates to unsubscribe+resubscribe fallback.
- Evidence: `backend_gap_resync_ack_timeout > 0`, lifecycle log `resync_timeout ... fallback to unsub+resub`.
- Recommendation (backend): make `RESYNC` response deterministic (snapshot + ack ordering and timeout budget).

### 4. Frames without `ts_server`
- Symptom: lag/clock diagnostics lose source-of-truth timestamp.
- Evidence: `backend_gap_missing_ts_server > 0`.
- Recommendation (backend): enforce `ts_server` on all `event`/`snapshot` frames.

### 5. Recurring sequence gaps
- Symptom: repeated desync and recovery cycles on active streams.
- Evidence: `seq_gap_count` growth + `backend_gap_seq_gap_recurring > 0`.
- Recommendation (backend): verify per-stream ordering/seq continuity and fanout queue behavior under load.

### 6. Frequent drops under pressure
- Symptom: degraded data quality and eventual desync/reconnect churn.
- Evidence: `backend_gap_frequent_drops > 0` plus rising drop-reason counters.
- Recommendation (backend): review drop policy, queue sizing, and publish-to-deliver latency budget.

## Operational Use
- Export `Copy Diagnostics` from affected sessions and attach to incident tickets.
- Group incidents by `server_instance_id` and `protocol_version` to isolate backend nodes/protocol regressions.
- Treat recurring non-zero gap counters as backend contract violations to be prioritized before feature work.
