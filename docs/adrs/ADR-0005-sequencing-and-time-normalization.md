# ADR-0005 — Sequencing & Time Normalization

**Status:** Accepted
**Date:** 2026-02-10

## Context

Exchange timestamps are inconsistent. Without a consistent ordering mechanism, orderbook building and replay correctness degrade.

## Decision

We introduce a Sequencer in `core/marketdata/app`:

- Maintains monotonic `seq` per `(venue, instrument)`.
- Derives `ts_ingest` using a clock abstraction.
- Envelope includes both `ts_exchange` and `ts_ingest`.
- Aggregation logic uses `seq` for ordering; `ts_exchange` is advisory only.

## Consequences

- Deterministic processing and reliable replay.
- Better correctness for derived artifacts (orderbooks/heatmaps/stats).

## Alternatives

- Rely on exchange timestamps (rejected: causes out-of-order issues).

## Amendment — 2026-02-12

### Sequence Authorities

Ha dois dominios de sequencia independentes:

- `envelope.seq` (autoridade de dominio por stream)
- sequencia de transporte (futura, JetStream)

`envelope.seq` continua sendo a ordenacao semantica para app/core.

### Replay Interaction (High-Level)

No replay deterministico, `seq` e timestamps de ingest devem vir do fixture para reproduzir o mesmo comportamento de dedup/gap/order.

### Persistencia de Sequencer

Continuidades cross-restart podem ser adicionadas no futuro via armazenamento de `lastSeq` por stream, sem alterar o contrato atual nesta fase.
