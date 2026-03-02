---
status: pending
generated: 2026-02-27
updated: 2026-02-27
owner: "codex"
objective: "Executar de forma robusta a evolucao do client Market Raccoon para operacao multi-exchange com gates de performance, leak safety e validacao E2E via Playwright."
scope:
  include:
    - "Execucao local com compose escalado: PROCESSOR_REPLICAS=2"
    - "Validacao client native conectado ao backend real"
    - "Validacao client web com browser automation (Playwright MCP)"
    - "Baseline e soak de memoria/threads/reconnect/drop/backpressure"
    - "Fault injection minima: restart de server durante sessao ativa"
    - "Evidence pack por rodada em .context/evidence/"
  exclude:
    - "Rework visual amplo de UI fora de observabilidade operacional"
    - "Copiar implementacao interna do MarketMonkey"
    - "Mudancas de produto sem impacto em robustez/performance"
references:
  - "docs/rfcs/RFC-0012-client-multi-exchange-evolution.md"
  - "docs/rfcs/RFC-0013-client-hardening-blueprint-marketmonkey-parity.md"
  - "docs/perf/performance-budgets.md"
  - "client/src/platform/native/marketdata_native.odin"
  - "client/src/platform/web/marketdata_web.odin"
  - "client/scripts/soak-native.sh"
---

# Client Multi-Exchange Hardening Execution

## Goal
Executar o plano multi-exchange de forma operacional, com validacao completa em runtime real (`make up PROCESSOR_REPLICAS=2`), client native + web, e gate E2E com Playwright MCP para garantir funcionamento e detectar regressoes de lifecycle/performance.

## Milestones

| Milestone | Objetivo | Dependencias | Exit Criteria | Evidencia |
|---|---|---|---|---|
| M0 — Baseline Runtime | Capturar baseline real de stack + client | — | Stack healthy + client native/web conectam e recebem dados | `.context/evidence/client-multi-exchange-m0-baseline-2026-02-27.md` |
| M1 — Runtime Hardening | Fechar invariantes de reconnect/teardown/ownership/queues | M0 | FI-01 e FI-04 sem leak/estado preso | `.context/evidence/client-multi-exchange-m1-hardening-2026-02-27.md` |
| M2 — Stream Metadata Consolidation | Consolidar metadata canonicamente no core e evitar mistura de streams | M1 | multiplos streams sem contaminacao + restore consistente | `.context/evidence/client-multi-exchange-m2-stream-metadata-2026-02-27.md` |
| M3 — Backpressure and Hot Path | Completar policy bounded por tipo e budget de frame/poll | M2 | sem caminho sem cap + counters observaveis + sem regressao critica | `.context/evidence/client-multi-exchange-m3-backpressure-hotpath-2026-02-27.md` |
| M4 — Playwright E2E Gate | Automatizar validacao web de conectividade/reconnect/stream switch | M0 | suite Playwright MCP verde com cenarios minimos | `.context/evidence/client-multi-exchange-m4-playwright-gate-2026-02-27.md` |
| M5 — Soak + Merge Gate | Validacao final com soak/fault + CI | M1,M2,M3,M4 | soak pass + make ci pass + evidence consolidada | `.context/evidence/client-multi-exchange-m5-final-gate-2026-02-27.md` |

## PREVC Execution

### P — Plan
- Confirmar escopo operacional com foco em robustez e zero tolerance a memory leak.
- Definir comandos e cenarios obrigatorios por milestone.
- Registrar gates objetivos de aceite e artefatos de evidencias.

### R — Review
- Revisar invariantes do RFC-0013 (R1..R5) nos pontos de runtime web/native.
- Revisar aderencia do contrato de stream/subject no roteamento do core.
- Revisar cobertura de cenarios de fault injection e telemetria.

### E — Execute
- `make up PROCESSOR_REPLICAS=2`
- `make -C client run-native-compose`
- `make -C client serve SERVE_PORT=8090`
- Executar fault injection e soak inicial (`client/scripts/soak-native.sh`).
- Executar validacao web com Playwright MCP.

