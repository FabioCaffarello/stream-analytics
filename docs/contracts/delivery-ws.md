# Delivery WS Contract (Envelope, Streams, Backpressure)

**Status:** Active
**Owner:** Product Architect
**Last updated:** 2026-03-06
**Relates to:** `docs/adrs/ADR-0002-event-envelope-and-versioning.md`, `docs/adrs/ADR-0007-delivery-ws-sessions.md`, `docs/adrs/ADR-0013-backpressure-overload-policies.md`, `docs/adrs/ADR-0014-stream-partitioning-strategy.md`, `docs/contracts/event-bus.md`, `docs/rfcs/RFC-0003-W2-DELIVERY-BC.md`

## Purpose

Define WS delivery contract for marketdata/aggregation/insights streams plus the canonical lifecycle streams (`signal.event`, `strategy.intent`, `execution.event`, `portfolio.state`) with explicit separation between current behavior and planned parity extensions.

## Terminology (canonical)

- `subject` (WS): `<stream_type>/<venue>/<symbol>/<timeframe>`.
- `subject` (bus): `{event}.v{version}.{venue}.{instrument}`.
- `stream_type`: namespaced event token (for example `marketdata.trade`).
- `symbol`: client-facing token in WS subject; canonicalized from `instrument` (`BTC-USDT` -> `BTCUSDT`).
- `symbol alias`: market-type suffix variant used by some clients (`BTCUSDT:SPOT`).
- `envelope`: canonical bus wrapper from ADR-0002.
- `frame`: WS JSON message emitted by the session actor.

## Data Planes

### Input Plane (bus/internal)

Accepted delivery router inputs:
- `marketdata.trade.v1.{venue}.{instrument}`
- `marketdata.bookdelta.v1.{venue}.{instrument}`
- `marketdata.markprice.v1.{venue}.{instrument}`
- `marketdata.liquidation.v1.{venue}.{instrument}`
- `insights.crossvenue.trade_snapshot.v1.global.{instrument}`
- `insights.crossvenue.spread_signal.v1.global.{instrument}`
- `insights.microstructure_evidence.v1.{venue}.{instrument}`
- `signal.event.v1.{venue}.{instrument}`
- `strategy.intent.v1.{venue}.{instrument}`
- `execution.event.v1.{venue}.{instrument}`
- `portfolio.state.v1.{venue}.{instrument}`
- `aggregation.snapshot.v1.{venue}.{instrument}`
- `aggregation.candle.v1.{venue}.{instrument}`
- `aggregation.stats.v1.{venue}.{instrument}`
- `insights.heatmap_snapshot.v1.{venue}.{instrument}`
- planned: `insights.heatmap_delta.v1.{venue}.{instrument}`
- planned: `insights.volume_profile_snapshot.v1.{venue}.{instrument}`
- planned: `insights.volume_profile_delta.v1.{venue}.{instrument}`

### Output Plane (WS frames)

Canonical WS subject:
- `<stream_type>/<venue>/<symbol>/<timeframe>`

Examples:
- `marketdata.trade/binance/BTCUSDT/raw`
- `marketdata.markprice/bybit/BTCUSDT/raw`
- `insights.crossvenue.trade_snapshot/global/BTCUSDT/raw`
- `aggregation.snapshot/binance/BTCUSDT/raw`
- `aggregation.candle/binance/BTCUSDT/1m`
- `aggregation.stats/binance/BTCUSDT/raw`
- `insights.heatmap_snapshot/binance/BTCUSDT/1m`
- `signal/regime_change/binance/BTCUSDT/raw`
- `strategy.intent/binance/BTCUSDT/raw`
- `execution.event/binance/BTCUSDT/raw`
- `portfolio.state/binance/BTCUSDT/raw`

