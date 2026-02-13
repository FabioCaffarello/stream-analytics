---
status: filled
generated: 2026-02-13
owner: release-engineering
---

# W0 Build Acceleration (Commit-Driven)

## Snapshot (P1)

### Makefile targets atuais
`help install-tools tools modules tidy tidy-check fmt fmt-check vet quick ci-local docs-check docs-check-fast docs-check-full check-doc-headers check-doc-links check-truth-map check-feature-pack-links check-pack-subjects-vs-event-bus registry-check docs-fix invariants-check lint test test-root test-workspace test-workspace-race test-unit test-integration test-race test-replay-golden test-replay-golden-if-needed test-soak soak-check test-short vuln build run clean docker-up docker-down up down up-infra ps logs pre-commit-install commit-msg-check proto-tools proto-lint proto-gen proto-breaking proto-check proto ci`

### Scripts em `scripts/`
`check-doc-headers.sh check-doc-links.sh check-domain-isolation.sh check-feature-pack-links.sh check-pack-subjects-vs-event-bus.sh check-pack-subjects.sh check-registry.sh check-truth-map.sh for-each-module.sh gofmt-all.sh list-modules.sh soak-test.sh validate-commit-msg.sh`

### Tempos baseline (local)
- `make build`: `0s`
- `make test-workspace` warm (2a execução): `4s`
- `make docs-check-full`: `5s`

### Top 3 hotspots de teste (sem cache, `-count=1`)
- `github.com/market-raccoon/cmd/consumer`: `65.593s`
- `github.com/market-raccoon/internal/adapters/jetstream`: `10.236s`
- `github.com/market-raccoon/internal/shared/replay`: `3.024s`

## Cadeia de commits C1..C8 (P2)

### C1
- Objetivo: endurecer o quick loop para rodar subset explícito (`fmt-check + vet + test-unit`), mantendo gate de invariantes.
- Arquivos: `Makefile`, `scripts/for-each-module.sh`.
- Gates: `make quick`, `make test-unit`, `make invariants-check`.
- Rollback: `git revert <hash>`.

### C2
- Objetivo: consolidar partição clara unit/integration/race/soak e aliases consistentes.
- Arquivos: `Makefile`, `scripts/soak-test.sh`.
- Gates: `make test-unit`, `make test-integration`, `make test-race`, `make soak-check`.
- Rollback: `git revert <hash>`.

### C3
- Objetivo: garantir alinhamento de workspace (`go.work`) e impedir regressão para `go test ./...` no root.
- Arquivos: `Makefile`, `scripts/list-modules.sh`.
- Gates: `make modules`, `make test-root`, `make test-workspace`.
- Rollback: `git revert <hash>`.

### C4
- Objetivo: padronizar enforcement de commit message via target make único.
- Arquivos: `Makefile`, `scripts/validate-commit-msg.sh`.
- Gates: `make commit-msg-check MSG='feat(build): sample'`, `make commit-msg-check MSG='bad message'` (deve falhar).
- Rollback: `git revert <hash>`.

### C5
- Objetivo: separar `docs-check-fast` de `docs-check-full` com cobertura mínima segura sem mascarar full.
- Arquivos: `Makefile`, `scripts/check-doc-links.sh`.
- Gates: `make docs-check-fast`, `make docs-check-full`.
- Rollback: `git revert <hash>`.

### C6
- Objetivo: robustecer replay golden com target de replay condicional por trigger.
- Arquivos: `Makefile`, `scripts/check-truth-map.sh`.
- Gates: `make test-replay-golden-if-needed REPLAY_GOLDEN_CHANGED='internal/shared/replay/foo.go'`, `make test-replay-golden-if-needed REPLAY_GOLDEN_CHANGED='README.md'`.
- Rollback: `git revert <hash>`.

### C7
- Objetivo: tornar `proto-check` incremental, evitando regen e dirty-tree desnecessários.
- Arquivos: `Makefile`, `scripts/check-registry.sh`.
- Gates: `make proto-check`, segundo `make proto-check` sem mudanças (deve manter tree limpa).
- Rollback: `git revert <hash>`.

### C8
- Objetivo: explicitar `ci-local` com stop-on-fail determinístico e timings por etapa.
- Arquivos: `Makefile`, `scripts/for-each-module.sh`.
- Gates: `make ci-local`.
- Rollback: `git revert <hash>`.

## Stop Conditions (R4)
1. Drift de gate: `docs-check-fast` aprova e `docs-check-full` falha para o mesmo estado de tree.
2. Regressão de workspace: módulo de `go.work` deixa de ser coberto por `test-workspace`.
3. Replay golden flakey: mesmo commit alterna PASS/FAIL sem mudanças.
4. Commit-msg enforcement inconsistente: validação diverge entre `MSG` e `MSG_FILE`.
5. Proto-check suja tree sem mudança de `.proto` nem toolchain.

## Ordem de execução e checkpoints
- Executar um único commit por ciclo: `C1 -> gates -> checkpoint MCP -> próximo`.
- Se qualquer gate falhar: parar imediatamente, registrar checkpoint de falha no MCP, não avançar cadeia.
