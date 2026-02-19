# Codex Prompt S5 — Production Hardening (Auth, TLS, Rate Limiting, Observability)

## Project Identity

Market Raccoon is a high-performance market intelligence platform. Go 1.25+, Hollywood actor model, NATS JetStream, DDD hexagonal architecture.

---

## Context

After S1-S4, the full pipeline is functional with real storage and delivery. This wave hardens for production:
- **No authentication** on WS connections
- **No TLS** on HTTP/WS endpoints
- **No rate limiting** per session
- **~80% metric wiring** — remaining 20% in store pipeline and new artifact writers
- **No chaos/soak E2E tests** for the full pipeline

---

## Task: Production Hardening

### Step 1: API Key authentication for WS connections

**File:** `internal/interfaces/ws/auth.go` (NEW)

Simple API key authentication via query parameter or header:

```go
package ws

import (
    "net/http"
    "strings"

    "github.com/market-raccoon/internal/shared/problem"
)

type AuthConfig struct {
    Enabled  bool
    APIKeys  map[string]string // key → client_id
}

func (cfg AuthConfig) Authenticate(r *http.Request) (string, *problem.Problem) {
    if !cfg.Enabled {
        return "anonymous", nil
    }
    key := r.URL.Query().Get("api_key")
    if key == "" {
        key = r.Header.Get("X-API-Key")
    }
    if key == "" {
        key = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
    }
    key = strings.TrimSpace(key)
    if key == "" {
        return "", problem.New(problem.Unauthorized, "missing API key")
    }
    clientID, ok := cfg.APIKeys[key]
    if !ok {
        return "", problem.New(problem.Unauthorized, "invalid API key")
    }
    return clientID, nil
}
```

**File:** `internal/interfaces/ws/server.go` (EXTEND)

Add auth check before WS upgrade:
```go
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
    clientID, p := s.auth.Authenticate(r)
    if p != nil {
        http.Error(w, p.Message, http.StatusUnauthorized)
        return
    }
    // ... existing upgrade logic, pass clientID to session ...
}
```

### Step 2: TLS support

**File:** `internal/interfaces/http/server.go` (EXTEND)

Add TLS config:
```go
type ServerConfig struct {
    // ... existing fields ...
    TLSCertFile string
    TLSKeyFile  string
}

func (s *Server) ListenAndServe() error {
    if s.cfg.TLSCertFile != "" && s.cfg.TLSKeyFile != "" {
        return s.httpServer.ListenAndServeTLS(s.cfg.TLSCertFile, s.cfg.TLSKeyFile)
    }
    return s.httpServer.ListenAndServe()
}
```

**File:** `internal/shared/config/schema.go`

Add TLS config:
```go
type HTTPConfig struct {
    // ... existing fields ...
    TLSCert string `json:"tls_cert"`
    TLSKey  string `json:"tls_key"`
}
```

### Step 3: Per-session rate limiting

**File:** `internal/actors/delivery/runtime/rate_limiter.go` (NEW)

Token bucket rate limiter per session:

```go
type RateLimiter struct {
    tokens     int
    maxTokens  int
    refillRate int // tokens per second
    lastRefill time.Time
    clock      clock.Clock
}

func NewRateLimiter(maxTokens, refillRate int, clk clock.Clock) *RateLimiter {
    return &RateLimiter{
        tokens:     maxTokens,
        maxTokens:  maxTokens,
        refillRate: refillRate,
        lastRefill: clk.Now(),
        clock:      clk,
    }
}

func (r *RateLimiter) Allow() bool {
    now := r.clock.Now()
    elapsed := now.Sub(r.lastRefill)
    refill := int(elapsed.Seconds()) * r.refillRate
    if refill > 0 {
        r.tokens = min(r.tokens+refill, r.maxTokens)
        r.lastRefill = now
    }
    if r.tokens <= 0 {
        return false
    }
    r.tokens--
    return true
}
```

Wire into session: reject subscribe/getrange when rate limited.

### Step 4: Complete metrics wiring

**Files to check and wire:**
- `internal/adapters/storage/timescale/candle_writer.go` → `metrics.IncProcessorCommit("candle_hot")`
- `internal/adapters/storage/timescale/stats_writer.go` → `metrics.IncProcessorCommit("stats_hot")`
- `internal/adapters/storage/clickhouse/candle_writer.go` → `metrics.IncProcessorCommit("candle_cold")`
- `internal/adapters/storage/clickhouse/stats_writer.go` → `metrics.IncProcessorCommit("stats_cold")`
- Session snapshot sends → `metrics.IncWSQuery("snapshot")`
- GetRange queries → `metrics.IncWSQuery("getrange")`
- Rate limit rejections → `metrics.IncWSQueryRejected("rate_limited")`
- Auth failures → `metrics.IncWSQueryRejected("unauthorized")`

