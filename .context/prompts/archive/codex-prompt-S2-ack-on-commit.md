# Codex Prompt S2 — ACK-on-Commit Boundary Hardening

## Project Identity

Market Raccoon is a high-performance market intelligence platform. Go 1.25+, Hollywood actor model, NATS JetStream, DDD hexagonal architecture.

---

## Context

After S1, real Timescale and ClickHouse drivers exist. However, the JetStream consumer currently ACKs messages after **parsing**, not after **durable commit**. This means data loss on crash between parse and commit.

**Current flow (BROKEN):**
```
JetStream msg → parse envelope → ACK → route to use case → commit to storage
                                  ^-- ACK too early! Data not yet persisted.
```

**Required flow:**
```
JetStream msg → parse envelope → route to use case → commit to storage → ACK
                                                                          ^-- ACK only after durable write
```

**Key files:**
- `internal/adapters/jetstream/consumer.go` — JetStream consumer with ACK/NAK/TERM
- `internal/adapters/jetstream/ingest_policy.go` — Disposition logic (ACK on success, NAK on transient, TERM on poison)
- `cmd/processor/bootstrap.go` — Wires `OnEnvelopeProcessed` callback for ACK decisions
- `internal/adapters/storage/committer.go` — `CommitAndAck()` pattern already exists but not wired end-to-end
- `internal/adapters/jetstream/ingest_conformance_test.go` — Golden table ACK/NAK/TERM tests

---

## Mandatory Patterns

### Errors: `*problem.Problem` (never plain `error` in domain/app)
### ACK semantics (ADR-0015 / docs/architecture/storage.md STO-6):
- **ACK:** Only after hot+cold write committed successfully
- **NAK:** On transient failure (network, timeout) — message redelivered
- **TERM:** On poison message (decode failure, validation failure) — message discarded

---

## Task: Harden ACK-on-Commit Boundary

### Step 1: Audit current ACK flow

Read these files to understand the current ACK mechanism:

1. `cmd/processor/bootstrap.go` lines 280-330 (JetStream source setup)
   - The consumer callback currently: enqueue to channel → wait for result → return problem
   - The result's Problem determines ACK/NAK/TERM

2. `internal/adapters/jetstream/consumer.go` — the `Consume` method
   - Callback returns `*problem.Problem`
   - `nil` → ACK, `Retryable` → NAK, else → TERM

3. `internal/actors/aggregation/runtime/processor.go` — `handleEnvelope`
   - Returns `*problem.Problem` which flows back through `OnEnvelopeProcessed`

**The current flow actually routes problems back through the channel, so ACK happens after processing.** Verify this is true end-to-end. The concern is whether the hot/cold write in `committedHotStore.Save()` happens BEFORE the problem result is sent back.

### Step 2: Verify the commit-before-ACK chain

Trace the exact flow:
1. JetStream consumer calls callback with envelope
2. Callback enqueues envelope to `ch` channel
3. ProcessorSubsystemActor receives envelope, calls `handleBookDelta`
4. `handleBookDelta` calls `Service.UpdateBook.Execute()` which calls `publisher.PublishSnapshot()` and `store.Save()`
5. The Execute result flows back through `OnEnvelopeProcessed` → `resultsCh`
6. Consumer callback reads from `resultsCh` and returns the problem
7. Consumer maps problem to ACK/NAK/TERM

**If `store.Save()` (the real Timescale write) happens INSIDE `Execute()`, then ACK already waits for commit.** Verify this by reading the `UpdateOrderBookFromEvents.Execute()` return path.

### Step 3: Fix any gaps

If the flow IS correct (commit before ACK), then the main task is:

1. **Add explicit contract test** proving ACK only happens after commit
2. **Add observability** — metric `processor_ack_after_commit_total` (success/failure)
3. **Add timeout protection** — if commit takes too long, NAK instead of hanging

If the flow is NOT correct (ACK before commit), refactor to ensure commit happens before the result is sent back through the channel.

### Step 4: Add conformance tests

**File:** `internal/adapters/jetstream/ack_commit_conformance_test.go` (NEW)