Proto rollout is controlled by config (`proto_rollout.*`) and can be refreshed with `POST /runtime/reload`.
- `proto_rollout.marketdata.trade`
- `proto_rollout.marketdata.bookdelta`
- `proto_rollout.marketdata.markprice`
- `proto_rollout.marketdata.liquidation`
- `proto_rollout.aggregation.candle|stats|snapshot`
- `proto_rollout.insights.volume_profile|heatmap|crossvenue`
- default for all flags is disabled (`false`), so rollout-controlled streams stay on JSON unless explicitly enabled.

Lifecycle stream note:
- `strategy.intent`, `execution.event`, and `portfolio.state` are routable through delivery for observability and audit use cases.
- Durable storage/read APIs for those streams are not wired yet; do not infer cold-path support from WS delivery support.

## Contracts

### Terminal Protocol v1 (institutional)

The terminal integration now supports explicit operations:
- `hello`
- `subscribe`
- `unsubscribe`
- `ping`
- `resync`

All stream frames (`event` and `snapshot`) include:
- `protocol_version`
- `server_instance_id`
- `stream_id`
- `seq`
- `ts_server`
- `venue`
- `symbol`
- `channel`

Resync semantics:
1. client detects gap/stale state and sends `resync` with `stream_id` and `last_seq`;
2. server emits deterministic `snapshot` (bounded cache, TTL);
3. server emits `ack` and resumes live stream.

### Client -> Server Commands

```json
{
  "op": "subscribe|unsubscribe|getrange",
  "subject": "marketdata.trade/binance/BTCUSDT/raw",
  "request_id": "r1",
  "params": {
    "from_ms": 0,
    "to_ms": 0,
    "end_ts": 0,
    "limit": 100
  }
}
```

GetRange range params:
- `to_ms` is the authoritative upper-bound parameter for range queries.
- `end_ts` is accepted only for backward compatibility and is mapped to `to_ms`.
- clients should send `to_ms`; server keeps `end_ts` compatibility for older clients.

### Server -> Client Frames

Hello (mandatory first control frame):
```json
{
  "type": "hello",
  "payload": {
    "proto_ver": 1,
    "server_time": 1710000000000,
    "capabilities": {
      "topics": [
        "marketdata.trade",
        "marketdata.bookdelta",
        "aggregation.candle",
        "aggregation.stats"
      ],
      "venues": ["binance", "bybit"]
    }
  }
}
```

Hello contract:
- `hello` MUST be delivered before data frames.
- `payload.proto_ver` MUST match client supported version.
- `payload.server_time` is required and must be unix epoch in milliseconds.
- `payload.capabilities.topics` is required and non-empty.
- on validation failure or version mismatch, client must enter `DESYNC(reason)` and request resync/reconnect.
- silent fallback on unknown/unsupported protocol versions is forbidden.

#### Extended Capabilities

The `capabilities` object in the `hello` frame includes additional fields for client self-tuning:

```json
{
  "type": "hello",
  "payload": {
    "proto_ver": 1,
    "server_time": 1710000000000,
    "capabilities": {
      "topics": ["marketdata.trade", "marketdata.bookdelta"],
      "venues": ["binance", "bybit"],
      "max_subscriptions": 256,
      "max_symbols_per_connection": 128,
      "max_frame_bytes": 65536,
      "outbound_queue_size": 1024,
      "metrics_cadence_ms": 5000,
      "keepalive_interval_ms": 30000,
      "rate_limit": {
        "enabled": true,
        "max_per_second": 50,
        "burst_capacity": 100
      },
      "supported_features": ["batching", "snapshot_hash", "prev_seq"]
    }
  }
}
```

Field semantics:
- `max_subscriptions`: hard cap on active subscriptions per session.
- `max_symbols_per_connection`: hard cap on distinct symbols per session.
- `max_frame_bytes`: maximum serialized frame size; oversized proto frames are silently dropped.
- `outbound_queue_size`: bounded outbound queue capacity (for backpressure awareness).
- `metrics_cadence_ms`: interval between server `metrics` frames.
- `keepalive_interval_ms`: server keepalive/ping interval.
- `rate_limit`: session-level command rate limiting config. Absent when disabled.
- `supported_features`: list of optional protocol features the server supports.

