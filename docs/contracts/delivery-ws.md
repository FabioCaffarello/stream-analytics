# Delivery WS Contract (Envelope, Streams, Backpressure)

**Status:** Active
**Owner:** Product Architect
**Last updated:** 2026-02-13
**Relates to:** `docs/adrs/ADR-0002-event-envelope-and-versioning.md`, `docs/adrs/ADR-0007-delivery-ws-sessions.md`, `docs/adrs/ADR-0013-backpressure-overload-policies.md`, `docs/adrs/ADR-0014-stream-partitioning-strategy.md`, `docs/contracts/event-bus.md`, `docs/rfcs/RFC-0003-W2-DELIVERY-BC.md`

## Purpose

Define WS delivery contract for marketdata/aggregation/insights streams with explicit separation between current behavior and planned parity extensions.

## Terminology (canonical)

- `subject` (WS): `<stream_type>/<venue>/<symbol>/<timeframe>`.
- `subject` (bus): `{event}.v{version}.{venue}.{instrument}`.
- `stream_type`: namespaced event token (for example `marketdata.trade`).
- `symbol`: client-facing token in WS subject; canonicalized from `instrument` (`BTC-USDT` -> `BTCUSDT`).
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
- planned: `insights.<heatmap_event>.v1.{venue}.{instrument}` (TBD registry key)
- planned: `insights.<volume_profile_event>.v1.{venue}.{instrument}` (TBD registry key)

### Output Plane (WS frames)

Canonical WS subject:
- `<stream_type>/<venue>/<symbol>/<timeframe>`

Examples:
- `marketdata.trade/binance/BTCUSDT/raw`
- `marketdata.markprice/bybit/BTCUSDT/raw`
- `insights.crossvenue.trade_snapshot/global/BTCUSDT/raw`
- `aggregation.snapshot/binance/BTCUSDT/raw`

## Contracts

### Client -> Server Commands

```json
{
  "op": "subscribe|unsubscribe|getrange",
  "subject": "marketdata.trade/binance/BTCUSDT/raw",
  "request_id": "r1",
  "params": {
    "from_ms": 0,
    "to_ms": 0,
    "limit": 100
  }
}
```

### Server -> Client Frames

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

## Backpressure

Current runtime behavior:
1. session lifecycle isolation and cleanup are implemented;
2. bounded per-session outbound queue is implemented;
3. drop policy is `drop_newest` with reason `queue_full`;
4. connection write failures close the session.

Planned parity policy:
1. stream-priority policies (`keep-latest` vs `drop_newest`) per stream class;
2. slow clients disconnected after threshold breach.

Required metrics:
- `ws_queue_depth`
- `ws_drops_total{reason}`
- `ws_send_latency_ms`
- `ws_clients_connected`

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
| Router broadcast only to subscribed sessions | Existing | `internal/actors/delivery/runtime/router.go` | `internal/actors/delivery/runtime/router_test.go:TestRouter_subscribeUnsubscribeAndBroadcast` |
| Disconnect cleanup and unregister | Existing | `internal/actors/delivery/runtime/session.go` | `internal/actors/delivery/runtime/session_test.go:TestSession_disconnectTriggersUnregister` |
| Deterministic range from durable store | Planned | `internal/core/delivery/ports/ports.go` | `internal/core/delivery/app/session_usecase_test.go:TestSessionService_GetRange_storeUnavailable` |
| Slow-client backpressure policy | Existing (drop_newest baseline) | `internal/actors/delivery/runtime/session.go` | `internal/actors/delivery/runtime/session_test.go:TestSession_backpressureDropsWhenQueueFull` |

## Observability

- `delivery_ws_active_sessions`
- `delivery_ws_subscriptions_total`
- `delivery_ws_queue_depth`
- `delivery_ws_drop_total`
- `delivery_ws_frame_latency_ms`
- `delivery_ws_range_latency_ms`

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

Tests to create for parity completion:
- `internal/actors/delivery/runtime/session_backpressure_test.go:TestWSBackpressureSlowClientDropPolicy` (TODO)
- `internal/interfaces/ws/delivery_contract_e2e_test.go:TestWSRangeDeterminismReplay` (TODO)
- `internal/interfaces/ws/delivery_contract_e2e_test.go:TestWSRaceSubscribeUnsubscribeNoLeak` (TODO)
- `internal/interfaces/ws/delivery_contract_e2e_test.go:TestWSReconnectResubscribeIdempotent` (TODO)

## Compatibility

- N/N-1 compatibility by envelope event version.
- New WS frame fields must remain optional.
- Do not remove mandatory fields without new `frame_version`.
- Default bus payload content type remains `application/json` with protobuf opt-in in ADR-0016.

## Evidence Hooks

Current evidence:
- `internal/core/delivery/domain/subject.go`
- `internal/actors/delivery/runtime/router.go`
- `internal/actors/delivery/runtime/session.go`
- `internal/actors/delivery/runtime/session_test.go`
- `internal/actors/delivery/runtime/router_test.go`
- `internal/core/delivery/app/session_usecase_test.go`
- `docs/rfcs/RFC-0003-W2-DELIVERY-BC.md`

TODO hooks (skeleton):
- `internal/core/delivery/domain/backpressure_policy.go` (TODO)
- `internal/actors/delivery/runtime/session_backpressure_test.go` (TODO)
- `internal/interfaces/ws/delivery_contract_e2e_test.go` (TODO)

## Failure Modes

- Slow WS client accumulation without explicit drop policy:
  - Mitigation: implement bounded outbound buffer and slow-client policy before parity close.
- network jitter/intermittency:
  - Mitigation: keepalive + reconnect/resubscribe idempotency.
- poison command frame:
  - Mitigation: structured error response without panic and keep session alive.
- upstream ack-on-enqueue drift:
  - Mitigation: enforce ack-on-commit before delivery stage.
