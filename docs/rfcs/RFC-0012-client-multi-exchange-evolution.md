# RFC-0012 - Client Multi-Exchange Evolution (MarketMonkey-Inspired, Raccoon-Native)

**Status:** Draft
**Owner:** Client/UX Architect
**Date:** 2026-02-26
**Last updated:** 2026-02-26
**Relates to:** `docs/rfcs/RFC-0010-W9-multi-exchange-readiness.md`, `docs/rfcs/RFC-0011-product-parity-marketmonkey.md`, `docs/adrs/ADR-0012-lifecycle-invariants-leak-prevention.md`, `docs/perf/performance-budgets.md`

---

## Objetivo

Evoluir o client do Market Raccoon para operar com multiplas exchanges e multiplos streams de forma segura, performatica e sem vazamentos de memoria/thread, usando o MarketMonkey como referencia de produto e ergonomia, sem copiar implementacao.

## Principios (Nao-Negociaveis)

- Performance maxima no hot path (`poll -> route -> render`).
- Zero vazamento de lifecycle (threads, sockets, subscriptions, buffers).
- Estado por stream sempre bounded (capacidade e/ou eviction).
- Backpressure explicito e observavel.
- Compatibilidade incremental: UI atual continua funcionando durante a migracao.

## Contexto Atual (Client)

Estado atual observado no codigo:

- O `Marketdata_Port` recebe `venue/symbol/channel`, mas `MD_Event` ainda nao carrega identidade de stream (evento chega ao core sem metadados de origem).
- O `App_State` usa stores unicos (`trades/orderbook/stats/heatmap/vpvr/candle`), o que impede multi-stream real sem mistura de dados.
- Entrypoints `native` e `web` assumem explicitamente single-symbol para evitar mistura e limitar carga.
- `native` e `web` usam singleton de estado (`g_md_state` / `g_web_state`), o que dificulta composicao futura e testes de multi-instancia.

### Gap Critico de Lifecycle (P0)

O client nativo cria thread de leitura de WS e mantinha sinal de stop interno sem API publica de encerramento no port. Isso criava risco de thread/socket ficarem vivos apos teardown do app.

**Mitigacao inicial nesta rodada (P0.1, parcial):**
- `Marketdata_Port` ganhou callback `shutdown`.
- `app.shutdown()` passou a encerrar o port de marketdata antes de destruir UI.
- `native` ganhou `native_shutdown()` com sinalizacao de stop + close socket + `thread.join/destroy`.
- `native/web` passaram a deduplicar subscriptions por `subject` (evita inflar re-subscribe state).

## Escopo

- Contrato do client marketdata (`ports`) para suportar roteamento por stream.
- Runtime marketdata `native` e `web` com lifecycle hardening.
- Core app state/store routing para multiplos streams.
- UI incremental para selecao de stream e comparacao cross-exchange.
- Soak/perf gates especificos do client.

## Nao-Escopo

- Mudancas no backend de normalizacao multi-exchange (ja tratado por RFC-0010 e ADR-0017).
- Copiar implementacao/stack do MarketMonkey.
- Rework visual completo da UI antes do particionamento de dados.

## Arquitetura Alvo (Resumo)

### 1. `StreamKey` canonico no client

Identidade minima:
- `venue`
- `instrument`
- `market_type`
- `channel`
- `timeframe` (quando aplicavel)

Todo evento entregue ao core deve carregar essa identidade.

### 2. Transporte separado de roteamento

- **Transporte WS:** parse, reconnect, backpressure local, entrega eventos normalizados.
- **Roteador de streams (core/app):** resolve `StreamKey`, aplica em stores particionados.

### 3. Stores particionados por stream (bounded)

Trocar stores globais por registry bounded:
- `Trades_Store` por stream (ring fixo)
- `Orderbook_Store` por stream (latest snapshot bounded)
- `Stats/Heatmap/VPVR/Candle` por stream (caps por tipo)

Politica:
- LRU + TTL para streams inativos
- limites configuraveis por categoria

### 4. UI incremental

- Fase 1: selecao de stream ativo (1 painel)
- Fase 2: multiplos paineis/comparacao cross-venue
- Reuso de stores por referencia (sem duplicar memoria por painel)

## Invariantes do Client (Leak/Perf)

### INV-C1: Nenhum recurso sem teardown

Toda conexao/socket/thread/timer/subscription deve ter caminho explicito de encerramento via `Marketdata_Port.shutdown`.

### INV-C2: Nenhum estado por stream sem bound

Todo registry e store por stream deve ter:
- capacidade maxima; e
- politica de eviction (LRU/TTL) quando aplicavel

### INV-C3: Backpressure por tipo de stream

