# ADR-0017 - Multi-Exchange Normalization Invariants

**Status:** Accepted
**Owner:** Governance Doc-First Maintainer
**Last updated:** 2026-02-13
**Date:** 2026-02-12
**Deciders:** Chief Architect
**Relates to:** ADR-0011, ADR-0018, `docs/rfcs/RFC-0010-W9-multi-exchange-readiness.md`, `docs/rfcs/EXECUTION-SEQUENCE.md`

---

## Contexto

Com suporte a multiplas exchanges no mesmo processo, era necessario fixar uma terminologia unica para identidade de instrumento e isolamento por stream.

O principal drift corrigido nesta rodada: diferenciar explicitamente chave canonica interna (`BTCUSDT`) de forma canonica de exibicao (`BTC-USDT`) para evitar contradicao entre ADR-0011, codigo e testes.

## Decisao

1. **Chave canonica interna de runtime**:
- `naming.CanonicalInstrument(raw)` gera `BTCUSDT` (sem separador).
- Esta forma e usada para mapa, dedup e chaves de stream.

2. **Forma canonica de exibicao/API**:
- `ParseCanonicalPair(raw)` materializa `BASE-QUOTE` (ex.: `BTC-USDT`).
- Esta forma e usada em contextos humanos e contratos de leitura.

3. **Identidade de stream**:
- stream id deve incluir `venue + instrument + market_type`.
- `market_type` nao entra na chave do instrumento, mas entra na identidade de stream.

4. **Boundary por adapter**:
- parsing e mapeamento venue-specific ficam em `internal/adapters/exchange/*`.
- `internal/core/*` opera apenas com identidade canonica.

## Consequencias

- Positivas:
- Eliminacao de ambiguidade entre canonical key e canonical display.
- Compatibilidade com os mapas e caches existentes (sem migracao disruptiva de chave).
- Isolamento de falhas e dados por exchange/market type mais previsivel.

- Negativas:
- Exige disciplina documental para nao confundir representacao interna e de exibicao.
- Check automatizado de pureza MEX-4 ainda precisa ser formalizado como gate dedicado.

## Invariantes

- `MEX-1`: canonical instrument deve ser deterministico e case-insensitive.
- `MEX-2`: simbolos equivalentes entre exchanges devem convergir para mesma chave canonica interna.
- `MEX-3`: subsistemas de exchanges diferentes devem executar de forma isolada.
- `MEX-4`: `internal/core/*` nao deve carregar termos/extracoes exchange-specific.
- `MEX-5`: parser de adapter deve normalizar `Venue` em uppercase.

## Implementation Matrix

| Feature | Status | Referencia |
|---|---|---|
| Canonical key sem separador (`BTCUSDT`) | Implemented | `internal/shared/naming/naming.go:22`, `internal/shared/naming/naming_test.go:35` |
| Canonical display `BASE-QUOTE` para identidade semantica | Implemented | `internal/core/marketdata/domain/instrument_identity.go:20`, `internal/core/marketdata/domain/instrument_identity_test.go:9` |
| Stream identity com `market_type` | Implemented | `internal/core/marketdata/domain/instrument_stream.go:30`, `cmd/consumer/main.go:693` |
| Parsing bybit com normalizacao canonica | Implemented | `internal/adapters/exchange/bybit/parser.go:165`, `internal/adapters/exchange/bybit/parser_test.go:1` |
| Multi-exchange e2e no mesmo processo | Implemented | `cmd/consumer/e2e_consumer_integration_test.go:24` |
| Gate dedicado MEX-4 para pureza exchange-specific em core | Planned | `docs/rfcs/RFC-0010-W9-multi-exchange-readiness.md` |

## Evidence

- Canonical naming:
- `internal/shared/naming/naming.go:22`
- `internal/shared/naming/naming_test.go:35`
- `internal/shared/naming/naming_test.go:56`

- Instrument identity and stream partition:
- `internal/core/marketdata/domain/instrument_identity.go:20`
- `internal/core/marketdata/domain/instrument_stream.go:30`

- Adapter normalization and multi-exchange runtime:
- `internal/adapters/exchange/bybit/parser.go:165`
- `cmd/consumer/e2e_consumer_integration_test.go:24`
- `internal/actors/runtime/guardian_test.go:99`

## Changelog

- 2026-02-12:
- ADR aceita para normalizacao multi-exchange.

- 2026-02-13:
- Clarificacao explicita entre chave canonica interna e canonical display.
- Inclusao de `Implementation Matrix` e `Evidence`.
