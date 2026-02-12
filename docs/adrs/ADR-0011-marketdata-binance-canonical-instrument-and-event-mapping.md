# ADR-0011 — MarketData Binance: Canonical Instrument and Event Mapping

**Status:** Accepted
**Date:** 2026-02-12

## Context

W3 introduz Binance como primeira source real. Era necessário fixar:
- formato canônico de instrumento;
- mapeamento estável de eventos Binance para event types internos;
- local da lógica exchange-specific.

## Decision

1. **Canonical instrument v1:** `BTCUSDT` (sem separador), alinhado a `naming.CanonicalInstrument`.
2. **Venue canônica:** `BINANCE`.
3. **Event mapping v1:**
   - Binance `aggTrade` -> `marketdata.trade` v1 (`TradeTickV1`)
   - Binance `depthUpdate` -> `marketdata.bookdelta` v1 (`BookDeltaV1`)
4. **Boundary:** parsing/binance-specific em `internal/adapters/exchange/binance`; `core/marketdata` permanece agnóstico de exchange.

## Consequences

- Contratos internos ficam estáveis para novas fontes.
- Mesma convenção de instrumento em todo pipeline facilita sequencer/dedup.
- Adapters podem evoluir independentemente do domínio.

## Alternatives Considered

- `BTC-USDT` como canônico: rejeitado por divergir da canonicalização já usada em domínio.
- parsing dentro de actors/runtime: rejeitado por violar separação de responsabilidades.
