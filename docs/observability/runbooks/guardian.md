---
type: doc
status: Active
last_updated: 2026-06-25
---

# Guardian / Actor Runtime Runbook

**Scope:** Hollywood actor supervision tree — restart storms, actor crashes, Guardian health.

---

## Alert: `ActorRestartStorm`

**Meaning:** An actor is restarting more than 5 times in 60 seconds.

**Immediate check:**
```bash
make logs service=processor | grep "actor\|panic\|restart\|killed"
make logs service=consumer | grep "actor\|panic\|restart\|killed"
```

**Resolution steps:**

1. Identify which actor is restarting: look for `actor=<name>` in log lines.
2. Check for the root cause error that preceded the first restart.
3. If the actor is a JetStream consumer actor: verify NATS connectivity (`make ps`).
4. If the actor is an exchange adapter: check exchange API status and rate limits.
5. Force a clean restart of the affected service if restarts don't self-heal within 2 min:
   ```bash
   docker compose restart <service>
   ```

---

## Alert: `GuardianPanic`

**Meaning:** The Guardian root actor has panicked — the service will exit.

This is a hard failure. Collect the panic stack from logs, then restart:
```bash
make logs service=processor | grep -A30 "panic:"
docker compose restart processor
```

File an incident with the panic stack attached.

---

## Actor supervision tree reference

```
Guardian
├── ExchangeSupervisor → [BinanceActor, BybitActor, ...]
├── ProcessorSupervisor → [AggregationActor, DeliveryActor, ...]
└── StoreSupervisor → [TimescaleActor, ClickHouseActor]
```

See `docs/architecture/diagrams/actor-supervision-tree.md` for the full tree.

---

## See also

- [Ingest Runbook](ingest.md)
- [SLO Definitions](../slo.md)
