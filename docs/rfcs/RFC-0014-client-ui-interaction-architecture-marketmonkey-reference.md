# RFC-0014 - Client UI Interaction Architecture (MarketMonkey-Reference, Raccoon-Native)

**Status:** Draft
**Owner:** Client UI/UX + Runtime
**Date:** 2026-02-27
**Last updated:** 2026-02-27
**Relates to:** `docs/rfcs/RFC-0012-client-multi-exchange-evolution.md`, `docs/rfcs/RFC-0013-client-hardening-blueprint-marketmonkey-parity.md`, `docs/client-roadmap-6.8-to-8.0.md`

---

## Objetivo

Definir arquitetura robusta e incremental para evoluir a UI do client com foco em interacao (filtros, botoes, topbar/navbar, sidebar, acoes de stream/timeframe), mantendo paridade funcional entre `native` e `web/wasm` e preservando invariantes de performance/lifecycle do client.

Referencia de produto: comportamento operacional inspirado no MarketMonkey (ergonomia, clareza de estado, velocidade de operacao), sem copiar implementacao.

## Problema Atual

O client atual tem widgets de dados e uma base forte de runtime, mas o modelo de interacao ainda esta no estagio inicial:

- Interacao principal por hotkeys (`Tab`, `1..6`) e scroll local de widgets.
- Ausencia de shell UI estruturada (navbar/sidebar/toolbar/filtros como componentes reutilizaveis).
- Input web ainda incompleto para parity total de mouse/scroll/botoes no core.
- Nao existe camada explicita de `UI actions/reducers` para fluxo previsivel de comandos de interface.

Estado observado no codigo:

- `client/src/core/app/app.odin`: stream registry robusto, top bar observavel, layout responsivo por recortes.
- `client/src/core/ui/primitives.odin`: painel, scroll area e tabela (sem controles interativos reutilizaveis).
- `client/src/platform/native/backend/glfw_backend.odin`: coleta completa de mouse/scroll/teclado.
- `client/src/platform/web/main.odin` + `client/web/odin.js`: loop/render robustos e teclado, mas input de mouse ainda sem pipeline equivalente ao native no core.

## Principios de Arquitetura

### P1. Interaction-first, render-agnostic

A UI deve ser modelada como estado + acoes. Render (RCL -> native/web) e apenas projecao.

### P2. Paridade de semantica entre plataformas

A mesma sequencia de input deve produzir o mesmo estado de UI em native e web.

### P3. Hot path bounded

Sem filas/eventos de UI sem cap; sem alocacao nao-controlada em loop de frame.

### P4. Evolucao incremental sem quebrar widgets atuais

A shell/interacao nova entra por camadas, reaproveitando stores e widgets existentes.

### P5. Observabilidade de interacao

Acoes relevantes (troca de stream, filtros, compare mode, falhas de input) precisam de metricas/logs de diagnostico.

## Arquitetura Alvo

## 1) UI Shell Estruturada

Composicao alvo por regioes:

- Topbar: status de conexao/runtime, stream ativo, timeframe, quick actions.
- Left Sidebar: navegacao de secoes/paineis e presets.
- Main Workspace: grid de paineis (candles, stats, heatmap, vpvr, trades, orderbook).
- Right Inspector/Filter Rail: filtros, configuracoes de painel, compare controls.

Cada regiao deve operar por contratos de estado e acoes, sem acessar diretamente ports/plataforma.

## 2) Camada de Interacao (Action Pipeline)

Fluxo:

1. `Input_State` (snapshot por frame)
2. `Interaction Engine` (hit-test, focus, edge detection)
3. `UI_Action` (intencao semantica)
4. `UI_Reducer` (atualiza `UI_State`)
5. `Effect Dispatcher` (aciona ports: marketdata/settings)
6. `build_ui` renderiza com estado consolidado

Acoes canonicas (exemplos):

- `SelectStream(subject_id)`
- `CycleStream(next|prev)`
- `SetTimeframe(tf)`
- `TogglePanel(panel_id)`
- `SetFilterVenue(venue)`
- `SetFilterSymbol(symbol)`
- `SetCompareMode(enabled)`
- `PinPanel(panel_id)`
- `ResetLayout`

## 3) UI State Model (Core)

Novo estado de shell/interacao (separado do dominio de marketdata):

- `Ui_Shell_State`: layout mode, sidebar state, topbar mode, mobile/desktop flags.
- `Ui_Filter_State`: venue/symbol/channel/timeframe/search + dirty/apply semantics.
- `Ui_Panel_State`: visibilidade, ordem, tamanho relativo, scroll/zoom por painel.
- `Ui_Focus_State`: foco de teclado, hover target, pointer capture.
- `Ui_Command_State`: fila bounded de acoes pendentes e ultimas acoes aplicadas.

Requisito: estado de UI serializavel para persistencia em `settings` (restaurar sessao).

## 4) Primitive Library v2 (RCL)

Criar biblioteca minima de controles reutilizaveis em `core/ui`, todos puros:

- `button`
- `icon_button`
- `toggle`
- `segmented_control` (timeframe, tabs)
- `select` (dropdown simples)
- `chip` (filtro ativo)
- `toolbar`
- `sidebar_section`
- `status_badge`

Todos os controles devem retornar eventos semanticos (ex.: `clicked`, `changed`) e nunca chamar ports diretamente.

## 5) Input Unification (Web + Native)

Evoluir `ports.Input_State` para suportar interacao rica com semantica identica:

- Mouse: `pos`, `delta`, `scroll`, `buttons_down`, `buttons_pressed`, `buttons_released`.
- Keyboard: `pressed`, `just_pressed`, `just_released`, modificadores.
- Frame timing: `delta_time` consistente.

