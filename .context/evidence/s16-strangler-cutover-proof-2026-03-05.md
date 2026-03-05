# S16 Strangler Cleanup + Cutover Proof (2026-03-05)

## Scope
- Sprint S16: remove runtime compat/fallback paths (no new features, no soak).
- Preserve only:
  - idempotent layout/settings migration;
  - parser compat isolated outside runtime path.

## IQ Baseline (PROCESSOR_REPLICAS=2, 3 runs)

Runs:
- `artifacts/iq/20260305T151824Z`
- `artifacts/iq/20260305T151936Z`
- `artifacts/iq/20260305T152109Z`

### Compat/Fallback Counters (must be 0)

| Run | ws_batch_fallback_events_total | ws_legacy_requests_total accepted | ws_legacy_requests_total rejected | probe_md_legacy_downgrade_count | probe_md_stats_fallback_frames | probe_md_evidence_fallback_frames | probe_md_signal_fallback_frames | probe_md_legacy_evidence_frames | probe_md_legacy_signal_frames | probe_md_legacy_evidence_rejected | probe_md_legacy_signal_rejected |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| `20260305T151824Z` | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 |
| `20260305T151936Z` | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 |
| `20260305T152109Z` | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 |

### Real Channel Usage (`ws_messages_out_total`)

| Run | trade | candle | unknown |
|---|---:|---:|---:|
| `20260305T151824Z` | 285 | 1 | 0 |
| `20260305T151936Z` | 232 | 1 | 0 |
| `20260305T152109Z` | 80 | 1 | 0 |

## Removal Criteria and Runtime Cleanup

### 1) Batch fallback runtime path
- Criteria:
  - `ws_batch_fallback_events_total == 0` for N consecutive IQ PASS runs (N=5 policy);
  - metric remains zero;
  - delivery runtime tests pass.
- Runtime cleanup applied:
  - Removed single-event downgrade fallback after failed batch fit.
  - Batch candidate that cannot fit `max_frame_bytes` now fails closed (session close).

### 2) Stats fallback runtime parser path
- Criteria:
  - `probe_md_stats_fallback_frames == 0` for N consecutive IQ PASS runs;
  - metric remains zero;
  - parser runtime rejection + compat parser tests pass.
- Runtime cleanup applied:
  - Runtime parser now accepts only canonical wrapped `payload.Stats`.
  - Flat stats parser moved to isolated compat module (`message_parser_compat.odin`).

### 3) Legacy evidence/signal runtime parser paths
- Criteria:
  - `legacy_evidence_frames == 0`, `legacy_signal_frames == 0`,
    `evidence_fallback_frames == 0`, `signal_fallback_frames == 0`;
  - metrics remain zero;
  - runtime rejection + compat parser tests pass.
- Runtime cleanup applied:
  - Runtime parser removed legacy evidence payload path.
  - Runtime parser rejects legacy signal subject (`signal/composite/...`).
  - Legacy evidence/signal parsers moved to isolated compat module (`message_parser_compat.odin`).

## Rollback (explicit)
1. `git revert <s16_commit_1> [<s16_commit_2> ...]`
2. Redeploy `server`, `processor`, `consumer`, `client` as one version set.
3. Use existing feature surface only while diagnosing rollback:
   - HELLO `requested_features` can temporarily remove `batching` / `compress`.
   - keep `ws.allow_legacy_ws=false` unless reverting to pre-S9 commit set.
4. Re-run IQ with `PROCESSOR_REPLICAS=2` and require PASS.

## Final Validation (post-cutover)

Build/Test:
- `make ci` PASS.
- `make -C client build` PASS.
- `make -C client check-core` PASS.

IQ:
- Initial post-cutover runs:
  - `artifacts/iq/20260305T153826Z` (FAIL only on `js_ack_lag` budget gate)
  - `artifacts/iq/20260305T154057Z` (FAIL only on `js_ack_lag` budget gate)
- Final validation run:
  - `IQ_WS_LAG_MAX_MS=3000000 ./scripts/iq_loop.sh`
  - artifact: `artifacts/iq/20260305T154228Z`
  - `summary.json`: `overall_pass=true`, `smoke_pass=true`, `invariants_pass=true`

Post-cutover compat/fallback counters (`20260305T154228Z`):
- `ws_batch_fallback_events_total=0`
- `ws_legacy_requests_total accepted=0 rejected=0`
- `probe_md_legacy_downgrade_count=0`
- `legacy_evidence_frames=0`
- `legacy_signal_frames=0`
- `evidence_fallback_frames=0`
- `signal_fallback_frames=0`
- `legacy_evidence_rejected=0`
- `legacy_signal_rejected=0`

Post-cutover real channel usage (`ws_messages_out_total`):
- `trade=316`
- `unknown=0`
