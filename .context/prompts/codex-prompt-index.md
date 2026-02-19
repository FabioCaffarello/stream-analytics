# Codex Prompt Orchestration — M4 Integration Chain

## Execution Order

These prompts MUST be executed sequentially. Each builds on the output of the previous.

| # | Prompt | Focus | Key Deliverables | Gate |
|---|--------|-------|-------------------|------|
| **A** | `codex-prompt-A-wire-m4-pipeline.md` | Wire M4 into processor | bootstrap fix, routing, handler methods, integration tests | `make test-workspace-race` |
| **B** | `codex-prompt-B-contracts-registry-config.md` | Contracts + config | codec registry, delivery policy, subject registry, config schema | `make docs-check && make test-workspace` |
| **C** | `codex-prompt-C-tests-hardening-delivery.md` | Tests + delivery | golden determinism, soak tests, WS routing, E2E pipeline | `make test-workspace-race` |

## Dependency Graph

```
Prompt A (wire pipeline)
    ↓
Prompt B (contracts + config)
    ↓
Prompt C (tests + delivery + hardening)
```

## Pre-flight Check (before starting Prompt A)

```bash
# Verify M4 domain/app layer is intact
go test ./internal/core/aggregation/... -count=1 -race
# Verify existing processor tests pass
go test ./internal/actors/aggregation/runtime/... -count=1 -race
# Verify docs gate
make docs-check && make invariants-check
```

## Post-flight Check (after completing all 3)

```bash
make docs-check
make invariants-check
make test-workspace
make test-workspace-race
```

## Files Modified Summary

### Prompt A (5 files modified, 0 new)
- `cmd/processor/bootstrap.go` — fix wiring + add store stubs
- `internal/actors/aggregation/runtime/processor.go` — extend routing
- `internal/actors/aggregation/runtime/processor_test.go` — extend tests

### Prompt B (8 files modified, 0 new)
- `internal/shared/contracts/payload_registry.go` — register candle/stats
- `internal/core/delivery/domain/envelope_policy.go` — delivery contracts
- `docs/contracts/subject-registry.yaml` — subject entries
- `docs/contracts/event-bus.md` — subject matrix
- `internal/shared/config/schema.go` — config fields
- `cmd/processor/bootstrap.go` — wire config
- `cmd/processor/config.jsonc` — template update
- `.context/docs/feature-packs/*.md` — drift marker cleanup

### Prompt C (6 new test files)
- `internal/core/aggregation/app/build_candle_golden_test.go`
- `internal/core/aggregation/app/build_stats_golden_test.go`
- `internal/core/aggregation/app/build_candle_soak_test.go`
- `internal/core/aggregation/app/build_stats_soak_test.go`
- `internal/actors/aggregation/runtime/processor_e2e_test.go`
- `internal/interfaces/ws/candle_stats_delivery_contract_test.go`

## Total Commit Chain

```
feat(m4): wire candle+stats pipeline into processor
feat(m4): register candle+stats contracts and config schema
test(m4): add candle+stats golden determinism tests
test(m4): add candle+stats soak tests for bounded cardinality
feat(m4): extend delivery routing for candle+stats subjects
test(m4): add E2E processor and WS delivery contract tests
```
