# Client Multi-Exchange M4 Widget Gate (2026-02-27)

## Objective
Criar um gate recorrente e determinístico para garantir que todas as widgets estejam funcionais no client.

## What Was Added
- Script de gate: `client/scripts/check-widgets-functional.sh`
  - build native do client
  - execução offline soak curto
  - parsing da linha `w[...]` do `runtime_probe`
  - fail automático se qualquer widget tiver contagem `<= 0`
- Target no Makefile do client:
  - `make -C client check-widgets`

## Validation
```bash
make -C client check-widgets
```

Resultado:
- `PASS`
- Coverage line:
  - `[soak] ... w[t=256 ob=25/25 st=8 hm=64 vp=80 c=20]`

## Interpretation
- `t` (trades), `ob` (orderbook asks/bids), `st` (stats), `hm` (heatmap), `vp` (vpvr), `c` (candle) todos com dados > 0.
- Garante que as sete widgets tenham pipeline funcional de dados no runtime do client (modo determinístico offline).

## Notes
- O gate offline é determinístico e ideal para regressão de UI/data-path.
- Em runtime real, a presença de dados depende de disponibilidade do stream; para isso, a rodada também valida ACKs de subscribe para todos os canais (`trade`, `bookdelta`, `stats`, `heatmap`, `vpvr`, `candle`).
