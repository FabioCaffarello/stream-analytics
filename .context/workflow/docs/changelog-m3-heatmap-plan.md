# Checkpoint: M3-heatmap-plan

Date: 2026-02-13
Mode: Commit-driven planning only (no implementation in this cycle)

## Input/Output Baseline

- Heatmap input events (current authority):
  - `marketdata.bookdelta.v1.{venue}.{instrument}`
  - `marketdata.trade.v1.{venue}.{instrument}`
- Heatmap output subjects (no TBD):
  - `insights.heatmap_snapshot.v1.{venue}.{instrument}` (draft)
  - `insights.heatmap_delta.v1.{venue}.{instrument}` (planned)

## Decisions (D1/D2/D3)

- D1: Keep heatmap under `insights.*` root.
- D2: `owner_bc=insights`, `producer_bc=insights`, `schema_authority_bc=insights`.
- D3: canonical instrument token remains uppercase alnum (`BTCUSDT`) on bus subjects.

## Architecture Constraints

- Cardinality caps and boundedness are mandatory per `(venue,instrument,timeframe)`.
- Backpressure policy must emit explicit drop reason labels.
- Replay determinism requires golden rebuild + byte-stability checks.
