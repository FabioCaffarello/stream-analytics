# Stage 69 — Execution Governance Hardening

**Date:** 2026-03-08
**Status:** COMPLETE
**Scope:** Reinforce governance, auditability, idempotency, and predictability of the execution subsystem without expanding real execution or altering upstream pipeline.

---

## Motivation

S68 left the signal layer semantically clear with explicit handoff to strategy. The execution subsystem had solid governance (three-stage pipeline, fail-closed design, 4-state control plane) but lacked:

- **Idempotency** — duplicate intents could generate duplicate events
- **Audit provenance** — no record of which grant/gate authorized or denied execution
- **Retry classification** — no distinction between transient and permanent rejections
- **Circuit breaker** — no per-adapter failure tracking or automatic protection
- **Directive forensics** — only the last directive was recorded, no history

## Changes

### 1. Reason Retryability Classification

**File:** `internal/core/execution/domain/reason.go`

Added `IsRetryable(reason string) bool` — classifies rejection reasons as transient (retryable) or permanent (not retryable). Uses exact constant matching (fail-closed: unknown reasons are NOT retryable).

**Retryable (transient) reasons (7):**
- `credentials_lease_expired` — lease can be refreshed
- `credentials_lease_inactive` — lease can be reactivated
- `credentials_unavailable_material_missing` — provider may recover
- `control_plane_paused` — operator may resume
- `control_plane_drained` — operator may resume
- `venue_runtime_failed_adapter_call` — venue may recover
- `adapter_selection_denied_circuit_open` — cooldown will expire

**All other reasons (30+) are permanent** — policy violations, structural mismatches, or terminal states.

**Tests:** `domain/reason_test.go` — 5 tests (42 sub-tests), exhaustive coverage of all constants.

### 2. Audit Provenance Enrichment (GovernanceRef)

**Files:** `domain/event.go`, `domain/audit.go`

Added `GovernanceRef` struct to `ExecutionProvenance`:
```go
type GovernanceRef struct {
    GrantID    string `json:"grant_id"`
    AdapterID  string `json:"adapter_id"`
    Mode       string `json:"mode"`
    Decision   string `json:"decision"` // allowed, denied_authorization, denied_adapter, etc.
}
```

Backwards-compatible: zero value is valid empty struct. Existing code unaffected.

Added `ExecutionDecisionRecord` — captures the full governance evaluation for each intent:
- Gate results: ControlPlaneGate, AuthorizationGate, AdapterGate, CredentialGate
- FinalDecision: "dispatched" or "rejected"
- FinalReason + GovernanceRef

**Tests:** `domain/audit_test.go` — 4 tests covering dispatched/rejected/zero-value construction.

### 3. Idempotency Guard

**Files:** `app/idempotency.go`, `app/governed_executor.go`

Bounded LRU cache (`idempotencyCache`, cap=4096) keyed by `intentID`. On duplicate intent:
- Short-circuits before any governance evaluation
- Returns rejection with `rejected_duplicate_intent`
- Decision record gates set to "skipped"

FIFO eviction when at capacity. Empty intentID passthrough (handled by downstream validation).

**Tests:** `app/idempotency_test.go` — 8 tests (seen/not-seen, eviction, capacity, empty ID).
**Integration:** `governed_executor_test.go` — `TestGovernedExecutor_IdempotencyRejectsDuplicateIntent`.

### 4. Control Plane Directive History

**Files:** `domain/control.go`, `app/control_plane.go`

Added `DirectiveHistory []ControlDirective` to `ControlSnapshot` — ring buffer of last 32 directives for operational forensics. Deep-copied in `Snapshot()` for immutability.

**Tests:** `control_plane_test.go` — 3 new tests (history recording, cap at 32, snapshot immutability).

### 5. Adapter Circuit Breaker

**Files:** `app/adapter_health.go`, `app/governed_executor.go`

Per-adapter failure tracking with circuit breaker:
- **Threshold:** 5 consecutive failures → circuit trips
- **Cooldown:** 30s → half-open (allows one probe)
- **Success:** resets failure counter and closes circuit
- **Tripped:** rejects with `adapter_selection_denied_circuit_open` (retryable)

GovernedExecutor inspects adapter results after dispatch:
- `failed` status → `recordFailure`
- `accepted`/`placed`/`filled`/`partially_filled` → `recordSuccess`

Public `AdapterHealthSnapshots()` method for external observability.

**Tests:** `app/adapter_health_test.go` — 9 tests (trip, cooldown, reset, isolation, defaults).
**Integration:** `governed_executor_test.go` — 3 tests (trip after failures, cooldown probe, success reset).

### 6. GovernedExecutor Decision Tracking

`GovernedExecutor.ExecuteAt` now builds a complete `ExecutionDecisionRecord` at every gate, stored via `lastDecisionRecord` field with `LastDecisionRecord()` accessor. Every rejection path populates the appropriate gate, final decision, and governance ref.

## Files Summary

| Action | File |
|--------|------|
| Modified | `internal/core/execution/domain/event.go` |
| Modified | `internal/core/execution/domain/control.go` |
| Modified | `internal/core/execution/domain/reason.go` |
| Created | `internal/core/execution/domain/audit.go` |
| Created | `internal/core/execution/domain/audit_test.go` |
| Created | `internal/core/execution/domain/reason_test.go` |
| Modified | `internal/core/execution/app/governed_executor.go` |
| Modified | `internal/core/execution/app/governed_executor_test.go` |
| Modified | `internal/core/execution/app/control_plane.go` |
| Modified | `internal/core/execution/app/control_plane_test.go` |
| Created | `internal/core/execution/app/idempotency.go` |
| Created | `internal/core/execution/app/idempotency_test.go` |
| Created | `internal/core/execution/app/adapter_health.go` |
| Created | `internal/core/execution/app/adapter_health_test.go` |

## Test Results

- **execution/app:** PASS (78.9% coverage)
- **execution/domain:** PASS (14.4% coverage)
- **execution/governance:** PASS (53.0% coverage)
- **actors (12 packages):** PASS — zero regressions
- **adapters (15 packages):** PASS — zero regressions

## Constraints Honored

- No real execution expansion
- No strategy logic in execution
- No upstream pipeline changes
- No wire changes
- Boundaries preserved: Strategy → Execution → Portfolio

## Success Criteria

| Criterion | Status |
|-----------|--------|
| Idempotent intent handling | DONE — dedup cache prevents duplicate events |
| Audit trail for governance decisions | DONE — GovernanceRef + ExecutionDecisionRecord |
| Transient vs permanent rejection classification | DONE — IsRetryable() on all reason constants |
| Adapter circuit breaker | DONE — auto-trip after 5 failures, 30s cooldown |
| Directive forensics | DONE — 32-entry ring buffer in ControlSnapshot |
| Clean boundaries with Strategy/Signal/Portfolio | DONE — zero cross-domain changes |
