# Terminal WS Runbook

## Endpoints
- `GET /ws`
- `GET /healthz`
- `GET /readyz`
- `GET /metrics`
- `GET /introspection`
- `GET /runtime/terminal` (localhost-only)

## Authentication
- API key: `X-API-Key: <key>` or `?api_key=<key>`.
- JWT (HS256): `Authorization: Bearer <token>`.
- Required scope: `read`.

## Protocol
Client operations:
- `hello`
- `subscribe`
- `unsubscribe`
- `ping`
- `resync`

Server frames:
- `hello`
- `ack`
- `pong`
- `metrics`
- `error`
- `snapshot`
- `event`
- `batch`

All `event`/`snapshot` frames include:
- `protocol_version`
- `server_instance_id`
- `stream_id`
- `seq`
- `ts_server`
- `venue`
- `symbol`
- `channel`

## Subscribe Example
```json
{"type":"subscribe","request_id":"sub-1","venue":"binance","symbol":"BTC-USDT","channel":"marketdata.trade"}
```

## Resync Example
```json
{"type":"resync","request_id":"rs-1","stream_id":"marketdata.trade/binance/BTCUSDT/raw","last_seq":123456}
```
Behavior:
1. Server emits `snapshot` from bounded snapshot cache.
2. Server emits `ack` for `resync`.
3. Live stream resumes in monotonic `seq` order.

## Stream Coherence (replicas=2)
- Strategy: `sticky_session`.
- Each WebSocket session is pinned to one router instance for the lifetime of the connection.
- Router enforces monotonic source `seq` per stream (`seq_invalid`/`seq_non_monotonic` are rejected).
- Delivered `seq` is contiguous per stream inside each session to keep client-side gap detection deterministic.

## Backpressure and Limits
- Outbound queue is bounded per connection.
- Policy: `drop_newest | drop_oldest | priority_drop`.
- Slow client disconnect after `delivery.slow_client_drop_threshold` drops.
- Explicit drop reasons include queue and frame guard paths (for example `queue_full`, `drop_oldest`, `priority_drop_self`, `frame_too_large`).
- Hard limits:
  - max connections per IP
  - max connections per key
  - max subscriptions per connection
  - max symbols per connection

## Perf Tuning
- Batching (`hello.requested_features` includes `batching`):
  - Server emits compact `batch` frames with one header (`stream_id`, `base_seq`, `count`, `ts_server_base`) and per-item deltas.
  - Batch writer is size-guarded by `delivery.max_frame_bytes`; oversized batches are split automatically (or downgraded to single-event writes).
  - Strangler counter: `ws_batch_fallback_events_total` counts events that were batch candidates but downgraded to single-event writes.
  - Use batching when stream fanout is bursty (book/trade bursts) and client parser supports Terminal V1 batch frames.
- Compression (`hello.requested_features` includes `compress`):
  - Server-driven, negotiated at hello.
  - Compression is applied only above payload threshold (default `1KB`) to avoid CPU regression on small frames.
  - `max_frame_bytes` is checked against final wire size (post-compression estimate), not only raw JSON size.
- Frame guard: `delivery.max_frame_bytes` is enforced on JSON/proto/batch paths; oversized frames are dropped with `frame_too_large`.
- Session cadence knobs:
  - `delivery.metrics_cadence_ms` controls `metrics` frame interval per session.
  - `delivery.keepalive_interval_ms` controls ping interval per session.
- Queue sizing: keep `processor.bus_capacity >= delivery.session_outbound_queue_size` (enforced by config validation) to avoid immediate pressure at session ingress.
- Context bootstrap: on `subscribe` to candle streams, server emits a bounded `range` backfill (`op=backfill`) before live flow to avoid empty-start charts; `watermark_seq` in the range frame indicates the highest seq included.

Recommended defaults:
- WASM/browser clients:
  - `delivery.session_outbound_queue_size`: `256-512`
  - `delivery.max_frame_bytes`: `65536`
  - `delivery.metrics_cadence_ms`: `5000`
  - `delivery.keepalive_interval_ms`: `20000`
  - features: `batching=on`, `compress=on`, `snapshot_hash=on`, `prev_seq=on`
