# RFC-0005 — W4: Observability & Profiling

**Status:** Accepted
**Date:** 2026-02-12
**Author:** Chief Architect
**Workflow:** W4 of PRD-0001
**Relates to:** ADR-0013 (Backpressure), ADR-0012 (Lifecycle), ADR-0018 (Topology)

---

## 1. Goal

Instrument the runtime with Prometheus metrics and pprof endpoints so that every pipeline stage is observable. After W4, operators can:
- Monitor ingest latency, throughput, and drop rates in Grafana
- Profile heap/CPU/goroutines in production via pprof
- Set alerts on backpressure, goroutine growth, and restart events

## 2. Scope

- Add `prometheus/client_golang` dependency
- Create `internal/shared/metrics/` package with registry helpers
- Instrument: ingest pipeline, backpressure queue, InMemoryBus, Guardian, WS consumer
- Add `/metrics` endpoint (Prometheus exposition format)
- Add `/debug/pprof/` endpoints (net/http/pprof, localhost-only)
- Add `SubjectFromEnvelope()` to envelope package (prep for ADR-0014)

## 3. Non-Goals

- Distributed tracing (OpenTelemetry spans) — deferred to future RFC
- Custom Grafana dashboards — operator responsibility
- Alerting rules — operator responsibility

## 4. Affected Modules

| File | Action | Change |
|------|--------|--------|
| `internal/shared/metrics/registry.go` | CREATE | Prometheus registry, metric constructors |
| `internal/shared/metrics/metrics.go` | CREATE | Metric definitions (counters, histograms, gauges) |
| `internal/shared/envelope/subject.go` | CREATE | `SubjectFromEnvelope()` function |
| `internal/shared/envelope/subject_test.go` | CREATE | Unit tests |
| `internal/core/marketdata/app/ingest.go` | ALTER | Add latency histogram, messages counter |
| `internal/actors/marketdata/runtime/subsystem.go` | ALTER | Expose queue depth gauge, drops counter |
| `internal/actors/runtime/guardian.go` | ALTER | Expose restart/degraded/rate-limited counters |
| `internal/adapters/bus/inmemory.go` | ALTER | Add per-subscriber drop counter |
| `internal/interfaces/http/server.go` | ALTER | Add `/metrics` and `/debug/pprof/` routes |
| `cmd/*/main.go` | ALTER | Wire metrics registry |
| `go.mod` (shared, actors, interfaces, cmd/*) | ALTER | Add prometheus dependency |
| `Makefile` | ALTER | No new targets needed (prometheus is a library) |

## 5. API Changes

### New package: `internal/shared/metrics`

```go
package metrics

import "github.com/prometheus/client_golang/prometheus"

// Registry returns the shared Prometheus registry.
func Registry() *prometheus.Registry

// Pre-defined metrics (created at init, registered lazily)
var (
    IngestTotal        *prometheus.CounterVec   // {venue, instrument, event_type, status}
    IngestLatency      *prometheus.HistogramVec // {venue, instrument, event_type}
    IngestStreamsActive prometheus.Gauge

    BackpressureQueueDepth  *prometheus.GaugeVec   // {venue}
    BackpressureDropsTotal  *prometheus.CounterVec  // {venue, policy}
    BackpressureActive      *prometheus.GaugeVec    // {venue}

    BusPublishedTotal  *prometheus.CounterVec  // {type, venue}
    BusDropsTotal      *prometheus.CounterVec  // {subscriber_id}

    WSConnectionsActive *prometheus.GaugeVec   // {exchange}
    WSReconnectsTotal   *prometheus.CounterVec // {exchange, reason}
    WSMessagesTotal     *prometheus.CounterVec // {exchange, stream}
    WSErrorsTotal       *prometheus.CounterVec // {exchange, kind}

    GuardianRestartsTotal    *prometheus.CounterVec // {subsystem, kind}
    GuardianDegradedTotal    *prometheus.CounterVec // {subsystem}
    GuardianRateLimitedTotal prometheus.Counter
    GuardianSubsystemState   *prometheus.GaugeVec  // {subsystem} 0=stopped,1=running,2=degraded

    ProcessGoroutines   prometheus.Gauge
    ProcessHeapAlloc    prometheus.Gauge
)
```

### Envelope subject helper

```go
// internal/shared/envelope/subject.go
func SubjectFromEnvelope(env Envelope) string
```

### InMemoryBus drop counting

```go
// internal/adapters/bus/inmemory.go
type InMemoryBus struct {
    // ... existing fields ...
    dropCounts []atomic.Uint64 // per-subscriber drop counter
}
```

## 6. Migration Strategy

No migration needed. Purely additive changes. Existing behavior unchanged. Metrics start at zero.

## 7. Observability Added

All metrics from PRD-0001 section B.5. See `internal/shared/metrics/metrics.go` for complete list.

pprof endpoints:
- `/debug/pprof/` — index
- `/debug/pprof/heap` — heap profile
- `/debug/pprof/goroutine` — goroutine dump
- `/debug/pprof/profile` — CPU profile (30s default)
- `/debug/pprof/mutex` — mutex contention
- `/debug/pprof/block` — block profile
- `/debug/pprof/trace` — execution trace

## 8. Test Plan

| Type | Test | Pass Criteria |
|------|------|---------------|
| Unit | `IngestTotal` increments on successful ingest | Counter value == expected |
| Unit | `BusDropsTotal` increments when subscriber channel full | Counter value > 0 |
| Unit | `SubjectFromEnvelope` deterministic | Same envelope → same subject |
| Integration | `GET /metrics` returns Prometheus format | Contains `ingest_messages_total` |
| Integration | `GET /debug/pprof/goroutine?debug=1` returns dump | Contains `goroutine` |
| Benchmark | Ingest throughput before/after instrumentation | Regression < 5% |

## 9. Acceptance Criteria

- [x] `curl localhost:8080/metrics` returns valid Prometheus exposition format
- [x] All metrics from section 5 are registered and emitting values after 1 minute of operation
- [x] `curl localhost:8080/debug/pprof/goroutine?debug=1` returns goroutine dump
- [x] pprof endpoints only accessible from localhost (or auth-protected)
- [x] `InMemoryBus` drop counter increments when subscriber is slow (test with blocked subscriber)
- [x] `go test -race ./...` verde across all modules
- [ ] No performance regression > 5% on ingest throughput (benchmark comparison)
- [ ] `SubjectFromEnvelope()` produces `{type}.v{version}.{venue_lower}.{instrument}` format

## 10. Execution Evidence (2026-02-12)

- Endpoint integration tests: `/metrics` and `/debug/pprof/*` validated in `internal/interfaces/http/server_test.go`.
- Full module matrix green (`go test` and `go test -race`) except `cmd/store` expected `no packages to test` (recorded in `.context/evidence/w4-full-test.txt` and `.context/evidence/w4-full-race.txt`).
- Metrics wired with cardinality guards and bounded labels in `internal/shared/metrics/metrics.go`; drop counters emitted from backpressure queue and in-memory bus.

## Changelog

- 2026-02-13:
  - normalizado status para taxonomia RFC (`Draft|Accepted`);
  - preservadas evidências operacionais da rodada W4.