```go
func TestACKOnlyAfterCommit_Success(t *testing.T) {
    // 1. Set up processor with a spy storage writer that tracks commit order.
    // 2. Send message through JetStream consumer callback.
    // 3. Assert: commit happened BEFORE ACK.
    // 4. Assert: ACK was sent (not NAK/TERM).
}

func TestNAKOnTransientCommitFailure(t *testing.T) {
    // 1. Set up processor with a storage writer that returns Retryable problem.
    // 2. Send message.
    // 3. Assert: NAK was sent (not ACK).
    // 4. Assert: message will be redelivered.
}

func TestTERMOnPoisonMessage(t *testing.T) {
    // 1. Send malformed envelope.
    // 2. Assert: TERM was sent (not ACK/NAK).
    // 3. Assert: no commit attempt.
}

func TestACKTimeout_NAKOnSlowCommit(t *testing.T) {
    // 1. Storage writer sleeps for longer than ack_wait.
    // 2. Assert: consumer deadline triggers NAK.
}

func TestIdempotentRedelivery_ACKOnDuplicate(t *testing.T) {
    // 1. Send same message twice (same idempotency key).
    // 2. First: commit + ACK.
    // 3. Second: idempotent skip (ON CONFLICT DO NOTHING) + ACK.
}
```

### Step 5: Add commit ordering spy

**File:** `internal/adapters/storage/commit_spy_test.go` (NEW)

```go
type commitOrderSpy struct {
    mu       sync.Mutex
    commits  []string // track commit calls in order
    ackOrder []string // track when ACK happens relative to commit
}

func (s *commitOrderSpy) Save(ctx context.Context, snap aggdomain.SnapshotProduced) *problem.Problem {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.commits = append(s.commits, snap.BookID.Venue+"/"+snap.BookID.Instrument)
    return nil
}

// Use this spy in the conformance tests to prove commit order.
```

### Step 6: Add ack_wait config and timeout handling

**File:** `internal/shared/config/schema.go`

Ensure `JetStreamConfig.AckWait` is properly configured:
```go
// Default: 30s — must be longer than expected commit latency
if c.JetStream.AckWait == "" {
    c.JetStream.AckWait = "30s"
}
```

### Step 7: Update processor commit metrics

**File:** `internal/actors/aggregation/runtime/processor.go`

After a successful commit, emit:
```go
metrics.IncProcessorCommit("success")
metrics.ObserveProcessorCommitLatency(elapsed.Seconds())
```

After a failed commit:
```go
metrics.IncProcessorCommit("failed")
```

---

## Reference Files

| File | Purpose |
|------|---------|
| `internal/adapters/jetstream/consumer.go` | JetStream consumer (ACK logic) |
| `internal/adapters/jetstream/ingest_policy.go` | Disposition mapping |
| `internal/adapters/jetstream/ingest_conformance_test.go` | Existing ACK tests |
| `cmd/processor/bootstrap.go` | Wiring (OnEnvelopeProcessed) |
| `internal/adapters/storage/committer.go` | CommitAndAck pattern |
| `internal/actors/aggregation/runtime/processor.go` | Envelope processing |
| `internal/core/aggregation/app/update_orderbook.go` | Use case (commit path) |
| `docs/architecture/storage.md` | STO-6 ACK-on-commit contract |

---

## Execution Rules

```bash
make test-workspace
make test-workspace-race
make invariants-check
```

### STOP CONDITIONS:
- ACK emitted before durable commit (ack-on-enqueue)
- Hanging consumer on slow commit (must timeout → NAK)
- Duplicate message causes error (must be idempotent → ACK)

### Commit:
```
fix(s2): harden ACK-on-commit boundary with conformance tests

- Verify commit-before-ACK chain is correct end-to-end
- Add ack_commit_conformance_test.go proving ACK order
- Add commit ordering spy for testability
- Add timeout protection: slow commit → NAK
- Add idempotent redelivery test: duplicate → ACK

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>
```

---

## Important Constraints

1. **ACK MUST wait for commit** — this is a data safety invariant, not optional
2. **NAK on transient** — database timeout, connection drop → redelivery
3. **TERM on poison** — decode failure, validation failure → discard forever
4. **Idempotent writes** — duplicate ACK after ON CONFLICT DO NOTHING is correct behavior
5. **Metrics** — every ACK/NAK/TERM must be observable
6. **No blocking consumers** — if commit hangs, ack_wait deadline triggers NAK automatically
