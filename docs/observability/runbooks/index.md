---
type: doc
status: Active
last_updated: 2026-06-25
---

# Observability Runbooks

Per-subsystem operational runbooks. All assume the full stack is running (`make up`).

---

| Runbook | Scope |
|---------|-------|
| [Cold-Path Runbook](../../operations/cold-path-runbook.md) | Store alerts, ClickHouse degradation |
| [Degradation Contract](../../operations/degradation.md) | ClickHouse failure propagation |
| [Sharding Guide](../../operations/sharding.md) | Horizontal processor scaling |
| [Shard Incidents](../../operations/shard-incidents.md) | Shard alert playbooks |
| [Validator](../../operations/validator.md) | JetStream event validator |
| [Emulator](../../operations/emulator.md) | Synthetic event injection |

See also: [SLO Definitions](../slo.md)
