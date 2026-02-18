---
status: draft
generated: 2026-02-17
agents:
  - type: "feature-developer"
    role: "Implement shard metrics, budgets, and fairness checks"
  - type: "test-writer"
    role: "Write soak tests with 50+ instruments and replay equivalence"
  - type: "documentation-writer"
    role: "Write operational runbook for shard incidents"
docs:
  - "project-overview.md"
  - "development-workflow.md"
  - "testing-strategy.md"
phases:
  - id: "phase-1"
    name: "Metrics & Budget Primitives"
    prevc: "E"
    agent: "feature-developer"
    steps:
      - "Add shard_events_total counter (group_id label)"
      - "Add shard_info gauge (shard_index, shard_count constant labels)"
      - "Add ShardBudget config + enforcement in consumer loop"
      - "Add shard_lag_budget gauge for dynamic alerting"
      - "Add fairness distribution test in shard_test.go"
  - id: "phase-2"
    name: "Alerts & Skew Detection"
    prevc: "E"
    agent: "feature-developer"
    steps:
      - "Define PromQL for hot-shard-skew alert"
      - "Add alert rules YAML to deploy/alerts/"
      - "Add /shardz HTTP endpoint for live shard status"
  - id: "phase-3"
    name: "Soak Tests (replay equivalence)"
    prevc: "V"
    agent: "test-writer"
    steps:
      - "Soak: 2 shards x 50 instruments"
      - "Soak: 4 shards x 50 instruments"
      - "Validate replay equivalence (sharded union == non-sharded)"
  - id: "phase-4"
    name: "Runbook & Docs"
    prevc: "C"
    agent: "documentation-writer"
    steps:
      - "Write shard incident runbook"
      - "Update docs/operations/sharding.md"
---

# Production-Grade Shardability: Budgets, Invariants & Runbook

> Make sharding production-grade with lag/backlog budgets per shard, fairness/distribution checks, hot-shard skew alerting, and operational runbook for shard incidents.

## Current State (Baseline)

O sharding existe e funciona — `FNV-1a(venue+instrument) % groupCount` — com:

| Ja existe | Status |
|-----------|--------|
| `ShardKey()` / `ShardGroup()` em `adapters/jetstream/shard.go` | Testado com golden values |
| Client-side dispatch (ack-and-skip) em `consumer.go` | OK |
| Config `shard.index` / `shard.count` + flags + env | OK |
| 4 metricas: `shard_consumer_lag`, `shard_redelivered_total`, `shard_ack_latency_seconds`, `shard_skip_total` | OK |
| Testes de partition invariant (exactly-once, union coverage) | 80+ assertions |
| `docs/operations/sharding.md` basico | OK |

## Gaps (o que falta para "production-grade")

| Gap | Criticidade |
|-----|-------------|
| Nao existe `shard_events_total` — impossivel calcular throughput por shard | **Alta** |
| Nao existe info gauge com `shard_index` + `shard_count` como labels constantes | Media |
| Nao existe conceito de **lag budget** — lag pode crescer indefinidamente sem alert | **Alta** |
| Nao existe PromQL de **hot-shard skew** (3x throughput do mediano) | **Alta** |
| Nao existe endpoint HTTP para status live dos shards | Media |
| Nao existe **soak test** com 50+ instrumentos x 2/4 shards | **Alta** |
| Runbook de incidente de shard (hot shard, skew, rebalanceamento) inexistente | **Alta** |
| Fairness check: sem teste que valida distribuicao estatistica do hash | Media |

## DoD (Definition of Done)

- [ ] Metricas por shard: `shard_index`, `shard_count`, `shard_lag`, `shard_events_total`
- [ ] Alerta: "hot shard skew" — um shard com 3x throughput do mediano por X minutos
- [ ] Soak: 2/4 shards com 50+ instrumentos sem regressao no replay equivalence
- [ ] Lag budget: configuravel, log warning quando excedido
- [ ] Runbook operacional em `docs/operations/shard-incidents.md`

---

## Phase 1 — Metrics & Budget Primitives

**Objetivo:** Preencher as lacunas de observabilidade e adicionar budgets configuraveis.

### 1.1 — `shard_events_total` counter

**Onde:** `internal/shared/metrics/metrics.go`

```go
ShardEventsTotal = prometheus.NewCounterVec(
    prometheus.CounterOpts{
        Name: "jetstream_shard_events_total",
        Help: "Total events successfully processed per shard group.",
    },
    []string{"group_id"},
)
```

