# Feature Pack: Delivery WS

## Purpose
- Delivery WS constraints only; authority: [delivery-ws](../../../docs/contracts/delivery-ws.md), [event-bus](../../../docs/contracts/event-bus.md), [ADR-0007](../../../docs/adrs/ADR-0007-delivery-ws-sessions.md).

## Inputs/Outputs
- Inputs: `marketdata.trade.v1.{venue}.{instrument}`, `marketdata.bookdelta.v1.{venue}.{instrument}`, `marketdata.markprice.v1.{venue}.{instrument}`, `marketdata.liquidation.v1.{venue}.{instrument}`.
- Outputs: WS subject `<stream_type>/<venue>/<symbol>/<timeframe>` and event frame fields `type|subject|seq|ts_ingest|payload`.
- Ordering refs: [ADR-0014](../../../docs/adrs/ADR-0014-stream-partitioning-strategy.md), [ADR-0015](../../../docs/adrs/ADR-0015-deterministic-replay-time-invariants.md).

## Invariants
- One connection maps to one isolated session actor ([ADR-0007](../../../docs/adrs/ADR-0007-delivery-ws-sessions.md)).
- Subject keeps exactly 4 segments ([delivery-ws](../../../docs/contracts/delivery-ws.md)).
- Unsubscribe/disconnect releases routing state ([delivery-ws](../../../docs/contracts/delivery-ws.md)).

## Backpressure
- Bounded queue policy with observable drops is mandatory ([ADR-0013](../../../docs/adrs/ADR-0013-backpressure-overload-policies.md)).
- Non-critical streams may use keep-latest when policy is explicit ([delivery-ws](../../../docs/contracts/delivery-ws.md)).

## Replay
- Range queries must be deterministic for same window and limit ([ADR-0015](../../../docs/adrs/ADR-0015-deterministic-replay-time-invariants.md)).
- Golden replay requirements: [RFC-0009](../../../docs/rfcs/RFC-0009-W8-deterministic-replay-golden-tests.md).

## Evidence Hooks
- `internal/core/delivery/domain/subject.go:25`
- `internal/core/delivery/app/session_usecase.go:36`
- `internal/actors/delivery/runtime/session.go:57`
- `internal/actors/delivery/runtime/router.go:43`
- TODO: `internal/core/delivery/domain/backpressure_policy.go`

## Acceptance Tests
- `TestParseSubject` -> `internal/core/delivery/domain/subject_test.go:10`
- `TestParseSubject_invalid` -> `internal/core/delivery/domain/subject_test.go:20`
- `TestSession_parseSubscribeUnsubscribeGetRange` -> `internal/actors/delivery/runtime/session_test.go:43`
- `TestSession_disconnectTriggersUnregister` -> `internal/actors/delivery/runtime/session_test.go:85`
- `TestRouter_subscribeUnsubscribeAndBroadcast` -> `internal/actors/delivery/runtime/router_test.go:51`
- `TestSessionService_GetRange_storeUnavailable` -> `internal/core/delivery/app/session_usecase_test.go:49`
- TODO: `TestWSBackpressureSlowClientDropPolicy` -> `internal/actors/delivery/runtime/session_backpressure_test.go`
