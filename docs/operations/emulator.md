# Emulator — CLI Test Event Injector

**Status:** Active
**Last updated:** 2026-06-25
**Relates to:** `cmd/emulator/main.go`, `deploy/configs/emulator.jsonc`, `docs/operations/validator.md`

---

## Purpose

`cmd/emulator` is a CLI tool for injecting synthetic market events into the Kafka analytics
pipeline. It is used to validate downstream consumers (validator, Flink, Metabase) without
requiring live exchange feeds. It emits a single event per invocation and exits — it is NOT
a long-running service.

Primary use cases:
- CI integration testing of the dataplane (validator + Flink pipeline)
- Manual smoke testing after infrastructure changes
- Generating known-bad events to verify validator rejection behaviour

---

## Usage

```bash
go run ./cmd/emulator \
  --config deploy/configs/emulator.jsonc \
  --binding orders \
  --scenario valid
```

### CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--config` | `config.jsonc` | Path to JSONC config file |
| `--binding` | `orders` | Runtime binding name (resolved from NATS KV store) |
| `--scenario` | `valid` | Event scenario: `valid` or `missing_required` |

### Output

On success, the emulator prints one line to stdout:
```
binding=<name> topic=<topic> message_id=<id> correlation_id=<id>
```

Exit code `0` on success, non-zero on error (NATS unreachable, Kafka produce failure, unknown binding).

---

## Scenarios

| Scenario | Behaviour | Use case |
|----------|-----------|----------|
| `valid` | Emits a well-formed event with all required fields populated | Positive path testing |
| `missing_required` | Emits an intentionally malformed event with missing required fields | Validator rejection testing |

---

## Runtime Dependencies

The emulator requires two runtime systems before invoking:

| Dependency | Role | Config key |
|-----------|------|-----------|
| NATS JetStream | Lookup the binding configuration from a KV bucket | `data_plane.state_bucket` |
| Kafka | Write the synthetic event to the binding's topic | `data_plane.kafka.brokers` |

The binding lookup works as follows:
1. Connect to NATS JetStream using `jetstream.url`
2. Open KV bucket `data_plane.state_bucket` (default: `MR_DATAPLANE`)
3. Get key `binding:<name>` → resolve topic name and event template
4. Write the event to the resolved Kafka topic

---

## Configuration (`deploy/configs/emulator.jsonc`)

Key fields:

```jsonc
{
  "data_plane": {
    "kafka": {
      "brokers": ["kafka:9092"]
    },
    "state_bucket": "MR_DATAPLANE",
    "result_limit": 100
  },
  "jetstream": {
    "url": "nats://nats:4222"
  }
}
```

---

## Code Anchors

| File | Purpose |
|------|---------|
| `cmd/emulator/main.go:17` | CLI flag definitions and scenario constants |
| `internal/application/emulatorruntime/` | Event emission logic |

---

## Related

- [Validator](validator.md) — downstream consumer that validates events the emulator injects
- [Analytics Pipeline](../architecture/analytics-pipeline.md) — the Kafka pipeline the emulator feeds