All new fields are `omitempty`/zero-value. Clients that predate this extension see the original `topics`+`venues` only.

Ack:
```json
{
  "type": "ack",
  "op": "subscribe",
  "request_id": "r1",
  "subject": "marketdata.trade/binance/BTCUSDT/raw"
}
```

Event:
```json
{
  "type": "event",
  "subject": "marketdata.trade/binance/BTCUSDT/raw",
  "seq": 123,
  "ts_ingest": 1710000005000,
  "payload": {}
}
```

Proto event frame negotiation:
- session can request proto frames via `GET /ws?format=proto` or `X-Delivery-Format: proto`.
- when session proto mode is active and rollout flag for the event type is enabled, event frames are sent as binary `envelope.v1.Envelope` protobuf.
- when proto mode is not requested, or rollout flag is disabled for that subject type, event frames stay JSON.

Client quick-start:
- web: connect with `wss://<host>/ws?format=proto`; parse binary frames as `envelope.v1.Envelope`.
- desktop: use the same query/header negotiation and decode binary protobuf frames.
- app/mobile: send `X-Delivery-Format: proto` during WS handshake when query params are constrained.

Current runtime event frame fields:
- mandatory: `type`, `subject`, `seq`, `ts_ingest`, `payload`
- planned extension: optional `content_type` (not emitted yet by `SessionActor.writeData`)

Error:
```json
{
  "type": "error",
  "op": "subscribe",
  "request_id": "r1",
  "problem": {
    "code": "VAL_VALIDATION_FAILED",
    "message": "subject must have 4 segments",
    "action_hint": "ACTION_HINT_NONE"
  }
}
```

Range:
```json
{
  "type": "range",
  "op": "getrange",
  "request_id": "r2",
  "subject": "marketdata.trade/binance/BTCUSDT/raw",
  "items": []
}
```

### Error Taxonomy

Every `error` frame includes an `action_hint` field guiding client recovery:

| Problem Code | Error Code | Action Hint | Client Behavior |
|---|---|---|---|
| `ValidationFailed`, `InvalidArgument` | `VALIDATION` | `ACTION_HINT_NONE` | Fix request and retry |
| `NotFound` | `NOT_FOUND` | `ACTION_HINT_NONE` | Subject does not exist |
| `Unavailable` | `RATE_LIMITED` | `ACTION_HINT_RETRY` | Back off and retry |
| `Conflict` | `RESYNC_REQUIRED` | `ACTION_HINT_RESYNC` | Send `resync` for the stream |
| `IntegrityViolation` | `RESYNC_REQUIRED` | `ACTION_HINT_RESUBSCRIBE` | Unsubscribe and resubscribe |
| `Internal` | `INTERNAL` | `ACTION_HINT_RECONNECT` | Close and reconnect |
| (other) | `INTERNAL` | `ACTION_HINT_RECONNECT` | Close and reconnect |

`action_hint` values:
- `ACTION_HINT_UNSPECIFIED` / empty — pre-taxonomy server or unclassified error.
- `ACTION_HINT_NONE` — no recovery action needed; fix the request.
- `ACTION_HINT_RETRY` — retry the same operation after a brief delay.
- `ACTION_HINT_RECONNECT` — close the connection and open a new session.
- `ACTION_HINT_RESUBSCRIBE` — unsubscribe and resubscribe to the affected stream.
- `ACTION_HINT_RESYNC` — send a `resync` command for the affected stream.

Backward compatibility: `action_hint` is `omitempty`. Clients that do not recognize the field continue with existing error handling.

### Snapshot Sequencing

Snapshot frames include additional integrity fields:

