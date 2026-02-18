# Codex Prompt Orchestration — C-Waves (Architecture Consolidation)

## Overview

C-waves consolidate Market Raccoon's architecture post-S-waves, closing the gap between infrastructure readiness and product parity with MarketMonkey. Each wave is self-contained with explicit gates.

## Prerequisite State

S1-S5 must be complete:
- Real storage drivers (pgx + clickhouse-go) → `IsProductionReady() = true`
- ACK-on-commit boundary enforced with soak tests
- Delivery subsystem with snapshot, GetRange, backpressure
- All artifact writers (candle/stats/heatmap/vpvr) for both DBs
- Auth, TLS, rate limiting on WS

Verify:
```bash
make test-workspace && make test-workspace-race && make docs-check && make invariants-check
```

## Execution Order

| # | Prompt | Focus | Key Deliverables | Gate |
|---|--------|-------|-------------------|------|
| **C1** | `codex-prompt-C1-consolidation-baseline.md` | Foundation lock | Proto hot-path activation, shard wiring, E2E pipeline benchmark | `make test-workspace-race && make proto-breaking && make bench-hotpath` |
| **C2** | `codex-prompt-C2-multi-exchange-parity.md` | Exchange expansion | Coinbase + HyperLiquid adapters, Binance spot/futures split, funding rate pipeline | `make test-workspace-race && make invariants-check` |
| **C3** | `codex-prompt-C3-operational-tooling.md` | Ops parity | Backfill command, history gap detector, replay from cold path | `make test-workspace-race` |
| **C4** | `codex-prompt-C4-production-soak.md` | Production proof | Multi-exchange soak (10M events), cold-path batch maturation, dashboard bootstrap | `make test-workspace-race && make soak-check` |

## Dependency Graph

```
C1 (proto + shard + bench)
    ↓
C2 (multi-exchange — needs proto wire + shard partitioning)
    ↓
C3 (ops tooling — needs cold path for backfill, multi-exchange for gap detection)
    ↓
C4 (production soak — exercises full pipeline with all exchanges)
```

### Why sequential?
- **C1→C2:** Multi-exchange adapters benefit from proto wire format (lower allocation per exchange). Shard partitioning is critical for multi-exchange throughput.
- **C2→C3:** Backfill and gap detection require exchange-specific replay fixtures and cold-path queries.
- **C3→C4:** Soak tests must exercise the full production topology including backfill recovery.

## Pre-flight Check (before starting C1)

```bash
# Verify S1-S5 state is green
make test-workspace
make test-workspace-race
make docs-check
make invariants-check
make proto-lint
make proto-breaking

# Verify proto infrastructure ready
grep -r "ProtoCodec" internal/shared/codec/ | grep -c "func"  # should be > 0
grep -r "BootstrapPayloadCodecRegistry" internal/shared/contracts/ | grep -c "func"  # should be > 0

# Verify shard infrastructure ready
grep -r "ShardKey" internal/adapters/jetstream/shard.go | grep -c "func"  # should be > 0

# Verify baselines exist
test -f .benchmarks/baseline.txt && echo "baseline exists"
```

## Post-flight Check (after completing all 4)

```bash
make test-workspace
make test-workspace-race
make docs-check
make invariants-check
make proto-breaking
make bench-hotpath

# Verify proto hot-path activated
grep -r "wire_format.*proto" cmd/*/config.jsonc

# Verify shard support
grep -r "shard-index" cmd/consumer/main.go cmd/processor/main.go

# Verify multi-exchange
grep -r "exchange.*coinbase\|exchange.*hyperliquid" internal/adapters/exchange/

# Verify ops tooling
test -f cmd/backfill/main.go && echo "backfill exists"

# Verify soak coverage
go test -list "TestSoak" ./cmd/processor/... ./internal/interfaces/ws/...
```

## Gap Analysis: MarketMonkey → Market Raccoon

### Already Ported (and better)
| Feature | MM Implementation | Raccoon Implementation | Improvement |
|---------|-------------------|----------------------|-------------|
| OrderBook | BTree (tidwall/btree) | Sorted levels + replay | Deterministic, golden tests |
| Candle OHLCV | float64 sampler | CandleV1 fixed-point | Precision, CA-1→CA-7 |
| Stats Window | Simple accumulator | StatsWindowV1 multi-input | Partial tolerance (ST-6) |
| Heatmap | Price grouping | Bucket+builder+policykit | Overload protection |
| Volume Profile | Simple bucketing | VPVR+overload+1M msg soak | Production hardened |
| WS Delivery | CBOR session | Session+Router+snapshot+backpressure | Feature-complete |
| Storage | DB interface | Real pgx+clickhouse-go+committer | ACK-on-commit guarantee |
| Auth | JWT/Supabase | API key+TLS+rate limiting | Simpler, extensible |
| Config | YAML+envvars | JSONC+validation+fail-fast | Robust validation |

### Gaps Closed by C-Waves
| Gap | MM Feature | C-Wave | Priority |
|-----|-----------|--------|----------|
| Proto wire format | CBOR encoding | C1 | P0 |
| Shard partitioning | Per-exchange containers | C1 | P0 |
| E2E regression gate | — (MM has none) | C1 | P0 |
| Coinbase adapter | 6 exchange support | C2 | P1 |
| HyperLiquid adapter | 6 exchange support | C2 | P1 |
| Spot/Futures split | binance vs binancef | C2 | P1 |
| Funding rate pipeline | prestats stream | C2 | P1 |
| Backfill tooling | cmd/backfill | C3 | P2 |
| Gap detection | cmd/history | C3 | P2 |
| Multi-exchange soak | Production deployment | C4 | P1 |

### Not Porting (design decisions)
| Feature | MM Has | Raccoon Decision | Reason |
|---------|--------|-----------------|--------|
| Consul discovery | Yes | Prometheus+probes | Simpler, sufficient |
| Caddy proxy | Yes | External concern | Deploy-layer decision |
| CBOR wire | Yes | Protobuf | Superior schema evolution |
| Float64 candles | Yes | Fixed-point | Correctness > convenience |
| Flat packages | Yes | DDD/Hexagonal | Architecture integrity |
| Odin client | Yes | Future (not now) | Backend consolidation first |
| 1s/5s candles | Yes | 1m minimum v1 | Evaluate demand later |

## Important Constraints (Cross-Wave)

1. **go.mod hygiene** — Any new `require` must have corresponding `replace` directive. Run `make tidy` after each wave.
2. **No cross-module imports** — Use shared wire DTOs (contracts pkg pattern) when payload types cross module boundaries.
3. **Feature flags** — All new features gated by config. Proto: `bus.wire_format`. Sharding: `shard.count`. Exchange: `consumer.exchange`.
4. **Backward compat** — Every wave must leave all existing tests passing. JSON remains default wire format.
5. **Soak tests behind `testing.Short()`** — Must not slow CI.
6. **Metrics cardinality** — Follow `docs/architecture/metrics-budget-label-policy.md`.
7. **`*problem.Problem` at boundaries** — Adapters wrap `error` to `*problem.Problem` at the port boundary.
8. **Proto only in shared/** — Never import proto/gen types from core/ or actors/.
