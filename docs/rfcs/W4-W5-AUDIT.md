# W4/W5 Post-Merge Audit (Staff+)

Date: 2026-02-12
Scope commits: `c745afa`, `00c786d`, `bcfc5b6`, `f4d4bb5`, `d8fb0ed`, `0510934`

## Executive Summary

Status: **hardening applied with residual low/medium risks documented**.

Applied patches (minimal):
- Bounded `subscriber_id` metric labels for `bus_dropped_total`.
- Bounded internal WS stream telemetry keys (`byWSStream`).
- Moved `BoundedMap` eviction callback execution out of map lock.
- Added lifecycle/timer cleanup asserts for guardian/manager stop paths.
- Added bounded-map microbench + CPU/mutex profile artifacts.

## Findings

### 1) `bus_dropped_total{subscriber_id=...}` cardinality churn risk
- Risk: **Medium**
- Evidence path/lines:
  - `internal/shared/metrics/metrics.go:258`
  - `internal/shared/metrics/metrics.go:395`
- Problem:
  - Label value was derived from per-subscriber index, which can churn and create many time series over runtime.
- Patch applied:
  - Bucketized subscriber index into fixed classes: `s0`, `s1_3`, `s4_15`, `s16_63`, `s64_255`, `s256_plus`, `unknown`.
- Validation:
  - `internal/shared/metrics/metrics_test.go:39`
  - `internal/adapters/bus/bus_test.go:147`
  - Test asserts 1000 publishes do not create unbounded label-pairs.

### 2) Internal telemetry map `byWSStream` unbounded by raw stream key
- Risk: **Medium** (memory/cardinality in long runs)
- Evidence path/lines:
  - `internal/actors/marketdata/runtime/telemetry.go:49`
  - `internal/actors/marketdata/runtime/telemetry.go:128`
- Problem:
  - Raw `ws_stream` (e.g. `symbol@aggTrade`) was used directly as map key.
- Patch applied:
  - Normalized to bounded stream classes: `aggtrade`, `depth`, `trade`, `other`, `unknown`.
- Validation:
  - `internal/actors/marketdata/runtime/telemetry_test.go:94`
  - Added test with 1000 distinct stream names; bucket set stays bounded.

### 3) `BoundedMap` `OnEvict` callback inside lock (lock contention / stall risk)
- Risk: **Medium**
- Evidence path/lines:
  - `internal/shared/ds/boundedmap.go:64`
  - `internal/shared/ds/boundedmap.go:85`
  - `internal/shared/ds/boundedmap.go:139`
- Problem:
  - Callback could do blocking/logging work while map mutex held, amplifying contention in hot paths.
- Patch applied:
  - Eviction events captured under lock, callback fired after unlock.
- Validation:
  - `internal/shared/ds/boundedmap_test.go:108` (callback re-enters `Len()` without deadlock).

### 4) Timer cleanup invariants on stop paths
- Risk: **Low**
- Evidence path/lines:
  - `internal/actors/runtime/guardian_test.go:99`
  - `internal/actors/marketdata/ws/manager_test.go:270`
- Patch applied:
  - Added asserts that scheduled timer maps are empty after stop (`scheduledRetry`, `scheduledPoison`).

### 5) Sweep-in-hot-path remains
- Risk: **Medium (performance)**
- Evidence path/lines:
  - `internal/core/marketdata/app/ingest.go:213`
  - `internal/core/aggregation/app/update_orderbook.go:153`
- Current status:
  - `Sweep()` is still called on every get-or-create path.
- Recommendation (not applied to avoid behavior change in this hardening pass):
  - Add sweep cadence throttling (op-count/time based) with explicit tests for TTL semantics.

### 6) Consumer lifecycle and pprof protection checks
- Risk: **Low**
- Evidence:
  - `defer close(donech)` present in `internal/actors/marketdata/ws/consumer.go:446`.
  - Stop order closes quit/cancel before conn close in `internal/actors/marketdata/ws/consumer.go:236`.
  - `WriteControl` calls include deadlines in `internal/actors/marketdata/ws/consumer.go:243` and `internal/actors/marketdata/ws/consumer.go:257`.
  - pprof localhost restriction tested: `internal/interfaces/http/server_test.go:385` (remote => 403).
  - `/metrics` remains public: `internal/interfaces/http/server_test.go:336`.

### 7) Determinism readiness
- Risk: **Low**
- Evidence:
  - Domain/app ingest clock remains injected: `internal/core/marketdata/app/ingest.go:65`, `internal/core/marketdata/app/ingest.go:154`.
  - No Prometheus imports added to core domain/app boundaries in this audit patchset.

## Evidence Runbook

### Tests executed (all passing)
- `go test ./internal/shared/metrics`
- `go test ./internal/adapters/bus`
- `go test ./internal/shared/ds`
- `go test ./internal/actors/runtime ./internal/actors/marketdata/ws ./internal/interfaces/http`
- `go test ./internal/actors/marketdata/runtime`

### Bench / profiles (Section D)
- Command:
  - `go test ./internal/shared/ds -run '^$' -bench 'BenchmarkBoundedMap(Concurrent)?PutGet$' -benchmem -benchtime=30s -cpuprofile .context/evidence/w4w5-audit-boundedmap-cpu.pprof -mutexprofile .context/evidence/w4w5-audit-boundedmap-mutex.pprof -mutexprofilefraction=1`
- Results:
  - `BenchmarkBoundedMapPutGet-10`: `48.66 ns/op`, `2 B/op`, `0 allocs/op`
  - `BenchmarkBoundedMapConcurrentPutGet-10`: `214.5 ns/op`, `3 B/op`, `0 allocs/op`
- Artifacts:
  - `.context/evidence/w4w5-audit-boundedmap-bench.txt`
  - `.context/evidence/w4w5-audit-boundedmap-cpu.pprof`
  - `.context/evidence/w4w5-audit-boundedmap-mutex.pprof`

Note: `go tool pprof`/`pprof` binary is unavailable in this environment, so profile artifacts were generated but not summarized with `top` output here.

## Audit Verdict

- **Not audit-clean** due to one residual medium risk (Sweep in hot path).
- All other targeted residual risks from A/B/C/E/F were either validated safe or hardened with minimal patches and tests.
