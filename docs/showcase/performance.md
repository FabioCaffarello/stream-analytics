---
description: Performance benchmarks — C4 soak results, latency percentiles, resource bounds, and the merge regression gate.
---

# Performance

Stream Analytics validates performance continuously through the C4 soak harness: a sustained load
test that drives 10 million events through 4 simulated exchanges and measures end-to-end throughput
and latency under realistic conditions.

---

## C4 Soak Baseline

| Metric | Value |
|--------|-------|
| Events processed | 10,000,000 |
| Exchanges simulated | 4 |
| Duration | ~85 seconds |
| **Throughput** | **117,697 evt/sec** |
| Latency p50 | **7 µs** |
| Latency p95 | **13 µs** |
| Latency p99 | **56 µs** |

!!! example "Reproduce the Soak"

    ```bash
    make soak-check
    ```

    Runs all 8 soak harnesses sequentially. Results are written to `artifacts/` with timestamps.
    The full suite takes approximately 5 minutes.

    Individual harnesses:

    ```bash
    make soak-c4-production    # Main 10M-event C4 harness
    make soak-pipeline         # Pipeline throughput soak
    make soak-ws-delivery      # WebSocket delivery under load
    make soak-roundtrip        # End-to-end roundtrip latency
    make soak-store            # Storage write throughput
    make soak-cold-path        # ClickHouse cold path query latency
    make soak-vpvr             # VPVR computation soak
    ```

---

## Why Latency is so Low

!!! tip "Latency Architecture"

    The hot path is designed to avoid all common latency sources:

    - **No `fmt.Sprintf` on hot paths** — string formatting uses `FieldHasher` for zero-allocation
      key computation.
    - **No `time.Now()` in core/actors** — all time-dependent code uses `clock.Clock`, enabling
      deterministic replay and avoiding syscall overhead in tight loops.
    - **NATS JetStream in-memory delivery** — the message bus operates with microsecond delivery
      for small envelopes on local loopback.
    - **Hollywood actor mailboxes** — lock-free MPSC channels between actors eliminate contention
      on the aggregation path.
    - **Canvas rendering** — the Odin client renders directly to `<canvas>` with no DOM diffing;
      each frame is a direct 2D draw call.

---

## Resource Bounds

Every subsystem declares explicit resource bounds enforced at runtime. See
[Boundedness Matrix](../contracts/boundedness-matrix.md) for the full table.

Key bounds:

| Subsystem | Bound | Value |
|-----------|-------|-------|
| Delivery | Max concurrent WebSocket sessions | Configurable per deployment |
| Aggregation | Candle window buffer depth | Per-stream, per-timeframe TTL |
| Heatmap | Snapshot history depth | TTL-based eviction |
| Evidence | LEL shard count | Fixed at deployment time |
| VPVR | Level count per symbol | Bounded by price range |

---

!!! warning "Merge Gate"

    Pull requests that regress C4 soak throughput by **more than 5%** relative to the baseline
    require explicit discussion and sign-off before merging.

    Soak results are stored in `artifacts/` and compared automatically by `make soak-check`.
    The CI nightly (`ci-nightly`) runs the full suite on every merge to `main`.
