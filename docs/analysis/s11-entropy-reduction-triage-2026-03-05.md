# S11 Triagem: Entropy Reduction + Scale Hardening (2026-03-05)

## Backend top-3 (>800 linhas)
1. `internal/actors/aggregation/runtime/processor.go` — 2297 linhas
2. `internal/actors/delivery/runtime/session_delivery.go` — 919 linhas
3. `internal/actors/delivery/runtime/router.go` — 809 linhas

## Client top-2 (foco parser + layer routing)
1. `client/src/core/services/message_parser.odin` — 1806 linhas
2. `client/src/core/layers/layer_strategies.odin` — 562 linhas

Observação de routing no client: `layer_strategies.odin` opera junto de `layer_registry.odin` e `market_store.odin`; a modularização será feita no conjunto `registry/store/reducers` preservando hot path sem alocação extra.

## Riscos principais
- Regressão de comportamento em políticas de backpressure da sessão (drop newest/oldest/priority) se a ordem de ações mudar.
- Regressão de coerência de sequência no router se amostragem/logging ou estado de stream for alterado fora de ordem.
- Regressão de políticas de overload/catch-up no processor se thresholds/defer/skip perderem equivalência.
- Aumento de alocação no parser/client ao mover código entre arquivos sem manter buffers/arranjos fixos.
- Risco de drift em owner-only/dedup no runtime de sinais ao formalizar métricas e IDs determinísticos.

## Plano de extração
1. Backend delivery: separar `DropPolicy` e coerência/log sampling do router em arquivos dedicados no mesmo package.
2. Backend aggregation: separar estratégias de `PolicyKit`, `CrossVenue` e limites/budgets de snapshot/catch-up em arquivos dedicados.
3. Client parser: dividir `message_parser.odin` em módulos `contract`, `batch` e `frames` mantendo API e hot path.
4. Client layer routing: separar responsabilidades `registry`, `store` e `reducers` (sem fallback novo; compat isolado).
5. Signals/Strategist: formalizar shard-key/owner-only, dedup replay-safe, IDs determinísticos via hash e observabilidade bounded.
6. Validar por commit: compilar + testes focados do módulo alterado antes de avançar.
