# Terminal WS Runbook

## Endpoints
- `GET /ws`
- `GET /ws/marketdata` (legacy compatibility route)
- `GET /healthz`
- `GET /readyz`
- `GET /metrics`
- `GET /introspection`

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

## Troubleshooting
- `401 unauthorized`: invalid/missing API key or bearer token.
- `403 missing read scope`: auth succeeded but token/key scope is insufficient.
- `429 rate limit exceeded`: IP or command rate limit hit.
- frequent `ws_dropped_total`: increase client consume rate or reduce subscriptions.
- recurring `resync_total`: investigate client-side gap handling and network stability.
