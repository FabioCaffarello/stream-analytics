---
type: doc
name: testing-strategy
description: Testing approaches, CI configurations, and test data management
category: qa
generated: 2026-03-05
status: filled
scaffoldVersion: "2.0.0"
---

# Testing Strategy

Market Raccoon tests are governed by the **Doc-First Strategy**. Acceptance Criteria defined in RFCs, ADRs, and Invariants must strictly translate to automated tests.

## 1. Domain Tests (Fast, Memory-Only)
`internal/core/*` packages contain 100% of the domain behavior. These tests execute instantly, simulate complex orderbooks or aggregates via struct inputs, and assert outputs reliably. They MUST NEVER rely on real databases, NATS clusters, or HTTP mocks.
*Target:* `make test-unit`

## 2. Invariants & Architecture Gates
Market Raccoon leverages specialized custom linting to enforce boundaries.
- `make invariants-check`: Verifies `INV-DOM` (business logic cannot import infrastructural logic).
- `validate_boundedness_matrix.mjs`: Ensures all subsystem caps are properly respected in their structures.

## 3. Soak & Pipeline Validation
To prevent Out-Of-Memory (OOM) bursts, memory leaks, or logical backpressure failures, we run deterministic bursts.
- `make soak-check`, `make soak-vpvr`, `make soak-c4-production`: Heavy sustained workloads that assert metrics (like `ws_backpressure_drops_total`) stay within budget limits.

## 4. Replay & Determinism (IQ Loop)
**Determinism is non-negotiable.** The system is capable of re-running `player.go` (`make test-replay-golden`) which hashes the output sequences via SHA arrays to guarantee exact identical event reconstruction.

Testing MUST cover missing timestamps, gap-filling resilience, regression tolerance, and out-of-order sequencing logic. See `docs/architecture/iq-loop-invariants.md`.
