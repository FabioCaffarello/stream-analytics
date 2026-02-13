# System Invariants

These rules are never violated.

---

## Determinism

Same input → same output.

---

## Replayability

All pipelines must be reconstructable.

---

## Domain Isolation

Business rules never live in actors.

---

## Replaceable Infrastructure

Adapters are swappable.

---

## Evidence Over Opinion

Insights must be explainable.


---

# O que fazer AGORA (ordem perfeita)

Crie:

```

docs/
 ├── adrs/
 ├── architecture/
 │     README.md
 │     ingestion.md
 │     insights.md
 │     system-invariants.md
 └── contracts/
       event-bus.md

```

Commit gigante inicial:

👉 `feat(architecture): foundational system design`

---

# Próximo passo que EU recomendo fortemente

Depois desses docs…

Crie UM documento que muda o destino do produto:

## 👉 `docs/architecture/moat.md`

Explica:

> por que esse sistema será difícil de copiar.

Isso guia TODAS as decisões futuras.

Posso escrever ele para você — e honestamente, é um dos documentos mais valiosos que uma startup deep-tech pode ter.

---

Se quiser, no próximo passo eu posso te entregar:

🔥 o blueprint do **runtime de actors ideal para market data**
🔥 como estruturar o **Instrument Actor** (talvez o actor mais importante do sistema)
🔥 o erro arquitetural que destrói 80% das plataformas de dados
🔥 como preparar o sistema para **10x volume** sem reescrever

Só falar — porque agora você está montando fundação de empresa, não só repo.

---

## E2E Hook Safety (2026-02-12)

- `E2E_TEST_MODE=1` is fail-closed unless `RUN_MODE=test` or `MARKET_RACCOON_MODE=test`.
- If the posture check fails, process startup exits with code `1` before runtime initialization.
- In E2E mode, probe HTTP binding is forced to loopback (`127.0.0.1`) regardless of configured probe address.

## Startup Fail-Fast Invariants

- Subject taxonomy validation is enforced for concrete publish subjects and startup subject patterns.
- Stream bounds validation is enforced before JetStream `AddStream`/`UpdateStream`: at least one hard bound (`MaxAge`/`MaxBytes`/`MaxMsgs`) must exist, `MaxAge` must be positive, and dedup window must be configured.
- Operational impact: startup fails immediately on config drift, preventing silent misrouting, unbounded retention, or retry storms in production.

## Retry Budget (NAK Bounded)

- Retryable ingest failures (`TRANSIENT_FAILURE`, `QUARANTINE_PUBLISH_FAILED`) are bounded by a local retry budget.
- Primary source of truth is JetStream delivery count (`NumDelivered` / `Nats-Num-Delivered` / `Nats-Msg-Redelivery`).
- When delivery count reaches budget `N`, action is `TERM` with reason code `TRANSIENT_EXHAUSTED` (no infinite transient NAK loop).
- If delivery metadata is unavailable, fallback uses a bounded in-memory partition tracker keyed by `venue|instrument` with fixed capacity and deterministic eviction.

## Runtime Boundedness (Adapter)

- JetStream replay merge buffer is bounded (`MergeBufferSize`); output channel is bounded (`OutputBufferSize`).
- Transient retry fallback state is bounded by fixed-capacity ring+map (`retryBudgetTracker`), never unbounded by message cardinality.
- On fallback tracker eviction, decision is observable via `ingest_drop_total{reason="buffer_full_drop"}` and deterministic log message.

## Protobuf-Free Domain Guardrail

- `internal/core/**`, `internal/actors/**`, and `internal/interfaces/**` must not import:
  - `google.golang.org/protobuf/*`
  - `github.com/golang/protobuf/*`
- `make invariants-check` enforces this rule and fails with:
  - `protobuf import violates Domain Isolation; move to internal/shared/contracts boundary`
