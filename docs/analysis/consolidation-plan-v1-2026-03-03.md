# Market Raccoon Consolidation Plan v1

**Date:** 2026-03-03
**Status:** Active
**Scope:** Full-stack consolidation roadmap to establish MR as a disruptive institutional market data platform
**Baseline:** SWOT v9 (4.5/5), 131K LOC, 1,666 tests, 6 exchanges, 22 instruments

---

## Strategic Vision

Market Raccoon is positioned to disrupt the institutional market data terminal space by combining:

1. **Multi-exchange real-time normalization** — a capability no competitor offers at sub-100us latency
2. **Self-hosted, zero-vendor-lock architecture** — prop desks retain full data sovereignty
3. **Dual-platform Odin/WASM client** — 1.1MB footprint, 13ms load, 60fps rendering
4. **Open architecture** — extensible via adapters, actors, and plugin boundaries

The consolidation plan addresses the gaps identified in SWOT v9 to achieve a **production-ready, secure, institutional-grade** platform.

---

## Phase 0: Client Bug Consolidation (COMPLETE)

**Duration:** Session (2026-03-03)
**Score impact:** +0.5 (Client Robustness 4/5 → 4.5/5)

| Bug | Status | Fix Summary |
|-----|--------|-------------|
| DIAG-1 (P0) | FIXED | Multi-replica timestamp tolerance (5s) + auto-recovery |
| DIAG-2 (P0) | FIXED | Stream picker venue+symbol deduplication |
| DIAG-3 (P1) | FIXED | Compare mode candle panes now use per-pane `[ci]` scroll/zoom |
| DIAG-4 (P1) | FIXED | TF switch now clears orderbook store + resets snapshot gate |
| DIAG-5 (P1) | FIXED | Stream counter shows correct deduped count |
| DIAG-6 (P1) | FIXED | Sequence gap threshold (>10) + auto-recovery |
| DIAG-8 (P2) | FIXED | Funding indicator skips synthetic zero-funding entries |
| DIAG-7 (P2) | DEFERRED | Mobile viewport clipping — cosmetic, P2 |

**Files modified:**
- `client/src/core/streams/stream_controller.odin` — tolerance + auto-recovery
- `client/src/core/streams/stream_controller_test.odin` — 4 new/updated tests
- `client/src/core/app/overlays.odin` — stream picker dedup
- `client/src/core/app/build_compare.odin` — per-pane scroll/zoom/TF
- `client/src/core/app/stream_views.odin` — orderbook clear on TF switch
- `client/src/core/widgets/indicator_funding.odin` — zero-funding filter

---

## Phase 1: Security Hardening (P0 — Critical Path)

**Duration:** 2-3 weeks
**PRD:** PRD-0004 M1
**Score impact:** Security 3.5/5 → 4.5/5
**Rationale:** No institutional client will deploy without encrypted transport and proper auth

### 1.1 TLS Termination
- Configure nginx reverse proxy with Let's Encrypt / self-signed certs
- All WS connections via `wss://`, HTTP via `https://`
- Plaintext → HTTPS redirect
- **Gate:** Zero plaintext connections in soak test

### 1.2 JWT Authentication
- `POST /auth/token` endpoint with configurable expiry (24h default)
- WS handshake accepts `Authorization: Bearer <jwt>`
- Token refresh without session drop (`{"op":"refresh_token"}`)
- API key auth retained as dev/backward-compat fallback
- **Dependencies:** `golang-jwt/jwt/v5` (already in PRD-0004 plan)

### 1.3 Secrets Management
- SOPS-encrypted config files for all credentials
- Per-service DB users with least-privilege
- Zero plaintext secrets in version control
- ADR-0020 completion gate
- **Gate:** `grep -r 'password\|secret\|api_key' deploy/` returns zero hits

### 1.4 Audit Logging
- Structured JSON logging for all auth events
- Session connect/disconnect with client IP, key ID, tenant
- Failed auth attempts with rate-based lockout

### 1.5 CORS & Rate Limiting
- Explicit origin allowlist for WS connections
- Per-key rate limiting (subscriptions + messages/sec)
- Per-IP connection limits (already implemented, needs production config)