### Step 5: Soak E2E test for full pipeline

**File:** `cmd/processor/soak_pipeline_test.go` (NEW)

```go
func TestSoak_FullPipeline_1M_Messages(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping soak test")
    }
    // 1. Set up processor with InMemoryBus + real AggregationService.
    // 2. Feed 1M trade envelopes for 100 instruments.
    // 3. Verify:
    //    - No panics or races.
    //    - ActiveCandles() <= MaxCandles.
    //    - ActiveBooks() <= MaxBooks.
    //    - All CandleClosed events published.
    //    - Memory usage bounded (check runtime.MemStats).
    // 4. Runtime: < 30 seconds on CI hardware.
}
```

**File:** `internal/interfaces/ws/soak_delivery_test.go` (NEW)

```go
func TestSoak_WSDelivery_SlowClients(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping soak test")
    }
    // 1. Set up WS server with 50 concurrent slow clients.
    // 2. Publish 100k envelopes to bus.
    // 3. Verify:
    //    - No goroutine leaks (runtime.NumGoroutine before/after).
    //    - Backpressure drops are logged and metriced.
    //    - Fast clients receive all messages.
    //    - Slow clients receive subset (drops counted).
}
```

### Step 6: Funding rate ingestion (P2)

**File:** `internal/adapters/exchange/binance/parser.go` (EXTEND)

Add funding rate parser to existing Binance exchange parser:

```go
func ParseFundingRate(raw []byte) (*mddomain.MarkPriceTickV1, *problem.Problem) {
    // Binance funding rate comes in the same stream as markprice.
    // The MarkPriceTickV1 already has a FundingRate field.
    // If the existing markprice parser already extracts FundingRate, this is a no-op.
    // If not, extend the parser to include FundingRate.
}
```

Check if the existing markprice parser already extracts `FundingRate` from the payload. If yes, this step is already done. If not, add the field extraction.

### Step 7: Config for auth and rate limiting

**File:** `internal/shared/config/schema.go`

```go
type WSConfig struct {
    // ... existing fields ...
    Auth     WSAuthConfig     `json:"auth"`
    RateLimit WSRateLimitConfig `json:"rate_limit"`
}

type WSAuthConfig struct {
    Enabled bool              `json:"enabled"`
    APIKeys map[string]string `json:"api_keys"` // key → client_id
}

type WSRateLimitConfig struct {
    Enabled        bool `json:"enabled"`
    MaxPerSecond   int  `json:"max_per_second"`
    BurstCapacity  int  `json:"burst_capacity"`
}
```

Defaults:
```go
if c.WS.RateLimit.MaxPerSecond == 0 {
    c.WS.RateLimit.MaxPerSecond = 100
}
if c.WS.RateLimit.BurstCapacity == 0 {
    c.WS.RateLimit.BurstCapacity = 200
}
```

---

## Reference Files

| File | Purpose |
|------|---------|
| `internal/interfaces/ws/server.go` | WS server to extend |
| `internal/interfaces/http/server.go` | HTTP server for TLS |
| `internal/actors/delivery/runtime/session.go` | Session for rate limiting |
| `internal/shared/config/schema.go` | Config to extend |
| `internal/shared/metrics/metrics.go` | Metrics to wire |
| `internal/shared/clock/fake.go` | FakeClock for rate limiter tests |
| `cmd/processor/bootstrap.go` | Pipeline wiring |
| `cmd/server/bootstrap.go` | Server wiring |

---

## Execution Rules

```bash
make test-workspace
make test-workspace-race
make docs-check
make invariants-check
```

### Commit sequence:
```
feat(s5): add API key authentication for WS connections
feat(s5): add TLS support for HTTP/WS endpoints
feat(s5): add per-session token bucket rate limiting
feat(s5): complete metrics wiring for all artifact writers
test(s5): add full pipeline soak test (1M messages)
test(s5): add slow client delivery soak test (50 clients)
feat(s5): add funding rate extraction to Binance parser
```

---

## Important Constraints

1. **Auth is optional** — disabled by default for dev, enabled in production config
2. **TLS is optional** — terminated at load balancer in most deployments
3. **Rate limiter uses clock.Clock** — testable with FakeClock
4. **API keys from config, not database** — simple deployment, no key management service
5. **Soak tests behind `testing.Short()`** — must not slow CI
6. **No goroutine leaks** — soak tests verify NumGoroutine before/after
7. **Metrics cardinality** — labels must follow `docs/observability/metrics-policy.md`
