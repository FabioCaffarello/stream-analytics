# RFC-0013 - Client Hardening Blueprint (MarketMonkey Parity-Informed, Raccoon-Native)

**Status:** Draft  
**Owner:** Client Runtime / Platform  
**Date:** 2026-02-26  
**Last updated:** 2026-02-26  
**Relates to:** `docs/rfcs/RFC-0011-product-parity-marketmonkey.md`, `docs/rfcs/RFC-0012-client-multi-exchange-evolution.md`, `docs/adrs/ADR-0012-lifecycle-invariants-leak-prevention.md`

---

## Objetivo

Definir um blueprint operacional para evoluir o client do Market Raccoon para multi-exchange com foco em robustez, performance e ausencia de leaks, usando o MarketMonkey como referencia de comportamento/arquitetura operacional (nao de implementacao), e convertendo bugs encontrados em melhorias estruturais e testaveis.

## Contexto (Por que este RFC existe)

O `RFC-0012` ja define a evolucao funcional/arquitetural do client multi-exchange (P0-P4). Este RFC complementa aquele plano com:

- estrategia de hardening guiada por bugs reais;
- matriz de paridade operacional inspirada no MarketMonkey;
- matriz de fault injection (web/native/backend);
- gates de memoria/perf/reconnect;
- trilhas de execucao para evitar patchs locais sem consolidacao estrutural.

## Escopo

- Hardening estrutural do client `native` e `web`.
- Regras de engenharia para corrigir bugs de forma reutilizavel.
- Validacao com soak + fault injection.
- Criterios para co-evoluir backend quando o contrato/entrega limitar robustez do client.
- Milestones de execucao e gates mensuraveis.

## Nao-Escopo

- Copiar codigo, stack ou implementacao interna do MarketMonkey.
- Rework visual completo de UI fora do necessario para observabilidade/operacao.
- Mudancas de produto nao relacionadas a robustez ou multi-exchange.

## Principios Estrategicos

### P1. Paridade conceitual, nao de codigo

Usar o MarketMonkey como referencia para:
- lifecycle de conexao/reconexao;
- multiplexacao por stream/exchange;
- backpressure;
- observabilidade de runtime;
- separacao de concerns (transporte, roteamento, stores, render).

Nao usar como fonte de copia de implementacao.

### P2. Todo bug relevante deve virar melhoria estrutural

Cada bug corrigido no client deve resultar em pelo menos um item abaixo:

1. invariante explicita;
2. protecao estrutural reutilizavel;
3. metrica para detectar regressao;
4. teste de reproducao (soak/fault injection/unit).

Se uma correcao nao gera pelo menos um desses artefatos, o risco de regressao permanece alto.

### P3. Nenhum path critico sem budget e sem limite

Para runtime de marketdata:
- nenhuma fila sem cap;
- nenhuma fase de IO sem timeout/cancelamento;
- nenhum store por stream sem bound/eviction;
- nenhuma reconexao sem backoff observavel.

### P4. Robustez do sistema > pureza de fronteira

Se um problema do client for causado por contrato/semantica fraca no backend (ACK, replay, backlog, sinalizacao de erro), o backend deve evoluir junto.

## Mapa de Paridade Operacional (MarketMonkey -> Market Raccoon)

Tabela para guiar o roadmap. "Paridade" aqui significa comportamento e invariantes.