Native:

- Continuar usando backend (`glfw`/`sdl2`) como origem unica de input.

Web:

- Expandir bridge JS (`odin.js`) para mouse/buttons/scroll + edges por frame.
- Expor foreign procs dedicadas para leitura pelo core.
- Garantir reset de edges apos `step()` (igual ao comportamento native).

## 6) Panel Orchestration

Introduzir orquestrador de paineis para evitar layout hardcoded no `build_ui`:

- Registro de paineis com metadados (`id`, prioridade, min/max size, mobile policy).
- Estrategia de layout por breakpoints (desktop/tablet/mobile).
- Politica de degrade sob viewport pequena (ocultar secundarios antes de essenciais).

## 7) Compare Mode (Multi-Exchange UX)

Suportar comparacao sem duplicar stores:

- Painel referencia `stream_id` + `widget_type`.
- Mesmo store por stream pode alimentar multiplos paineis.
- Budgets por painel para evitar starvation no painel ativo.

## Invariantes de Qualidade

- INV-UI-1: Nenhuma acao de UI altera stores de dominio sem passar por reducer/effect.
- INV-UI-2: Nenhuma fila de interacao sem capacidade maxima e contadores de drop.
- INV-UI-3: Nenhum componente interativo depende de API de plataforma.
- INV-UI-4: Input web/native com semantica equivalente para hover/click/scroll/keys.
- INV-UI-5: Estado de shell/restaure de sessao deve ser deterministico e idempotente.

## Plano de Execucao (Milestones)

## U0 - Baseline e Contratos

Entregaveis:

- Definir `UI_Action`, `UI_State` e reducer minimo.
- Mapear comandos atuais (`Tab`, `1..6`) para actions.
- Congelar cenarios baseline de UX para regressao.

Aceite:

- Hotkeys atuais continuam funcionando via action pipeline.
- Sem regressao de build native/wasm.

## U1 - Input Parity Web/Native

Entregaveis:

- `Input_State` v2 com edges/modifiers/delta.
- Bridge web com mouse/buttons/scroll.
- Adaptadores native/web unificados em semantica.

Aceite:

- Scroll/zoom/hover identicos em widget candle/trades/orderbook nas duas plataformas.
- Sem crescimento nao-bounded em buffers de input no web.

## U2 - Primitive Library v2

Entregaveis:

- Componentes interativos base (`button`, `toggle`, `segmented_control`, `select`, `chip`).
- Hit-testing/focus acessivel por mouse+teclado.

Aceite:

- Timeframe selector migra de hotkey-only para segmented control + teclado.
- Stream selector inicial com botoes next/prev + `Tab` como atalho.

## U3 - Shell UI (Topbar + Sidebar + Filter Rail)

Entregaveis:

- Topbar operacional (status + stream + timeframe + quick actions).
- Sidebar com navegacao de paineis/presets.
- Filter rail para venue/symbol/channel.

Aceite:

- Usuario troca contexto (stream/timeframe/filtros) sem usar apenas teclado.
- Persistencia de shell/filter state em settings.

## U4 - Panel Orchestrator

Entregaveis:

- Registro de paineis e layout por breakpoints.
- Reflow robusto desktop/mobile.

Aceite:

- Remocao de hardcoded principal de layout do `build_ui` atual.
- Mobile com degradacao previsivel (sem overlap/quebra visual).

## U5 - Compare Mode Foundations

Entregaveis:

- Multipainel por stream/widget.
- Seletor de comparacao cross-exchange.

Aceite:

- 2-4 paineis simultaneos sem starvation.
- Stream switching sem mistura de dados.

## U6 - Hardening + Evidence Pack

Entregaveis:

- Scripts/cenarios de regressao visual e funcional.
- Gate de robustez UI para native/web.

Aceite:

- Soak de interacao (troca de stream/filtro/timeframe) sem leak/regressao.
- Evidence pack com metricas de input/action/render.

## Test Plan

Build e gates de client:

```bash
make -C client check-core
make -C client check-core-imports
make -C client build-native
make -C client build-wasm
```

Validacao web (WASM):

- Ambiente: `http://127.0.0.1:8090/` (com stack local ativa).
- Usar Playwright para cenarios de interacao:
  - `stream switch`
  - `timeframe switch`
  - `scroll trades`
  - `chart zoom`
  - `filter apply/reset`

Validacao native:

```bash
make -C client run-native-compose
```

Executar smoke manual/automatizado com roteiros equivalentes aos cenarios web.

## Risks e Mitigacoes

| Risk | Impact | Mitigacao |
|---|---|---|
| Divergencia de semantica de input entre web/native | Alto | U1 com contrato unico + cenarios espelhados |
| Crescimento de complexidade no `build_ui` | Medio/Alto | U4 com panel orchestrator e registry |
| Acoes UI acopladas ao runtime de marketdata | Alto | action/reducer/effect boundary (INV-UI-1) |
| Regressao de performance ao adicionar shell/components | Medio | budgets por frame + profiling continuo |
| Compare mode duplicar memoria | Alto | referencia por stream + stores compartilhados |

## Acceptance

- Arquitetura de interacao e shell definida com milestones U0-U6.
- Contrato de input parity e biblioteca de componentes definidos.
- Plano de validacao web/native e de hardening operacional explicito.
- RFC alinhado aos RFCs 0012/0013 (runtime + hardening) sem conflito de fronteira.

## Changelog

- 2026-02-27:
  - RFC criada para consolidar arquitetura de UI/interacao robusta inspirada em comportamento do MarketMonkey.
  - Definidos milestones U0-U6 com foco em parity web/native e hardening de UX operacional.
