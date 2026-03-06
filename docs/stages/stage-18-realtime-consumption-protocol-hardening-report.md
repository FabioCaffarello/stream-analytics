# Stage 18 -- Realtime Consumption Protocol Hardening

**Date:** 2026-03-06
**Branch:** codex/s9-legacy-removal-cutover
**Status:** COMPLETE (Slices 1-4 delivered)

---

## Executive Summary

Stage 18 consolidates the backend as a formally hardened realtime surface, closing protocol gaps in subscribe/snapshot/delta/resync semantics, gap detection chain correctness, and HTTP vs WS consumption policy -- without starting client evolution or expanding the trading surface.

After S17 (session overview + freshness APIs), the backend had 14+ HTTP endpoints, 11 WS invariants, and snapshot-before-delta ordering (WS-11), but still had protocol hardening gaps: `prev_seq` chain reset on resync, doc/code mismatch in subscribe ordering, missing HTTP vs WS policy guidance, and no contract coverage for key chain/handshake invariants.

**Mission:** Harden the realtime consumption protocol with tested contracts, correct gap-detection semantics, documented consumption policy, and aligned docs -- all as backend-only work.

**Outcome:** All four slices were delivered: protocol bug-fix and contract tests (Slice 1), resync diagnostics (Slice 2), bootstrap handshake hardening (Slice 3), and delivery diagnostics surface (Slice 4).