- Native clients (LAN or colocated):
  - `delivery.session_outbound_queue_size`: `512-1024`
  - `delivery.max_frame_bytes`: `131072` (if payloads justify it)
  - `delivery.metrics_cadence_ms`: `2000-5000`
  - `delivery.keepalive_interval_ms`: `10000-20000`
  - features: `batching=on`, `compress=auto` (enable on WAN; disable on low-latency LAN if CPU is limiting)

CPU vs bandwidth tradeoff:
- Higher batching + compression lowers outbound bytes and serialization pressure on busy streams.
- Compression helps most on repetitive JSON payloads (`bookdelta`, dense snapshots) and hurts on tiny/control frames.
- For CPU-constrained environments, keep batching enabled first and tune compression threshold/feature flag second.

## Observability
Key metrics:
- `ws_clients_connected`
- `ws_clients_connected_by_mode{mode}`
- `ws_clients_total{mode}`
- `ws_subscriptions_active`
- `ws_control_frames_total{type}`
- `ws_messages_out_total{channel}`
- `ws_bytes_out_total{channel}`
- `ws_dropped_total{reason,channel,priority}`
- `ws_batch_frames_total`
- `ws_batch_events_total`
- `ws_batch_fallback_events_total`
- `ws_compress_applied_total`
- `ws_compress_bytes_in_total`
- `ws_compress_bytes_out_total`
- `ws_queue_len`
- `ws_queue_capacity`
- `ws_lag_seconds{channel}` (also deprecated `ws_lag_ms`)
- `ws_publish_to_deliver_latency_seconds{channel}` (also deprecated `ws_publish_to_deliver_latency_ms`)
- `ws_send_latency_seconds` (also deprecated `ws_send_latency_ms`)
- `delivery_router_sessions_active`
- `serialize_errors_total`
- `auth_fail_total`
- `resync_total`
- `ws_resync_rejected_total{reason}`
- `ws_contract_violations_total{reason}`
- `delivery_router_coherence_mode{mode}`

Introspection:
- `GET /introspection` returns stream-level status with seq/lag/drop counters.
- `GET /runtime/terminal` (localhost-only) returns terminal WS state snapshot:
  ```bash
  curl -s http://localhost:8080/runtime/terminal | jq
  ```
  Returns JSON with active connections, per-stream metrics, and queue state. Limited to 100 entries.

Tenant metrics (Grafana examples):
```promql
# Drops by tenant
sum by (tenant_id, reason) (rate(ws_tenant_drops_total[5m]))

# Queue depth by tenant
ws_tenant_queue_depth{tenant_id="acme"}

# Active connections by tenant
ws_tenant_connections_active

# Messages delivered by tenant and channel
sum by (tenant_id, channel) (rate(ws_tenant_messages_out_total[5m]))
```

Tenant label cardinality control:
- `ws.tenant_metrics.include_tenant_label`: include real tenant labels (`true` default for compatibility).
- `ws.tenant_metrics.tenant_whitelist`: optional allowlist of tenants with explicit label series.
- `ws.tenant_metrics.fallback`: `unknown` or `hash_bucket` for non-whitelisted tenants.

Backpressure metrics:
```promql
# Current backpressure level (0=normal, 3=critical)
ws_backpressure_level

# Queue high watermark (peak between metrics emissions)
ws_queue_high_watermark
```

Batch fast-path strangler removal criteria:
- Removal gate: `ws_batch_fallback_events_total == 0` for **5 consecutive IQ runs**.
- IQ gate: `scripts/iq/analyze_iq_run.mjs` fails when fallback events are non-zero.
- Temporary override (explicit only): `IQ_ALLOW_BATCHED_FALLBACK=1`.

## Per-Tenant Limits

Configure tenant-specific overrides in `ws.tenant_limits`:

```json
{
  "ws": {
    "tenant_limits": {
      "acme": {
        "max_connections_per_key": 50,
        "max_subs_per_connection": 512,
        "rate_limit": {
          "enabled": true,
          "max_per_second": 100,
          "burst_capacity": 200
        }
      }
    }
  }
}
```

When a tenant has configured limits, they override the global `max_subs_per_connection` and `rate_limit` for all sessions authenticated under that tenant. Unconfigured tenants use global defaults.

## Legacy Route Hard Cutover (Sprint S9)

`/ws/marketdata` is permanently disabled. The server always returns **410 Gone** and increments `ws_legacy_requests_total{status="rejected"}` for regression detection.

