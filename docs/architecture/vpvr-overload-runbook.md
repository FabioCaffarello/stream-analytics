# VPVR Overload Runbook

**Status:** Draft
**Owner:** Runtime Resilience
**Last updated:** 2026-02-14

## Levels L0-L3

- `L0`: normal emit, sem degradação.
- `L1`: compress leve de snapshot.
- `L2`: compress maior + cadence `N=2` + drop determinístico de delta em `N` ímpar.
- `L3`: compress máximo + cadence `N=4` + drop de delta em janela aberta.
- `window_close`: snapshot final sempre emitido.

## Alert Thresholds (paramétricos)

- `A1` (`l3_stuck`): `vpvr_overload_level==3` por `N` janelas consecutivas.
- `A2` (`drop_without_close`): `vpvr_drop_total` sobe por `N` janelas sem avanço em `window_close`.
- `A3` (`latency_budget`): `vpvr_processing_latency_ms` acima do budget por `N` janelas.

Parâmetros recomendados:
- `N_l3_stuck=8`
- `N_drop_without_close=4`
- `latency_budget_ms=25`
- `N_latency_budget=6`

## Dashboard Queries (PromQL exemplos)

- overload atual:
`max by (venue,instrument,timeframe) (vpvr_overload_level)`

- stuck em L3 (janela operacional de 10m):
`max_over_time(vpvr_overload_level[10m]) == 3 and min_over_time(vpvr_overload_level[10m]) == 3`

- taxa de drop:
`sum by (reason) (increase(vpvr_drop_total[5m]))`

- degradations:
`sum by (action) (increase(vpvr_degrade_total[5m]))`

- compress ratio p95:
`histogram_quantile(0.95, sum(rate(vpvr_compress_ratio_bucket[5m])) by (le))`

- processing latency p95/p99:
`histogram_quantile(0.95, sum(rate(vpvr_processing_latency_ms_bucket[5m])) by (le))`
`histogram_quantile(0.99, sum(rate(vpvr_processing_latency_ms_bucket[5m])) by (le))`

## Operação

- Se `A1` disparar: reduzir fanout de delivery e revisar queue depth/occupancy.
- Se `A2` disparar: investigar consumidores lentos e confirmar emissão de `window_close`.
- Se `A3` disparar: reduzir cardinalidade de payload e validar pressure upstream.