```json
{
  "type": "snapshot",
  "subject": "aggregation.snapshot/binance/BTCUSDT/raw",
  "snapshot_seq": 2,
  "watermark_seq": 456,
  "snapshot_hash": "a1b2c3d4e5f67890",
  "payload": {}
}
```

- `snapshot_seq`: per-session per-subject counter, incremented on every snapshot emission (subscribe and resync). Allows clients to detect duplicate or reordered snapshots.
- `watermark_seq`: highest confirmed upstream sequence at the time of snapshot capture. Clients can use this to know what seq range the snapshot covers.
- `snapshot_hash`: FNV-64a hex hash of the snapshot payload bytes. Clients can verify payload integrity or detect cache staleness.

All fields are `omitempty`. `snapshot_seq == 0` means legacy snapshot (pre-F3).

### Client Gap Detection via prev_seq

Event frames include a `prev_seq` field for client-side gap detection:

```json
{
  "type": "event",
  "subject": "marketdata.trade/binance/BTCUSDT/raw",
  "seq": 124,
  "prev_seq": 123,
  "ts_ingest": 1710000005000,
  "payload": {}
}
```

- `prev_seq`: the `seq` value of the immediately preceding event for the same subject within this session.
- `prev_seq == 0`: first event after subscribe/resync, or legacy server (pre-F3).
- Gap detection: if `event.prev_seq != 0 && event.prev_seq != last_received_seq`, the client has a gap and should send `resync`.

`prev_seq` is tracked independently per subject within a session.

### Feature Negotiation

Clients can declare requested features via `ClientHello`:

```json
{
  "op": "hello",
  "requested_features": ["batching", "snapshot_hash"]
}
```

The server advertises `supported_features` in its `hello` frame (see Extended Capabilities). Feature activation requires both client request and server support. Unknown features are silently ignored.

Currently supported features:
- `batching` — reserved for future batched frame delivery.
- `snapshot_hash` — FNV-64a integrity hash on snapshot frames.
- `prev_seq` — previous sequence tracking on event frames.

### BatchedFrame (reserved)

The `BatchedFrame` message type is defined in proto but reserved for future use. When activated, it allows the server to bundle multiple `ServerFrame` items into a single WebSocket message with `first_seq` and `last_seq` bounds. This is not yet emitted by the server.

### Backpressure Hints

The periodic `metrics` frame includes backpressure awareness fields:

```json
{
  "type": "metrics",
  "payload": {
    "queue_len": 512,
    "queue_capacity": 1024,
    "queue_high_watermark": 768,
    "backpressure_level": 2,
    "recommended_action": "reduce_subscriptions",
    "subscriptions": 10,
    "total_delivered": 50000,
    "total_dropped": 5
  }
}
```

Backpressure levels:
| Level | Name | Queue Ratio | Recommended Action |
|---|---|---|---|
| 0 | Normal | < 50% | `none` |
| 1 | Elevated | >= 50% | `none` |
| 2 | High | >= 75% | `reduce_subscriptions` |
| 3 | Critical | >= 95% | `reconnect` |

- `queue_capacity`: total outbound queue size.
- `queue_high_watermark`: peak queue depth since last metrics emission, then reset.
- `backpressure_level`: 0–3 severity indicator.
- `recommended_action`: suggested client recovery action.

All fields are `omitempty`/zero-value. `backpressure_level == 0` means normal (pre-F5 behavior).

### Multi-tenant Observability

When `tenant_id` is present in the authenticated principal, all session metrics are additionally emitted with tenant-scoped Prometheus labels:

- `ws_tenant_drops_total{tenant_id, reason}` — drops per tenant.
- `ws_tenant_queue_depth{tenant_id}` — current queue depth per tenant.
- `ws_tenant_connections_active{tenant_id}` — active connections per tenant.
- `ws_tenant_messages_out_total{tenant_id, channel}` — messages delivered per tenant.