**Onde incrementar:** `internal/adapters/jetstream/consumer.go`, depois do ack bem-sucedido (junto ao bloco existente de shard metrics):

```go
if c.cfg.ShardGroupCount > 1 {
    metrics.IncShardEvents(strconv.Itoa(c.cfg.ShardGroupID))
}
```

**Helper:** Adicionar `IncShardEvents(groupID string)` em metrics.go, seguindo padrao existente.

**Teste:** Adicionar em `consumer_test.go` que o counter incrementa para shard >= 2.

### 1.2 — `shard_info` info gauge

**Onde:** `internal/shared/metrics/metrics.go`

```go
ShardInfo = prometheus.NewGaugeVec(
    prometheus.GaugeOpts{
        Name: "jetstream_shard_info",
        Help: "Static info labels for shard topology. Value is always 1.",
    },
    []string{"shard_index", "shard_count"},
)
```

**Onde setar:** `NewConsumer()`, uma vez durante init:

```go
if cfg.ShardGroupCount > 1 {
    metrics.ShardInfo.WithLabelValues(
        strconv.Itoa(cfg.ShardGroupID),
        strconv.Itoa(cfg.ShardGroupCount),
    ).Set(1)
}
```

Isso permite queries como:
```promql
jetstream_shard_info{shard_count="4"}  # descobre topologia
```

### 1.3 — ShardBudget (MaxLag) config

**Onde:** `internal/shared/config/schema.go`, dentro de `ShardConfig`:

```go
type ShardConfig struct {
    Index  int `json:"index"`
    Count  int `json:"count"`
    // MaxLag is the lag budget per shard. When exceeded, a warning is logged.
    // 0 means no budget enforcement.
    MaxLag int `json:"max_lag"`
}
```

**Propagacao:** `internal/shared/bootstrap/shard.go` — propagar `cfg.Shard.MaxLag` para `ConsumerConfig` igual a Index/Count.

**Enforcement:** `internal/adapters/jetstream/consumer.go`, no loop de lag observation (onde ja faz `SetShardConsumerLag`):

```go
if c.cfg.MaxLag > 0 && lagI64 > int64(c.cfg.MaxLag) {
    c.logger.Warn("shard lag budget exceeded",
        "group_id", c.cfg.ShardGroupID,
        "lag", lagI64,
        "budget", c.cfg.MaxLag,
    )
}
```

**Validacao:** Em `validateShard()` do `loader.go`:

```go
if s.MaxLag < 0 {
    return problem.Newf(codeInvalid, "shard.max_lag must be >= 0, got %d", s.MaxLag)
}
```

### 1.4 — `shard_lag_budget` gauge (para alert dinamico)

```go
ShardLagBudget = prometheus.NewGaugeVec(
    prometheus.GaugeOpts{
        Name: "jetstream_shard_lag_budget",
        Help: "Configured maximum acceptable lag per shard group. 0 = no budget.",
    },
    []string{"group_id"},
)
```

Setar uma vez no `NewConsumer()` se `ShardGroupCount > 1`. PromQL fica:

```promql
jetstream_shard_consumer_lag > jetstream_shard_lag_budget > 0
```

### 1.5 — Fairness distribution test

**Onde:** `internal/adapters/jetstream/shard_test.go`

```go
func TestShardGroup_FairnessDistribution(t *testing.T) {
    venues := []string{"binance", "bybit", "okx", "kraken"}
    instruments := top50Instruments() // helper com 50 instruments reais

    for _, groupCount := range []int{2, 4, 8} {
        t.Run(fmt.Sprintf("groups=%d", groupCount), func(t *testing.T) {
            buckets := make(map[int]int)
            for _, v := range venues {
                for _, i := range instruments {
                    key := ShardKey(fmt.Sprintf("marketdata.bookdelta.v1.%s.%s", v, i))
                    g := ShardGroup(key, groupCount)
                    buckets[g]++
                }
            }
            minC, maxC := math.MaxInt, 0
            for _, c := range buckets {
                if c < minC { minC = c }
                if c > maxC { maxC = c }
            }
            ratio := float64(maxC) / float64(minC)
            t.Logf("groupCount=%d distribution=%v ratio=%.2f", groupCount, buckets, ratio)
            assert.Less(t, ratio, 2.5)
        })
    }
}
```

---

## Phase 2 — Alerts & Skew Detection

**Objetivo:** Alertas PromQL para hot-shard e endpoint de diagnostico.

### 2.1 — Alert rules YAML

**Arquivo:** `deploy/alerts/shard-alerts.yaml`

