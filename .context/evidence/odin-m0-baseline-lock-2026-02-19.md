# Odin M0 Baseline Lock (2026-02-19)

## Objective
Freeze the factual backend capability baseline before execution waves M1..M10.

## Baseline Snapshot
- Implemented and stable: 5-exchange consumer, auth/rate-limit, orderbook aggregation, VPVR, cross-venue signals, markprice/liquidation, cold-path writer.
- Implemented with maturity gap: WS delivery without slow-client disconnect threshold.
- C3 pending: cold-path read ports (SELECT), `cmd/backfill`, gap detector CLI.
- Advanced capabilities now mandatory before Odin client start: candle aggregation, stats aggregation, heatmap delivery, durable Timescale getrange, standalone funding pipeline.

## Launch Policy
- Odin client start remains blocked until all milestones M0..M10 are complete with evidence.

## Evidence Anchors
- Plan: `.context/plans/odin-v0-capability-maturity.md`
- PRD: `docs/prd/PRD-0002-backend-stable-and-odin-ready.md`
- WS Contract: `docs/contracts/delivery-ws.md`