Empty `tenant_id` is normalized to `"default"`. These metrics are additive to existing unlabeled metrics for backward compatibility.

Per-tenant limit overrides can be configured in `ws.tenant_limits`:

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

When a tenant has a configured override, its limits take precedence over global defaults for `max_subs_per_connection` and `rate_limit`.

## Invariants

- `WS-1`: session is an isolated actor; one session failure cannot cascade.
- `WS-2`: WS subject always has 4 segments (`stream_type/venue/symbol/timeframe`).
- `WS-3`: per-subject ordering by `seq` is preserved inside one session.
- `WS-4`: no unbounded per-session queue in parity target design.
- `WS-5`: unsubscribe/remove session must release routing state and memory.
- `WS-6`: when `getrange` is requested with symbol alias (`SYMBOL:MARKET_TYPE`) and no direct rows exist, delivery may perform one deterministic fallback lookup using canonical `SYMBOL`.
- `WS-7`: orderbook deltas require snapshot-first on the client side; snapshot gap must trigger desync and resubscribe.
- `WS-8`: protocol gate is mandatory (`hello` + `proto_ver` + required capabilities fields).
- `WS-9`: `snapshot_seq(N) < snapshot_seq(N+1)` within a session for the same subject.
- `WS-10`: `prev_seq(event[N]) == seq(event[N-1])` for the same subject within a session.

## Backpressure

Current runtime behavior:
1. session lifecycle isolation and cleanup are implemented;
2. bounded per-session outbound queue is implemented;
3. drop policy is configurable (`drop_newest|drop_oldest|priority_drop`) with labeled drop reasons;
4. slow clients are disconnected after `delivery.slow_client_drop_threshold` breached;
5. connection write failures close the session.

Planned parity policy:
1. stream-priority policies (`keep-latest` vs `drop_newest`) per stream class;
2. per-stream dynamic thresholds by client tier/SLA.

Required metrics:
- `ws_queue_depth`
- `ws_drops_total{reason}`
- `ws_send_latency_ms`
- `ws_clients_connected`
- `delivery_range_alias_fallback_total{outcome}`

## Storage Strategy

- Primary hot source: in-memory read models.
- Secondary durable hot source (planned): Timescale for multi-instance `getrange`.
- Cold path (ClickHouse) is not read directly by WS in real time; only via backfill jobs.

## Replay Strategy

- `getrange` must be deterministic for same window and limit.
- operational replay (fixture/jetstream) must reproduce equivalent frames per subject.
- output ordering validated by `(ts_ingest, seq)`.

## Implementation Matrix

| Feature | Status | Evidence | Tests |
|---|---|---|---|
| Subject parser and 4-segment validation | Existing | `internal/core/delivery/domain/subject.go` | `internal/core/delivery/domain/subject_test.go:TestParseSubject`, `internal/core/delivery/domain/subject_test.go:TestParseSubject_invalid` |
| Session command handling (`subscribe`, `unsubscribe`, `getrange`) | Existing | `internal/actors/delivery/runtime/session.go` | `internal/actors/delivery/runtime/session_test.go:TestSession_parseSubscribeUnsubscribeGetRange` |
| Session emits protocol hello on attach | Existing | `internal/actors/delivery/runtime/session.go` | `internal/actors/delivery/runtime/session_test.go:TestSession_emitsHelloOnAttach` |
| Router broadcast only to subscribed sessions | Existing | `internal/actors/delivery/runtime/router.go` | `internal/actors/delivery/runtime/router_test.go:TestRouter_subscribeUnsubscribeAndBroadcast` |
| Disconnect cleanup and unregister | Existing | `internal/actors/delivery/runtime/session.go` | `internal/actors/delivery/runtime/session_test.go:TestSession_disconnectTriggersUnregister` |
| Deterministic range from durable store | Planned | `internal/core/delivery/ports/ports.go` | `internal/core/delivery/app/session_usecase_test.go:TestSessionService_GetRange_storeUnavailable` |
| Alias fallback for candle getrange (`SYMBOL:MARKET_TYPE` -> `SYMBOL`) | Existing | `internal/core/delivery/app/session_usecase.go` | `internal/core/delivery/app/session_usecase_test.go:TestSessionService_GetRange_marketTypeAliasFallback` |
| Slow-client backpressure policy + threshold disconnect | Existing | `internal/core/delivery/domain/backpressure_policy.go`, `internal/actors/delivery/runtime/session.go`, `internal/shared/config/schema.go` | `internal/actors/delivery/runtime/session_backpressure_test.go:TestWSBackpressureSlowClientDropPolicy`, `internal/actors/delivery/runtime/session_backpressure_test.go:TestWSBackpressureSlowClientThresholdDisconnects` |

