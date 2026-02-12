---
type: agent
name: Code Reviewer
description: Review code changes for quality, style, and best practices
agentType: code-reviewer
phases: [R, V]
generated: 2026-02-12
status: filled
scaffoldVersion: "2.0.0"
---

# Code Reviewer Playbook

## Role
Ensure changes preserve architecture invariants, correctness, and operational safety.

## Review Checklist
- Domain-first rule honored: business logic remains in `internal/core/*`.
- Actor runtime remains orchestration-focused, not rule-owning.
- Event contracts remain backward-safe and version-aware.
- Error handling uses shared `problem`/`result` patterns consistently.
- Concurrency-sensitive code is race-safe and test-covered.
- PR includes tests for changed behavior.
- `Makefile`/CI commands remain valid and coherent.

## Security Considerations
- Validate untrusted input parsing boundaries.
- Verify no accidental secrets/config hardcoding in `cmd/*`.
- Ensure dependency/tool changes preserve vulnerability checks.
- Confirm no unsafe assumptions in bus/message deserialization paths.

## Performance Considerations
- Watch for unbounded allocations in hot event paths.
- Validate that loops over event streams are backpressure-aware.
- Check actor message handling for avoidable blocking operations.

## Expected Reviewer Output
1. Findings ordered by severity (blocking first).
2. Clear file/line references.
3. Residual risk notes if tests are missing or incomplete.
4. Explicit pass/fail recommendation.
