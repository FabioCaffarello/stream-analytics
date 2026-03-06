---
type: doc
name: project-overview
description: High-level overview of the project, its purpose, and key components
category: overview
generated: 2026-03-05
status: filled
scaffoldVersion: "2.0.0"
---

# Market Raccoon - Project Overview

Market Raccoon is an institutional-grade market intelligence backend. Built on Go, it uses an actor-based concurrency model (`github.com/anthdm/hollywood/actor`), deterministic event processing, and NATS JetStream to normalize exchange streams and serve low-latency multi-timeframe insights.

## Canonical Architecture (The 9 Core Documents)

Future agents and contributors MUST refer to the official architecture directory at [`docs/`](../../docs/README.md). This is where the absolute truth is stored:

1. **[`docs/architecture/subsystems.md`](../../docs/architecture/subsystems.md)** - Explains the 7 core subsystems (MarketData, Aggregation, Delivery, Insights, Evidence, Signals, Storage), boundedness caps, and inputs/outputs.
2. **[`docs/architecture/sequencing-model.md`](../../docs/architecture/sequencing-model.md)** - Details strict monotonic sequence chaining (`seq`, `prev_seq`, `DecideMonotonic`), time primitives (`ts_ingest`, `ts_server`), and replay determinism.
3. **[`docs/architecture/iq-loop-invariants.md`](../../docs/architecture/iq-loop-invariants.md)** - The runtime guardrails matrix.
4. **[`docs/architecture/decisions.md`](../../docs/architecture/decisions.md)** - An index of active Architectural Decision Records (ADRs) and Requests for Comments (RFCs).

## Key Subsystems

- **Consumer (`cmd/consumer`)**: Ingests WebSocket streams from multiple venues, normalizes to Canonical Market Model (CMM), and publishes to JetStream.
- **Processor (`cmd/processor`)**: Applies domain rules to build deterministic orderbook aggregation, OHLCV candles, metrics, heatmaps, and signal evidence.
- **Server (`cmd/server`)**: Binds WebSocket delivery sessions applying rate-limits and backpressure, plus routing runtime supervision via Guardian.
- **Store (`cmd/store`)**: TimescaleDB/Clickhouse persistence.
- **Client (`client/src/`)**: Odin/WASM highly-optimized interface that binds to terminal WS frames and validates sequences strictly.

## Core Properties

- **Domain Isolation (`INV-DOM`)**: Business logic inside `internal/core` MUST NOT import `adapters/`, `interfaces/`, or `actors/` packages.
- **Strict Determinism (`INV-DET`)**: Time `time.Now()` is injected externally. Replays of identical inputs guarantee identical OHLCV and event outputs.
- **Event-Driven**: Complete separation between hot-path processing (in-memory fast actors) and cold-path processing (Database storage and analytics).