Trade, orderbook, heatmap, vpvr, stats e candle devem ter politicas independentes (ring, latest-wins, drop counters).

### INV-C4: Hot path com alocacao controlada

Sem alocacao persistente em `poll()`/`drain_marketdata()`/render loop; uso de temporarios deve ser limpo por frame.

### INV-C5: Shutdown deterministico

Teardown do client deve:
1. bloquear novo trabalho
2. encerrar conexoes
3. aguardar threads
4. limpar refs globais/singletons

## Plano de Evolucao (P0-P4)

## P0 - Lifecycle Hardening + Port Contract Basico

### Entregaveis
- `Marketdata_Port.shutdown`
- teardown nativo seguro (`close + join + destroy`)
- teardown web seguro (`ws_close + clear state`)
- dedup de subscriptions por `subject`
- hooks/contadores minimos de reconnect/drop/backlog

### Aceite
- ciclos repetidos de connect/disconnect sem crescimento de thread
- reinicializacao do port nao acumula subscriptions duplicadas
- build `native` e `wasm` verdes

### Status desta rodada
- **Parcialmente implementado** (shutdown do port + join/destroy + dedup de subs)

## P1 - Metadata de Stream + Registry de Stores

### Entregaveis
- `MD_Event` com metadados de origem (ou `StreamKey`)
- roteamento de eventos por stream no core
- registry bounded de stores por stream
- health por exchange/stream (nao apenas global)

### Aceite
- multiplos streams simultaneos sem mistura de dados
- cardinalidade bounded (`active_streams <= limite`)
- eviccao e reativacao testadas

## P2 - UI Single-View Multi-Exchange

### Entregaveis
- seletor de `venue/instrument/market_type/timeframe`
- persistencia em settings
- top bar com source ativo + health agregado

### Aceite
- troca de stream em runtime sem realloc explosivo
- frame pacing preservado

## P3 - Compare Mode (Multi-Painel)

### Entregaveis
- paineis comparativos (ex.: mesmo instrumento em exchanges diferentes)
- budgets por painel (render/parse)
- degradacao progressiva sob burst

### Aceite
- 2-4 paineis sem starvation do painel ativo
- drops priorizados por tipo (snapshot antes de trades/candles ativos)

## P4 - Soak, Perf Tuning e Rollout

### Entregaveis
- soak tests do client (native + web)
- overlay/telemetria de parse-poll-render
- tuning de caps por perfil

### Aceite
- memoria/thread estaveis em soak longo
- reconnects repetidos sem leak
- backlog bounded em bursts

## Performance & Memory Gates (Client)

Metas iniciais (ajustaveis apos baseline):

- `poll()` + roteamento: sem regressao perceptivel no frame loop local
- `active_streams`: bounded por config (ex.: 64/128 no client)
- `trade_ring` por stream: cap fixo
- `latest-wins` por snapshot streams (orderbook/stats/heatmap/vpvr/candle)
- reconnect state: sem crescimento de subscriptions apos reconnects repetidos

## Test Plan

### Build/Smoke (obrigatorio por mudanca de client)

```bash
make -C client build-native
make -C client build-wasm
```

### Core safety gates (quando mexer em `client/src/core`)

```bash
make -C client check-core
make -C client check-core-imports
```

### Futuro (P0/P4)

- teste de ciclo `connect/disconnect/reconnect` automatizado (native)
- soak de 30-60 min com amostragem de memoria/thread
- teste de cardinalidade e eviction para registry de streams

## Acceptance

- Plano P0-P4 publicado com entregaveis, gates e criterios de aceite por fase.
- Invariantes de lifecycle/memoria/performance do client definidos e testaveis.
- P0.1 inicial entregue: `Marketdata_Port.shutdown` integrado ao teardown do app nativo e dedup basico de subscriptions.
- `client` compila em `native` e `wasm` apos evolucao do contrato do port.

## Riscos e Mitigacoes

| Risco | Impacto | Mitigacao |
|---|---|---|
| Mistura de dados entre exchanges por stores globais | Alto | P1: `StreamKey` + routing + stores particionados |
| Leak de thread/socket em teardown/recreate | Alto | P0: `shutdown` explicito + `join/destroy` + close socket |
| Duplicacao de subscriptions em reconnect | Medio/Alto | P0: dedup por `subject`; P1: registry com ref-count |
| Regressao de frame pacing com multi-stream | Alto | budgets por frame + latest-wins + degrade policy |
| Drift entre `native` e `web` | Medio | contrato de port unico + rollout paralelo + testes equivalentes |

## Changelog

- 2026-02-26:
  - RFC criada com plano P0-P4 para evolucao do client multi-exchange.
  - Registrado hardening inicial de lifecycle (`Marketdata_Port.shutdown`) como P0.1 parcial.
