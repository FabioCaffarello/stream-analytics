---
type: doc
status: Active
last_updated: 2026-06-25
---

# NATS JetStream Bus Runbook

**Scope:** NATS JetStream message bus — stream health, consumer lag, subject gaps.

---

## Alert: `NATSStreamUnhealthy`

**Meaning:** A JetStream stream is in a degraded state (no leader, lagging replicas, or storage failure).

**Immediate check:**
```bash
docker compose exec nats nats stream report
docker compose exec nats nats server report jetstream
```

**Common causes:**

| Cause | Signal | Action |
|-------|--------|--------|
| Raft leader election | `no leader` in report | Wait up to 30 s for re-election; restart NATS only if stuck |
| Storage full | `storage_used` near limit | Purge old messages or increase retention limits |
| Network partition | Split cluster in `nats server list` | Restore network; allow raft to converge |

---

## Alert: `JetStreamConsumerLag`

**Meaning:** A push consumer's `num_pending` is growing — processor is not keeping up.

```bash
docker compose exec nats nats consumer report <stream> <consumer>
```

If the processor is healthy, the lag should drain. If not, see [Ingest Runbook](ingest.md).

---

## Gap detection

Clients detect sequence gaps via `prev_seq != last_seq + 1`. When a gap is detected:
1. Consumer logs `gap detected seq=N prev=M` — this is expected on reconnect.
2. Gap-fill is triggered automatically (see `docs/architecture/diagrams/sequence-exchange-recovery.md`).
3. If gap-fill does not complete within 30 s, check exchange adapter connectivity.

---

## Manual NATS operations

```bash
# List all streams
docker compose exec nats nats stream ls

# Purge a stream (WARNING: loses data)
docker compose exec nats nats stream purge <stream> --confirm

# Check consumer state
docker compose exec nats nats consumer info <stream> <consumer>
```

---

## See also

- [Ingest Runbook](ingest.md)
- [Consumer Stall Runbook](consumer-stall.md)