**Deliverables:**
- [ ] TLS nginx config + docker-compose integration
- [ ] JWT auth middleware + token endpoint
- [ ] SOPS encrypted config template
- [ ] Audit log middleware
- [ ] Security soak test (100K evt/sec with TLS + JWT overhead < 5ms p95)

---

## Phase 2: Client Feature Parity & Polish (P0-P1)

**Duration:** 3-4 weeks
**PRD:** PRD-0006 M1-M5
**Score impact:** Functional Coverage 4.5/5 → 5/5, Client Robustness 4.5/5 → 5/5

### 2.1 Funding Rate & Liquidation Chart Layers (M1)
- Funding rate line chart (already partially fixed in Phase 0)
- Liquidation volume bars in candle subplot
- Data sourced from existing stats stream
- **LOC estimate:** ~200

### 2.2 Drawing Tools v2 (M2)
- Horizontal/vertical lines (already exist), rectangles, trend lines
- Color picker (8-color palette, no custom RGB)
- Persistence via settings store
- **LOC estimate:** ~250

### 2.3 DOM (Depth of Market) Widget (M3)
- 6-column depth ladder: price, bid size, bid fill, trade, ask fill, ask size
- Heatmap intensity coloring for size columns
- VWAP/TWAP reference lines
- Trade fill tracking from trade stream
- **LOC estimate:** ~550
- **Dependencies:** Existing `dom_store` and `footprint_store` services

### 2.4 Footprint Charts (M4)
- Standard footprint (bid/ask volume per price level per candle)
- Delta footprint (net bid-ask per level)
- Client-side trade accumulation into price bins (already in `footprint_store`)
- Imbalance highlighting (configurable threshold)
- **LOC estimate:** ~550

### 2.5 Per-Layer Settings & Chart Polish (M5)
- Inline settings UI for each indicator/overlay
- Toggle visibility, color, line width per layer
- Smooth scroll animations
- **LOC estimate:** ~300

### 2.6 Mobile Viewport Fix (DIAG-7)
- Fix top bar clipping at 480px width
- Responsive layout adaptation for touch devices
- PWA manifest for mobile bookmarking
- **LOC estimate:** ~100

**Deliverables:**
- [ ] 5 new chart layers (funding line, liq bars, DOM, footprint, delta footprint)
- [ ] Drawing tool v2 with persistence
- [ ] Per-layer settings UI
- [ ] Mobile viewport fix + PWA manifest
- [ ] All layers tested with Playwright diagnostic suite

---

## Phase 3: Wire Protocol Evolution (P1)

**Duration:** 2 weeks
**PRD:** PRD-0004 M2
**Score impact:** Performance 5/5 maintained with 40-60% bandwidth reduction

### 3.1 CBOR Encoding
- `fxamacker/cbor/v2` integration in delivery layer
- Per-session encoding negotiation via subscribe message
- JSON remains default; CBOR opt-in
- Backward compatible — existing clients unaffected
- **Gate:** `len(cbor) / len(json) <= 0.60` for 1000 candle frames

### 3.2 Per-Message Compression
- RFC 7692 `permessage-deflate` support
- Configurable compression level (1-9, default 6)
- Context takeover for repeated field names
- **Gate:** < 500ns p95 marshal/unmarshal per frame

### 3.3 Client CBOR Support
- Odin CBOR decoder for WASM client
- Negotiation during subscribe handshake
- Fallback to JSON if decode fails

---

## Phase 4: Backend Performance & Storage (P1)

**Duration:** 2-3 weeks
**PRD:** PRD-0004 M3-M4

### 4.1 B-Tree Order Book
- Replace `map[float64]*Level` with `tidwall/btree`
- O(log n) lookups, efficient range queries for depth bands
- Depth rebalancing: prune levels beyond configurable % from mid-price
- Per-instrument depth band configuration
- **Gate:** Lookup < 200ns p95, range query < 1us p95 for 100 levels

### 4.2 Automated Backup System
- TimescaleDB: `pg_dump` with compression, 7 daily + 4 weekly retention
- ClickHouse: native backup command with same retention
- Restore scripts tested in CI
- S3/MinIO upload option
- **Gate:** Full backup+restore cycle < 10 minutes in CI

