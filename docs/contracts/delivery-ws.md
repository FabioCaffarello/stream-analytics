# Delivery WS Contract (Envelope, Streams, Backpressure)

**Status:** Active
**Owner:** Product Architect
**Last updated:** 2026-03-02
**Relates to:** `docs/adrs/ADR-0002-event-envelope-and-versioning.md`, `docs/adrs/ADR-0007-delivery-ws-sessions.md`, `docs/adrs/ADR-0013-backpressure-overload-policies.md`, `docs/adrs/ADR-0014-stream-partitioning-strategy.md`, `docs/contracts/event-bus.md`, `docs/rfcs/RFC-0003-W2-DELIVERY-BC.md`

## Purpose

Define WS delivery contract for marketdata/aggregation/insights streams with explicit separation between current behavior and planned parity extensions.

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

Proto rollout is controlled by config (`proto_rollout.*`) and can be refreshed with `POST /runtime/reload`.
- `proto_rollout.marketdata.trade`
- `proto_rollout.marketdata.bookdelta`
- `proto_rollout.marketdata.markprice`
- `proto_rollout.marketdata.liquidation`
- `proto_rollout.aggregation.candle|stats|snapshot`
- `proto_rollout.insights.volume_profile|heatmap|crossvenue`
- default for all flags is disabled (`false`), so rollout-controlled streams stay on JSON unless explicitly enabled.

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
    "message": "subject must have 4 segments"
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

## Invariants

- `WS-1`: session is an isolated actor; one session failure cannot cascade.
- `WS-2`: WS subject always has 4 segments (`stream_type/venue/symbol/timeframe`).
- `WS-3`: per-subject ordering by `seq` is preserved inside one session.
- `WS-4`: no unbounded per-session queue in parity target design.
- `WS-5`: unsubscribe/remove session must release routing state and memory.
- `WS-6`: when `getrange` is requested with symbol alias (`SYMBOL:MARKET_TYPE`) and no direct rows exist, delivery may perform one deterministic fallback lookup using canonical `SYMBOL`.
- `WS-7`: orderbook deltas require snapshot-first on the client side; snapshot gap must trigger desync and resubscribe.
- `WS-8`: protocol gate is mandatory (`hello` + `proto_ver` + required capabilities fields).

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