```yaml
groups:
  - name: shard_health
    rules:
      - alert: ShardHotSkew
        # Fires when any shard has 3x the median throughput sustained for 5 min
        expr: |
          (
            rate(jetstream_shard_events_total[5m])
            / ignoring(group_id)
            group_left()
            quantile without(group_id) (0.5, rate(jetstream_shard_events_total[5m]))
          ) > 3
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: >-
            Shard {{ $labels.group_id }} has {{ $value | humanize }}x median throughput
          runbook_url: "docs/operations/shard-incidents.md#hot-shard-skew"

      - alert: ShardLagBudgetExceeded
        expr: |
          jetstream_shard_consumer_lag > jetstream_shard_lag_budget > 0
        for: 3m
        labels:
          severity: critical
        annotations:
          summary: >-
            Shard {{ $labels.group_id }} lag {{ $value }} exceeds budget
          runbook_url: "docs/operations/shard-incidents.md#lag-budget-exceeded"

      - alert: ShardConsumerHighLag
        expr: jetstream_shard_consumer_lag > 10000
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: >-
            Shard group {{ $labels.group_id }} lag is high ({{ $value }} messages)
```

### 2.2 — `/shardz` HTTP endpoint

**Onde:** `internal/interfaces/http/server.go`, novo handler.

```
GET /shardz -> 200 JSON
{
  "shard_index": 0,
  "shard_count": 2,
  "lag": 1234,
  "events_total": 567890,
  "skip_total": 123456,
  "budget": 50000,
  "budget_ok": true
}
```

Implementacao: ler valores via `prometheus.DefaultGatherer` ou manter `atomic.Int64` internos no consumer e expor via callback. A segunda opcao e mais simples e deterministica.

**Alternativa minimalista:** Nao criar endpoint novo — o `/metrics` do Prometheus ja expoe tudo. Depende da preferencia ops. O endpoint agrega valor se quisermos JSON legivel para debug rapido sem Grafana.

---

## Phase 3 — Soak Tests (Replay Equivalence)

**Objetivo:** Provar que 2/4 shards com carga realista (50+ instrumentos) mantem replay equivalence.

### 3.1 — Helper: gerador de subjects realistas

**Onde:** `internal/adapters/jetstream/shard_test.go` (helper privado, nao exportado)

```go
func top50Instruments() []string {
    return []string{
        "BTCUSDT", "ETHUSDT", "BNBUSDT", "SOLUSDT", "XRPUSDT",
        "DOGEUSDT", "ADAUSDT", "AVAXUSDT", "DOTUSDT", "MATICUSDT",
        "LINKUSDT", "TRXUSDT", "UNIUSDT", "ATOMUSDT", "LTCUSDT",
        "ETCUSDT", "NEARUSDT", "APTUSDT", "FILUSDT", "ARBUSDT",
        "OPUSDT", "SHIBUSDT", "PEPEUSDT", "INJUSDT", "SUIUSDT",
        "TIAUSDT", "SEIUSDT", "FTMUSDT", "ALGOUSDT", "GRTUSDT",
        "AAVEUSDT", "MKRUSDT", "SNXUSDT", "CRVUSDT", "LDOUSDT",
        "RNDRUSDT", "IMXUSDT", "SANDUSDT", "MANAUSDT", "AXSUSDT",
        "DYDXUSDT", "GMXUSDT", "PENDLEUSDT", "STXUSDT", "WLDUSDT",
        "JUPUSDT", "BOMEUSDT", "WUSDT", "ENAUSDT", "ONDOUSDT",
    }
}

func generateRealisticSubjects(venues, instruments []string) []string {
    events := []string{"marketdata.bookdelta.v1", "marketdata.trade.v1", "aggregation.snapshot.v1"}
    var out []string
    for _, v := range venues {
        for _, i := range instruments {
            for _, e := range events {
                out = append(out, fmt.Sprintf("%s.%s.%s", e, v, i))
            }
        }
    }
    return out
}
```

### 3.2 — Soak: 2 shards x 50 instruments

```go
func TestShard_Soak_2Shards_50Instruments(t *testing.T) {
    venues := []string{"binance", "bybit", "okx", "kraken"}
    subjects := generateRealisticSubjects(venues, top50Instruments())
    assertShardInvariants(t, subjects, 2, 2.5)
}
```

### 3.3 — Soak: 4 shards x 50 instruments