## Observability

- `delivery_ws_active_sessions`
- `delivery_ws_subscriptions_total`
- `delivery_ws_queue_depth`
- `delivery_ws_drop_total`
- `delivery_ws_frame_latency_ms`
- `delivery_ws_range_latency_ms`
- `delivery_range_alias_fallback_total{outcome}`

Minimum:
- lag
- drop
- queue depth

## Acceptance Tests

Existing tests:
- `internal/actors/delivery/runtime/session_test.go:TestSession_parseSubscribeUnsubscribeGetRange`
- `internal/actors/delivery/runtime/session_test.go:TestSession_disconnectTriggersUnregister`
- `internal/actors/delivery/runtime/router_test.go:TestRouter_subscribeUnsubscribeAndBroadcast`
- `internal/core/delivery/domain/subject_test.go:TestParseSubject`
- `internal/core/delivery/domain/subject_test.go:TestParseSubject_invalid`
- `internal/core/delivery/app/session_usecase_test.go:TestSessionService_GetRange_storeUnavailable`
- `internal/interfaces/ws/delivery_contract_e2e_test.go:TestWSRangeDeterminismReplay`
- `internal/interfaces/ws/delivery_contract_e2e_test.go:TestWSRaceSubscribeUnsubscribeNoLeak`
- `internal/interfaces/ws/delivery_contract_e2e_test.go:TestWSReconnectResubscribeIdempotent`
- `internal/interfaces/ws/heatmap_delivery_contract_test.go:TestWSDelivery_HeatmapSnapshot_RoutedToSubscriber`

Tests to create for parity completion:
- None for current delivery WS baseline.

## Compatibility

- N/N-1 compatibility by envelope event version.
- New WS frame fields must remain optional.
- Do not remove mandatory fields without new `frame_version`.
- Default bus payload content type is `application/protobuf`; JSON remains supported via rollout/fallback policy in ADR-0016.
- `getrange` keeps backward compatibility with `end_ts`; `to_ms` is the canonical parameter.

## Evidence Hooks

Current evidence:
- `internal/core/delivery/domain/subject.go`
- `internal/actors/delivery/runtime/router.go`
- `internal/actors/delivery/runtime/session.go`
- `internal/actors/delivery/runtime/session_test.go`
- `internal/actors/delivery/runtime/router_test.go`
- `internal/shared/metrics/metrics.go`
- `internal/core/delivery/app/session_usecase_test.go`
- `docs/rfcs/RFC-0003-W2-DELIVERY-BC.md`

TODO hooks (skeleton):
- none for current delivery WS baseline.

## Failure Modes

- Slow WS client accumulation without explicit drop policy:
  - Mitigation: bounded outbound queue + drop policy + threshold-based disconnect (`delivery.slow_client_drop_threshold`).
- network jitter/intermittency:
  - Mitigation: keepalive + reconnect/resubscribe idempotency.
- poison command frame:
  - Mitigation: structured error response without panic and keep session alive.
- upstream ack-on-enqueue drift:
  - Mitigation: enforce ack-on-commit before delivery stage.
