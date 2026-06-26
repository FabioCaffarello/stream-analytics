# Validator — JetStream Schema Validation Service

**Status:** Active
**Last updated:** 2026-06-25
**Relates to:** `cmd/validator/main.go`, `deploy/configs/validator.jsonc`, `deploy/compose/docker-compose.yml:233`, `docs/operations/emulator.md`

---

## Purpose

`cmd/validator` is a long-running JetStream consumer service that validates incoming dataplane
messages against their binding schema. It runs as a sidecar in the compose stack alongside
the core services.

Primary use cases:
- Schema compliance enforcement for dataplane messages
- CI gate: paired with the emulator to test valid/invalid event flows
- Observability: validation metrics exposed via `/metrics`

---

## HTTP Endpoints

Port: `:8089` (configured in `deploy/configs/validator.jsonc` → `http.addr`)

| Endpoint | Method | Response | Notes |
|----------|--------|----------|-------|
| `/healthz` | GET | `{"status":"ok"}` (always 200) | Liveness — always returns 200 |
| `/readyz` | GET | `{"ready":true}` 200 / `{"ready":false}` 503 | Readiness — true after consume loop starts |

`ready` is set to `true` atomically after the JetStream consumer loop initializes.
Code anchor: `cmd/validator/main.go:92`

```bash
# Smoke check
curl -sf http://127.0.0.1:8089/readyz && echo "validator: OK"
```

---

## JetStream Consumer

| Parameter | Value |
|-----------|-------|
| Consumer durable | `validator-v1` |
| Filter subjects | `dataplane.message.>` |
| AckWait | 30s |
| MaxAckPending | 256 |
| MaxDeliver | 10 |
| DeliverPolicy | New (latest messages only) |

---

## Processing Flow

For each JetStream message:

1. Deserialize envelope → `dataplane.MessageFromEnvelope`
2. Resolve binding from NATS KV store (bucket: `data_plane.state_bucket`)
3. `validatorruntime.Processor.Process` — validate required fields, compute violations
4. Publish validation result back to NATS result store (KV bucket, limited to `data_plane.result_limit` entries)
5. Log result: `binding=<name> message_id=<id> status=<ok|violation> violations=<N>`
6. ACK on success; propagate error (TERM) on permanent validation failures

Code anchor: `internal/application/validatorruntime/` — validation processor logic

---

## Result Store

Validation results are persisted in a NATS KV bucket:
- Bucket: `data_plane.state_bucket` (default: `MR_DATAPLANE`)
- Key pattern: `result:<message_id>`
- Retention: last `data_plane.result_limit` entries (default: 100)

Results are queryable via NATS CLI:
```bash
nats kv get MR_DATAPLANE result:<message_id> --server nats://localhost:4222
```

---

## Configuration (`deploy/configs/validator.jsonc`)

Key fields:

```jsonc
{
  "http": {
    "addr": ":8089"
  },
  "data_plane": {
    "state_bucket": "MR_DATAPLANE",
    "result_limit": 100
  },
  "jetstream": {
    "url": "nats://nats:4222",
    "stream": "MARKETDATA",
    "durable": "validator-v1"
  }
}
```

---

## Prometheus Metrics

Key metrics exposed at `:8089/metrics` (via shared metrics middleware):

| Metric | Type | Description |
|--------|------|-------------|
| `dataplane_validation_total{status}` | Counter | Total validations by status (`ok`, `violation`, `error`) |
| `dataplane_validation_violations_total` | Counter | Total field violations detected |
| `dataplane_messages_consumed_total` | Counter | JetStream messages consumed |

---

## Code Anchors

| File | Purpose |
|------|---------|
| `cmd/validator/main.go:92` | Atomic ready flag + consume loop start |
| `cmd/validator/main.go:144` | HTTP server with `/healthz` and `/readyz` |
| `internal/application/validatorruntime/` | Validation processor |

---

## Related

- [Emulator](emulator.md) — injects synthetic events that the validator processes
- [Local Dev](../local-dev.md) — validator is part of the default `core` compose profile