### V — Validate
- Stack: `make smoke`
- Build client: `make -C client build-native && make -C client build-wasm`
- Core safety: `make -C client check-core && make -C client check-core-imports`
- Gates repo: `make ci`
- Gates runtime: reconnect/fault/soak/playwright com evidencias.

### C — Complete
- Consolidar evidence pack final de M0..M5.
- Atualizar status do plano para `completed` apos todos os gates verdes.

## Acceptance Criteria
1. Stack local com 2 replicas de processor sobe e permanece healthy por toda a rodada (`make up PROCESSOR_REPLICAS=2`, `make smoke`).
2. Client native conectado ao compose executa ciclo de reconnect/fault sem leak visivel de RSS/threads alem dos thresholds acordados.
3. Client web passa validacao automatizada Playwright MCP para fluxo basico + reconnect.
4. Nenhuma fila/registry critica no client fica sem cap/policy/counter observavel.
5. Rodada final fecha com `make ci` e evidence pack completo.

## Risks
| Risk | Impact | Mitigation |
|---|---|---|
| Regressao intermitente em reconnect | Alto | fault injection repetivel + counters de reconnect/drop |
| Leak sutil em long-run | Alto | soak com amostragem de RSS/threads e threshold fail-fast |
| Drift web vs native | Alto | suite comum de cenarios e gate Playwright para web |
| Flakiness de ambiente local | Medio | baseline primeiro, comandos deterministas e evidencia de health |

## Commands Matrix

```bash
make up PROCESSOR_REPLICAS=2
make smoke
make -C client run-native-compose
make -C client serve SERVE_PORT=8090
client/scripts/soak-native.sh --duration-sec 900 --sample-sec 2 --log-ms 1000 -- --ws-url=ws://127.0.0.1:8080/ws --api-key=prod_key_1
```

## Execution Log
- 2026-02-27: `M0` concluido com stack escalada (`PROCESSOR_REPLICAS=2`), validacao web via Playwright MCP (incluindo reconnect/resubscribe apos restart), soak native de 120s com fault injection (2 restarts) e resultado `PASS`.
- Evidence: `.context/evidence/client-multi-exchange-m0-baseline-2026-02-27.md`
- 2026-02-27: `M1` hardening slice 1 aplicada (race safety em shutdown/reconnect native + drop telemetry end-to-end no web bridge) com build/check/soak `PASS` e validacao Playwright de reconnect/resubscribe.
- Evidence: `.context/evidence/client-multi-exchange-m1-hardening-2026-02-27.md`
- 2026-02-27: `M2` slice 1 aplicada com consolidacao de metadata canônica por stream no core (`Stream_View_Slot`), removendo lookup ad-hoc em pontos críticos de persistencia/UI/getrange.
- Evidence: `.context/evidence/client-multi-exchange-m2-stream-metadata-2026-02-27.md`
- 2026-02-27: `M2` slice 2 aplicada para funcionamento de todas as widgets com dados reais (layout inclui heatmap/vpvr + subscriptions web/native para canais de insights), validado em native e Playwright web.
- Evidence: `.context/evidence/client-multi-exchange-m2-all-widgets-functional-2026-02-27.md`
- 2026-02-27: `M4` slice 1 aplicada com gate recorrente `check-widgets` (offline deterministic soak + parsing de cobertura `w[...]`) para garantir regressao-zero em todas as widgets.
- Evidence: `.context/evidence/client-multi-exchange-m4-widget-gate-2026-02-27.md`
- 2026-02-27: `M3` slice 1 aplicada com runtime WS switch no web client (sem reload), reconexao robusta com override de endpoint/API key, painel operacional `Apply Live` e validacao Playwright + gate online.
- Evidence: `.context/evidence/client-multi-exchange-m3-backpressure-hotpath-2026-02-27.md`
