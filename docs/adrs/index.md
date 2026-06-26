---
type: doc
status: Active
last_updated: 2026-06-25
---

# Architecture Decision Records

ADRs capture significant architectural decisions, their context, options considered,
and the rationale for the chosen approach.

Each file follows the naming pattern `ADR-NNNN-short-title.md` and must pass the
`scripts/ci/docs/check-doc-headers.sh` format gate, which requires:

- `**Status:**` line (Proposed / Accepted / Superseded / Deprecated)
- `## Evidence` section with code and test anchors
- `## Changelog` section with dated entries

Use `docs/doc-contract-template.md` as the starting template.

---

## Index

| ADR | Title | Decision (one line) |
|-----|-------|---------------------|
| [ADR-0000](ADR-0000-foundation.md) | Foundation | Layer dependency direction, 12 bounded contexts, `clock.Clock`, `FieldHasher`, and NATS envelope sequencing established as non-negotiable primitives. |
| [ADR-0032](ADR-0032-stream-reliability-model.md) | Stream Reliability Model | Adopt a 5-layer health pipeline (Detect → Classify → Signal → Recover → Surface) driven by `prev_seq` gap detection. |
| [ADR-0033](ADR-0033-orderflow-domain-blueprint.md) | Orderflow Domain Blueprint | CMM (`internal/core/marketmodel/`) is the single canonical orderflow representation; exchange adapters must not leak raw types past their boundary. |
| [ADR-0034](ADR-0034-stream-health-recovery-completion.md) | Stream Health Recovery Completion | Gap → backfill from last good seq; disconnect → exponential backoff + gap-fill on reconnect; Guardian restart → re-subscribe without history replay. |
| [ADR-0035](ADR-0035-orderflow-contract-architecture.md) | Orderflow Contract Architecture | All orderflow events use `{event}.v{version}.{venue}.{instrument}`; new types must be registered in `docs/contracts/subject-registry.yaml`. |
| [ADR-0036](ADR-0036-analytics-delivery-semantics.md) | Analytics Delivery Semantics | Analytics path is at-least-once Kafka + effectively-exactly-once sink via idempotent upsert on natural keys; checkpointing interval 60s; FIRST_VALUE/LAST_VALUE deterministic via `venue:instrument` key partitioning. |
