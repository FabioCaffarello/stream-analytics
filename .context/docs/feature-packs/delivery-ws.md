# Feature Pack: Delivery WS

## Purpose
- Keep WS session/router contract aligned with current runtime behavior.
- Separate existing delivery behavior from planned parity extensions.
- Make delivery backpressure and replay expectations explicit and testable.

## Inputs/Outputs
- Authority: [`docs/contracts/delivery-ws.md`](../../../docs/contracts/delivery-ws.md), [`docs/contracts/event-bus.md`](../../../docs/contracts/event-bus.md), [`docs/adrs/ADR-0007-delivery-ws-sessions.md`](../../../docs/adrs/ADR-0007-delivery-ws-sessions.md), [`docs/adrs/ADR-0014-stream-partitioning-strategy.md`](../../../docs/adrs/ADR-0014-stream-partitioning-strategy.md).
- Inputs (bus subjects currently/planned accepted by delivery routing):
- `marketdata.trade.v1.{venue}.{instrument}`
- `marketdata.bookdelta.v1.{venue}.{instrument}`
- `marketdata.markprice.v1.{venue}.{instrument}`
- `marketdata.liquidation.v1.{venue}.{instrument}`
- `insights.crossvenue.trade_snapshot.v1.global.{instrument}`
- `insights.crossvenue.spread_signal.v1.global.{instrument}`
- `aggregation.snapshot.v1.{venue}.{instrument}` (planned)
- Outputs:
- WS subject format: `<stream_type>/<venue>/<symbol>/<timeframe>`
- Current runtime event frame fields: `type`, `subject`, `seq`, `ts_ingest`, `payload`

## Invariants
- One WS connection maps to one isolated session actor ([`ADR-0007`](../../../docs/adrs/ADR-0007-delivery-ws-sessions.md)).
- WS subject keeps 4 segments (`stream_type/venue/symbol/timeframe`) ([`docs/contracts/delivery-ws.md`](../../../docs/contracts/delivery-ws.md)).
- Per-subject ordering in a session remains stable ([`ADR-0014`](../../../docs/adrs/ADR-0014-stream-partitioning-strategy.md)).
- Session unsubscribe/disconnect must release routing state ([`docs/contracts/delivery-ws.md`](../../../docs/contracts/delivery-ws.md)).

## Backpressure
- Current runtime: lifecycle isolation exists; explicit slow-client queue policy is still TODO ([`docs/contracts/delivery-ws.md`](../../../docs/contracts/delivery-ws.md)).
- Policy authority is [`ADR-0013`](../../../docs/adrs/ADR-0013-backpressure-overload-policies.md): bounded queue + observable drops + deterministic policy.
- Non-critical streams can use keep-latest strategy when policy is implemented ([`docs/contracts/delivery-ws.md`](../../../docs/contracts/delivery-ws.md)).

## Replay
- Deterministic replay/time authority is [`ADR-0015`](../../../docs/adrs/ADR-0015-deterministic-replay-time-invariants.md).
- `getrange` behavior should stay deterministic for same window+limit (contract requirement).
- Reuse replay package (`internal/shared/replay/*`) for fixture-backed validation when adding range parity tests.

## Evidence Hooks
- `internal/core/delivery/domain/subject.go`
- `internal/core/delivery/app/session_usecase.go`
- `internal/actors/delivery/runtime/session.go`
- `internal/actors/delivery/runtime/router.go`
- `internal/actors/delivery/runtime/session_test.go`
- TODO: `internal/core/delivery/domain/backpressure_policy.go`
- TODO: `internal/interfaces/ws/delivery_contract_e2e_test.go`

## Acceptance Tests
- `TestParseSubject` - `internal/core/delivery/domain/subject_test.go`
- `TestParseSubject_invalid` - `internal/core/delivery/domain/subject_test.go`
- `TestSession_parseSubscribeUnsubscribeGetRange` - `internal/actors/delivery/runtime/session_test.go`
- `TestSession_disconnectTriggersUnregister` - `internal/actors/delivery/runtime/session_test.go`
- `TestRouter_subscribeUnsubscribeAndBroadcast` - `internal/actors/delivery/runtime/router_test.go`
- `TestSessionService_GetRange_storeUnavailable` - `internal/core/delivery/app/session_usecase_test.go`
- TODO: `TestWSBackpressureSlowClientDropPolicy` - `internal/actors/delivery/runtime/session_backpressure_test.go`
- TODO: `TestWSRangeDeterminismReplay` - `internal/interfaces/ws/delivery_contract_e2e_test.go`
