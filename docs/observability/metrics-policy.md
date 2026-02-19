# Metrics Budget & Label Policy

**Status:** Active
**Last updated:** 2026-02-19

## Objetivo

Definir governanca minima para manter instrumentacao Prometheus-ready com baixo custo de GC/scrape e sem explosao de series.

## Label Budget (hard)

- Labels estaveis permitidas por padrao: `stream`, `venue` (opcionalmente `bc` quando realmente necessario).
- Cardinalidade maxima por metrica deve ser previsivel e limitada por enum/bucket.
- Proibido usar labels com cardinalidade potencialmente alta:
  - `instrument`
  - `symbol`
  - `request_id`
  - `subject` (subject completo)
  - `window_id`
  - `seq`
  - qualquer identificador dinamico por mensagem/sessao

## Nomeacao e tipos

- Toda metrica deve usar unidade no nome quando aplicavel:
  - latencia: `*_ms` ou `*_seconds`
  - tamanho: `*_bytes`
  - razao: `*_ratio`
  - contagem acumulada: `*_total`
- `Counter` somente sobe.
- `Gauge` pode subir/descer.
- `Histogram` para distribuicao (latencia/tamanho/ratio).

## Tabela permitidos vs proibidos

| Tema | Permitido | Proibido |
| --- | --- | --- |
| Labels de particionamento | `stream`, `venue`, `bc` | `instrument`, `symbol`, `subject` completo |
| IDs dinamicos | bucket/enum estavel | `request_id`, `window_id`, `seq`, IDs por mensagem |
| Counters | `*_total` monotonicos | uso de counter para estado atual |
| Gauges | estado instantaneo | usar gauge para acumulador irreversivel |
| Histograms | `*_ms`, `*_seconds`, `*_bytes`, `*_ratio` | histogram sem unidade no nome |

## Guardrails

- Novas metricas devem passar por teste de budget de labels.
- Mudancas de labels exigem justificativa de cardinalidade e impacto de scrape.
- Determinismo/replay/ack-on-commit sao inegociaveis: sem `time.Now` no core de decisao e sem side effect nao deterministico em caminho de replay.
