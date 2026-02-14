# PolicyKit adoption

`internal/shared/policykit` centraliza backpressure determinístico por stream.

## Como adotar (stream)
1. Defina um `policykit.Engine` (normalmente `ThresholdEngine`).
2. Colete somente sinais determinísticos (`queue/backlog/occupancy/counters`).
3. Resolva categoria por subject com `CategoryResolver`.
4. Aplique `Decision` com `Applier.Apply(...)` no emit path.
5. Preserve guardrail: `close/final` nunca pode ser drop/compress.

## Regras operacionais
- Engine é funcional/puro (sem `time.Now`, `sleep`, ticker no domínio).
- `DropDelta` só remove categoria Delta.
- `DegradeStride(N)` usa contagem de eventos.
- `CompressSnapshot` não atua em `close/final`.
- Mesma entrada gera mesma saída.

## Métricas comuns por stream
- `policykit_overload_level{stream,venue,instrument}`
- `policykit_drop_total{stream,reason}`
- `policykit_degrade_total{stream,action}`
- `policykit_compress_total{stream}`
- `policykit_latency_ms{stream}`

## Streams já migrados
- VPVR emit path (`insights.volume_profile`)
- Aggregation processor `marketdata.bookdelta` (opt-in)
