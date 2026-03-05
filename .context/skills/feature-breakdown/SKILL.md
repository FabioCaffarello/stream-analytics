---
type: skill
name: Feature Breakdown
description: Break down features into implementable tasks adhering to Market Raccoon architecture
skillSlug: feature-breakdown
phases: [P]
generated: 2026-03-05
status: filled
scaffoldVersion: "2.0.0"
---

# Feature Breakdown (Market Raccoon)

## When to Use
- Translating a PRD or RFC into atomic Git commits or JIRA tickets.
- Planning the execution loop for an Agent.

## Instructions

Whenever breaking down a feature, you MUST structure it around Market Raccoon's Bounded Contexts.

### Step 1: Subsystem Identification
Determine which subsystem is affected based on `docs/architecture/subsystems.md`. 
- Is it MarketData (Ingestion)? Aggregation (Orderbook/Candles)? Delivery (WS)?
- If it spans multiple, the breakdown must isolate the commits per subsystem.

### Step 2: Invariant Mapping
Identify which invariants from `docs/architecture/system-invariants.md` must be guarded.
- Add an explicit checklist step: *"Assert `make invariants-check` and `INV-DOM` (No infra cross-contamination)"*.

### Step 3: Core vs Adapter
Separate the tasks strictly into:
- **Core Domain (Unit Testable)**: `internal/core/{domain}/` updates. State-in, state-out. No I/O.
- **Actor Runtime (Integration)**: `internal/actors/` loop wiring.
- **Adapters (Optional)**: `internal/adapters/` if pulling external network data.

### Step 4: Golden Test Signoff
For every sequence or aggregative feature, explicitly define the final step as:
- *"Regenerate `player.go` hashes and ensure `make test-replay-golden` passes."*

### Example Structure for Models
```markdown
# Breakdown: {Feature}

## Phase 1: Core Domain (No I/O)
- [ ] Implement pure struct logic in `internal/core/{domain}/...`
- [ ] Add unit test targeting `INV-DET` (time-injected).

## Phase 2: Actor Integration
- [ ] Wire domain into `internal/actors/...` ensuring `context.Context` propagation.
- [ ] Ensure Subsystem Caps are respected (Backpressure check).

## Phase 3: Validation
- [ ] Run `make soak-check` locally.
- [ ] Ensure Golden Replays match checksums.
```
