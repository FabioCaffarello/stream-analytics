## Entrypoint Contract Map
Date: 2026-03-05
Scope: `cmd/*` no `go.work` + diretórios de entrypoint vazios sob `cmd/`

### 1) Inventário e classificação
| Entrypoint | Estado | Tipo de runtime |
|---|---|---|
| `cmd/server` | Implementado | Serviço HTTP/WS + guardian |
| `cmd/consumer` | Implementado | Serviço de ingest + guardian |
| `cmd/processor` | Implementado | Serviço de processamento + guardian |
| `cmd/store` | Implementado | Serviço de persistência + guardian |
| `cmd/backfill` | Implementado | Job/CLI batch (sem servidor HTTP) |
| `cmd/migrate` | Implementado | CLI de migração DB (sem guardian/HTTP) |
| `cmd/signals` | Incompleto (placeholder) | Pretenso serviço dedicado |
| `cmd/strategist` | Incompleto (placeholder) | Pretenso serviço dedicado |
| `cmd/credentials-broker` | Vazio | Placeholder de entrypoint |
| `cmd/executor` | Vazio | Placeholder de entrypoint |
| `cmd/portfolio` | Vazio | Placeholder de entrypoint |

### 2) Contrato padrão observado nos entrypoints maduros
Contrato comum (forte aderência em `server/consumer/processor/store`):
1. `main.go` define flags e overrides.
2. `bootstrap.LoadAndValidate` centraliza load + validate.
3. `Run(ctx, cfg, ...)` em `bootstrap.go` como composition root.
4. Lifecycle explícito: start runtime, `ListenAndServe`, `SignalChannel`, shutdown com timeout.
5. Log estruturado + problemas retornados por erro.

### 3) Matriz comparativa de contrato
| Entrypoint | Flags + overrides | `LoadAndValidate` | `Run(...)` | Guardian/Actor | HTTP health/readiness | Signal handling + graceful shutdown |
|---|---|---|---|---|---|---|
| `server` | Sim | Sim | Sim | Sim | Sim (`/readyz`) | Sim |
| `consumer` | Sim | Sim | Sim | Sim | Sim (`/readyz`) | Sim |
| `processor` | Sim | Sim | Sim | Sim | Sim (`/readyz`) | Sim |
| `store` | Sim | Sim | Sim | Sim | Sim (`/readyz`) | Sim |
| `backfill` | Sim | Sim | Sim (job) | Não | Não | N/A (execução batch) |
| `migrate` | Sim | Não (`bootstrap`) | Não (`runCommand`) | Não | Não | N/A (execução CLI) |
| `signals` | Não | Não | Não | Não | Não | Não |
| `strategist` | Não | Não | Não | Não | Não | Não |

### 4) Evidências de código (âncoras)
- Contrato maduro em `server`:
- `main + LoadAndValidate + Run`: `cmd/server/main.go:25`, `cmd/server/main.go:31`, `cmd/server/main.go:44`
- lifecycle: `cmd/server/bootstrap.go:265`, `cmd/server/bootstrap.go:544`, `cmd/server/bootstrap.go:550`, `cmd/server/bootstrap.go:573`
- Contrato maduro em `consumer`:
- `main + overrides + shard flags`: `cmd/consumer/main.go:25-32`
- lifecycle: `cmd/consumer/bootstrap.go:65`, `cmd/consumer/bootstrap.go:227`, `cmd/consumer/bootstrap.go:232`, `cmd/consumer/bootstrap.go:150`
- Contrato maduro em `processor`:
- `main + overrides + shard flags`: `cmd/processor/main.go:25-32`
- lifecycle: `cmd/processor/bootstrap.go:347`, `cmd/processor/bootstrap.go:716`, `cmd/processor/bootstrap.go:722`, `cmd/processor/bootstrap.go:743`
- Contrato maduro em `store`:
- `main + overrides`: `cmd/store/main.go:24-28`
- lifecycle: `cmd/store/bootstrap.go:49`, `cmd/store/bootstrap.go:149`, `cmd/store/bootstrap.go:155`, `cmd/store/bootstrap.go:175`
- Placeholders `signals/strategist`:
- `cmd/signals/main.go:3` (`func main() {}`)
- `cmd/strategist/main.go:3` (`func main() {}`)
- `cmd/signals/bootstrap.go:1` e `cmd/strategist/bootstrap.go:1` só `package main`
- Base reutilizável de bootstrap:
- `internal/shared/bootstrap/config.go:15`
- `internal/shared/bootstrap/signal.go:11`

### 5) Lacunas críticas (M1 findings)
1. `cmd/signals` e `cmd/strategist` não implementam nenhum contrato mínimo de runtime.
2. `docker-compose` os trata como serviços core, com healthcheck e `restart: unless-stopped`, mas os binários encerram imediatamente (exit 0), gerando crash loop operacional.
3. Configuração de mount aponta para paths tratados como arquivo (`../configs/signals.jsonc`, `../configs/strategist.jsonc`), porém esses paths estão como diretórios vazios no estado atual.
4. Há sobreposição funcional de domínio já embutida:
- `processor` já instancia `SubsystemSignals` (`signalruntime`) para `signal.event`.
- `server` já instancia `signalsruntime` (composer/strategist) para `signal.composite` quando `signals.use_composer=true`.

### 6) Impacto de arquitetura/refatoração
Sem decisão de topologia, implementar `cmd/signals`/`cmd/strategist` direto cria risco de:
- duplicação de emissão de sinais;
- dupla ownership policy para mesmo stream;
- divergência de boundedness/limiter entre runtime embutido e runtime dedicado.

Por isso, M2 (ADR) é bloqueante para execução segura:
- Opção A: consolidar em runtime embutido e remover serviços dedicados de `core`.
- Opção B: extrair para binários dedicados e remover wiring embutido correspondente.
- Opção C: cutover híbrido com flags e janela de dual-run controlada.

### 7) Decisão operacional imediata recomendada
Antes da implementação funcional:
1. impedir falso-verde no gate: `smoke`/runtime-gate deve falhar com qualquer serviço `Restarting`;
2. classificar `signals/strategist` como `experimental` até M3 (ou removê-los do profile `core`);
3. manter mudança separada: primeiro refatoração estrutural (bootstrap), depois comportamento (engine/wiring).

### 8) Saída do M1
- Status: concluído
- Artefato: este documento
- Próximo passo: abrir ADR de topologia (`M2`) com base nas lacunas e sobreposição identificadas.
