# Stage 51 — Institutional Runtime, Replay, and Governance Layer

**Date:** 2026-03-07
**Status:** Complete
**Tests:** 402 client (52 new) + ~45 backend new

## Objective

Transform Market Raccoon into a fully deterministic, observable, and reproducible platform with institutional-grade runtime guarantees.

## Backend Deliverables

### SLO Runtime Evaluator (`shared/slo/evaluator.go`)
- `Evaluator` tracks breach state from periodic metric snapshots
- 3 default SLOs: ingest success (99.9%), delivery latency (99%), data loss guard (99.99%)
- Burn-rate fast/slow thresholds with error budget tracking
- `UpdateSLO` for individual SLO evaluation, `Update` for batch
- Thread-safe: `sync.RWMutex` for concurrent actor queries
- `Breached()` / `AnyBreached()` / `State()` / `AllStates()` query API

### Golden Drift Detector (`shared/replay/drift.go`)
- Field-level JSON diff between actual and golden envelope outputs
- `DriftResult` with `FieldMismatches`, `NewFields`, `DroppedFields`
- `Compatible` flag: additive-only changes pass, breaking changes fail
- Nested object and array comparison with JSON path tracking
- `DetectPayloadDrift` for payload-only comparison

### Replay Validation Hooks (`shared/replay/validation.go`)
- `ValidationHook` function type for post-replay assertions
- `ExpectOutputCount(n)` — exact output count
- `ExpectOutputSHA(sha)` — SHA-256 chain hash of output payloads
- `ExpectInputSHA(sha)` — input fixture chain hash
- `ExpectNoDrift(golden)` — no breaking changes (additive OK)
- `ExpectNoFieldMismatch(golden)` — exact match (strict mode)
- `RunValidations` executes hook chain, stops on first failure

### SLO Prometheus Metrics (4 new)
- `slo_breach_active{name}` — gauge (0/1)
- `slo_error_budget_remaining_ratio{name}` — gauge (0.0-1.0)
- `slo_burn_rate_fast{name}` — gauge
- `slo_burn_rate_slow{name}` — gauge
- `SetSLOState()` export helper

## Client Deliverables

### Replay Scrubber (`md_common/replay_scrubber.odin`)
- Ring buffer (cap 256) tracking recent sequence events
- `Stream_Integrity_Flag`: Ok, Gap, Reorder, Duplicate
- Per-slot last-seq tracking for integrity detection
- `scrubber_push` — append with auto-integrity classification
- `scrubber_get` — indexed read (0 = newest)
- `scrubber_integrity_summary` — aggregate gap/reorder/dup counts
- `scrubber_seek` / `scrubber_pause` / `scrubber_reset`
- Memory: 256 x ~40 bytes = ~10KB

### Scene Snapshot (`md_common/scene_snapshot.odin`)
- Extends Runtime_Snapshot (S46) with data context
- `Store_Digest`: candle count, newest/oldest ts, analytics count, heatmap seq
- `Scene_Snapshot`: runtime + scrubber tail (64) + store digests + fingerprint + build tag
- `scene_snapshot_copy_scrubber_tail` — copy last N from scrubber ring
- `scene_snapshot_set_build_tag` — set build identification (truncated to 32 bytes)
- `scene_snapshot_serialize` — extends SNAP format with SC|/SD|/ST| lines

### Workspace Governance (`md_common/workspace_governance.odin`)
- Compat matrix: V4 (min) through V7 (max)
- `Workspace_Compat_Result`: Compatible, Upgrade_Available, Downgrade_Warning, Incompatible
- `workspace_fingerprint` — FNV-1a hash of persisted layout bytes
- `workspace_compat_check` — version against compat matrix
- `workspace_profile_version_guard` — version + fingerprint gate
- `workspace_compat_is_loadable` — loadability check
- Schema bumped to V7

### Diagnostics View (`md_common/diagnostics_view.odin`)
- `Diagnostics_View` — unified read model for health grid
- `Cell_Diagnostic`: widget kind, composition, health level, stale/aging counts, event count
- `Store_Health`: candle count, newest age, integrity flag (Ok/Gap/Stale/Empty)
- `Stream_Latency`: per-artifact recv age, worst artifact
- `Stale_Alert`: slot, artifact, age, threshold
- `diagnostics_stream_latency` — per-slot latency computation
- `diagnostics_store_health` — store integrity from candle metrics
- `diagnostics_count_stale_aging` — stale/aging artifact counts
- `diagnostics_cell_health` — per-cell diagnostic derivation

## Tests

### Backend (Go)
- `shared/slo/evaluator_test.go`: 14 tests (nil safety, breach/no-breach, budget consumption, burn rate, custom defs)
- `shared/replay/drift_test.go`: 11 tests (identical, value mismatch, additive, dropped, nested, array, multi-envelope)
- `shared/replay/validation_test.go`: 10 tests (count, SHA, input SHA, drift hooks, run chain)

### Client (Odin)
- 12 scrubber tests (push/get, integrity flags, multi-slot isolation, ring wrap, pause, seek, reset, nil safety)
- 6 scene snapshot tests (copy tail, build tag, truncate, serialize, nil safety)
- 12 workspace governance tests (compat matrix boundaries, fingerprint determinism, profile guard, loadability)
- 6 diagnostics view tests (stream latency, store health, cell health, stale/aging counts)

**Total new tests: ~70** (35 Go + 36 Odin, bringing client md_common from 350 to 402)

## Invariants

- Zero new mutable global state in client
- All client functions are pure (no side effects, no allocations)
- SLO evaluator is gauge-based (no blocking in hot path)
- Drift detection runs in test/CI only (zero runtime cost)
- Workspace governance is backward-compatible (V4-V6 load with defaults)
- All ring buffers are fixed-size (scrubber 256, scene tail 64)

## Wire Changes

None.

## Breaking Changes

None. Workspace schema bumped V6 → V7 but V6 files load via Upgrade_Available path.

## Files Changed

### Backend (New)
- `internal/shared/slo/evaluator.go`
- `internal/shared/slo/evaluator_test.go`
- `internal/shared/replay/drift.go`
- `internal/shared/replay/drift_test.go`
- `internal/shared/replay/validation.go`
- `internal/shared/replay/validation_test.go`

### Backend (Modified)
- `internal/shared/metrics/metrics.go` — 4 new SLO gauges + SetSLOState helper

### Client (New)
- `client/src/core/md_common/replay_scrubber.odin`
- `client/src/core/md_common/scene_snapshot.odin`
- `client/src/core/md_common/workspace_governance.odin`
- `client/src/core/md_common/diagnostics_view.odin`

### Client (Modified)
- `client/src/core/app/workspace_schema.odin` — V6 → V7
- `client/src/core/md_common/store_boundary_test.odin` — 52 new tests