The client hard-cutover policy is also active:
- Terminal V1 timeout must not downgrade to Legacy JSON.
- Any legacy evidence/signal subject frame is rejected and counted in client probes.

There are no runtime override flags for this cutover path.

Monitoring:
```promql
# Must stay at 0 after S9 cutover
rate(ws_legacy_requests_total{status="accepted"}[5m])

# Legacy route hits should be rejected-only and investigated
rate(ws_legacy_requests_total{status="rejected"}[5m])
```

Rollback (safe):
1. Roll back to the last release tag before Sprint S9.
2. Redeploy `server`, `processor`, `consumer`, `client` as one version set.
3. Run IQ loop with `PROCESSOR_REPLICAS=2` and verify PASS before re-enabling traffic.
4. Confirm `ws_clients_total{mode="legacy"}` and `ws_legacy_requests_total{status="accepted"}` behavior matches the rollback release expectations.

## HELLO Negotiation

Clients may send a `hello` frame to negotiate protocol features. The server validates `requested_features` against the supported set and responds with the intersection.

Client hello:
```json
{"type":"hello","request_id":"h-1","requested_features":["batching","prev_seq","snapshot_hash"]}
```

Successful ack:
```json
{"type":"ack","op":"hello","request_id":"h-1","negotiated_features":["batching","prev_seq","snapshot_hash"]}
```

Error on unknown feature:
```json
{"type":"error","op":"hello","request_id":"h-1","code":"validation_failed","message":"unsupported features: cbor"}
```

Supported features: `batching`, `compress`, `prev_seq`, `snapshot_hash`. Unknown features are rejected entirely (no partial accept).

## Error Code Reference

| Code | Meaning | Action Hint | When |
|------|---------|-------------|------|
| `validation_failed` | Request payload failed schema/field validation | Fix request and retry | Invalid subscribe fields, unknown features in hello |
| `unknown_channel` | Requested channel is not governed | Use a valid channel name | Subscribe with unrecognized channel |
| `max_subscriptions` | Subscription limit reached for this connection | Unsubscribe unused streams first | Subscribe exceeds `max_subs_per_connection` |
| `rate_limited` | Command rate limit exceeded | Back off and retry | Too many control frames per second |
| `internal` | Unexpected server-side error | Retry or reconnect | Serialization failure, store error |
| `snapshot_unavailable` | No cached snapshot for resync | Wait for live data | Resync for stream with no recent snapshot |
| `seq_invalid` | Source sequence <= 0 | Bug in upstream producer | Envelope with non-positive seq |
| `seq_non_monotonic` | Source sequence not strictly increasing | Duplicate or reordered upstream event | Envelope seq <= last accepted |

## Metric-Based Troubleshooting

| Symptom | Metric | Action |
|---------|--------|--------|
| Clients dropping messages | `rate(ws_dropped_total[5m]) > 0` | Reduce subscriptions, increase client consume rate, or tune `backpressure_policy` |
| High delivery latency | `ws_publish_to_deliver_latency_seconds{quantile="0.99"} > 0.1` | Check queue utilization (`ws_queue_len / ws_queue_capacity`), consider reducing subscriptions |
| Transcode cache misses | `rate(transcode_cache_misses[5m])` trending up | Increase cache size or check for high event-type cardinality |
| Snapshot cache misses | `rate(delivery_ws_snapshot_cache_misses_total[5m])` trending up | Increase cache TTL or max entries |
| Auth failures | `rate(auth_fail_total[5m]) > 0` | Check API key rotation, JWT signing key, token expiry |
| Legacy clients active | `rate(ws_legacy_requests_total{status="accepted"}[5m]) > 0` | Migrate clients to `/ws` before disabling legacy route |
| Coherence violations | `rate(delivery_router_coherence_violations_total[5m]) > 0` | Investigate upstream producer ordering; check for duplicate publishers |
| Feature negotiation errors | `rate(ws_contract_violations_total{reason="unknown_feature"}[5m]) > 0` | Client requesting unsupported features; update client SDK |

## Troubleshooting
- `401 unauthorized`: invalid/missing API key or bearer token.
- `403 missing read scope`: auth succeeded but token/key scope is insufficient.
- `429 rate limit exceeded`: IP or command rate limit hit.
- frequent `ws_dropped_total`: increase client consume rate or reduce subscriptions.
- recurring `resync_total`: investigate client-side gap handling and network stability.
