---
status: filled
generated: 2026-02-12
owners:
  - feature-developer
  - code-reviewer
docs:
  - docs/README.md
  - AGENTS.md
phases:
  - id: P
    name: Plan
    prevc: P
  - id: R
    name: Review
    prevc: R
  - id: E
    name: Execute
    prevc: E
  - id: V
    name: Verify
    prevc: V
---

# WX-docker-nats-compose

## Objetivo
- Organizar o deploy local em `deploy/` com Docker Compose para `nats` (JetStream persistente), `server`, `consumer` e `processor`.
- Garantir bootstrap local com 1 comando, healthchecks reais e ordem de subida via `depends_on` com `service_healthy`.
- Preservar arquitetura limpa: somente infra (`deploy/*`, `Makefile`, docs) e wiring mínimo em `cmd/*` para aceitar config montada.

## Escopo
- Incluído:
  - mover/normalizar compose e configuração do NATS para `deploy/`.
  - criar `deploy/configs/server.jsonc`, `deploy/configs/consumer.jsonc`, `deploy/configs/processor.jsonc`.
  - criar Dockerfiles multi-stage dedicados por serviço em `deploy/docker/`.
  - ajustar `Makefile` para `up`, `down`, `ps`, `logs`, `up-infra`.
  - atualizar instruções rápidas em documentação.
- Excluído:
  - alteração de domínio/casos de uso.
  - implementação completa de adapter NATS no core (somente preparação de runtime/config).

## Layout Alvo
- `deploy/compose/docker-compose.yml`
- `deploy/nats/nats-server.conf`
- `deploy/configs/server.jsonc`
- `deploy/configs/consumer.jsonc`
- `deploy/configs/processor.jsonc`
- `deploy/docker/server.Dockerfile`
- `deploy/docker/consumer.Dockerfile`
- `deploy/docker/processor.Dockerfile`

## Riscos e Mitigação
- Risco: caminhos de build quebrados por workspace Go com múltiplos módulos.
  - Mitigação: copiar `go.work`, `go.work.sum` e `go.mod/go.sum` dos módulos necessários antes do `COPY . .`.
- Risco: serviços sem readiness real (consumer/processor não expõem HTTP health hoje).
  - Mitigação: healthcheck obrigatório para `nats` e `server`; `consumer`/`processor` dependem de `nats` saudável e validamos ausência de crash loop por logs.
- Risco: regressão por imagem `nats:latest`.
  - Mitigação: pin explícito de versão estável no compose.

## Critérios de Aceite
- `docker compose -f deploy/compose/docker-compose.yml up -d` sobe os 4 serviços.
- NATS inicia com JetStream ativo e volume nomeado `nats_data` persistente.
- Portas publicadas somente em `127.0.0.1`.
- `curl http://127.0.0.1:8222/healthz` retorna sucesso.
- `curl http://127.0.0.1:8080/readyz` retorna sucesso para `server`.
- `docker compose ... logs` não mostra crash loop contínuo.

## Arquivos Planejados
- `/Volumes/OWC Express 1M2/Develop/market-raccoon/deploy/compose/docker-compose.yml`
- `/Volumes/OWC Express 1M2/Develop/market-raccoon/deploy/nats/nats-server.conf`
- `/Volumes/OWC Express 1M2/Develop/market-raccoon/deploy/configs/server.jsonc`
- `/Volumes/OWC Express 1M2/Develop/market-raccoon/deploy/configs/consumer.jsonc`
- `/Volumes/OWC Express 1M2/Develop/market-raccoon/deploy/configs/processor.jsonc`
- `/Volumes/OWC Express 1M2/Develop/market-raccoon/deploy/docker/server.Dockerfile`
- `/Volumes/OWC Express 1M2/Develop/market-raccoon/deploy/docker/consumer.Dockerfile`
- `/Volumes/OWC Express 1M2/Develop/market-raccoon/deploy/docker/processor.Dockerfile`
- `/Volumes/OWC Express 1M2/Develop/market-raccoon/Makefile`
- `/Volumes/OWC Express 1M2/Develop/market-raccoon/docs/README.md`
- `/Volumes/OWC Express 1M2/Develop/market-raccoon/AGENTS.md` (somente se necessário para mapa do repositório)

## Checkpoints de Commit
1. `chore(plan): define wx docker nats compose`
2. `feat(deploy): add compose layout dockerfiles and mounted configs`
3. `chore(makefile): add compose operation targets`
4. `docs(deploy): document local docker nats stack`
5. `chore(verify): record docker compose validation evidence`

## Fases PREVC até V

### P - Plan
1. Registrar estado atual e restrições.
2. Consolidar escopo, riscos, aceite e arquivos.
3. Vincular plano ao workflow.
4. Checkpoint no tracking do plano.

### R - Review
1. Revisar decisões de infra:
  - sem `nats:latest`, sem subnet fixa por padrão.
  - healthchecks e `depends_on` com `service_healthy`.
  - binds em `127.0.0.1`.
2. Confirmar aderência à Clean Architecture (infra + wiring mínimo).

### E - Execute
1. Implementar estrutura `deploy/` e Dockerfiles multi-stage.
2. Atualizar compose para os 4 serviços + volume nomeado.
3. Montar JSONC em `/etc/market-raccoon/` e ajustar comandos `-config`.
4. Atualizar `Makefile` com alvos de operação do compose.
5. Registrar commits incrementais e checkpoints no tracking.

### V - Verify
1. Subir stack com compose do novo caminho.
2. Validar health (`8222/healthz`, `8080/readyz`) e logs sem crash loop.
3. Registrar evidências no tracking e marcar fase V como concluída.