### 4.3 Rolling Update Automation
- Zero-downtime update script: drain → wait ACKs → restart → health check
- Works with PROCESSOR_REPLICAS > 1
- Integrated with Docker Compose profiles

### 4.4 Server Version Handshake
- Hello frame includes `server_version` and `min_client_version`
- Client displays upgrade notice when version is below minimum
- Enables coordinated upgrades across fleet

---

## Phase 5: Server-Driven Market Discovery (P2)

**Duration:** 1-2 weeks
**PRD:** PRD-0006 M6

### 5.1 Markets API
- `GET /api/v1/markets` returns configured venues, instruments, and capabilities
- Client replaces hardcoded market list with server-driven discovery
- Enables dynamic instrument addition without client update
- **LOC estimate:** ~200 client + ~150 backend

### 5.2 Dynamic Subscription
- Client auto-discovers available markets on connect
- Exchange manager panel populated from server API
- Per-market capability flags (has_funding, has_liq, has_heatmap)

---

## Phase 6: Intelligence Layer Foundation (P2)

**Duration:** 4-6 weeks
**PRD:** PRD-0005 (to be written)

### 6.1 Cross-Exchange Order Book
- Unified best bid/offer across all venues
- Arbitrage opportunity detection
- Cross-venue sweep alert correlation

### 6.2 Anomaly Detection
- Statistical anomaly scoring for order book imbalances
- Volume spike detection (extends whale alert EMA)
- Funding rate divergence alerts

### 6.3 Smart Alerting
- Configurable alert rules (price, volume, funding, spread thresholds)
- Alert delivery via WS push (client notification panel)
- Alert history with replay

### 6.4 Evidence Framework Enhancement
- Extend existing evidence events with ML confidence scores
- Feature vector export for external model training
- Backtesting harness for alert strategies

---

## Phase 7: Platform Scalability (P2-P3)

**Duration:** 6-8 weeks
**PRD:** PRD-0007/0008 (to be written)

### 7.1 Kubernetes Deployment
- Helm charts for all services
- Horizontal pod autoscaling for processor replicas
- Service mesh (Istio/Linkerd) for inter-service mTLS

### 7.2 Multi-Region Architecture
- Geo-distributed NATS clusters
- Regional TimescaleDB replicas
- CDN deployment for WASM client (< 100ms global load)

### 7.3 New Exchange Integrations
- Plugin adapter interface for third-party exchange connectors
- Target: OKX, Gate.io, Deribit, BitMEX
- Exchange capability auto-detection

### 7.4 Multi-Tenant White-Label
- Per-tenant branding, instrument lists, rate limits
- Tenant isolation at session and storage levels
- Per-seat licensing model

---

## Consolidated Roadmap Timeline

```
2026-Q1 (Mar)
  ├── Phase 0: Client bug consolidation ✓ COMPLETE
  ├── Phase 1: Security hardening (weeks 1-3)
  └── Phase 2: Client feature parity (weeks 1-4, parallel track)

2026-Q2 (Apr-May)
  ├── Phase 3: Wire protocol evolution (weeks 5-6)
  ├── Phase 4: Backend performance & storage (weeks 5-8)
  └── Phase 5: Market discovery (weeks 7-8)

2026-Q2-Q3 (Jun-Jul)
  ├── Phase 6: Intelligence layer (weeks 9-14)
  └── Phase 7: Platform scalability (weeks 15-22)
```

---

## Critical Success Metrics

| Metric | Current | Phase 1 | Phase 2 | Phase 4 | Phase 7 |
|--------|---------|---------|---------|---------|---------|
| SWOT Score | 4.5/5 | 4.7/5 | 4.9/5 | 5.0/5 | 5.0/5 |
| Security Score | 3.5/5 | 4.5/5 | 4.5/5 | 4.5/5 | 5/5 |
| Client Bugs Open | 1 (P2) | 0 | 0 | 0 | 0 |
| TLS Coverage | 0% | 100% | 100% | 100% | 100% |
| Wire Efficiency | JSON-only | JSON-only | JSON-only | CBOR -50% | CBOR -50% |
| Throughput | 117K/s | 117K/s | 117K/s | 130K+/s | 200K+/s |
| Exchanges | 6 | 6 | 6 | 6 | 10+ |
| Client Indicators | 8 | 8 | 12+ | 12+ | 12+ |
| OB Performance | O(n) | O(n) | O(n) | O(log n) | O(log n) |

