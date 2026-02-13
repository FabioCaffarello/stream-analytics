# Delivery WS Contract (Envelope, Streams, Backpressure)

**Status:** Draft
**Owner:** Product Architect
**Last updated:** 2026-02-13
**Relates to:** `docs/adrs/ADR-0002-event-envelope-and-versioning.md`, `docs/adrs/ADR-0007-delivery-ws-sessions.md`, `docs/adrs/ADR-0013-backpressure-overload-policies.md`, `docs/adrs/ADR-0014-stream-partitioning-strategy.md`, `docs/contracts/event-bus.md`, `docs/rfcs/RFC-0003-W2-DELIVERY-BC.md`

## Purpose

Define WS delivery contract for marketdata/aggregation/insights streams with envelope compatibility, backpressure policy, and behavior for slow clients.

## Data Planes

### Input Plane (bus/internal)

Accepted delivery router inputs:
- `marketdata.trade.v1.{venue}.{instrument}`
- `marketdata.bookdelta.v1.{venue}.{instrument}`
- `marketdata.markprice.v1.{venue}.{instrument}`
- `marketdata.liquidation.v1.{venue}.{instrument}`
- `insights.crossvenue.trade_snapshot.v1.global.{instrument}`
- `insights.crossvenue.spread_signal.v1.global.{instrument}`
- planned: `aggregation.snapshot.v1.{venue}.{instrument}`
- planned: `insights.heatmap.bucket.v1.{venue}.{instrument}`
- planned: `insights.volume_profile.snapshot.v1.{venue}.{instrument}`

### Output Plane (WS frames)

Canonical WS subject:
- `<stream_type>/<venue>/<symbol>/<timeframe>`

Examples:
- `marketdata.trade/binance/BTC-USDT/raw`
- `marketdata.markprice/bybit/BTC-USDT/raw`
- `insights.crossvenue.trade_snapshot/global/BTC-USDT/raw`
- `aggregation.snapshot/binance/BTC-USDT/raw` (planned)

## Contracts

### Client -> Server Commands

```json
{
  "op": "subscribe|unsubscribe|getrange",
  "subject": "marketdata.trade/binance/BTC-USDT/raw",
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
  "subject": "marketdata.trade/binance/BTC-USDT/raw"
}
```

Event:
```json
{
  "type": "event",
  "subject": "marketdata.trade/binance/BTC-USDT/raw",
  "seq": 123,
  "ts_ingest": 1710000005000,
  "content_type": "application/json",
  "payload": {}
}
```

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
  "subject": "marketdata.trade/binance/BTC-USDT/raw",
  "items": []
}
```

## Invariants

- `WS-1`: session is an isolated actor; one session failure cannot cascade.
- `WS-2`: WS subject always has 4 segments (`stream_type/venue/symbol/timeframe`).
- `WS-3`: per-subject ordering by `seq` is preserved inside one session.
- `WS-4`: no unbounded per-session queue.
- `WS-5`: unsubscribe/remove session must release routing state and memory.

## Backpressure

Per-session policy:
1. bounded outbound buffer;
2. for non-critical streams (`heatmap`, `volume_profile`), drop `oldest` and keep latest;
3. for critical streams (`orderbook`, `markprice`), prioritize delivery and cap throughput;
4. if client stays slow above threshold, graceful disconnect.

Required metrics:
- `delivery_ws_queue_depth{session_id}`
- `delivery_ws_drop_total{stream_type,reason}`
- `delivery_ws_slow_client_total{reason}`

## Storage Strategy

- Primary hot source: in-memory read models.
- Secondary durable hot source (planned): Timescale for multi-instance `getrange`.
- Cold path (ClickHouse) is not read directly by WS in real time; only via backfill jobs.

## Replay Strategy

- `getrange` must be deterministic for same window and limit.
- Operational replay (fixture/jetstream) must reproduce equivalent frames per `subject`.
- Output ordering validated by `(ts_ingest, seq)`.

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

Planned test names:
- `TestWSSessionSubscribeUnsubscribeContract`
- `TestWSRouterBroadcastOnlySubscribedSessions`
- `TestWSSubjectValidation4Segments`
- `TestWSBackpressureSlowClientDropPolicy`
- `TestWSRangeDeterminismReplay`
- `TestWSRaceSubscribeUnsubscribeNoLeak`

Scenarios:
- persistent slow client;
- marketdata bursts on multiple instruments;
- reconnect with resubscribe;
- concurrent subscribe/unsubscribe races.

## Compatibility

- N/N-1 compatibility by envelope event version.
- New WS frame fields must remain optional.
- Do not remove mandatory fields without new `frame_version`.
- Default `content_type`: `application/json`; protobuf WS remains future opt-in.

## Evidence Hooks

Current evidence:
- `internal/core/delivery/domain/subject.go`
- `internal/actors/delivery/runtime/router.go`
- `internal/actors/delivery/runtime/session.go`
- `internal/actors/delivery/runtime/session_test.go`
- `internal/actors/delivery/runtime/router_test.go`
- `docs/rfcs/RFC-0003-W2-DELIVERY-BC.md`

TODO hooks (skeleton):
- `internal/core/delivery/domain/backpressure_policy.go` (TODO)
- `internal/actors/delivery/runtime/session_backpressure_test.go` (TODO)
- `internal/interfaces/ws/delivery_contract_e2e_test.go` (TODO)

## Failure Modes

- Slow WS client accumulating backlog:
  - Mitigation: per-stream drop policy + controlled disconnect.
- Network jitter/intermittency:
  - Mitigation: keepalive + reconnect/resubscribe idempotency.
- Poison command frame:
  - Mitigation: structured error without panic and keep session alive.
- Ack-on-enqueue in upstream stage:
  - Mitigation: enforce ack-on-commit before delivery stage.
