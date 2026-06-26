---
type: doc
status: Active
last_updated: 2026-06-25
---

# Consumer Stall Runbook

**Scope:** Exchange consumer has stopped receiving market data — zero throughput alert.

---

## Alert: `ConsumerStall`

**Meaning:** A consumer has produced zero events for > 30 seconds on an active exchange.

**Immediate check:**
```bash
make logs service=consumer | grep "stall\|disconnect\|reconnect\|error\|exchange="
make ps
```

---

## Decision tree

```
ConsumerStall fired
│
├─ Exchange API down? → Check exchange status page
│   └─ Yes → Wait for exchange to recover; consumer will reconnect automatically.
│
├─ Network issue? → make ps shows nats unhealthy / network errors in log
│   └─ Yes → Restore network; consumer reconnects within back-off window (max 30 s).
│
├─ Rate-limited by exchange? → Log shows 429 or "too many connections"
│   └─ Yes → Reduce connection count; wait out the ban window.
│
├─ Consumer actor crashed? → Guardian restarted the consumer actor
│   └─ Yes → Check restart count; see Guardian Runbook if restart storm.
│
└─ Unknown → Restart the consumer service
    docker compose restart consumer
```

---

## Back-off and reconnect behaviour

The consumer uses exponential back-off (base 500 ms, max 30 s) on disconnect. After reconnect, gap-fill is triggered automatically for up to `CONSUMER_GAP_FILL_WINDOW` (default 5 min of missed data).

See `docs/architecture/diagrams/sequence-exchange-recovery.md` for the full sequence.

---

## Checking gap-fill completion

```bash
make logs service=consumer | grep "gap.*filled\|gap.*failed\|backfill"
```

If gap-fill fails, the consumer logs the gap range and continues with live data.

---

## See also

- [Ingest Runbook](ingest.md)
- [Bus Runbook](bus.md)
- [Guardian Runbook](guardian.md)
