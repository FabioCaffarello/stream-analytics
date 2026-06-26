# Liquidity Evidence Layer (LEL) v1

**Status:** Active
**Owner:** Evidence + Delivery
**Last updated:** 2026-03-04

## Goal

LEL v1 emits deterministic liquidity evidence from canonical aggregation inputs only.

- Inputs: `aggregation.snapshot` (DOM V2) + `aggregation.tape` (MMT #2)
- Output: `liquidity.evidence`
- Constraints: deterministic, bounded, no wall-clock usage, `PROCESSOR_REPLICAS=2` compatible

## Canonical Contract

- Event type: `liquidity.evidence`
- Subject pattern: `liquidity.evidence.v1.{venue}.{instrument}`
- Proto schema: `proto/liquidity/v1/evidence.proto`
- Proto message: `liquidity.v1.LiquidityEvidenceV1`
- Wire DTO: `contracts.LiquidityEvidenceV1`
- Schema version: `version = 1`

Fields:

- `evidence_type` (`BOOK_IMBALANCE | ABSORPTION | SWEEP | THINNING | SPREAD_REGIME`)
- `ts_ingest_ms`, `venue`, `symbol`, `window_ms`
- `severity` (`low|medium|high|critical`)
- `confidence` in `[0,1]`
- `metrics[]` sorted/unique by key, finite values, max 8
- `explain[]` non-empty strings, max 4, each <= 120 chars
- `stream_id`, `seq`, `watermark{seq_start,seq_end}`

## Rule Set (v1)

Implemented rules:

- `BOOK_IMBALANCE`
- `ABSORPTION`
- `SWEEP`
- `THINNING`
- `SPREAD_REGIME`

Each rule keeps O(1) memory per stream (fixed rings + counters + two-slot comparisons) and enforces per-stream cooldown.

## Determinism + Boundedness

- No `time.Now()` in domain/app rule evaluation.
- Global LEL state store bounded (max entries + TTL eviction).
- Per-rule stream maps bounded with deterministic eviction.
- Non-monotonic per-stream sequence is rejected (`non_monotonic_seq`).
- Same input sequence yields byte-identical output.

Replica ownership (for `PROCESSOR_REPLICAS=2`):

- `owner = FNV1a64(venue,symbol) % replicaCount`
- only owner replica evaluates and emits LEL evidence for that stream

## Observability

Pre-created and always exposed on `/metrics`:

- `lel_evidence_emitted_total{type,severity,venue}`
- `lel_evidence_dropped_total{reason}`
- `lel_state_entries`
- `lel_state_evicted_total{reason}`
- `lel_eval_latency_seconds`
- `lel_input_processed_total{kind}`
- `lel_wire_budget_bytes{type}`

Label policy:

- no symbol-level labels
- bounded labels only (`type`, `severity`, `venue`, `reason`, `kind`)

## WS Delivery Contract

- WS channel alias `evidence` resolves to `liquidity.evidence`
- channel enum mapping: `CHANNEL_EVIDENCE`
- routing/delivery keeps existing batching/backpressure/compression behavior

Legacy alias support remains accepted for command parsing:

- `insights.microstructure_evidence` -> normalized to `liquidity.evidence`
