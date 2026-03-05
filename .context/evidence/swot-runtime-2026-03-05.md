## SWOT: Market Raccoon runtime (core + client :8090), perspectiva de robustez operacional e evolução sem legado
Date: 2026-03-05

### Quadrants

| Internal | |
|---|---|
| **Strengths** (assets, patterns, capabilities) | **Weaknesses** (gaps, debt, constraints) |
| - `make up PROCESSOR_REPLICAS=2` sobe stack completa com smoke pass.<br>- `legacy-check` aprovado e rota legada de websocket desativada no servidor.<br>- `client` em `:8090` carrega WASM e responde `GET /api/v1/markets` com 200.<br>- `consumer`, `processor(2)`, `server`, `store` saudáveis e sem `ERROR/FATAL/PANIC` nos logs coletados. | - `signals` e `strategist` em crash loop por `main()` vazio e política `restart: unless-stopped`.<br>- Endpoints `:8084/readyz` e `:8085/readyz` indisponíveis (serviços não sustentam processo).<br>- `processor-1/2` registram `WARN` recorrente (`seq_non_monotonic` e `stale_event`) no subsistema de sinais.<br>- Smoke depende de acesso ao socket Docker fora do sandbox, reduzindo portabilidade do gate local. |

| External | |
|---|---|
| **Opportunities** (unlocks, market, integrations) | **Threats** (risks, competition, dependencies) |
| - Formalizar baseline de confiabilidade com gate que falha em qualquer serviço reiniciando.<br>- Implementar runtime mínimo para `signals/strategist` (loop, readyz, shutdown, logs estruturados) e remover falso-verde operacional.<br>- Criar suite E2E Playwright para `:8090` cobrindo boot WASM + handshake WS + fluxo de dados.<br>- Instrumentar alertas de taxa de `WARN` por motivo (`seq_non_monotonic`, `stale_event`) por shard. | - Reinício contínuo de dois serviços pode mascarar ausência de capacidade real em produção.<br>- Eventos fora de ordem podem degradar qualidade analítica e decisões downstream.<br>- Escalar shards sem tratar ordenação aumenta risco de inconsistência entre réplicas.<br>- Dependência forte de checks manuais eleva risco de regressão silenciosa no cutover. |

### Key Implications
| | Opportunity 1: Runtime hardening de serviços | Threat 1: Degradação por ordem/consistência |
|---|---|---|
| **Strength 1: Stack e smoke já automatizados** | Leverage: estender `make smoke` para incluir `signals/strategist` e impedir aprovação com crash loop. | Defend: usar pipeline atual para adicionar budget de `WARN` por shard e abortar rollout acima do limite. |
| **Strength 2: Legado já bloqueado por guardrails** | Leverage: integrar `legacy-check` no gate pré-deploy obrigatório de branches de runtime. | Defend: manter rota legada hard-disabled e monitorar qualquer regressão de endpoint legado. |
| **Weakness 1: `signals/strategist` sem processo ativo** | Invest: implementar skeleton de serviço com `readyz`, `liveness`, telemetria e teste de boot em compose. | Mitigate: retirar serviços do profile `core` até estarem funcionais ou marcar explicitamente como `experimental`. |
| **Weakness 2: WARN recorrente de ordenação em processors** | Invest: escrever testes de regressão por `(venue,instrument,seq)` e validar invariantes de monotonicidade por shard. | Mitigate: adicionar circuito de proteção para descarte/retentativa observável com métricas e alarmes. |

1. Crash loop silencioso de `signals/strategist` invalida a noção de "stack robusta"; precisa ser tratado como bloqueador de prontidão.
2. A base já tem bons controles anti-legado e readiness dos serviços centrais; isso permite endurecer gate sem reescrever arquitetura.
3. Os `WARN` de ordenação são o principal risco técnico para escala multi-réplica; prioridade alta para invariantes e teste de regressão.

### Recommended Next Step
Produzir um RFC de "Runtime Hardening & Sequencing Reliability" seguido de ADR para ciclo de vida de serviços (`signals/strategist`) e plano de execução em milestones.

### Evidence (execução local)
- `make up PROCESSOR_REPLICAS=2`: stack subiu com `consumer`, `processor-1`, `processor-2`, `server`, `store`, `client`.
- `make smoke` (com acesso Docker): `consumer`, `processor`, `server`, `store` prontos.
- HTTP checks (host): `8090/healthz=200`, `8090/api/v1/markets=200`, `8080/readyz=200`, `8083/readyz=200`, `8084/readyz=000`, `8085/readyz=000`.
- `legacy-check`: `legacy-scan: pass (all)`.
- Inspeção de containers: `compose-signals-1` e `compose-strategist-1` em `restarting`, `exit=0`, `restarts=13`.
- Causa técnica direta: `cmd/signals/main.go` e `cmd/strategist/main.go` com `func main() {}`.