```go
func TestShard_Soak_4Shards_50Instruments(t *testing.T) {
    venues := []string{"binance", "bybit", "okx", "kraken"}
    subjects := generateRealisticSubjects(venues, top50Instruments())
    assertShardInvariants(t, subjects, 4, 2.0)
}
```

### 3.4 — `assertShardInvariants` helper (DRY)

```go
func assertShardInvariants(t *testing.T, subjects []string, groupCount int, maxSkewRatio float64) {
    t.Helper()

    shardBuckets := make(map[int][]string)
    for _, s := range subjects {
        g := ShardGroup(ShardKey(s), groupCount)
        shardBuckets[g] = append(shardBuckets[g], s)
    }

    // Invariant 1: union == total (exactly-once, no drops)
    total := 0
    for _, bucket := range shardBuckets {
        total += len(bucket)
    }
    require.Equal(t, len(subjects), total, "exactly-once violated")

    // Invariant 2: same venue+instrument always in same shard
    byInstrument := make(map[string]int)
    for g, bucket := range shardBuckets {
        for _, s := range bucket {
            parts := strings.Split(s, ".")
            key := parts[len(parts)-2] + "." + parts[len(parts)-1]
            if prev, ok := byInstrument[key]; ok {
                require.Equal(t, prev, g, "instrument %s split across shards", key)
            }
            byInstrument[key] = g
        }
    }

    // Invariant 3: fairness — max/min ratio within threshold
    minC, maxC := math.MaxInt, 0
    for _, bucket := range shardBuckets {
        if len(bucket) < minC { minC = len(bucket) }
        if len(bucket) > maxC { maxC = len(bucket) }
    }
    ratio := float64(maxC) / float64(minC)
    t.Logf("groupCount=%d subjects=%d ratio=%.2f distribution=%v",
        groupCount, len(subjects), ratio, bucketSizes(shardBuckets))
    assert.Less(t, ratio, maxSkewRatio, "skew ratio too high")

    // Invariant 4: replay equivalence — sorted sharded union == sorted input
    var sharded []string
    for _, bucket := range shardBuckets {
        sharded = append(sharded, bucket...)
    }
    sort.Strings(sharded)
    sorted := make([]string, len(subjects))
    copy(sorted, subjects)
    sort.Strings(sorted)
    require.Equal(t, sorted, sharded, "replay equivalence broken")
}
```

---

## Phase 4 — Runbook & Docs

### 4.1 — `docs/operations/shard-incidents.md`

Novo arquivo — runbook operacional com:

**Sections:**
1. **Alerts Reference** — tabela com todos os alerts, severity, meaning, SLO
2. **Scenario: Hot Shard (Skew)** — symptoms, root causes, resolution steps
3. **Scenario: Lag Budget Exceeded** — symptoms, resolution steps
4. **Scenario: Manual Rebalance** — when, step-by-step procedure, never-do list
5. **Scenario: Shard Consumer Stuck** — detection via redelivery spike, resolution
6. **Dashboards** — Grafana queries recomendadas

### 4.2 — Atualizar `docs/operations/sharding.md`

- Adicionar link para runbook
- Referencia para novas metricas (`shard_events_total`, `shard_info`, `shard_lag_budget`)
- Secao de alerts expandida

---

## Files Changed (Estimativa)

| File | Change |
|------|--------|
| `internal/shared/metrics/metrics.go` | +3 metricas + 3 helpers |
| `internal/shared/config/schema.go` | +MaxLag field em ShardConfig |
| `internal/shared/config/loader.go` | +validacao MaxLag |
| `internal/adapters/jetstream/consumer.go` | +inc events, +budget check, +info init |
| `internal/adapters/jetstream/shard_test.go` | +fairness + soak tests (~150 linhas) |
| `internal/interfaces/http/server.go` | +/shardz endpoint (opcional) |
| `internal/shared/bootstrap/shard.go` | +propagacao MaxLag |
| `deploy/alerts/shard-alerts.yaml` | novo — 3 alert rules |
| `deploy/configs/*.jsonc` | +max_lag field |
| `docs/operations/shard-incidents.md` | novo — runbook completo |
| `docs/operations/sharding.md` | +referencias novas metricas e runbook |

## Execution Order

```
Phase 1 (metricas + budget) -> Phase 2 (alerts + endpoint) -> Phase 3 (soak) -> Phase 4 (docs)
```

Phase 3 pode iniciar em paralelo com Phase 2 — os soak tests usam apenas `ShardKey`/`ShardGroup`, nao precisam de metricas novas. Phase 4 so depois de tudo estabilizar.
