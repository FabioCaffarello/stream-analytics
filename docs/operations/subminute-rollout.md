# Sub-Minute Rollout Runbook (1s/5s)

**Status:** Active
**Last updated:** 2026-02-27

## Purpose

Operar rollout canario e rollback rapido de timeframes sub-minute (`1s`, `5s`) no backend, sem deploy de codigo, usando gates de configuracao em `processor` e `server`.

## Controls

Configuracao compartilhada (processor + server):

```jsonc
"processor": {
  "subminute_rollout": {
    "enabled": true,
    "venues": [],
    "instruments": []
  }
}
```

Semantica:
- `enabled=false`: rollback imediato de `1s/5s` (write + read paths bloqueados).
- `enabled=true` + listas vazias: rollout global de `1s/5s`.
- `enabled=true` + allow-list: rollout canario por `venue`/`instrument`.

## Rollout Matrix

1. Stage A (rollback baseline):
- `enabled=false`
- Esperado: `1s/5s` ausentes; `1m+` intacto.

2. Stage B (canario por venue/instrument):
- `enabled=true`
- `venues=["binance"]`
- `instruments=["BTCUSDT"]`
- Esperado: somente escopo permitido recebe `1s/5s`.

3. Stage C (global):
- `enabled=true`
- `venues=[]`, `instruments=[]`
- Esperado: `1s/5s` ativos para todos os escopos.

## Validation Gate

Execucao local (sem compose):

```bash
make subminute-rollout-gate
```

Execucao completa (compose smoke + runtime gate):

```bash
make subminute-rollout-gate-full
```

Relatorio:
- `.context/evidence/subminute-rollout-gate/latest.md`

## Promotion Criteria

Promover para proximo stage somente se:
- gate de rollout passar (`pass` em todas as etapas);
- `make invariants-check` e `make lint` verdes;
- sem regressao observavel de drops/latencia para `1m+`.

## Rollback

Rollback operacional imediato:

1. Ajustar config:
- `processor.subminute_rollout.enabled=false`

2. Recarregar/reiniciar componentes conforme procedimento operacional local.

3. Revalidar:
- `make subminute-rollout-gate`
- verificar ausencia de `1s/5s` e continuidade de `1m+`.

## Observability Hints

- `ingest_drop_total{reason="subminute_rollout_blocked"}` no write path.
- `ws_query_rejected_total{reason="subminute_rollout_blocked"}` no read path.
- motivos de catch-up:
  - `bookdelta_catchup_skip`
  - `trade_catchup_skip`
  - `liquidation_catchup_skip`
  - `markprice_catchup_skip`
