## ADR Input: Signals/Strategist Topology
Date: 2026-03-05
Related plan: `.context/plans/signals-strategist-entrypoint-hardening.md` (M2)

### Contexto técnico
- `processor` já executa `SubsystemSignals` (`signalruntime`) e publica `signal.event`.
- `server` já executa `signalsruntime` (composer/strategist) e publica `signal.composite`.
- `cmd/signals` e `cmd/strategist` existem, mas ainda são placeholders sem runtime.
- Compose sobe `signals`/`strategist` como `core`, causando restart loop por binário vazio.

### Restrições e invariantes que não podem quebrar
1. Ownership cross-subsystem (`signals` e `strategist`) deve permanecer determinístico por shard.
2. Monotonicidade (`seq`, dedup, stale/non-monotonic) não pode regredir durante migração.
3. Boundedness (state caps, rate limits, dedup windows) deve continuar com budgets explícitos.
4. Contrato de eventos (`signal.event`, `signal.composite`) e delivery policy devem permanecer estáveis.

### Opções de topologia
#### Opção A — Manter embutido (server/processor) e remover serviços dedicados
- Prós:
- Menor risco imediato.
- Zero migração de fluxo entre processos.
- Menor esforço operacional de curto prazo.
- Contras:
- Mantém acoplamento de responsabilidade em entrypoints não especializados.
- Dificulta isolamento de capacidade e deploy independente de sinais.

#### Opção B — Extrair totalmente para serviços dedicados (`cmd/signals` e `cmd/strategist`)
- Prós:
- Melhor separação de bounded context.
- Escala e deploy independentes.
- Observabilidade operacional mais clara por serviço.
- Contras:
- Maior risco de regressão na migração (duplicidade, perda de mensagens, ownership drift).
- Exige cutover cuidadoso em runtime e compose.

#### Opção C — Híbrida (dual-run controlado por flags) com alvo final em B
- Prós:
- Reduz risco de migração via rollout progressivo.
- Permite comparar outputs/metrics entre velho e novo path.
- Mantém rollback simples por flag.
- Contras:
- Complexidade temporária maior.
- Requer disciplina de desligamento do path legado ao final.

### Avaliação resumida
| Critério | A | B | C |
|---|---|---|---|
| Risco imediato | Melhor | Pior | Bom |
| Tempo para estabilizar runtime | Melhor | Médio | Médio |
| Escalabilidade/isolamento futuro | Fraco | Melhor | Melhor |
| Complexidade de implementação | Melhor | Médio/alto | Alto (temporário) |
| Reversibilidade (rollback) | Médio | Médio | Melhor |

### Recomendação para ADR
Recomendar **Opção C** como estratégia de migração, com objetivo final em **Opção B**:
1. construir entrypoints dedicados robustos (M3) sem ativar tráfego produtivo;
2. habilitar dual-run por flags e comparar métricas-chave de ownership/seq/drops;
3. cortar path embutido por etapas após equivalência comprovada;
4. remover wiring legado e simplificar para topologia dedicada.

### Blueprint de refatoração (alto nível)
1. Extrair bootstrap comum de serviços runtime para helper reutilizável (sem alterar regras de negócio).
2. Implementar `Run()` em `cmd/signals` e `cmd/strategist` com composition root explícito.
3. Introduzir flags de fonte/publicação para alternar entre path embutido e dedicado.
4. Adicionar testes de paridade:
- equivalência de eventos emitidos (tipo/seq/chave);
- invariantes de ownership por réplica;
- budgets de drop/dedup/rate limit.
5. Atualizar compose e gates para impedir falso-verde (`Restarting` => fail).

### Métricas de decisão para cutover
- `ownership_contract_*` por subsystem sem aumento de violações.
- `signal_drop_total{reason=*}` dentro do budget definido.
- ausência de `ERROR/FATAL/PANIC` e ausência de restart loop.
- paridade de volume `signal.event` e `signal.composite` em janela de observação.
