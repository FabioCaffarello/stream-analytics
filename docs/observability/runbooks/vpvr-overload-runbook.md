---
type: doc
status: Active
last_updated: 2026-06-25
---

# VPVR Overload Runbook

**Scope:** Volume Profile Visible Range (VPVR) computation — CPU spike, shard overload, memory pressure.

---

## Alert: `VPVRComputeOverload`

**Meaning:** VPVR shard computation is saturating CPU (> 80% for > 2 min) or the queue depth is growing.

**Immediate check:**
```bash
make logs service=processor | grep "vpvr\|shard\|overload\|pressure"
curl -s http://localhost:9090/api/v1/query?query=vpvr_compute_duration_seconds | jq .
```

**Common causes and actions:**

| Cause | Signal | Action |
|-------|--------|--------|
| Wide VPVR window requested | High compute duration | Cap window via `VPVR_MAX_WINDOW_BARS` |
| Too many concurrent shards | Shard count metric high | Reduce `VPVR_MAX_SHARDS` or scale processor |
| Hot pair | One symbol dominates CPU | Enable per-pair rate limiting via `VPVR_RATE_LIMIT` |
| Recompute storm after reconnect | Burst on reconnect | Expected; allow drain — typically < 30 s |

---

## Shard ownership

VPVR shards are assigned per symbol-exchange pair. If a shard is stuck:
```bash
make logs service=processor | grep "shard.*stuck\|shard.*timeout"
```

A stuck shard self-heals via Guardian restart after the actor timeout fires (~10 s).

---

## Manual relief

To shed load temporarily, reduce the number of VPVR-enabled subscriptions via the cockpit, or restart the processor with a reduced pair list:
```bash
docker compose restart processor
```

---

## Useful Grafana panels

- `VPVR` dashboard → `compute_duration`, `shard_count`, `queue_depth`

---

## See also

- [Guardian Runbook](guardian.md)
- [Ingest Runbook](ingest.md)
- [SLO Definitions](../slo.md)
