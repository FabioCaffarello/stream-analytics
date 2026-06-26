# Signal Engine Contract

**Status:** Retired
**Owner:** Signals
**Last updated:** 2026-03-06
**Relates to:** `docs/contracts/canonical-market-model.md`, `docs/contracts/liquidity-evidence-layer.md`

---

> **Retired in S9 (codex/s9-legacy-removal-cutover).** `cmd/signals` and all signal/signals/strategy actor packages were removed. The surviving liquidity signal equivalent is `docs/contracts/liquidity-evidence-layer.md`. This document is preserved as historical reference only — do not use it for new development.

## Purpose

Define the deterministic Signal Engine contract as a first-class subsystem built only from Canonical Market Model (CMM) events plus Liquidity Evidence Layer (LEL) events.

## Inputs

- Canonical market envelopes:
  - `marketdata.trade`
  - `marketdata.bookdelta`
  - `aggregation.candle`
  - `aggregation.stats`
- Canonical evidence envelope:
  - `insights.microstructure_evidence`

Signal Engine consumers must not subscribe to raw exchange payloads.

## Output Contract

- Canonical event type: `signal.event` (version `1`)
- Canonical proto payload: `marketmodel.v1.SignalEvent`
- Published in `MarketEvent.oneof` as `signal`
- Event-bus subject pattern: `signal.event.v1.{venue}.{instrument}`
- Delivery WS channel: `signal`
- Delivery routing format: `signal/{type}/{venue}/{symbol}/{timeframe}`

`SignalEvent` fields:
- `type`
- `ts_server`
- `scope` (`stream` or `market`)
- `venue` / `symbol` (required for `stream`; empty for `market`)
- `severity`
- `confidence` in `[0,1]`
- `features` (sorted by key, deterministic)
- `explanation`
- `explain[]` (deterministic explainability fragments)
- `signal_id` (deterministic unique signal identifier)
- `rule_id`
- `rule_version`
- `input_watermark` (deterministic seq ranges)
- `correlation_id` (deterministic hash)
- `correlation_ids[]` (deterministic related ids, including evidence linkage)

## Initial v0 Rules

- `regime_change`
  - Triggered by evidence bursts with minimum count and type diversity.
- `liquidity_collapse`
  - Triggered by co-occurrence of thinning and spread explosion evidence in the same deterministic window.
- `persistent_imbalance_signal`
  - Triggered by persistent imbalance plus absorption confirmation.
- `venue_divergence_signal`
  - Deterministic stub; emitted only when multi-venue aggregator capability is explicitly enabled.

Rules are deterministic and configuration-driven through engine/rule config structs.

## Determinism Guarantees

- No wall-clock dependence in rule evaluation (`time.Now` is not used).
- Stable processing order from input stream order.
- Feature lists are normalized and sorted before emit.
- Watermarks are deterministic and bounded.
- Replay invariant: identical input stream produces identical `SignalEvent` sequence and deterministic proto bytes.

## Boundedness Guarantees

- Fixed-cap ring buffers for rolling windows.
- Per-stream window cap.
- Per-tenant stream cap.
- Global stream cap.
- TTL eviction for stale streams.
- Dedup ring with bounded window.
- No unbounded map growth; evictions are explicit and metered.

## Horizontal Scaling and Sharding

- Signal Engine runs as a dedicated `cmd/signals` service.
- Ownership is computed by consistent hash of `StreamKey`.
- `PROCESSOR_REPLICAS` and replica index produce stable owner selection.
- Owner-only emit rule: only the owning replica can publish `signal.event`, preventing duplicates under replica fanout.

## Observability

- `signal_emit_total{type,severity}`
- `signal_emitted_total{type,severity}` (backward-compatible alias)
- `signal_state_entries`
- `signal_evicted_total{reason}`
- `signal_eval_latency_seconds`
- `signal_dedup_total{type}`
- `signal_wire_bytes{type}`

Cardinality policy: no symbol-level labels in default signal metrics.

## Explicit Non-Responsibility

Signal Engine has no execution responsibility. It only emits analytical signal events and does not place orders, route orders, or perform risk/execution actions.

## Semantic Boundary (Stage 3)

- `signal.event` remains the canonical signal stream and must stay non-execution.
- `strategy.intent` is the decision contract for future strategist runtime and must not be encoded inside `signal.event` metadata.
- `signal.composite` is retired from strategist runtime intake in Stage 6 and is not a strategy contract anchor.
- Any residual `signal.composite` handling is compatibility-only for historical replay/read paths.