| Dominio | Referencia conceitual (MarketMonkey) | Estado alvo no Raccoon client | Gap atual (2026-02-26) | Prioridade |
|---|---|---|---|---|
| Conectividade | Reconnect resiliente + re-subscribe seguro | `web/native` convergentes em semantica + backoff + timeout | `native` corrigido em handshake/reconnect; `web` ainda depende de bridge JS simples | P0 |
| Lifecycle | Teardown deterministico | `shutdown` sempre fecha recursos/timers/threads | `native`/`web` com `shutdown`; precisa expandir testes de ciclo | P0 |
| Multiplexacao | Streams independentes por venue/channel | routing por stream + stores particionados | avancado (registry + routing); consolidar metadata e cobertura | P1 |
| Backpressure | filas bounded + drop policy por tipo | caps + latest-wins + counters em todo caminho | parte pronta; bridge JS tinha fila unbounded (corrigido) | P0/P1 |
| Observabilidade | health + runtime metrics | top bar + overlay + soak logs + thresholds | top bar/soak existem; falta thresholds automaticos e overlay detalhado | P1/P4 |
| Fault tolerance | validacao com falhas reais | suite de fault injection repetivel | manual (restart server); falta automacao e budget gates | P2/P4 |
| Compare mode | multi-painel sem duplicar stores | compare por referencia + budgets | ainda nao implementado | P3 |

## Bugs Reais (Seeds) -> Correcoes Estruturais

Incidentes reais ja observados nesta rodada e a regra estrutural derivada:

| Incidente | Root cause | Correcao aplicada | Regra estrutural permanente |
|---|---|---|---|
| `native` preso em `Connecting` apos restart do server | handshake WS sem timeout/EOF robusto | timeout de handshake + EOF tratado + retry/backoff | toda fase de IO de reconnect deve ser bounded por timeout/cancelamento |
| `web` queue JS potencialmente infinita | `wsMsgQueue` sem cap no bridge JS | cap de fila + drop counter (`wsMsgDropCount`) | toda queue entre loops (`JS <-> WASM`, thread <-> main) deve ter cap + policy |
| `web` handlers antigos alterando estado global | callbacks async de socket antigo sem ownership | guard por `wsEpoch` + detach de handlers | callbacks async devem validar ownership/token antes de mutar estado global |

## Invariantes de Runtime (Obrigatorias)

Estas invariantes complementam o `RFC-0012` e devem ser usadas como checklist de review.

### INV-R1: Ownership de callbacks async

Todo callback de recurso substituivel (ex.: socket) deve validar que ainda pertence ao recurso ativo antes de atualizar estado global.

### INV-R2: Queue bounded + policy explicita

Toda fila de mensagens/eventos precisa de:
- capacidade maxima;
- politica de overflow (`drop-oldest`, `drop-newest`, `latest-wins`);
- contador de drops.

### INV-R3: Reconnect bounded

Toda tentativa de reconnect deve ter:
- timeout de handshake/IO;
- backoff exponencial bounded;
- contador de tentativas;
- logs/metricas observaveis.

### INV-R4: Stream cardinality bounded

Registros/stores por stream devem respeitar `CAP` configuravel e eviction observavel (`eviction_count`).

### INV-R5: Teardown deterministico

Teardown deve ser repetivel e idempotente: duas chamadas de shutdown nao podem vazar/duplicar cleanup nem causar crash.

## Arquitetura de Execucao (Trilhas Paralelas)

Para evitar que bugs virem patchs isolados, a execucao deve ocorrer em 3 trilhas com sincronizacao por gate.

### Trilha A - Hardening Runtime

Foco:
- lifecycle;
- reconnect;
- ownership async;
- backpressure/caps;
- memory safety.

Outputs:
- fixes estruturais;
- invariantes;
- metricas;
- soak/fault scripts.

### Trilha B - Multi-Exchange Functional

Foco:
- stream identity;
- routing;
- stores particionados;
- UI stream-aware/comparacao.

Outputs:
- features multi-exchange;
- metadata de stream;
- compare mode (fases posteriores).

### Trilha C - Validation & Evidence

Foco:
- soak nativo/web;
- fault injection;
- thresholds e regressao;
- correlacao com logs backend.

Outputs:
- evidence packs;
- tabelas de baseline;
- gates (pass/fail).

## Matriz de Fault Injection (Minimo Obrigatorio)

Cada cenario deve ser reproduzivel localmente via `compose`.

