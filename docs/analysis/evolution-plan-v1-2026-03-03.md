# Market Raccoon — Institutional Deterministic Market Infrastructure

## Evolution Plan v1 — 2026-03-03

> **Objetivo:** Permitir execução incremental pelo CODEX com segurança, escalabilidade e zero regressão.

---

## Table of Contents

1. [Design Invariants](#1-design-invariants)
2. [Block A — Multi-Exchange Deterministic Aggregation Layer](#2-block-a--multi-exchange-deterministic-aggregation-layer)
3. [Block B — Liquidity Intelligence Engine](#3-block-b--liquidity-intelligence-engine)
4. [Block C — Signal Engine via WebSocket](#4-block-c--signal-engine-via-websocket)
5. [Phased Roadmap (CODEX-Executable)](#5-phased-roadmap-codex-executable)
6. [Technical Risks & Mitigations](#6-technical-risks--mitigations)
7. [Legacy Strangler Strategy](#7-legacy-strangler-strategy)

---

## 1. Design Invariants

These invariants are **non-negotiable** across all three blocks. Every commit MUST preserve them.

| ID | Invariant | Enforcement |
|----|-----------|-------------|
| INV-DET-01 | **Determinism: live = replay** — No `time.Now()` in domain/app. All time via `clock.Clock` injection. | `make invariants-check` grep guard |
| INV-BND-01 | **Boundedness** — No state grows indefinitely. Every map/slice has a cap, TTL, or eviction policy. | Code review gate + cap constants in domain |
| INV-SST-01 | **Single source of truth** — Limits, caps, feature flags live in `deploy/configs/server.jsonc` only. No magic numbers in code. | Config schema validation at startup |
| INV-STR-01 | **Strangler pattern** — New code wraps old via feature flags. Old paths removed only after new path passes validation gate. | Feature flag in config + `_legacy` suffix |
| INV-SEQ-01 | **Monotonic sequencing** — Envelope.seq is monotonically increasing per {venue, instrument} partition. Gaps > threshold trigger alert. | `seq_gap_total` Prometheus counter |
| INV-IDP-01 | **Idempotency** — Every published envelope has a deterministic `idempotency_key` (FNV-1a hash of content). JetStream dedup window = 5min. | `artifact_publisher.go` enforced |
| INV-ISO-01 | **Bounded context isolation** — `internal/core/*` cannot import actors, adapters, interfaces. | `make invariants-check` import guard |

### Boundedness Strategy (Global)

```
Cap Type          | Default      | Config Key                    | Eviction
─────────────────┼──────────────┼───────────────────────────────┼──────────────
OrderBook levels  | 500/side     | aggregation.orderbook_max_lev | prune furthest from mid
Candle windows    | 96/TF/key    | aggregation.candle_window_cap | close oldest
Stats windows     | 96/TF/key    | aggregation.stats_window_cap  | close oldest
Heatmap cells     | 2000/snap    | insights.heatmap_cell_cap     | merge smallest bins
VPVR buckets      | 400/window   | insights.vpvr_bucket_cap      | merge smallest volume
Evidence buffer   | 1000/kind    | evidence.buffer_cap           | ring buffer (overwrite oldest)
Session subs      | 100/session  | delivery.max_subs_per_session | reject new subscription
TranscodeCache    | 16 shards    | delivery.cache_shards         | per-shard LRU eviction
Signal windows    | 50/signal    | signals.window_cap            | slide (drop oldest)
```

---

## 2. Block A — Multi-Exchange Deterministic Aggregation Layer

### 2.1 Current State

- 6 exchanges, parsers produce `IngestRequest` → `Envelope` → JetStream
- ProcessorSubsystemActor consumes envelopes, routes by type
- OrderBook uses `map[float64]float64` (HashMap, O(n) for sorted operations)
- Candle/Stats aggregation is deterministic (fixed-point math, sorted rollup)
- Multi-replica tolerance: 5s ts regression, seq gap <= 10

### 2.2 Target Architecture

```
                    ┌──────────────────────────────────┐
                    │      Exchange WS Adapters (6)     │
                    │  Binance│Bybit│CB│HL│Kraken│KrkF  │
                    └────────────┬─────────────────────┘
                                 │ IngestRequest
                    ┌────────────▼─────────────────────┐
                    │   MarketData BC (unchanged)       │
                    │   IngestMarketData → Envelope     │
                    │   idempotency_key + seq + ts      │
                    └────────────┬─────────────────────┘
                                 │ Envelope (JetStream)
                    ┌────────────▼─────────────────────┐
                    │   Aggregation BC (EVOLVED)        │
                    │                                   │
                    │  ┌─────────────────────────────┐  │
                    │  │ OrderBook (B-Tree)      NEW │  │
                    │  │ - O(log n) insert/lookup    │  │
                    │  │ - Range queries             │  │
                    │  │ - Depth rebalancing          │  │
                    │  │ - Cap: 500 levels/side       │  │
                    │  └─────────────────────────────┘  │
                    │                                   │
                    │  ┌─────────────────────────────┐  │
                    │  │ Watermark Window Manager NEW│  │
                    │  │ - Event-time windows         │  │
                    │  │ - Late arrival tolerance      │  │
                    │  │ - Deterministic close         │  │
                    │  │ - Cap: 96 open windows/key   │  │
                    │  └─────────────────────────────┘  │
                    │                                   │
                    │  ┌─────────────────────────────┐  │
                    │  │ Multi-TF Candle Rollup  OK  │  │
                    │  │ (1s→5s→1m→5m→15m→1h→4h→1d) │  │
                    │  └─────────────────────────────┘  │
                    │                                   │
                    │  ┌─────────────────────────────┐  │
                    │  │ Cross-Venue Book Merge  NEW │  │
                    │  │ - Synthetic global book      │  │
                    │  │ - Best bid/ask across venues │  │
                    │  │ - Spread signal generation   │  │
                    │  └─────────────────────────────┘  │
                    └────────────┬─────────────────────┘
                                 │ Artifacts (JetStream)
                    ┌────────────▼─────────────────────┐
                    │   Storage + Delivery (unchanged)  │
                    └──────────────────────────────────┘
```

### 2.3 New Contracts (Proto v2)

#### `aggregation/v2/orderbook.proto`
```protobuf
message OrderBookStateV2 {
  string venue = 1;
  string instrument = 2;
  uint64 seq = 3;
  int64  ts_exchange_ms = 4;
  repeated PriceLevelV2 bids = 5;  // sorted descending
  repeated PriceLevelV2 asks = 6;  // sorted ascending
  float  mid_price = 7;
  float  spread_bps = 8;
  uint32 depth_levels = 9;         // actual count
  BookHealth health = 10;
}

message PriceLevelV2 {
  int64 price_fp = 1;   // fixed-point (scale 1e8)
  int64 size_fp = 2;    // fixed-point (scale 1e8)
  uint32 order_count = 3;
}

enum BookHealth {
  BOOK_HEALTH_HEALTHY = 0;
  BOOK_HEALTH_CROSSED = 1;
  BOOK_HEALTH_STALE = 2;
  BOOK_HEALTH_THIN = 3;
}
```

#### `aggregation/v2/cross_venue_book.proto`
```protobuf
message CrossVenueBookSnapshotV1 {
  string instrument = 1;
  int64  ts_server_ms = 2;
  repeated VenueLevel best_bids = 3; // top N across venues
  repeated VenueLevel best_asks = 4;
  float  global_spread_bps = 5;
  float  venue_divergence_bps = 6;   // max spread diff across venues
}

message VenueLevel {
  string venue = 1;
  int64  price_fp = 2;
  int64  size_fp = 3;
}
```

### 2.4 Boundedness Strategy (Block A)

| State | Cap | TTL | Eviction | Config Key |
|-------|-----|-----|----------|------------|
| OrderBook levels per side | 500 | none (snapshot replaces) | prune furthest from mid | `aggregation.orderbook_max_levels` |
| Open candle windows per key | 96 | window duration + 30s grace | force-close oldest | `aggregation.candle_window_cap` |
| Cross-venue book venues | 6 (hardcoded exchange count) | 30s stale threshold | mark stale, exclude from merge | `aggregation.xvenue_stale_threshold_ms` |
| OrderBook snapshot buffer | 1 per {venue, instrument} | replaced on each snapshot | overwrite | N/A (latest-wins) |

### 2.5 Required Metrics (Block A)

```
# OrderBook
mr_orderbook_levels_total{venue,instrument,side}         gauge
mr_orderbook_spread_bps{venue,instrument}                 gauge
mr_orderbook_update_duration_seconds{venue}               histogram
mr_orderbook_prune_total{venue,instrument}                counter
mr_orderbook_crossed_total{venue,instrument}              counter
mr_orderbook_stale_total{venue,instrument}                counter

# Cross-Venue
mr_xvenue_spread_bps{instrument}                          gauge
mr_xvenue_divergence_bps{instrument}                      gauge
mr_xvenue_merge_duration_seconds{instrument}              histogram
mr_xvenue_venues_active{instrument}                       gauge

# Watermark Windows
mr_window_open_total{venue,instrument,tf}                 gauge
mr_window_late_arrival_total{venue,instrument,tf}         counter
mr_window_force_close_total{venue,instrument,tf}          counter
```

### 2.6 Deterministic Tests (Block A)

| Test | Input | Expected | Gate |
|------|-------|----------|------|
| `TestBTreeOrderBook_DeterministicReplay` | 10K book deltas (golden file) | Bit-identical final state | `make test` |
| `TestBTreeOrderBook_PruneAtCap` | 600 levels inserted (cap=500) | Exactly 500 levels, furthest pruned | `make test` |
| `TestCrossVenueBook_MergeOrder` | 3 venues, overlapping prices | Deterministic merged book | `make test` |
| `TestWatermarkWindow_LateArrival` | Events with ts < watermark | Correctly assigned or dropped | `make test` |
| `TestWatermarkWindow_ForcedClose` | 97 open windows (cap=96) | Oldest closed, 96 remaining | `make test` |
| `TestOrderBook_HashMap_vs_BTree` | Same 10K deltas | Identical final state (migration validation) | Migration gate only |

### 2.7 Legacy Removal (Block A)

| Legacy Code | Strangler Wrapper | Removal Gate |
|-------------|-------------------|--------------|
| `map[float64]float64` in OrderBook | `OrderBookV2` with B-Tree behind same `ApplyDelta/ApplySnapshot` interface | All golden tests pass with V2 + 0 diff in output |
| Inline window management in ProcessorSubsystemActor | `WatermarkWindowManager` injected via constructor | ProcessorSubsystemActor tests pass with new manager |
| Manual cross-venue join in `JoinCrossVenueTrades` | `CrossVenueBookMerger` domain service | Cross-venue tests pass + spread signal equivalent |

---

## 3. Block B — Liquidity Intelligence Engine

### 3.1 Current State

- Heatmap (2.5% binning) — production-ready
- VPVR (0.5% binning, overload policy) — production-ready
- CrossVenue trade snapshot — production-ready
- CrossVenue spread signal — production-ready (flag-gated)
- Evidence BC — placeholder (proto defined, domain skeleton)

### 3.2 Target Architecture

```
                    ┌──────────────────────────────────┐
                    │    Aggregation Artifacts          │
                    │  (OrderBook, Candle, Stats)       │
                    └──────┬───────────┬───────────────┘
                           │           │
              ┌────────────▼──┐  ┌─────▼──────────────┐
              │  Microstructure│  │  Regime Detection   │
              │  Analyzer      │  │  Engine             │
              │                │  │                     │
              │  - Imbalance   │  │  - Volatility regime│
              │  - Absorption  │  │  - Trend strength   │
              │  - Spoofing    │  │  - Mean reversion   │
              │  - Sweep detect│  │  - Breakout scoring  │
              │                │  │                     │
              │  Evidence:     │  │  Evidence:          │
              │  per-event     │  │  per-window         │
              └───────┬────────┘  └──────┬─────────────┘
                      │                  │
              ┌───────▼──────────────────▼─────────────┐
              │   Evidence Aggregator                   │
              │   - Conflation (same kind, same window) │
              │   - Ring buffer (1000/kind cap)         │
              │   - Severity escalation (Low→Critical)  │
              │   - Confidence decay (time-weighted)     │
              └───────────────┬────────────────────────┘
                              │ EvidenceEvent
              ┌───────────────▼────────────────────────┐
              │   Evidence Publisher (JetStream)        │
              │   subject: evidence.v1.{kind}.{venue}   │
              └────────────────────────────────────────┘
```

### 3.3 New Domain Models

#### Microstructure Detector (`internal/core/evidence/domain/`)

```go
// Absorption: large resting orders consumed without price movement
type AbsorptionSignal struct {
    Venue       string
    Instrument  string
    Side        string    // "bid" or "ask"
    PriceFP     int64     // fixed-point
    VolumeAbsorbed int64  // fixed-point
    OrderCount  int32
    DurationMs  int64
    Confidence  float64   // [0, 1]
    SeqFirst    uint64
    SeqLast     uint64
}

// Imbalance: persistent bid/ask volume asymmetry
type ImbalanceSignal struct {
    Venue       string
    Instrument  string
    BidVolumeFP int64
    AskVolumeFP int64
    Ratio       float64   // bid/ask ratio
    Depth       int32     // levels considered
    DurationMs  int64
    Confidence  float64
}

// SweepDetection: rapid multi-level consumption
type SweepSignal struct {
    Venue       string
    Instrument  string
    Side        string
    LevelsSwept int32
    TotalVolume int64     // fixed-point
    DurationMs  int64     // time to sweep
    PriceImpact int64     // fixed-point bps
    Confidence  float64
}
```

#### Regime Detector (`internal/core/evidence/domain/`)

```go
type RegimeKind string

const (
    RegimeTrending      RegimeKind = "trending"
    RegimeRanging       RegimeKind = "ranging"
    RegimeBreakout      RegimeKind = "breakout"
    RegimeHighVolatility RegimeKind = "high_volatility"
    RegimeLowVolatility  RegimeKind = "low_volatility"
)

type RegimeSignal struct {
    Venue       string
    Instrument  string
    Timeframe   string
    Kind        RegimeKind
    Strength    float64   // [0, 1]
    Confidence  float64   // [0, 1]
    WindowStart int64     // ms
    WindowEnd   int64     // ms
    Features    []FeaturePair // name/value evidence
}
```

### 3.4 Contracts (Proto)

#### `evidence/v2/microstructure.proto`
```protobuf
message MicrostructureEvidenceV2 {
  string kind = 1;           // absorption|imbalance|sweep|spoofing
  int64  ts_server_ms = 2;
  string venue = 3;
  string instrument = 4;
  Severity severity = 5;
  double confidence = 6;     // [0, 1]
  repeated Feature features = 7;
  string reason = 8;
  uint64 seq_first = 9;
  uint64 seq_last = 10;
  int64  duration_ms = 11;   // NEW: how long the pattern persisted
  int64  price_impact_bps = 12; // NEW: price movement caused
}

message RegimeEvidenceV1 {
  string kind = 1;           // trending|ranging|breakout|high_vol|low_vol
  int64  ts_server_ms = 2;
  string venue = 3;
  string instrument = 4;
  string timeframe = 5;
  double strength = 6;       // [0, 1]
  double confidence = 7;     // [0, 1]
  int64  window_start_ms = 8;
  int64  window_end_ms = 9;
  repeated Feature features = 10;
}
```

### 3.5 Boundedness Strategy (Block B)

| State | Cap | TTL | Eviction | Config Key |
|-------|-----|-----|----------|------------|
| Evidence ring buffer per kind | 1000 | 5min auto-expire | overwrite oldest | `evidence.buffer_cap` |
| Active microstructure windows | 50 per {venue, instrument} | window duration + 10s | force-close | `evidence.micro_window_cap` |
| Regime history per key | 20 | 1h | drop oldest | `evidence.regime_history_cap` |
| Feature accumulator | 100 features/event | N/A | truncate | hardcoded (domain invariant) |
| Confidence decay half-life | N/A | 60s | exponential decay | `evidence.confidence_decay_hl_ms` |

### 3.6 Required Metrics (Block B)

```
# Microstructure
mr_evidence_detected_total{kind,venue,instrument,severity}     counter
mr_evidence_confidence{kind,venue,instrument}                   gauge
mr_evidence_duration_seconds{kind,venue}                        histogram
mr_evidence_buffer_size{kind}                                   gauge
mr_evidence_buffer_evicted_total{kind}                          counter

# Regime
mr_regime_current{venue,instrument,tf}                          gauge (enum label)
mr_regime_strength{venue,instrument,tf}                         gauge
mr_regime_transition_total{venue,instrument,from,to}            counter
mr_regime_detection_duration_seconds{venue,instrument}          histogram

# Pipeline Health
mr_evidence_publish_duration_seconds{kind}                      histogram
mr_evidence_publish_errors_total{kind,error_code}               counter
```

### 3.7 Deterministic Tests (Block B)

| Test | Input | Expected | Gate |
|------|-------|----------|------|
| `TestAbsorption_DeterministicDetection` | Golden book deltas (large bid absorbed) | Exact confidence, duration, volume | `make test` |
| `TestImbalance_RatioCalculation` | 10 bid levels, 3 ask levels with known volumes | Exact ratio to 6 decimal places | `make test` |
| `TestSweep_MultiLevel` | 5 ask levels consumed in 200ms | LevelsSwept=5, correct PriceImpact | `make test` |
| `TestRegime_Trending` | 20 candles with monotonic close increase | Kind=trending, Strength > 0.8 | `make test` |
| `TestRegime_Transition` | Trending→Ranging sequence | Correct transition event | `make test` |
| `TestEvidenceBuffer_RingOverwrite` | 1001 events (cap=1000) | Oldest overwritten, newest preserved | `make test` |
| `TestConfidenceDecay_Deterministic` | Event at t=0, query at t=30s, t=60s | Exact decayed values (inject clock) | `make test` |

### 3.8 Legacy Removal (Block B)

| Legacy Code | Strangler Wrapper | Removal Gate |
|-------------|-------------------|--------------|
| `evidence/domain/evidence.go` (skeleton) | Replace with full `MicrostructureEvidenceV2` + `RegimeEvidenceV1` | New domain passes all existing evidence tests |
| Inline spread check in `ProcessorSubsystemActor` | Extract to `MicrostructureAnalyzer` domain service | Identical spread signals produced |
| `JoinCrossVenueTrades` standalone function | Integrate into `CrossVenueBookMerger` (Block A) | Cross-venue test suite passes |

---

## 4. Block C — Signal Engine via WebSocket

### 4.1 Design Principle

> **Signals are decision-support evidence, NOT execution instructions.**
> The Signal Engine delivers structured, auditable intelligence to connected clients.
> It NEVER issues buy/sell directives. (ADR-0008)

### 4.2 Target Architecture

```
              ┌──────────────────────────────────────┐
              │  Evidence Publisher (Block B)          │
              │  subject: evidence.v1.{kind}.{venue}   │
              └─────────┬────────────────────────────┘
                        │
              ┌─────────▼────────────────────────────┐
              │  Signal Composer                       │
              │                                       │
              │  Inputs:                              │
              │  - MicrostructureEvidence (Block B)   │
              │  - RegimeEvidence (Block B)           │
              │  - CrossVenueBook (Block A)           │
              │  - Candle/Stats (existing)            │
              │                                       │
              │  Processing:                          │
              │  1. Multi-evidence correlation         │
              │  2. Confidence aggregation             │
              │  3. Deduplication (same signal window) │
              │  4. Rate limiting (max N/min/key)      │
              │                                       │
              │  Output: CompositeSignal              │
              └─────────┬────────────────────────────┘
                        │
              ┌─────────▼────────────────────────────┐
              │  Signal Delivery (WS Extension)       │
              │                                       │
              │  New subject pattern:                 │
              │  signal.v1.{kind}.{venue}.{instrument}│
              │                                       │
              │  Client subscribes:                   │
              │  {"op":"subscribe",                   │
              │   "subject":"signal/absorption/       │
              │     binance/BTC-USDT/1m"}             │
              │                                       │
              │  Server delivers:                     │
              │  {"type":"signal",                    │
              │   "kind":"absorption",                │
              │   "confidence":0.87,                  │
              │   "severity":"high",                  │
              │   "evidence":[...],                   │
              │   "ts_server":1709481600000}          │
              └──────────────────────────────────────┘
```

### 4.3 New Contracts

#### WS Signal Payload (JSON, future CBOR)
```json
{
  "type": "signal",
  "subject": "signal/absorption/binance/BTC-USDT/1m",
  "seq": 42,
  "ts_server": 1709481600000,
  "payload": {
    "kind": "absorption",
    "venue": "binance",
    "instrument": "BTC-USDT",
    "timeframe": "1m",
    "severity": "high",
    "confidence": 0.87,
    "evidence": [
      {"label": "volume_absorbed", "value": "125.5"},
      {"label": "price_level", "value": "67250.00"},
      {"label": "duration_ms", "value": "3200"},
      {"label": "order_count", "value": "47"}
    ],
    "regime": "trending",
    "regime_strength": 0.72,
    "reason": "Large bid absorption at support with trending regime"
  }
}
```

#### `signals/v1/composite.proto`
```protobuf
message CompositeSignalV1 {
  string kind = 1;              // primary signal type
  string venue = 2;
  string instrument = 3;
  string timeframe = 4;
  int64  ts_server_ms = 5;
  Severity severity = 6;
  double confidence = 7;        // [0, 1] — aggregated
  repeated Feature evidence = 8;
  string regime_kind = 9;       // current regime context
  double regime_strength = 10;
  string reason = 11;           // human-readable explanation
  uint64 seq = 12;
  repeated string source_kinds = 13; // evidence kinds that contributed
}
```

### 4.4 Signal Composition Rules

```
Rule 1: Single-evidence signal
  IF microstructure.confidence > 0.7 AND severity >= Medium
  THEN emit CompositeSignal(kind=micro.kind, confidence=micro.confidence)

Rule 2: Regime-boosted signal
  IF microstructure.confidence > 0.5
  AND regime.kind matches (e.g., absorption + trending)
  AND regime.strength > 0.6
  THEN emit CompositeSignal(confidence = micro.confidence * (1 + 0.2 * regime.strength))
  CAP confidence at 0.99

Rule 3: Cross-venue confirmation
  IF same micro.kind detected on >= 2 venues within 5s window
  THEN emit CompositeSignal(confidence = max(confidences) * 1.15)
  ADD evidence from all venues
  CAP confidence at 0.99

Rule 4: Deduplication
  IF same kind + venue + instrument + timeframe emitted within last 30s
  THEN skip (unless confidence > previous * 1.2)

Rule 5: Rate limiting
  MAX 10 signals per minute per {venue, instrument}
  MAX 100 signals per minute globally
```

### 4.5 Boundedness Strategy (Block C)

| State | Cap | TTL | Eviction | Config Key |
|-------|-----|-----|----------|------------|
| Signal dedup window | 50 per key | 30s | time-based expiry | `signals.dedup_window_ms` |
| Signal rate counter | per {venue, instrument} | 60s sliding | reset on window slide | `signals.rate_limit_per_min` |
| Global rate counter | 1 | 60s sliding | reset | `signals.global_rate_limit_per_min` |
| Composition buffer | 100 pending correlations | 5s correlation window | force-emit or discard | `signals.correlation_window_ms` |
| Client signal subscriptions | 20 per session | session lifetime | reject new | `signals.max_subs_per_session` |

### 4.6 Required Metrics (Block C)

```
# Signal Composition
mr_signal_emitted_total{kind,venue,instrument,severity}        counter
mr_signal_deduplicated_total{kind,venue,instrument}            counter
mr_signal_rate_limited_total{venue,instrument}                  counter
mr_signal_composition_duration_seconds{kind}                    histogram
mr_signal_confidence_distribution{kind}                         histogram

# Signal Delivery
mr_signal_delivered_total{kind,session_id}                      counter
mr_signal_delivery_latency_seconds{kind}                        histogram
mr_signal_subscribers_active{kind}                              gauge

# Signal Quality
mr_signal_correlation_hit_total{kind}                           counter  (rule 3 fired)
mr_signal_regime_boost_total{kind,regime}                       counter  (rule 2 fired)
```

### 4.7 Deterministic Tests (Block C)

| Test | Input | Expected | Gate |
|------|-------|----------|------|
| `TestSignalComposer_SingleEvidence` | Absorption at confidence=0.8 | Signal emitted with confidence=0.8 | `make test` |
| `TestSignalComposer_RegimeBoost` | Absorption(0.6) + Trending(0.8) | Signal at confidence=0.6*(1+0.2*0.8)=0.696 | `make test` |
| `TestSignalComposer_CrossVenue` | Same absorption on binance+bybit within 5s | Merged signal, confidence boosted 15% | `make test` |
| `TestSignalComposer_Dedup` | Same signal twice within 30s | Second dropped | `make test` |
| `TestSignalComposer_RateLimit` | 11 signals in 1 minute | 11th dropped, counter incremented | `make test` |
| `TestSignalDelivery_WS` | Client subscribes to `signal/absorption/binance/BTC-USDT/1m` | Receives matching signal JSON | `make test` |
| `TestSignalDelivery_NoAutoExecution` | Any signal payload | No `action`, `order`, or `execute` field exists | `make test` |

### 4.8 Legacy Removal (Block C)

| Legacy Code | Strangler Wrapper | Removal Gate |
|-------------|-------------------|--------------|
| Inline cross-venue spread signal in ProcessorSubsystemActor | Route through SignalComposer | `processor.go` no longer directly emits spread signals |
| `EnableSpreadSignal` flag | Replace with per-signal-kind config in `signals.*` namespace | All signal tests pass under new config |

---

## 5. Phased Roadmap (CODEX-Executable)

### Phase Conventions

- **Commit size:** Max 300 LOC changed per commit. Prefer 50-150 LOC.
- **No soak tests:** Unit + golden tests only. Soak is a separate, manual gate.
- **Validation trigger:** `make test` must pass after every commit.
- **Feature flags:** All new behavior behind config flags. Default = off.

---

### Phase 1: Foundation Hardening (Block A prerequisite)

> **Goal:** B-Tree OrderBook + Boundedness caps

| Step | Description | Files | DoD | Commit |
|------|-------------|-------|-----|--------|
| 1.1 | Define `OrderBookV2` interface in domain | `aggregation/domain/orderbook_v2.go` | Interface compiles, existing tests pass | `feat(aggregation): define OrderBookV2 interface with B-Tree contract` |
| 1.2 | Implement B-Tree OrderBook (insert, lookup, range) | `aggregation/domain/btree_orderbook.go` | 10 unit tests pass (insert, lookup, sorted iteration, range) | `feat(aggregation): implement B-Tree OrderBook with sorted levels` |
| 1.3 | Add level cap + pruning logic | `aggregation/domain/btree_orderbook.go` | Cap test: 600 inserts → 500 levels | `feat(aggregation): add level cap and furthest-from-mid pruning` |
| 1.4 | Port `ApplyDelta/ApplySnapshot` to V2 | `aggregation/domain/btree_orderbook.go` | Golden replay test (10K deltas) matches V1 output | `feat(aggregation): port ApplyDelta/ApplySnapshot to B-Tree` |
| 1.5 | Add config keys for caps | `deploy/configs/server.jsonc`, `internal/shared/config/` | Config loads, schema validates | `feat(config): add aggregation boundedness cap config keys` |
| 1.6 | Wire V2 into ProcessorSubsystemActor behind flag | `actors/aggregation/runtime/processor.go` | Flag off = V1, flag on = V2, both pass existing tests | `feat(aggregation): wire OrderBookV2 behind feature flag` |
| 1.7 | Add OrderBook metrics | `actors/aggregation/runtime/processor.go` | 6 metrics registered, test increments verified | `feat(metrics): add orderbook level/spread/prune metrics` |
| 1.8 | Remove V1 OrderBook (flag → on, remove old code) | `aggregation/domain/orderbook.go` (delete) | All tests pass, no references to old map-based book | `refactor(aggregation): remove legacy HashMap OrderBook` |

**Phase 1 DoD:** `make test` passes. OrderBook is B-Tree. All caps enforced. Metrics emitting.

---

### Phase 2: Watermark Window Manager (Block A)

| Step | Description | Files | DoD | Commit |
|------|-------------|-------|-----|--------|
| 2.1 | Define `WindowManager` interface | `aggregation/domain/window_manager.go` | Interface compiles | `feat(aggregation): define WindowManager interface` |
| 2.2 | Implement watermark-based window lifecycle | `aggregation/domain/watermark_window.go` | Open/close/late-arrival tests pass | `feat(aggregation): implement watermark window manager` |
| 2.3 | Add window cap + force-close logic | `aggregation/domain/watermark_window.go` | 97 windows (cap=96) → oldest closed | `feat(aggregation): add window cap with force-close eviction` |
| 2.4 | Wire into candle/stats aggregation | `aggregation/app/build_candle.go`, `build_stats.go` | Existing candle/stats golden tests pass | `feat(aggregation): wire watermark windows into candle/stats builds` |
| 2.5 | Add window metrics | `actors/aggregation/runtime/processor.go` | 3 metrics registered | `feat(metrics): add window open/late/force-close metrics` |

**Phase 2 DoD:** `make test` passes. Windows bounded. Late arrivals handled deterministically.

---

### Phase 3: Cross-Venue Book Merger (Block A)

| Step | Description | Files | DoD | Commit |
|------|-------------|-------|-----|--------|
| 3.1 | Define `CrossVenueBookMerger` domain service | `aggregation/domain/cross_venue_book.go` | Interface + VenueLevel VO defined | `feat(aggregation): define CrossVenueBook domain model` |
| 3.2 | Implement merge logic (best bid/ask across venues) | `aggregation/domain/cross_venue_book.go` | 5 unit tests (2 venues, 3 venues, stale venue, empty venue, spread calc) | `feat(aggregation): implement cross-venue book merge` |
| 3.3 | Add stale venue detection (30s threshold) | `aggregation/domain/cross_venue_book.go` | Stale venue excluded from merge | `feat(aggregation): add stale venue detection in cross-book` |
| 3.4 | Define proto `CrossVenueBookSnapshotV1` | `proto/aggregation/v2/cross_venue_book.proto` | Proto compiles | `feat(proto): define CrossVenueBookSnapshotV1` |
| 3.5 | Wire into ProcessorSubsystemActor | `actors/aggregation/runtime/processor.go` | Cross-venue snapshot published on book update | `feat(aggregation): wire cross-venue merger into processor` |
| 3.6 | Add cross-venue metrics | metrics files | 4 metrics registered | `feat(metrics): add cross-venue spread/divergence metrics` |

**Phase 3 DoD:** `make test` passes. Cross-venue book merges on every book update. Stale venues excluded.

---

### Phase 4: Microstructure Detection (Block B)

| Step | Description | Files | DoD | Commit |
|------|-------------|-------|-----|--------|
| 4.1 | Define microstructure signal VOs | `evidence/domain/microstructure.go` | Domain models compile, invariants validated | `feat(evidence): define microstructure signal domain models` |
| 4.2 | Implement absorption detector | `evidence/app/detect_absorption.go` | Golden test: known absorption pattern → correct signal | `feat(evidence): implement absorption detection` |
| 4.3 | Implement imbalance detector | `evidence/app/detect_imbalance.go` | Golden test: known imbalance → correct ratio | `feat(evidence): implement imbalance detection` |
| 4.4 | Implement sweep detector | `evidence/app/detect_sweep.go` | Golden test: 5-level sweep → correct signal | `feat(evidence): implement sweep detection` |
| 4.5 | Implement evidence ring buffer | `evidence/domain/evidence_buffer.go` | 1001 inserts (cap=1000) → oldest overwritten | `feat(evidence): implement bounded ring buffer for evidence` |
| 4.6 | Add confidence decay (time-weighted) | `evidence/domain/confidence_decay.go` | Deterministic decay at t=30s, t=60s (clock injected) | `feat(evidence): add time-weighted confidence decay` |
| 4.7 | Wire detectors into EvidenceSubsystemActor | `actors/evidence/runtime/subsystem.go` | Actor receives book updates, emits evidence | `feat(evidence): wire microstructure detectors into actor` |
| 4.8 | Add evidence metrics | metrics files | 5 metrics registered | `feat(metrics): add evidence detection/buffer metrics` |

**Phase 4 DoD:** `make test` passes. 3 microstructure detectors producing evidence. Ring buffer bounded.

---

### Phase 5: Regime Detection (Block B)

| Step | Description | Files | DoD | Commit |
|------|-------------|-------|-----|--------|
| 5.1 | Define regime signal VOs | `evidence/domain/regime.go` | Domain models compile | `feat(evidence): define regime signal domain models` |
| 5.2 | Implement volatility regime detector | `evidence/app/detect_volatility_regime.go` | High/low volatility correctly classified from ATR data | `feat(evidence): implement volatility regime detection` |
| 5.3 | Implement trend regime detector | `evidence/app/detect_trend_regime.go` | Trending/ranging correctly classified from candle closes | `feat(evidence): implement trend regime detection` |
| 5.4 | Implement breakout detector | `evidence/app/detect_breakout.go` | Breakout scored from volume spike + price movement | `feat(evidence): implement breakout detection` |
| 5.5 | Add regime history bounded store | `evidence/domain/regime_store.go` | 21 inserts (cap=20) → oldest dropped | `feat(evidence): add bounded regime history store` |
| 5.6 | Wire regime detectors into Evidence actor | `actors/evidence/runtime/subsystem.go` | Actor receives candle closes, emits regime evidence | `feat(evidence): wire regime detectors into actor` |
| 5.7 | Add regime metrics | metrics files | 4 metrics registered | `feat(metrics): add regime detection/transition metrics` |

**Phase 5 DoD:** `make test` passes. Regime detection emitting. History bounded.

---

### Phase 6: Signal Composer (Block C)

| Step | Description | Files | DoD | Commit |
|------|-------------|-------|-----|--------|
| 6.1 | Define `CompositeSignalV1` domain model | `signals/domain/composite_signal.go` (new BC) | Model compiles, invariants validated | `feat(signals): define CompositeSignal domain model` |
| 6.2 | Implement single-evidence composition (Rule 1) | `signals/app/compose_signal.go` | Test: micro(0.8) → signal(0.8) | `feat(signals): implement single-evidence signal composition` |
| 6.3 | Implement regime-boost composition (Rule 2) | `signals/app/compose_signal.go` | Test: micro(0.6)+trend(0.8) → signal(0.696) | `feat(signals): implement regime-boosted signal composition` |
| 6.4 | Implement cross-venue confirmation (Rule 3) | `signals/app/compose_signal.go` | Test: 2 venues → boosted confidence | `feat(signals): implement cross-venue signal confirmation` |
| 6.5 | Implement dedup + rate limiting (Rules 4-5) | `signals/app/signal_rate_limiter.go` | Test: dedup within 30s, rate limit at 10/min | `feat(signals): implement signal dedup and rate limiting` |
| 6.6 | Define proto `CompositeSignalV1` | `proto/signals/v1/composite.proto` | Proto compiles | `feat(proto): define CompositeSignalV1` |
| 6.7 | Wire into new SignalSubsystemActor | `actors/signals/runtime/subsystem.go` | Actor receives evidence, emits signals | `feat(signals): create SignalSubsystemActor` |
| 6.8 | Add signal metrics | metrics files | 6 metrics registered | `feat(metrics): add signal composition/delivery metrics` |

**Phase 6 DoD:** `make test` passes. Signals composed from evidence. Rate-limited. No execution fields.

---

### Phase 7: Signal Delivery via WS (Block C)

| Step | Description | Files | DoD | Commit |
|------|-------------|-------|-----|--------|
| 7.1 | Add `signal` subject pattern to delivery router | `actors/delivery/runtime/router.go` | Router accepts `signal.v1.*` subjects | `feat(delivery): add signal subject routing` |
| 7.2 | Add signal subscription handling | `actors/delivery/runtime/session.go` | Client can subscribe to `signal/{kind}/{venue}/{instrument}/{tf}` | `feat(delivery): add signal subscription handling` |
| 7.3 | Implement signal→WS JSON serialization | `interfaces/ws/signal_frame.go` | Signal frame matches contract spec exactly | `feat(interfaces): implement signal WS frame serialization` |
| 7.4 | Add signal subscription cap (20/session) | `delivery/domain/session.go` | 21st signal sub rejected | `feat(delivery): enforce signal subscription cap` |
| 7.5 | Add signal delivery metrics | metrics files | 3 metrics registered | `feat(metrics): add signal WS delivery metrics` |
| 7.6 | Add safety test: no execution fields | test file | Verify no `action/order/execute/buy/sell` in any signal payload | `test(signals): verify no execution fields in signal payloads` |

**Phase 7 DoD:** `make test` passes. Signals delivered via WS. Subscription caps enforced. Safety verified.

---

### Phase 8: Client Integration (Odin)

| Step | Description | Files | DoD | Commit |
|------|-------------|-------|-----|--------|
| 8.1 | Add signal frame parser (Odin) | `client/src/core/app/signal_frame.odin` | Parses signal JSON correctly | `feat(client): add signal frame parser` |
| 8.2 | Add signal subscription to WS manager | `client/src/core/streams/stream_controller.odin` | Can subscribe/unsubscribe to signal subjects | `feat(client): add signal subscription support` |
| 8.3 | Add signal store (ring buffer, 50/kind) | `client/src/core/app/signal_store.odin` | Bounded store, oldest overwritten | `feat(client): add bounded signal store` |
| 8.4 | Add signal overlay widget | `client/src/core/widgets/signal_overlay.odin` | Renders signal badges on chart | `feat(client): add signal overlay widget` |
| 8.5 | Add signal panel (list view) | `client/src/core/widgets/signal_panel.odin` | Lists active signals with confidence bars | `feat(client): add signal panel widget` |

**Phase 8 DoD:** Client renders signals. No execution UI. Stores bounded.

---

### Phase Summary

```
Phase 1  ████████  Foundation (B-Tree OB)         Block A   ~8 commits
Phase 2  █████     Watermark Windows               Block A   ~5 commits
Phase 3  ██████    Cross-Venue Book                 Block A   ~6 commits
Phase 4  ████████  Microstructure Detection         Block B   ~8 commits
Phase 5  ███████   Regime Detection                 Block B   ~7 commits
Phase 6  ████████  Signal Composer                  Block C   ~8 commits
Phase 7  ██████    Signal WS Delivery               Block C   ~6 commits
Phase 8  █████     Client Integration               Block C   ~5 commits
─────────────────────────────────────────────────────────────
Total                                                         ~53 commits
```

### Dependency Graph

```
Phase 1 ──→ Phase 2 ──→ Phase 3 ──┐
                                    ├──→ Phase 6 ──→ Phase 7 ──→ Phase 8
Phase 4 ──→ Phase 5 ──────────────┘

Block A: Phases 1 → 2 → 3
Block B: Phases 4 → 5
Block C: Phases 6 → 7 → 8 (requires A.3 + B.5)

Parallelizable: Phase 1-3 (Block A) || Phase 4-5 (Block B)
Sequential: Phase 6 requires both Block A + Block B complete
```

---

## 6. Technical Risks & Mitigations

### Risk 1: Cardinality Explosion

**Threat:** Metrics with `{venue, instrument}` labels → 6 venues × N instruments × M metric types.

**Current state:** ~20 instruments per venue → 120 combinations. Manageable.

**Growth danger:** If instruments grow to 200+, label cardinality reaches 1200+ per metric.

**Mitigation:**
- CAP: `max_instruments_per_venue = 50` in config (enforced at subscription time)
- AGGREGATE: Venue-level aggregation for high-cardinality metrics (e.g., `mr_evidence_detected_total` → aggregate by venue, not instrument)
- PRUNE: Metrics with 0 value for > 1h auto-deregistered via `prometheus.Unregister`
- GATE: `make lint-metrics` checks label cardinality < 500 unique series

### Risk 2: Memory Leaks in Long-Running Actors

**Threat:** Maps/slices in actors grow over days without eviction.

**Current state:** TranscodeCache has LRU eviction. OrderBook has no cap. Evidence buffer is a placeholder.

**Mitigation:**
- ALL maps in actors MUST have a cap declared in config (INV-BND-01)
- Guardian periodic Snapshot message reports map sizes → alert if growing
- `mr_actor_state_size_bytes{subsystem}` gauge reported every 30s
- Every Phase DoD includes: "No unbounded state introduced"

### Risk 3: Non-Determinism in Regime Detection

**Threat:** Floating-point calculations in regime strength/confidence produce different results across platforms.

**Mitigation:**
- Use fixed-point where possible (int64 with scale factor)
- Where float64 is necessary (confidence, strength), round to 6 decimal places at output boundary
- Golden tests with exact expected values (not epsilon comparison)
- `math.Round(v*1e6)/1e6` at every domain model constructor

### Risk 4: Latency Spike from Evidence Pipeline

**Threat:** Microstructure detection adds latency to the hot path (book update → evidence → signal → delivery).

**Mitigation:**
- Evidence detection runs in **separate goroutine** within EvidenceSubsystemActor (not inline in aggregation)
- Budget: evidence detection MUST complete in < 1ms p99 (measured via `mr_evidence_duration_seconds`)
- If p99 > 1ms for 5 consecutive windows → circuit breaker disables detection for 30s
- Signal composition is **async** (reads from evidence bus, not inline)

### Risk 5: Backend↔Client Drift

**Threat:** Signal JSON schema changes on backend but client Odin parser not updated → silent data loss.

**Mitigation:**
- **Contract test:** `TestSignalFrame_BackendClientParity` in Go generates a signal JSON, Odin parser must decode all fields
- **Version field:** Every signal frame includes `"schema_version": 1`. Client rejects unknown versions.
- **Schema registry:** `internal/shared/contracts/signal_registry.go` defines canonical field list. Both Go serializer and Odin parser reference same field names (verified in CI).
- **Backward compatibility:** New fields are additive only. Removed fields go through deprecation (1 version with `_deprecated` suffix, then removal).

### Risk 6: Signal Rate Amplification

**Threat:** 6 venues × N instruments × multiple signal kinds → hundreds of signals/second overwhelming clients.

**Mitigation:**
- Rate limit: 10 signals/min per {venue, instrument} (configurable)
- Global cap: 100 signals/min total
- Client-side: ring buffer (50/kind) prevents unbounded UI state
- Dedup window: 30s per {kind, venue, instrument, tf}
- Severity filter: client can subscribe with `min_severity=high` filter

### Risk 7: Cross-Venue Clock Skew

**Threat:** Exchange timestamps differ by seconds → cross-venue book merge produces incorrect "stale" classifications.

**Mitigation:**
- Use `ts_ingest` (server-side monotonic) for staleness, NOT `ts_exchange`
- Stale threshold: 30s (configurable) — conservative enough for any exchange lag
- `mr_xvenue_clock_skew_seconds{venue}` gauge tracks max(ts_ingest - ts_exchange) per venue
- Alert if skew > 10s sustained for 5 minutes

### Risk 8: Protobuf Version Incompatibility

**Threat:** Proto v2 messages consumed by code expecting v1 → marshal errors or silent field drops.

**Mitigation:**
- Version in subject: `evidence.v1.*` vs `evidence.v2.*` — consumers filter by subject
- Proto fields are additive only (never renumber or remove)
- `converter_completeness_test.go` validates all proto→domain conversions
- Rollout flag: `proto_version = 1` in config → only v1 published until explicitly switched

---

## 7. Legacy Strangler Strategy

### Principles

1. **Feature flag first:** Every new path is gated by a config flag (default: off)
2. **Parallel run:** New and old paths run simultaneously during validation
3. **Output comparison:** Automated test compares old vs new output for N golden inputs
4. **Flag flip:** Once comparison passes, flip flag to new path
5. **Cleanup commit:** Remove old path + feature flag in a separate commit
6. **No hybrid state:** A flag is either fully-on or fully-off. No partial migration.

### Execution Template

```
Commit 1: feat(X): define new interface + domain model
Commit 2: feat(X): implement new path behind flag (flag=off)
Commit 3: test(X): add golden comparison test (old vs new)
Commit 4: feat(X): wire new path into actor (flag=off, parallel run)
  → Validation: `make test` + golden comparison passes
Commit 5: feat(config): flip flag to new path (flag=on)
  → Validation: `make test` + all existing tests pass
Commit 6: refactor(X): remove legacy path + feature flag
  → Validation: `make test` + no references to old code
```

### Tracked Migrations

| Migration | Phase | Flag Key | Status |
|-----------|-------|----------|--------|
| HashMap → B-Tree OrderBook | 1 | `aggregation.use_btree_orderbook` | Pending |
| Inline windows → WatermarkWindowManager | 2 | `aggregation.use_watermark_windows` | Pending |
| Inline cross-venue → CrossVenueBookMerger | 3 | `aggregation.use_xvenue_merger` | Pending |
| Skeleton evidence → Full microstructure | 4 | `evidence.enable_microstructure` | Pending |
| Inline spread signal → SignalComposer | 6 | `signals.use_composer` | Pending |

---

## Appendix A: Config Schema Additions

```jsonc
// deploy/configs/server.jsonc — new keys
{
  "aggregation": {
    "orderbook_max_levels": 500,
    "candle_window_cap": 96,
    "stats_window_cap": 96,
    "xvenue_stale_threshold_ms": 30000,
    "use_btree_orderbook": false,
    "use_watermark_windows": false,
    "use_xvenue_merger": false
  },
  "evidence": {
    "enable_microstructure": false,
    "buffer_cap": 1000,
    "micro_window_cap": 50,
    "regime_history_cap": 20,
    "confidence_decay_hl_ms": 60000
  },
  "signals": {
    "use_composer": false,
    "dedup_window_ms": 30000,
    "rate_limit_per_min": 10,
    "global_rate_limit_per_min": 100,
    "correlation_window_ms": 5000,
    "max_subs_per_session": 20,
    "window_cap": 50
  }
}
```

## Appendix B: Validation Gates per Commit

Every commit MUST satisfy:

```bash
# Gate 1: Compilation
go build ./...

# Gate 2: Tests
make test

# Gate 3: Invariants
make invariants-check

# Gate 4: No unbounded state (manual review aid)
grep -r "make(map\[" internal/core/ internal/actors/ | grep -v "_test.go"
# → Every hit must have a corresponding cap constant or eviction comment

# Gate 5: No time.Now() in domain
grep -rn "time.Now()" internal/core/
# → Must return zero results

# Gate 6: Metrics cardinality
# All new metrics with {venue,instrument} labels documented in this plan
```

## Appendix C: New Bounded Contexts

This plan introduces **1 new BC**:

### `internal/core/signals/` (Block C, Phase 6)

```
internal/core/signals/
├── domain/
│   ├── composite_signal.go      # CompositeSignalV1 VO
│   ├── signal_kind.go           # Kind enum
│   └── composition_rules.go     # Rule definitions
├── app/
│   ├── compose_signal.go        # Signal composer use case
│   └── signal_rate_limiter.go   # Dedup + rate limiting
├── ports/
│   └── signal_publisher.go      # Port interface
└── go.mod
```

The `evidence` BC already exists (skeleton) and gets populated in Phases 4-5.

---

*End of Evolution Plan v1*