**Constraints (all met):**
- No trading surface expansion
- No new domain types or streams
- No domain logic in cmd/*
- No storage/federation details leaked to client
- No client-side changes
- No breaking changes to frozen wire format

---

## 1. Current-State Audit (Post S17)

### 1.1 Protocol Maturity Assessment

| Area | State | Evidence |
|---|---|---|
| `hello` gate (WS-8) | Implemented | `session_protocol.go:emitHello` + test |
| `subscribe` flow | Implemented | `session_commands.go:handleSubscribe` + 5 tests |
| `unsubscribe` flow | Implemented | `session_commands.go:handleUnsubscribe` + test |
| `resync` flow | Implemented (bug fixed in S18) | `session_commands.go:handleResync` + test |
| `getrange` / `getlast` | Implemented | `session_commands.go:handleGetRange/GetLast` + 7 tests |
| `ping` / `pong` | Implemented | `session_protocol.go:handlePing` + test |
| Feature negotiation | Implemented | `session_protocol.go:handleClientHello` + test |
| Snapshot-before-delta (WS-11) | Implemented + tested | `session_commands.go:emitSnapshot` called before ack |
| `prev_seq` gap detection (WS-10) | Implemented + **bug fixed** | `session_delivery.go:writeDeliveryEvent` |
| `snapshot_seq` monotonicity (WS-9) | Implemented + tested | `session_commands.go:emitSnapshot` |
| Backpressure hints | Implemented | `session_metrics_tick.go` |
| Error taxonomy + action_hint | Implemented | `session_protocol.go:wsErrorMappingFromProblem` |
| Rate limiting | Implemented | `rate_limiter.go` + test |
| Batching | Implemented | `session_delivery.go:writeDeliveryBatchFromQueue` |
| Compression | Implemented | `session_delivery.go:planWireCompression` |
| SeqPolicy (monotonic ownership) | Implemented | `seq_policy.go` + `ownership.DecideMonotonic` + test |

### 1.2 Protocol Gaps Found

| Gap | Severity | Impact | Resolution |
|---|---|---|---|
| **G1: prev_seq not reset on resync** | P0 | After resync, first event carries stale prev_seq instead of 0; client gap detection sees false chain continuation | **Fixed in Slice 1** -- `delete(s.lastDeliveredSeq, subjectKey)` after resync |
| **G2: delivery-ws.md subscribe flow wrong** | P1 | Doc says ack-before-snapshot, code does snapshot-before-ack (correct behavior); doc/code mismatch | **Fixed in Slice 1** -- doc aligned to code |
| **G3: No prev_seq contract test** | P1 | Chain correctness untested; regression risk | **Fixed in Slice 1** -- `TestProtocol_PrevSeqChain_MonotonicAcrossEvents` |
| **G4: No snapshot_seq monotonicity test** | P1 | WS-9 invariant untested across subscribe+resync | **Fixed in Slice 1** -- `TestProtocol_SnapshotSeq_IncrementsOnResync` |
| **G5: No HTTP vs WS consumption policy** | P2 | Client has no guidance on when to use HTTP vs WS | **Fixed in Slice 1** -- consumption policy table in delivery-ws.md |
| **G6: WS getrange lacks federation bridge** | P3 | WS getrange is in-memory only; deep history requires HTTP | **Deferred** -- HTTP endpoints serve this need; document as policy |
| **G7: snapshot_seq absent in SnapshotWireCache path** | P3 | When wire cache is active, snapshot_seq=0 (legacy marker); correct by spec but limits client verification | **Documented** -- by-design; cache shares wire bytes across sessions |

### 1.3 What Works Well (No Change Needed)

| Surface | Assessment |
|---|---|
| Session actor isolation (WS-1) | Strong -- one session crash cannot cascade |
| Per-subject seq ordering (WS-3) | Strong -- envelope.Seq preserved through delivery |
| Bounded outbound queue (WS-4) | Strong -- configurable size + 3 drop policies |
| Cleanup on disconnect (WS-5) | Strong -- unregister + state cleanup |
| Snapshot delivery ordering (WS-11) | Strong -- code flow guarantees, now tested |
| Error taxonomy | Strong -- 7 problem codes, 6 action hints |
| Backpressure metrics | Strong -- 4 levels, queue depth, recommendations |
| Feature negotiation | Strong -- batching, snapshot_hash, prev_seq, compression |

---

## 2. Stage 18 Architecture

### 2.1 Design Decisions

**D1: prev_seq chain reset on resync is the correct semantic**

The doc specifies `prev_seq == 0` on first event after subscribe/resync. After resync, the client rebuilds state from a fresh snapshot and starts a new gap-detection chain. Continuing the old chain would cause false gap detection if the resync was triggered by a real gap.

**D2: Subscribe flow order is snapshot-before-ack (not ack-before-snapshot)**

The actual code emits snapshot first, then ack. This is correct because:
- Snapshot establishes initial state before the client considers subscription "active"
- Ack confirms the subscription is set up and events will follow
- This matches resync flow (snapshot → ack) for consistency

**D3: HTTP vs WS consumption policy is a contract, not a guideline**

The consumption policy is formalized in delivery-ws.md so both current and future clients have an authoritative reference. Rules:
- HTTP for bootstrap, discovery, and deep federated history
- WS for realtime delivery and lightweight queries
- WS getrange is in-memory only (by design, not a limitation)

**D4: WS getrange federation bridge is deferred**

The HTTP data endpoints (`/api/v1/candles`, etc.) already provide federated access to hot+cold storage. Adding a federation bridge to WS getrange would:
- Increase WS handler complexity
- Risk latency spikes in the actor mailbox
- Duplicate functionality already available via HTTP

Clients should use HTTP for deep history. WS getrange serves lightweight bootstrap/reconnect scenarios from in-memory state.

**D5: SnapshotWireCache snapshot_seq=0 is by-design**

The wire cache shares serialized bytes across sessions. Per-session `snapshot_seq` cannot be baked into shared cache entries. The spec already handles this: `snapshot_seq == 0` means "legacy snapshot (pre-F3)" and clients should not rely on it for ordering. The non-cached and fallback paths correctly increment snapshot_seq.

---

## 3. Realtime Protocol Strategy

### 3.1 Official Bootstrap Flow

```
1. Client opens WebSocket connection
2. Server emits `hello` (mandatory first frame):
   - proto_ver, server_time, capabilities, supported_features
3. Client MAY send `hello` with requested_features (optional)
4. Client sends `subscribe` per subject
5. Per subscribe:
   a. Server emits `snapshot` (if HotSnapshotProvider or last event available)
   b. Server emits `ack`
   c. Server streams `event` frames (first event has prev_seq=0)
```

### 3.2 Official Reconnect Flow

```
1. Client detects connection loss
2. Client opens new WebSocket connection
3. Server emits `hello` (fresh session, new instance)
4. Client re-subscribes to all subjects
5. Same subscribe flow as bootstrap (snapshot → ack → events)
```

No session resumption. Each connection is a fresh session with fresh seq chains.

### 3.3 Official Resync Flow

```
1. Client detects gap via prev_seq mismatch OR stale state
2. Client sends `resync` with stream_id and last_seq
3. Server emits `snapshot` (incremented snapshot_seq, updated watermark)
4. Server emits `ack` for resync
5. Server resets prev_seq chain (first event carries prev_seq=0)
6. Server resumes event delivery
```

### 3.4 Gap Detection Protocol

Client-side gap detection via `prev_seq`:
- On each `event` frame, verify: `event.prev_seq == last_received_seq` for the same subject
- `prev_seq == 0` is normal: means first event after subscribe/resync
- If `prev_seq != 0 && prev_seq != last_received_seq`: gap detected → send `resync`

Server-side coherence via `SeqPolicy`:
- Monotonic ownership detection with configurable stale-gap window (2048)
- Processor handoff awareness (multi-replica tolerance)
- Automatic resync conversion on large gaps

### 3.5 HTTP vs WS Consumption Policy

| Transport | Use For | Not For |
|---|---|---|
| HTTP | Bootstrap, discovery, deep history, freshness | Realtime events, lightweight queries |
| WS | Realtime events, subscribe/unsubscribe, resync, ping | Deep historical queries, bootstrap config |

See `docs/contracts/delivery-ws.md` "HTTP vs WS Consumption Policy" for the full table.

---

## 4. Prioritized Slices

```
Slice 1: Protocol Contract Tests + Bug Fix + Doc Alignment        [COMPLETE]
  - FIX: prev_seq chain reset on resync (P0 bug)
  - FIX: delivery-ws.md subscribe flow order (doc/code mismatch)
  - ADD: HTTP vs WS consumption policy to delivery-ws.md
  - ADD: 5 protocol contract tests (prev_seq chain, resync reset,
         snapshot_seq monotonicity, WS-11 snapshot-before-ack, no-snapshot ack-first)
  - ADD: Stage 18 report

Slice 2: Resync Watermark Diagnostics                             [COMPLETE]
  - Emit watermark_seq + snapshot_seq in resync ack frame
  - Per-session resync_count in metrics frame
  - 3 contract tests: watermark propagation, monotonicity, resync counter

Slice 3: Bootstrap Handshake Hardening                            [COMPLETE]
  - Optional config gate: require client hello before subscribe/resync/getrange
  - Hello ack diagnostics: ts_server + clock_skew_ms
  - 4 contract tests: gate reject/allow + skew/no-skew behavior
  - Bootstrap wiring: cmd/server -> wsserver -> session config

Slice 4: Delivery Sequence Diagnostics Endpoint                   [COMPLETE]
  - GET /api/v1/delivery/diagnostics (localhost-only) with per-stream seq/drop/resync state
  - Metrics frame diagnostics: dropped_count + subject_count
  - 3 tests: metrics counters + HTTP endpoint success + localhost guard
```

---

## 5. Slice 1 Implementation

### 5.1 Bug Fix: prev_seq Reset on Resync

**Root cause:** `handleResync` in `session_commands.go` did not clear `lastDeliveredSeq[subjectKey]` after emitting the resync snapshot. After resync, the first event carried the stale prev_seq from before the resync.

**Fix:** Added `delete(s.lastDeliveredSeq, subject.String())` after the resync snapshot emission, before the ack.

**Files changed:**
| File | Change |
|---|---|
| `internal/actors/delivery/runtime/session_commands.go` | +2 lines: clear lastDeliveredSeq after resync |

### 5.2 Doc Alignment: Subscribe Flow Order

**Root cause:** delivery-ws.md WS-11 section described subscribe flow as "ack → snapshot → events" but code does "snapshot → ack → events" (correct behavior).

**Fix:** Updated doc to match code: snapshot → ack → events. Also documented that first event after subscribe/resync carries `prev_seq == 0`.

**Files changed:**
| File | Change |
|---|---|
| `docs/contracts/delivery-ws.md` | Fixed subscribe flow order, added prev_seq reset note on resync, added HTTP vs WS consumption policy table |

### 5.3 Protocol Contract Tests

| Test | Invariant | What it Verifies |
|---|---|---|
| `TestProtocol_PrevSeqChain_MonotonicAcrossEvents` | WS-10 | prev_seq=0 on first event, then prev_seq=seq(N-1) for subsequent events |
| `TestProtocol_PrevSeqZero_AfterResync` | WS-10 | prev_seq chain resets to 0 after resync (the bug fix) |
| `TestProtocol_SnapshotSeq_IncrementsOnResync` | WS-9 | snapshot_seq goes 1→2→3 across subscribe+resync+resync |
| `TestProtocol_WS11_SnapshotBeforeAck_OnSubscribe` | WS-11 | First frame is snapshot (when available), second is ack |
| `TestProtocol_WS11_NoSnapshot_AckFirst` | WS-11 | When no snapshot available, ack is the first frame |

**Files created:**
| File | LOC | Purpose |
|---|---|---|
| `internal/actors/delivery/runtime/session_protocol_contract_test.go` | 261 | 5 protocol contract tests + 2 helpers |

---

## 5b. Slice 2 Implementation

### 5b.1 Resync Ack Watermark + Snapshot Seq

After a resync, the ack frame now carries `watermark_seq` and `snapshot_seq` so the client knows exactly what the snapshot covers and can verify monotonicity.

**Design:** The `watermark_seq` is sourced from `lastSnapshot[subjectKey].Seq` — the seq of the most recent event delivered before the resync. The `snapshot_seq` is the per-subject counter already maintained by `emitSnapshot`. Both fields use `omitempty` so subscribe acks (which don't need watermark info) remain unchanged.

**Files changed:**
| File | Change |
|---|---|
| `internal/actors/delivery/runtime/session_protocol.go` | `wsAckFrame` gains `WatermarkSeq` and `SnapshotSeq` fields (omitempty) |
| `internal/actors/delivery/runtime/session_commands.go` | `handleResync` populates watermark from lastSnapshot, increments resyncCount |

### 5b.2 Per-Session Resync Counter in Metrics Frame

A new `resync_count` field in `wsMetricsPayload` tracks the total number of resyncs across all subjects for this session. Unlike the existing `resync_total` (which comes from the global observability snapshot), `resync_count` is per-session and monotonically increasing.

**Files changed:**
| File | Change |
|---|---|
| `internal/actors/delivery/runtime/session.go` | `SessionActor` gains `resyncCount int64` field |
| `internal/actors/delivery/runtime/session_protocol.go` | `wsMetricsPayload` gains `ResyncCount int64` field |
| `internal/actors/delivery/runtime/session_metrics_tick.go` | `handleMetricsTick` emits `resyncCount` |

### 5b.3 Contract Tests

| Test | What it Verifies |
|---|---|
| `TestProtocol_ResyncAck_CarriesWatermark` | Resync ack contains watermark_seq matching last delivered event's seq, and snapshot_seq >= 1 |
| `TestProtocol_ResyncWatermark_MonotonicAcrossResyncs` | watermark_seq and snapshot_seq are monotonically increasing across multiple resyncs |
| `TestProtocol_MetricsFrame_ResyncCount` | resync_count=0 before any resync, resync_count=2 after two resyncs |

**Files changed:**
| File | LOC added | Purpose |
|---|---|---|
| `internal/actors/delivery/runtime/session_protocol_contract_test.go` | ~160 | 3 contract tests + 2 helpers (readAckFrame, readMetricsFrame) |

---

## 5c. Slice 3 Implementation

### 5c.1 Optional Require-Client-Hello Gate

An optional compatibility-safe gate was added to reject `subscribe`, `resync`, and `getrange` until the client sends `{"op":"hello"}` when `delivery.require_client_hello=true`.

**Files changed:**
| File | Change |
|---|---|
| `internal/actors/delivery/runtime/session.go` | `SessionConfig.RequireClientHello` added |
| `internal/actors/delivery/runtime/session_protocol.go` | `requireHelloGate` helper + validation error emission |
| `internal/actors/delivery/runtime/session_commands.go` | gate enforcement in `handleSubscribe`, `handleResync`, `handleGetRange` |
| `internal/interfaces/ws/server.go` | `WithRequireClientHello` option + session config propagation |
| `cmd/server/bootstrap.go` | wiring from config to WS server option |
| `internal/shared/config/schema.go` | `delivery.require_client_hello` config field |

### 5c.2 Hello Ack Clock Diagnostics

Hello ack now emits `ts_server` always, and emits `clock_skew_ms` when client sends `ts_client`.

**Files changed:**
| File | Change |
|---|---|
| `internal/actors/delivery/runtime/session_protocol.go` | `wsHelloAckFrame` gains `TsServer` + `ClockSkewMs`; `handleClientHello` computes skew |

### 5c.3 Contract Tests

| Test | What it Verifies |
|---|---|
| `TestProtocol_HelloGate_RejectsSubscribeWithoutHello` | gated commands are rejected before hello |
| `TestProtocol_HelloGate_AllowsAfterHello` | subscribe succeeds after hello |
| `TestProtocol_HelloAck_ClockSkew` | hello ack includes skew when `ts_client` is provided |
| `TestProtocol_HelloAck_NoClockSkewWithoutTsClient` | hello ack omits skew diagnostic when client timestamp is absent |

---

## 5d. Slice 4 Implementation

### 5d.1 Delivery Diagnostics HTTP Endpoint

Added operational endpoint `GET /api/v1/delivery/diagnostics` (localhost-only) to expose terminal WS stream state for per-subject sequence/debug analysis.

**Response includes:**
- Session-level counters: `connections_active`, `resync_total`, `drops_total`
- Per-stream entries: `stream_id`, `last_seq`, `last_ts_*`, `lag_ms`, `delivered_total`, `dropped_total`, `resync_total`

**Files changed:**
| File | Change |
|---|---|
| `internal/interfaces/http/delivery_diagnostics_handlers.go` | new response model + handler |
| `internal/interfaces/http/server.go` | route registration (`localhostOnly`) |

### 5d.2 Metrics Frame Diagnostic Counters

Metrics payload now includes:
- `dropped_count`: per-session dropped outbound events
- `subject_count`: tracked subject chains (`lastDeliveredSeq` size)

**Files changed:**
| File | Change |
|---|---|
| `internal/actors/delivery/runtime/session_protocol.go` | payload fields added |
| `internal/actors/delivery/runtime/session_metrics_tick.go` | field emission in metrics tick |

### 5d.3 Tests

| Test | What it Verifies |
|---|---|
| `TestProtocol_MetricsFrame_DiagnosticCounters` | metrics frame includes `dropped_count` and `subject_count` |
| `TestServer_DeliveryDiagnostics_ReturnsSnapshot` | HTTP endpoint returns per-stream state with expected counters |
| `TestServer_DeliveryDiagnostics_RemoteForbidden` | endpoint is localhost-protected |

---

## 6. Validation

```
internal/actors/delivery/runtime:  13 protocol contract tests PASS (Slices 1-4)
internal/actors/delivery/runtime:  all existing tests PASS (zero regressions)
internal/interfaces/http:          diagnostics endpoint tests PASS
cmd/server:                        builds clean
cmd/processor:                     builds clean
cmd/executor:                      builds clean
```

---

## 7. Risks

| Risk | Severity | Mitigation |
|---|---|---|
| prev_seq reset changes client behavior on resync | Low | Client should already handle prev_seq=0 after resync (documented contract); no existing client relies on stale prev_seq |
| SnapshotWireCache skips snapshot_seq | Low | By-design; snapshot_seq=0 means legacy per spec; non-cached paths emit correctly |
| WS getrange lacks federation (deferred) | Low | HTTP data endpoints cover deep history; documented in consumption policy |
| `require_client_hello` compatibility gate may reject legacy clients | Low | Default is `false`; rollout can be staged per environment |

---

## 8. Non-Goals (Explicit)

- Client-side changes
- New domain types or streams
- WS getrange federation bridge (deferred to S19+)
- Trading surface expansion
- Session resumption across connections
- Breaking wire format changes
- Protobuf-only mode enforcement

---

## 9. Recommended Next Stage

**S19 candidate:** WS getrange federation bridge (only if product/client needs deep history directly through WS).

Alternative next priorities:
1. **Client evolution** -- protocol is now hardened with explicit contracts and diagnostics
2. **Ops automation** -- alerting/runbook wiring on `/api/v1/delivery/diagnostics`
3. **S19+** -- WS getrange federation bridge and/or new domain streams only if required
