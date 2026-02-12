# RFC-0004 — W3 Sources/MarketData v1 (Binance)

**Status**: Implemented (W3)
**Date**: 2026-02-12
**Relates to**: RFC-0001, ADR-0004, ADR-0011

## 1. Visão geral

W3 adiciona a primeira source real de market data via Binance WebSocket, mantendo as fronteiras:
- `internal/adapters/exchange/binance`: endpoint builder e parser exchange-specific.
- `internal/core/marketdata`: permanece agnóstico de exchange.
- `cmd/consumer`: apenas wiring/config de modo fake vs binance real.

## 2. Contratos de parser (v1)

Entrada do parser:
- `data []byte` (payload WS raw ou envelope combinado `{stream,data}`).
- `recvAt time.Time` (timestamp local de recebimento).

Saída do parser:
- `app.IngestRequest` quando mensagem suportada.
- `skip=true` para mensagens não suportadas/controle.
- `problem` para payload inválido (caller faz log + skip, sem derrubar subsystem).

Mapeamento de eventos:
- Binance `aggTrade` -> `EventType=marketdata.trade`, `Version=1`, payload `domain.TradeTickV1`.
- Binance `depthUpdate` -> `EventType=marketdata.bookdelta`, `Version=1`, payload `domain.BookDeltaV1`.

## 3. Estratégia de idempotência/dedup

No W3, parser não cria idempotency key diretamente.
A idempotência continua no `core/marketdata/domain.InstrumentStream`, onde:
- `IdempotencyKey` é determinística com hash de `(venue, instrument, event_type, seq)`.
- dedup é feita por janela (`DedupWindow`) após sequenciamento.

Implicação:
- fonte pode reenviar payload igual; dedup efetiva depende de sequência + stream state.

## 4. Semântica de ordering

W3 mantém sequencer atual (in-memory) no consumer:
- monotonicidade por `(venue,instrument)` via `ports.Sequencer`.
- timestamps de exchange (`TsExchange`) são advisory.
- ordenação efetiva do envelope continua por `Seq`.

## 5. Endpoint / Source strategy (v1)

Endpoint Binance combinado:
- `wss://stream.binance.com:9443/stream?streams=<symbol>@aggTrade/<symbol>@depth@100ms`

Planejamento ws.Manager:
- `streams_per_ticker=2`
- bucketização e rotatividade continuam em `actors/marketdata/ws.Manager`.

## 6. Limites W3

- Sem JetStream/NATS real obrigatório em runtime.
- Sem backfill/replay histórico de source.
- Sem auth/autz de source.
- Parse errors só geram log+skip (sem dead-letter ainda).

## 7. Migração para W4 (JetStream)

W4 troca somente integração de transporte/pub-sub:
1. Substituir publisher/log por adapter JetStream.
2. Manter parser Binance e contratos `IngestRequest` estáveis.
3. Evoluir idempotency para usar `Msg-ID` em publish e dedup window de broker.
