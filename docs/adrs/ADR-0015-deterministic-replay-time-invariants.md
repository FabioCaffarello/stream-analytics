# ADR-0015 — Deterministic Replay & Time Invariants

**Status:** Accepted
**Date:** 2026-02-12
**Deciders:** Chief Architect
**Relates to:** PRD-0001 section E.5, RFC-0009 (W8)

---

## Amendment (2026-02-13)

Accepted after W8 replay/golden implementation and deterministic validation.

Changelog evidence:
- Replay fixture writer/reader stack: `internal/shared/replay/writer.go`, `internal/shared/replay/canon.go` (`file:symbol Writer`, `file:symbol DecodeFixtureRecord`).
- Golden replay tests: `internal/shared/replay/golden_test.go` (`file:test TestGoldenReplay`, `file:test TestGoldenReplayByteStable50Runs`).
- Sequencer monotonic replay checks: `internal/shared/replay/sequencer_test.go` (`file:test TestReplaySequencerMonotonicPerStreamDeterministic`).
- Consumer replay golden test: `cmd/consumer/replay_test.go` (`file:test TestReplayIngestGolden1000`).
-- Core purity guard script (`time.Now` ban in `internal/core`): `scripts/ci/check-domain-isolation.sh` (`file:symbol scan_time_now_with_rg`).

## Context

The system's competitive moat depends on deterministic event pipelines (docs/prd/moat.md). Currently:
- Domain logic uses `clock.Clock` port — good, enables deterministic time.
- Sequencer is in-memory and ephemeral — lost on restart.
- No mechanism to record and replay event streams.
- No golden tests to validate replay determinism.

PRD-0001 section E.5 defines the replay architecture. This ADR formalizes the invariants.

## Decision

### INV-R1: Domain Logic is Pure

All code in `internal/core/*/domain/` and `internal/core/*/app/` MUST be deterministic given:
- Same input messages
- Same clock values
- Same sequencer values

No implicit time (`time.Now()`), no randomness, no I/O in domain logic. All non-determinism is injected via ports.

**Verification:** Grep for `time.Now()` in `internal/core/` — must return 0 results. `clock.Clock` is the only time source.

### INV-R2: Sequence Monotonicity Per Stream

For any `(venue, instrument)` pair, `seq(n+1) > seq(n)` always. Violations produce `MD_OUT_OF_ORDER` problem and are rejected.

**Verification:** Already enforced in `InstrumentStream.BuildEnvelope()`. Golden test validates over 1000-event fixture.

### INV-R3: Timestamp Authority

- `TsExchange`: advisory timestamp from exchange. May be zero, out-of-order, or skewed. Used for display only.
- `TsIngest`: authoritative timestamp from our clock. Used for ordering, TTL, and business logic.

In replay mode, `TsIngest` comes from the recorded fixture, not from wall clock.

### INV-R4: Idempotency Keys Are Deterministic

`IdempotencyKey = hash.HashFields(venue, instrument, eventType, fmt.Sprint(seq))` when not source-provided. This is a pure function of inputs — replay produces identical keys.

**Verification:** Golden test: replay fixture → compare idempotency keys byte-for-byte.

### INV-R5: Codec Registry Is Append-Only

Old decoders are never removed from `codec.Registry`. This ensures any fixture from any point in time can be replayed regardless of current schema version.

**Verification:** Code review rule. Removing a decoder entry is a breaking change flagged in PR review.

### Replay Architecture

```
Recording:
  Recorder wraps EventPublisher
  → intercepts Publish()
  → writes envelope as JSON line to fixture file
  → forwards to real publisher

Replaying:
  Player reads fixture file (JSON-lines)
  → injects FakeClock with TsIngest from each envelope
  → injects ReplaySequencer that returns seq from fixture
  → calls IngestMarketData.Execute() per envelope
  → captures output envelopes
  → compares against golden file
```

### Replay Modes

| Mode | Sequencer | Clock | Use Case |
|------|-----------|-------|----------|
| **Full replay** | Fixture seq | Fixture TsIngest | System rebuild, golden tests |
| **Catchup window** | Live seq (continue from last) | Wall clock | Recovery after downtime |
| **Live + record** | Live seq | Wall clock | Fixture capture for future replay |

### Fixture Format

JSON-lines (`.jsonl`), one envelope per line:
```json
{"type":"marketdata.trade","version":1,"venue":"BINANCE","instrument":"BTCUSDT","ts_exchange":1710000001000,"ts_ingest":1710000002000,"seq":1,"idempotency_key":"abc...","meta":{},"payload":"base64..."}
```

Advantages:
- Streamable (no need to load entire file)
- Append-only (no corruption from partial writes)
- Human-readable (debuggable with `jq`)

## Rationale

Deterministic replay is the foundation for:
- **Correctness testing:** golden tests catch unintended behavior changes
- **Backtesting:** replaying historical data through new logic
- **Debugging:** reproducing production issues with recorded fixtures
- **Compliance:** auditable proof that same inputs produce same outputs

## Alternatives Considered

1. **Event sourcing with persistent store:** Deferred — JetStream provides replay via durable consumers. Full event sourcing is a future optimization.
2. **Binary fixture format (protobuf):** Deferred until W6 (protobuf layer). JSON-lines is sufficient and human-debuggable.
3. **Non-deterministic replay (skip clock/seq injection):** Rejected — defeats the purpose of replay.

## Consequences

### Positive
- Golden tests catch regressions in domain logic deterministically
- Recorded fixtures serve as integration test data
- Replay validates that schema version migrations don't break processing

### Negative
- Fixture files can be large (1000 envelopes ~ 5MB JSON-lines)
- ReplaySequencer adds a new Sequencer implementation (simple, but new code)
- Golden test maintenance: intentional output changes require regenerating golden files

### Invariants (testable)
- `R1`: `grep -r "time.Now()" internal/core/` returns 0 matches
- `R2`: Replay of 1000-event fixture produces identical output envelopes (byte-for-byte golden test)
- `R3`: Replay with FakeClock uses fixture timestamps (assert `TsIngest` in output matches fixture)
- `R4`: Idempotency keys in replay output match keys in fixture (golden test)
- `R5`: Adding new decoder to codec.Registry does not break existing fixture replay

## Rollout Plan

1. Implement `internal/shared/replay/recorder.go` (RFC-0009/W8)
2. Implement `internal/shared/replay/player.go` with FakeClock + ReplaySequencer (RFC-0009/W8)
3. Record 1000-event fixture from live Binance stream (RFC-0009/W8)
4. Create golden test: `TestGoldenReplay` (RFC-0009/W8)
5. Add `-record` and `-replay` flags to `cmd/consumer` (RFC-0009/W8)
6. Add INV-R1 grep check to CI (RFC-0005/W4)

## Evidence

- Validation gate: `make docs-check-full`
- Authority path: file-local ADR source.
