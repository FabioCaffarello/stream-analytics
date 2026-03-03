# Terminal WS Runbook

## Endpoints
- `GET /ws`
- `GET /ws/marketdata` (legacy compatibility route)
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
- Publish→deliver batching: session flush writes in bounded batches to reduce actor mailbox churn (`batching` feature remains backward compatible).
- Compression: WebSocket permessage-deflate is enabled by default; keep enabled for WAN/browser clients unless CPU is the bottleneck.
- Frame guard: `delivery.max_frame_bytes` is enforced on both JSON and proto paths; oversized frames are dropped with `frame_too_large`.
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
  - compression: enabled
- Native clients (LAN or colocated):
  - `delivery.session_outbound_queue_size`: `512-1024`
  - `delivery.max_frame_bytes`: `131072` (if payloads justify it)
  - `delivery.metrics_cadence_ms`: `2000-5000`
  - `delivery.keepalive_interval_ms`: `10000-20000`
  - compression: enabled by default; disable only after CPU profiling

## Observability
Key metrics:
- `ws_clients_connected`
- `ws_clients_connected_by_mode{mode}`
- `ws_clients_total{mode}`
- `ws_subscriptions_active`
- `ws_control_frames_total{type}`
- `ws_messages_out_total{channel}`
- `ws_bytes_out_total{channel}`
- `ws_dropped_total{reason,channel}`
- `ws_queue_len`
- `ws_lag_ms{channel}`
- `ws_publish_to_deliver_latency_ms{channel}`
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

## Legacy Route Deprecation

The `/ws/marketdata` route is the legacy entry point. New clients should use `/ws` exclusively. The deprecation follows a 3-phase timeline controlled by `ws.allow_legacy_ws` in config:

| Phase | Config | Behavior |
|-------|--------|----------|
| 1. Both active (current) | `allow_legacy_ws: true` (default) | Both `/ws` and `/ws/marketdata` served; legacy requests counted via `ws_legacy_requests_total{status=accepted}` |
| 2. Legacy disabled | `allow_legacy_ws: false` | `/ws/marketdata` returns **410 Gone**; counter increments `{status=rejected}` |
| 3. Route removed | (code change) | `/ws/marketdata` handler removed entirely |

Target shutdown window:
- Earliest disable date: **June 30, 2026**.
- Disable criteria:
  - `rate(ws_clients_total{mode="legacy"}[7d]) == 0`
  - no customer profile explicitly requiring legacy fallback
  - zero `ws_legacy_requests_total{status="accepted"}` in staged canary window

Config example:
```json
{
  "ws": {
    "allow_legacy_ws": false
  }
}
```

Monitoring (Phase 1 → 2 transition):
```promql
# Legacy clients still active (should trend to zero before Phase 2)
rate(ws_legacy_requests_total{status="accepted"}[5m])

# Legacy vs v1 connection mix
sum by (mode) (rate(ws_clients_total[15m]))

# Rejected legacy requests after flag-off
rate(ws_legacy_requests_total{status="rejected"}[5m])
```

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

Supported features: `batching`, `prev_seq`, `snapshot_hash`. Unknown features are rejected entirely (no partial accept).

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
| High delivery latency | `ws_publish_to_deliver_latency_ms{quantile="0.99"} > 100` | Check queue depth (`ws_queue_len`), consider reducing subscriptions |
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
