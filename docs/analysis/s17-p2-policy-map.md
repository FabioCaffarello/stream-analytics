# S17/P2 Policy Map (Cross-Subsystem Ownership/Monotonic/Dedup)

## Scope

Mapped sources of truth currently used by delivery, signal, and strategist for ownership and monotonicity.

- `internal/shared/ownership/contract.go`
- `internal/shared/ownership/monotonic.go`
- `internal/actors/delivery/runtime/seq_policy.go`
- `internal/actors/signal/runtime/subsystem_owner_policy.go`
- `internal/actors/signals/runtime/subsystem.go`

## Policy Table

| Policy | Inputs | Outputs | Invariants |
|---|---|---|---|
| `ownership.OwnerReplica` | `subsystem`, `StreamKey{venue,instrument,channel,timeframe}`, `replicaCount` | owner replica index (`int`) | Deterministic for same tuple; owner in `[0, replicaCount)`; replicaCount `<=1` => owner `0`; subsystem salt (`signals`, `strategist`, `delivery`) isolates ownership domains. |
| `ownership.DecideMonotonic` | `MonotonicInput{streamKey,isSnapshot,candidateSeq,candidateWatermark,lastSeq,lastWatermark,candidateOwner,lastOwner,handoffWatermarkSeq,pendingResyncWatermark,staleGapWindow}` | `MonotonicDecision{action,rejectReason,violationType,coherenceReason,resyncWatermark,duplicate,outOfOrder}` | `candidateSeq<=0` or empty stream => drop `seq_invalid`; exact seq replay => drop `replay_duplicate`; strictly advancing seq => accept (even if watermark regresses); non-advancing seq with watermark regression or stale gap => drop `stale_event`; severe non-monotonic fallback => drop `seq_non_monotonic`; owner change with lower/equal seq under handoff => `convert_to_resync` with `owner_change`. |
| Delivery `defaultSeqPolicy.Decide` | `seqPolicyInput{streamKey,eventType,candidateSeq,candidateTsIngest,lastSeq,lastTsIngest,candidateProcessorID,lastProcessorID,handoffWatermarkSeq,pendingResyncWatermark}` | `seqPolicyDecision{action,rejectReason,violationType,coherenceReason,resyncWatermark,duplicate,outOfOrder}` | Thin adapter over `ownership.DecideMonotonic`; snapshot detection by `eventType` containing `snapshot`; preserves reject/coherence reasons from ownership contract; duplicate/out-of-order flags drive metrics and sampling. |
| Signal owner policy (`acceptOwner`) | `marketmodel.StreamKey`, `replicaID`, `replicaCount` | allow/reject (bool), drop reason `owner_reject` | In `ReplicaCount=2`, only computed owner may proceed; non-owner always rejected and must not emit. |
| Signal monotonic policy (`acceptMonotonicProgress`) | stream canonical key, candidate `(seq,watermark)`, stored `(lastSeq,lastWatermark,lastOwner)`, owner id | allow/reject (bool), monotonic reject reason from ownership decision | Uses `ownership.DecideMonotonic`; replay duplicate classified as duplicate; stale/out-of-order classified with ownership reason; rejected events do not progress stream state. |
| Strategist owner policy (`acceptOwner`) | `(venue,instrument,channel)`, `replicaID`, `replicaCount` | allow/reject (bool), drop reason `owner_reject` | In `ReplicaCount=2`, only computed owner may proceed; non-owner always rejected and must not emit. |
| Strategist monotonic policy (`acceptMonotonic`) | stream key, channel, candidate `(seq,watermark)`, stored `(lastSeq,lastWatermark)` | allow/reject (bool), monotonic reject reason from ownership decision | Uses `ownership.DecideMonotonic`; duplicate/out-of-order flags feed ownership metrics; advancing seq remains acceptable even when watermark regresses. |

## Cross-Subsystem Consistency Targets (S17/P2)

For a fixed stream key and deterministic event sequence, the expected semantics are:

1. Duplicate input (`candidateSeq == lastSeq`) is rejected as `replay_duplicate`.
2. Out-of-order input (`candidateSeq < lastSeq`) is rejected (`stale_event` within stale-gap, otherwise `seq_non_monotonic`).
3. Watermark regression with seq advancing (`candidateSeq > lastSeq` and `candidateWatermark < lastWatermark`) is accepted.
4. Owner-only with `ReplicaCount=2`: exactly one replica is eligible to accept; non-owner rejects with `owner_reject`.
5. Duplicate replay must not produce double-emit (owner gating + monotonic dedup combined).
