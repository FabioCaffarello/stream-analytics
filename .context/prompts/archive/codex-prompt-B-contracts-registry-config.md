# Codex Prompt B — Register Candle+Stats Contracts, Delivery Policy, Config Schema

## Project Identity

Market Raccoon is a high-performance market intelligence platform. Go 1.25+, Hollywood actor model, NATS JetStream, DDD hexagonal architecture.

---

## Context

After Prompt A, the processor pipeline routes trade→candle and liquidation/markprice→stats. However:
1. Candle/Stats event types are NOT in the codec/payload registry (needed for envelope publishing)
2. Candle/Stats are NOT in the delivery contracts map (WS server won't accept them)
3. Candle/Stats subjects are NOT in `docs/contracts/subject-registry.yaml`
4. Config schema has NO candle/stats-specific fields
5. `docs/contracts/event-bus.md` subject matrix is missing candle/stats rows

---

## Mandatory Patterns

### Errors: `*problem.Problem` (never plain `error`)
### Envelope contract (ADR-0002):
```go
envelope.Envelope{
    Type:           "aggregation.candle",
    Version:        1,
    Venue:          "binance",
    Instrument:     "btcperp",
    TsExchange:     tsExchange,
    TsIngest:       tsIngest,
    Seq:            seq,
    IdempotencyKey: hash.HashFields(venue, instrument, timeframe, fmt.Sprintf("%d", windowStart)),
    ContentType:    "application/json",
    Payload:        payloadBytes,
}
```

### Import order: stdlib → external → monorepo

---

## Task: Register Contracts and Extend Config

### Step 1: Register candle/stats in codec payload registry

**File:** `internal/shared/contracts/payload_registry.go`

Add constants:
```go
const (
    aggregationEventTypeCandle = "aggregation.candle"
    aggregationEventTypeStats  = "aggregation.stats"
)
```

Add registrations in `BootstrapPayloadCodecRegistry` (or the options variant), following existing pattern:

```go
// Candle — JSON codec
codec.RegisterPayload(codec.SchemaKey{
    Type:    aggregationEventTypeCandle,
    Version: 1,
    Format:  envelope.ContentTypeJSON,
}, codec.JSONCodec[aggdomain.CandleClosed]{})

// Stats — JSON codec
codec.RegisterPayload(codec.SchemaKey{
    Type:    aggregationEventTypeStats,
    Version: 1,
    Format:  envelope.ContentTypeJSON,
}, codec.JSONCodec[aggdomain.StatsWindowClosed]{})
```

**NOTE:** This requires `internal/shared/contracts/go.mod` to have a `require` and `replace` for `internal/core/aggregation`. Check if this dependency already exists. If not, the codec registration should use a generic intermediate DTO struct rather than importing core domain types directly. Follow the existing pattern — check how `mddomain.TradeTickV1` is referenced.

If the contracts package already imports marketdata domain, follow the same pattern for aggregation domain. If it uses a separate DTO, define one.

### Step 2: Add candle/stats to delivery contracts

**File:** `internal/core/delivery/domain/envelope_policy.go`

Add to `deliveryContracts` map:
```go
"aggregation.candle": {version: 1, ownerBC: "aggregation", producerBC: "aggregation", schemaAuthority: "aggregation"},
"aggregation.stats":  {version: 1, ownerBC: "aggregation", producerBC: "aggregation", schemaAuthority: "aggregation"},
```

### Step 3: Add candle/stats to subject-registry.yaml

**File:** `docs/contracts/subject-registry.yaml`

Add entries following existing pattern:
```yaml
- subject: aggregation.candle.v1.{venue}.{instrument}
  event_type: aggregation.candle
  version: 1
  owner_bc: aggregation
  producer_bc: aggregation
  schema_authority_bc: aggregation
  status: draft
  description: Closed OHLCV candle for one venue/instrument/timeframe window

- subject: aggregation.stats.v1.{venue}.{instrument}
  event_type: aggregation.stats
  version: 1
  owner_bc: aggregation
  producer_bc: aggregation
  schema_authority_bc: aggregation
  status: draft
  description: Closed stats window (liq volume, markprice, funding) for one venue/instrument/timeframe
```

### Step 4: Add candle/stats to event-bus.md subject matrix

**File:** `docs/contracts/event-bus.md`

Add rows to the Subject taxonomy table:
```
| aggregation.candle.v1.{venue}.{instrument}  | Closed OHLCV candle | aggregation | aggregation | Draft |
| aggregation.stats.v1.{venue}.{instrument}   | Closed stats window | aggregation | aggregation | Draft |
```

### Step 5: Extend config schema

**File:** `internal/shared/config/schema.go`

Add candle/stats config to `ProcessorConfig`:
```go
type ProcessorConfig struct {
    BusCapacity    int                    `json:"bus_capacity"`
    MaxInstruments int                    `json:"max_instruments"`
    Insights       ProcessorInsightsConfig `json:"insights"`
    Candle         ProcessorCandleConfig   `json:"candle"`   // NEW
    Stats          ProcessorStatsConfig    `json:"stats"`    // NEW
}

type ProcessorCandleConfig struct {
    Enabled    bool `json:"enabled"`
    MaxCandles int  `json:"max_candles"`
}

type ProcessorStatsConfig struct {
    Enabled    bool `json:"enabled"`
    MaxWindows int  `json:"max_windows"`
}
```

Add defaults in `applyDefaults()`:
```go
if c.Processor.Candle.MaxCandles == 0 {
    c.Processor.Candle.MaxCandles = 50_000
}
if c.Processor.Stats.MaxWindows == 0 {
    c.Processor.Stats.MaxWindows = 50_000
}
```

### Step 6: Wire config in bootstrap

**File:** `cmd/processor/bootstrap.go`

Use new config fields when building AggregationService:
```go
aggSvc := aggapp.NewAggregationService(aggapp.AggregationServiceConfig{
    Update: aggapp.UpdateConfig{MaxBooks: cfg.Processor.MaxInstruments},
    Candle: aggapp.BuildCandleConfig{MaxCandles: cfg.Processor.Candle.MaxCandles},
    Stats:  aggapp.BuildStatsConfig{MaxWindows: cfg.Processor.Stats.MaxWindows},
    Publisher:   artifactPub,
    Store:       hotStore,
    CandleStore: &logCandleHotStore{logger: logger},
    StatsStore:  &logStatsHotStore{logger: logger},
})
```

### Step 7: Update config.jsonc templates

**File:** `cmd/processor/config.jsonc`

Add candle/stats sections:
```jsonc
{
  "processor": {
    "bus_capacity": 1024,
    "max_instruments": 2048,
    // Candle aggregation (OHLCV from trades)
    "candle": {
      "enabled": true,
      "max_candles": 50000
    },
    // Stats aggregation (liq volume + markprice + funding per timeframe)
    "stats": {
      "enabled": true,
      "max_windows": 50000
    }
  }
}
```

### Step 8: Add feature pack drift marker cleanup

**Files:**
- `.context/docs/feature-packs/candle-aggregation.md`
- `.context/docs/feature-packs/stats-aggregation.md`

Update subjects from `(planned, not in event-bus.md matrix)` to remove the planned marker since they are now in the matrix.

---

## Reference Files

| File | Purpose |
|------|---------|
| `internal/shared/contracts/payload_registry.go` | Codec registry to extend |
| `internal/core/delivery/domain/envelope_policy.go` | Delivery contracts to extend |
| `docs/contracts/subject-registry.yaml` | Subject registry to extend |
| `docs/contracts/event-bus.md` | Subject matrix to extend |
| `internal/shared/config/schema.go` | Config schema to extend |
| `cmd/processor/bootstrap.go` | Composition root to update |
| `cmd/processor/config.jsonc` | Config template to update |
| `.context/docs/feature-packs/candle-aggregation.md` | Feature pack to update |
| `.context/docs/feature-packs/stats-aggregation.md` | Feature pack to update |

---

## Execution Rules

### Before EVERY commit:
```bash
make docs-check           # documentation governance
make invariants-check     # domain isolation + layering
```

### Before commits touching runtime code:
```bash
make test-workspace       # all modules
make test-workspace-race  # with -race flag
```

### STOP CONDITIONS:
- Pack-subject guard failure (subject not in event-bus.md)
- Registry-check failure (registry vs runtime mismatch)
- Layering violation (core importing adapters)
- Go module cycle or missing replace directive

### Commit message format:
```
feat(m4): register candle+stats contracts and config schema

- Register aggregation.candle/stats v1 in payload codec registry
- Add candle/stats to delivery contracts and subject-registry.yaml
- Extend ProcessorConfig with candle/stats config sections
- Update event-bus.md subject matrix and config.jsonc templates

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>
```

---

## Important Constraints

1. **No cross-BC imports in core** — contracts package CAN import domain types (check existing pattern)
2. **Subject root `aggregation` is already accepted** in subject_validation.go
3. **Schema authority** for candle/stats is `aggregation` (same BC produces and owns schema)
4. **Status: draft** for new subjects — not `active` until tested end-to-end
5. **Config defaults must be safe** — `MaxCandles=50_000` and `MaxWindows=50_000` match use case defaults
6. **JSONC comments are stripped** by config loader — comments in config.jsonc are fine
