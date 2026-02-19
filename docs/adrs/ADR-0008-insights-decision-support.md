# ADR-0008 — Insights Are Decision Support, Not Execution

**Status:** Accepted
**Date:** 2026-02-10

## Context

We want agents/insights while avoiding regulatory risk and user harm. Execution automation increases risk significantly and complicates compliance.

## Decision

Insights are informational and evidence-based:

- Insights produce `Insight{type, confidence, evidence[], window, venue, instrument}`.
- No “BUY/SELL/ENTRY/STOP” directives in outputs.
- UI presents insights as hypotheses with evidence and disclaimers.
- Every insight output is auditable: inputs, rules/models, and derived evidence are logged.

Execution automation is explicitly out of scope for the initial product.

## Consequences

- Lower regulatory and reputational risk.
- Higher trust via evidence + audit trail.

## Alternatives

- Signal product (rejected: higher liability and harder compliance).
- Fully automated trading agents (rejected for MVP).

## Evidence

- Validation gate: `make docs-check-full`
- Authority path: file-local ADR source.

## Changelog

- 2026-02-13: added required header sections for docs compliance.
