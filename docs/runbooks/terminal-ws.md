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
- Hard limits:
  - max connections per IP
  - max connections per key
  - max subscriptions per connection
  - max symbols per connection

## Observability
Key metrics:
- `ws_clients_connected`
- `ws_clients_connected_by_mode{mode}`
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

# Rejected legacy requests after flag-off
rate(ws_legacy_requests_total{status="rejected"}[5m])
```

## Troubleshooting
- `401 unauthorized`: invalid/missing API key or bearer token.
- `403 missing read scope`: auth succeeded but token/key scope is insufficient.
- `429 rate limit exceeded`: IP or command rate limit hit.
- frequent `ws_dropped_total`: increase client consume rate or reduce subscriptions.
- recurring `resync_total`: investigate client-side gap handling and network stability.
