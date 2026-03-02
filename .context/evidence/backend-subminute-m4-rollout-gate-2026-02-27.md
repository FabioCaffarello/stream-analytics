# Evidence - Backend Sub-Minute M4 Rollout Gate (2026-02-27)

## Scope

Concluir a fatia M4 slice 3 com gate operacional para canario/rollback de `1s/5s`, incluindo script executavel, alvo de Makefile e runbook.

## Changes in Scope

- `scripts/test/util/subminute-rollout-gate.sh`
  - adiciona execucao padrao dos testes de rollout:
    - `./cmd/processor` (gate write-path)
    - `./cmd/server` (gate read-path)
    - `./internal/actors/aggregation/runtime` (catch-up hardening)
  - gera relatorio versionado em `.context/evidence/subminute-rollout-gate/<timestamp>/report.md`
  - atualiza ponteiro `.context/evidence/subminute-rollout-gate/latest.md`
  - opcoes extras: `--with-smoke`, `--with-runtime-gate`
- `Makefile`
  - `subminute-rollout-gate`
  - `subminute-rollout-gate-full`
- `docs/operations/subminute-rollout.md`
  - runbook de canario/rollback com matriz de rollout e criterios de promocao.

## Commands Executed

```bash
make subminute-rollout-gate
make shell-script-check
```

## Command Results

- `make subminute-rollout-gate`: PASS
  - run dir: `.context/evidence/subminute-rollout-gate/20260227T122458Z`
  - all steps PASS:
    - `processor-rollout-tests`
    - `server-rollout-tests`
    - `runtime-catchup-tests`
- `make shell-script-check`: PASS

## Artifacts

- Report (latest): `.context/evidence/subminute-rollout-gate/latest.md`
- Report (run): `.context/evidence/subminute-rollout-gate/20260227T122458Z/report.md`
- Logs:
  - `.context/evidence/subminute-rollout-gate/20260227T122458Z/processor-rollout-tests.log`
  - `.context/evidence/subminute-rollout-gate/20260227T122458Z/server-rollout-tests.log`
  - `.context/evidence/subminute-rollout-gate/20260227T122458Z/runtime-catchup-tests.log`

## Notes

- Foi corrigido bug de escaping no script (linhas de report com backticks), evitando recursao por command substitution.
- Existem diretorios antigos da tentativa com erro em `.context/evidence/subminute-rollout-gate/`; artefatos atuais validos estao no timestamp acima e em `latest.md`.
