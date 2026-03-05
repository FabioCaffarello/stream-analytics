# Odin Client Playwright Audit (2026-03-05)

## Context
- App URL: `http://localhost:8090`
- Browser: Playwright MCP (Chrome)
- Cache policy used in this audit:
  - `Network.clearBrowserCache`
  - `Network.clearBrowserCookies`
  - `Network.setCacheDisabled({ cacheDisabled: true })`
  - `localStorage.clear()` and `sessionStorage.clear()` before cold-start checks

## Visual Evidence
- Cold baseline (storage/cache limpos):
  - `.context/evidence/screenshots/market-raccoon-12-cold-baseline.png`
- Online baseline (auto-connect reativado):
  - `.context/evidence/screenshots/market-raccoon-13-online-baseline.png`
- Após 3 cliques no topo (timeframe):
  - `.context/evidence/screenshots/market-raccoon-14-after-3-clicks.png`

## Key Measurements (runtime probes)

### Cold baseline
- `interactiveCount` no DOM: `0` (UI é canvas-only)
- Estado visual: `OFFLINE`, `sub not acked`

### Online baseline (após `mr.settings.auto_connect=1`)
- `probe_stream_count`: `1`
- `probe_active_tf_index`: `2` (`1m`)
- `probe_md_subscribe_ack_count`: `6`
- `probe_active_live_candle`: `1`

### Troca de timeframe por teclado (`key: 2`, 1m -> 5s)
- Antes: `tf=2`, `sw=0`, `sc=1`, `acks=6`, `uaq=0`
- Depois: `tf=1`, `sw=1`, `sc=1`, `acks=16`, `uaq=1`
- Observação: um único switch de TF gerou +10 acks e churn de subs/unsubs no lifecycle.

### 3 cliques no topo (`x=440,470,500; y=14`)
- Estado inicial: `tf=1`, `sw=1`, `sc=1`, `acks=16`, `uaq=1`
- Após clique `x=440`: `tf=5`, `sw=3`, `sc=1`, `acks=16`, `uaq=3`
- Após clique `x=470`: `tf=6`, `sw=5`, `sc=3`, `acks=32`, `uaq=5`
- Após clique `x=500`: `tf=7`, `sw=7`, `sc=3`, `acks=64`, `uaq=7`
- Resultado: crescimento de streams `1 -> 3` sem troca explícita de mercado, e +48 acks em poucos cliques.

### Saturação de tape
- `tapeParse=1647`
- `tapeDrop=1391`
- Drop rate aproximado: `84.46%`

## Findings

1. Interação de timeframe está instável por clique (um clique podendo implicar múltiplos switches).
2. Há churn excessivo de subscribe/unsubscribe/ack por troca de timeframe.
3. Há acúmulo de streams para o mesmo mercado após interações de TF (`1/1 -> 1/3`, duplicados em lista).
4. UI totalmente canvas reduz testabilidade, acessibilidade e observabilidade de interação (sem DOM interativo).
5. Pipeline de tape demonstra perda significativa sob carga (`~84%` drops), exigindo revisão de capacidade/prioridade.

## Code Hotspots Related to Findings
- `client/src/core/app/top_bar.odin` (layout dinâmico + hit targets + ações de TF)
- `client/src/core/app/actions.odin` (fila de UI actions e aplicação)
- `client/src/core/app/reconcile.odin` (diff de subscrição e churn)
- `client/src/core/layers/data_source.odin` (fallback para `channel_sid` quando market-id resolve falha)
- `client/src/core/app/layer_marketdata.odin` (alocação de slots por `subject_id`)
- `client/src/core/app/stream_slots.odin` (invariantes e reparo de slots)

## Immediate Risk
- Com poucos cliques de TF, cresce o custo de reconciliação/subscrição e o risco de estado inconsistente de stream ativa, degradando previsibilidade da navegação e do diagnóstico operacional.