| ID | Cenario | Alvo | Metodo | Observacao esperada | Gate |
|---|---|---|---|---|---|
| FI-01 | restart do `server` | `web`, `native` | `docker compose restart server` | reconnect + resubscribe sem leak/estado preso | `rc` incrementa, retorna a `Connected`, sem `ev/fix` anomalo |
| FI-02 | indisponibilidade curta WS | `web`, `native` | parar `server` 5-15s e subir | backoff progride, recupera apos retorno | sem queue growth ilimitado |
| FI-03 | burst de marketdata | client+backend | aumentar carga/local replay | backlog bounded, drops observaveis e controlados | sem crescimento de memoria nao-bounded |
| FI-04 | restart sequencial (3x) | `web`, `native` | restart repetido do `server` | reconnect repetido sem degradacao progressiva | sem acumulacao de subscriptions |
| FI-05 | shutdown durante reconnect | `native`, `web` | encerrar client em `Connecting/Backoff` | teardown limpo | sem thread/socket residual |

## Gates de Robustez (Mensuraveis)

### G1. Leak Gate (client native)

- `RSS` e `threads` estaveis em soak (baseline + tolerancia definida)
- sem crescimento monotonicamente nao-bounded apos reconnects repetidos

### G2. Queue/Cardinality Gate

- `active_streams <= CAP`
- queues JS/WASM/threaded bounded
- `drop_count` observavel (quando houver overload)

### G3. Reconnect Gate

- reconnect com backoff bounded
- retorno a `Connected`
- re-subscribe completo
- sem estado preso em `Connecting/Reconnecting`

### G4. Runtime Integrity Gate

- `eviction_count` e `repair_count` apenas quando esperado
- `repair_count` deve permanecer zero em cenarios nominais

### G5. UX Operational Gate

- top bar/overlay mostram dados suficientes para diagnostico rapido:
  - `subs`
  - `q`
  - `drop`
  - `p`
  - `rc`
  - `ev`
  - `fix`

## Milestones (Curtos, Estruturais)

### M1 - Runtime Hardening Baseline (1-2 dias)

Escopo:
- consolidar fixes de reconnect/lifecycle (native/web)
- fechar queues unbounded e ownership async
- documentar invariantes R1-R5 no code review checklist

Aceite:
- `FI-01` e `FI-04` manuais passam em `web` e `native`
- `make -C client build-native`
- `make -C client build-wasm`

### M2 - Fault Injection Harness + Thresholds (1-2 dias)

Escopo:
- automatizar restarts durante soak
- sumarizador de `RSS/threads` + metricas runtime
- criterio `pass/fail` por thresholds

Aceite:
- script executa soak 15-30 min e gera relatorio
- falha automatica em crescimento acima do budget

### M3 - Stream Metadata Consolidation (P1 hardening-aware)

Escopo:
- consolidar metadata canonica de stream no core
- reduzir dependencia da UI em lookup ad-hoc do port
- validar restore/selection sob reconnect

Aceite:
- troca/restaure de stream sem mistura
- metadata consistente apos reconnect

### M4 - Backpressure Policy Completion

Escopo:
- revisar todas as filas/stores por tipo
- garantir policy/cap/drop counter em todos os caminhos
- harmonizar semantica `web` vs `native`

Aceite:
- tabela de policies publicada
- sem caminho "sem cap" restante no client

### M5 - Compare Mode Foundations (P3 prep)

Escopo:
- stores por stream reutilizaveis por painel
- budget por painel
- degradacao progressiva definida

Aceite:
- 2 paineis sem starvation do painel ativo

### M6 - Backend Co-Evolution (quando acionado)

Escopo condicional:
- contrato de ACK/re-subscribe;
- sinalizacao de overload/backlog;
- metrica de delivery websocket;
- hints de erro para diagnostico do client.

Aceite:
- melhora mensuravel em reconnect diagnostics / reducao de ambiguidades

## Quando Evoluir o Backend (Trigger Matrix)

Evoluir backend imediatamente se qualquer um ocorrer:

- client precisa inferir estado critico por heuristica local (ex.: reconnect/ack ambiguo);
- falta metrica/telemetria para distinguir problema de rede vs servidor;
- contrato WS nao permite re-subscribe/ack deterministico;
- overload no delivery causa comportamento client-side nao observavel;
- payload/subject impedem identity de stream sem parsing fragil.