---

## Competitive Moat Analysis

### Why Market Raccoon Wins

| Capability | TradingView | Bloomberg | Bookmap | Market Raccoon |
|-----------|-------------|-----------|---------|----------------|
| Multi-exchange normalization | Partial | Yes (high cost) | No | Yes (6 exchanges, sub-100us) |
| Self-hosted deployment | No | No | No | **Yes** |
| Custom indicator SDK | Limited | Yes | No | Yes (Odin/WASM) |
| Real-time order flow | No | Delayed | Yes | Yes (DOM, footprint, sweeps) |
| Data sovereignty | No | No | No | **Yes** |
| Cross-venue analysis | No | Yes | No | Yes (Phase 6) |
| Sub-minute timeframes | Yes | Yes | Yes | Yes (1s, 5s supported) |
| WASM client (<2MB) | No (heavy JS) | No (desktop) | No (Java) | **Yes (1.1MB)** |
| Zero-alloc hot path | N/A | N/A | N/A | **Yes (verified)** |
| Open architecture | No | No | No | **Yes** |

### Defensible Differentiators

1. **Architecture quality** — 14 invariants, zero debt, 1.07:1 test ratio
2. **Performance ceiling** — 117K evt/sec validated, zero-alloc hot path
3. **Dual-platform from single codebase** — Odin → native + WASM simultaneously
4. **Multi-exchange first-class** — not an afterthought, but a core architectural primitive
5. **Self-hosted data sovereignty** — critical for institutional compliance

---

## Risk Mitigation

| Risk | Probability | Impact | Mitigation |
|------|------------|--------|------------|
| Odin ecosystem stagnation | Medium | High | Platform abstraction layer enables renderer swap; WebGPU fallback |
| Exchange API breaking changes | High | Medium | Canonical normalization (ADR-0017), per-exchange adapters with version pinning |
| Security breach pre-hardening | Low | Critical | Phase 1 is P0 critical path; no production deployment until complete |
| WASM Canvas2D perf ceiling | Medium | Medium | WebGL/WebGPU renderer option in Phase 7; current 60fps sufficient for MVP |
| Regulatory compliance gaps | Medium | High | Phase 7 addresses data residency; legal review before institutional sales |
| Key person dependency | High | High | Comprehensive docs (21 ADRs, 15 RFCs), zero debt, readable code |

---

## Investment Case

### Total Addressable Market
- **Crypto market data terminals:** $2-5B globally
- **Prop trading infrastructure:** $500M-1B
- **Quantitative fund tooling:** $1-3B

### Revenue Model Options
1. **Per-seat SaaS** — $500-2,000/mo per trading desk
2. **Self-hosted enterprise license** — $50-200K/year per deployment
3. **Data API access** — metered pricing for normalized multi-exchange data
4. **White-label platform** — custom-branded terminals for brokers/exchanges

### Competitive Advantage Timeline
- **Now (Q1 2026):** Only self-hosted, multi-exchange, zero-debt terminal with WASM client
- **Post Phase 2:** Full order flow suite (DOM, footprint) — matches Bookmap
- **Post Phase 6:** Intelligence layer — unique cross-venue analytics
- **Post Phase 7:** Multi-tenant scalability — platform play

---

## Governance

- All phases produce ADRs for architectural decisions
- PRDs written for each major phase before implementation
- System invariants extended for new boundaries
- SWOT analysis at each phase completion
- Soak testing gate before any production deployment

---

*This plan supersedes the client roadmap 6.8-8.0 and integrates with PRD-0004 (backend), PRD-0006 (client), and future PRD-0005/0007/0008.*
*Next review: End of Phase 1 (target: 2026-03-24)*
