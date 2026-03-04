# Liquidity Evidence Layer (LEL)

**Status:** Active
**Owner:** Evidence + Delivery
**Last updated:** 2026-03-04

## Decision

LEL is a first-class subsystem on top of CMM.

- CMM is the only model.
- Evidence is only produced from CMM (no legacy inputs).

## Scope

- Engine and boundaries:
  - `EvidenceEngine` orchestrates deterministic rule evaluation.
  - `EvidenceRule` strategies implement rule-local state and thresholds.
  - `FeatureExtractor` provides pure feature functions.
  - `EvidenceStateStore` provides bounded per-stream state with TTL and hard caps.
  - `EvidenceEmitter` publishes canonical evidence envelopes.
- Input channels:
  - Canonical `marketdata.trade` and `marketdata.bookdelta` payloads (CMM types).
- Output channel:
  - `insights.microstructure_evidence`, delivered as WS `channel=evidence`.

## Canonical Evidence Contract

Proto contract: `proto/evidence/v1/evidence.proto` (`evidence.v1.MicrostructureEvidenceV1`)

Required fields:

- `type`
- `ts_server`
- `venue`
- `symbol`
- `stream_id`
- `seq`
- `severity`
- `confidence`
- `features` (sorted by key)
- `explanation`
- `rule_version`
- `input_watermark { seq_start, seq_end }`

## Rules (v0)

Implemented as independent strategies with explicit config/thresholds:

- `SpreadExplosion`
- `LiquidityThinning`
- `PersistentImbalance`
- `Absorption`

Each rule emits deterministic `EvidenceEvent` with explicit `rule_version="v0"` and input sequence watermark.

Default thresholds:

- Shared:
  - `max_streams=256` per rule
  - `cooldown_ms=5000`
- `SpreadExplosion`:
  - `min_samples=10`
  - `min_zscore=2.5`
  - `min_spread_bps=10`
- `LiquidityThinning`:
  - `min_samples=10`
  - `min_drop_pct=0.30`
  - `max_zscore=-2.0`
- `PersistentImbalance`:
  - `imbalance_threshold=0.30`
  - `min_consecutive=10`
- `Absorption`:
  - `min_samples=10`
  - `max_price_move_pct=0.10`
  - `min_volume_ratio=2.0`

## Determinism Guarantees

- No wall-clock dependency in evaluation (`ts_server` only).
- Stable feature ordering (`features` sorted by key before emit).
- No map iteration in emitted payload construction.
- Per-stream monotonic sequence gating (`seq` must increase for a given `stream_id`).
- Same canonical input stream produces byte-identical evidence sequence.

## Boundedness Guarantees

- Per-stream state is bounded by fixed windows/ring buffers.
- Global stream-state cap enforced by `EvidenceStateStore`.
- TTL eviction enforced on every observation.
- Eviction reasons are explicit and metered.
- No unbounded map growth in engine/store hot path.

## Observability

- `evidence_emitted_total{type,severity,venue}`
- `evidence_dropped_total{reason}`
- `evidence_state_entries`
- `evidence_state_evicted_total{reason}`
- `evidence_eval_latency_seconds`

Label policy:

- No symbol-level labels.
- Low-cardinality bounded labels only (`type`, `severity`, `venue`, `reason`).

## Delivery Contract Notes

- WS Terminal V1 maps `insights.microstructure_evidence` to `channel=evidence`.
- Subscribe command channel alias `evidence` resolves to `insights.microstructure_evidence`.
- Evidence frames use the same delivery limits, batching, and compression path as other market channels.

## Migration Note

“CMM is the only model.”
There is zero legacy fallback in the hot path for liquidity evidence: aggregation-side legacy evidence generation is removed, and evidence emission now originates only from canonical CMM inputs.
