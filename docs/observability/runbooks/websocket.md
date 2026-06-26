---
type: doc
status: Active
last_updated: 2026-06-25
---

# WebSocket Delivery Runbook

**Scope:** Terminal_V1 WebSocket server — session health, fan-out latency, backfill failures.

---

## Alert: `WSDeliveryLatencyHigh`

**Meaning:** WebSocket fan-out p95 latency exceeds 20 ms.

**Immediate check:**
```bash
make logs service=server | grep "delivery\|fanout\|slow\|session"
```

**Common causes:**

| Cause | Signal | Action |
|-------|--------|--------|
| High session count | `ws_active_sessions` metric high | Verify load balancing; check session limits per instance |
| Codec bottleneck | `transcode_cache_misses` climbing | Increase cache size via `WS_TRANSCODE_CACHE_SIZE` |
| Slow client backpressure | `ws_send_blocked_total` rising | Drop slow clients (already automatic after write timeout) |

---

## Alert: `WSBackfillFailed`

**Meaning:** A client requested backfill and the server failed to deliver it.

```bash
make logs service=server | grep "backfill\|error\|timeout"
```

Backfill reads from TimescaleDB. Check DB connectivity:
```bash
make ps | grep timescaledb
```

---

## Terminal_V1 protocol quick reference

```
Client → Server: Hello{session_id, subscriptions}
Server → Client: Welcome{snapshot}
Client → Server: Subscribe{subjects}
Server → Client: Event stream
Client → Server: Backfill{from, to, subjects}
Server → Client: BackfillResult
```

Full protocol: `docs/contracts/delivery-ws.md`.

---

## Active session inspection

```bash
# Count active WebSocket sessions (via Prometheus)
curl -s http://localhost:9090/api/v1/query?query=ws_active_sessions
```

---

## See also

- [Ingest Runbook](ingest.md)
- [SLO Definitions](../slo.md)