## Evidence Pack (Obrigatorio por rodada de hardening)

Cada rodada relevante deve anexar:

- comando(s) executados;
- duracao de soak;
- logs do client (`[soak]`, reconnects, acks);
- logs backend correlatos (`server`, `consumer`, `processor`);
- snapshot de metricas finais (`drop`, `qmax`, `rc`, `ev`, `fix`);
- conclusao: `PASS`, `PASS with caveats`, ou `FAIL`.

## Test Plan

### Build/Smoke basico (client + stack local)

```bash
make up PROCESSOR_REPLICAS=2
make smoke
make -C client build-native
make -C client build-wasm
```

### Soak/Fault Injection (manual inicial)

```bash
make -C client run-native-compose NATIVE_FLAGS='--soak-seconds=45 --soak-log-ms=1000 --soak-multi'
docker compose -f deploy/compose/docker-compose.yml --env-file deploy/envs/local.env restart server
```

### Soak automatizado (quando aplicavel)

```bash
client/scripts/soak-native.sh --duration-sec 900 --sample-sec 2 --log-ms 1000 -- --ws-url=ws://127.0.0.1:8080/ws --api-key=prod_key_1
```

### Validacao Web (Playwright / manual)

- abrir `http://127.0.0.1:8090/`
- confirmar WS conectado + ACKs de subscribe
- executar `FI-01`/`FI-04` e validar reconnect/resubscribe sem churn anomalo

## Plano de Governanca de Bugs (Operacional)

Para bugs no client:

1. Reproduzir (preferencia: comando/script + duracao).
2. Classificar:
   - lifecycle
   - reconnect
   - backpressure
   - routing/multi-stream
   - UI/render
   - contrato/backend
3. Corrigir localmente (menor fix seguro).
4. Generalizar (invariante/policy/guard).
5. Instrumentar (metrica/log).
6. Revalidar com fault injection/soak.

## Acceptance

- Blueprint operacional publicado com matriz de paridade, fault injection matrix e gates mensuraveis.
- Regra "bug -> melhoria estrutural" definida e aplicavel em review.
- Milestones curtos (M1-M6) definidos com criterio de aceite.
- Triggers de co-evolucao backend explicitados para evitar heuristicas fragies no client.

## Riscos e Mitigacoes

| Risco | Impacto | Mitigacao |
|---|---|---|
| Time perde foco em bugs e posterga evolucao estrutural | Alto | aplicar governanca de bugs e exigir artefato estrutural por fix |
| Over-engineering antes de baseline | Medio | usar fault injection + metrics para priorizar por evidencia |
| Divergencia `web` vs `native` | Alto | invariantes comuns + semantica unificada + cenario FI obrigatorio nos dois |
| Backend evolui sem foco no contrato do client | Medio | trigger matrix + evidence pack compartilhado |

## Decisoes Pendentes

- Thresholds exatos de `RSS`/threads para leak gate (dependem de baseline por maquina).
- Se a fila JS do `web` deve expor contador de drops ao WASM/top bar via foreign proc dedicado.
- Se o backend deve expor metricas especificas por conexao WS para correlacao client/server.

## Referencias

- `docs/rfcs/RFC-0011-product-parity-marketmonkey.md`
- `docs/rfcs/RFC-0012-client-multi-exchange-evolution.md`
- `client/src/platform/native/marketdata_native.odin`
- `client/src/platform/native/ws_client.odin`
- `client/src/platform/web/marketdata_web.odin`
- `client/web/odin.js`
- `client/scripts/soak-native.sh`

## Changelog

- 2026-02-26:
  - RFC criada como blueprint operacional complementar ao `RFC-0012`.
  - Registrados incidentes reais (reconnect native, queue/ownership web) como seeds de hardening estrutural.
  - Definidas trilhas, fault injection matrix e gates de robustez para evolucao multi-exchange.
