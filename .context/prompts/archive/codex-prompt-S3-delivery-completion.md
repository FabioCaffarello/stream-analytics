# Codex Prompt S3 — Delivery Completion (Snapshots + GetRange + Backpressure)

## Project Identity

Market Raccoon is a high-performance market intelligence platform. Go 1.25+, Hollywood actor model, NATS JetStream, DDD hexagonal architecture.

---

## Context

After S1+S2, storage is real and ACK boundary is hardened. The delivery subsystem has:
- **Session actor** (`session.go`, 537 LOC) — WS lifecycle, JSON/Protobuf format negotiation
- **Router** (`router.go`, 214 LOC) — subject→session multiplexing
- **BackpressurePolicy** (`backpressure_policy.go`, 34 LOC) — only `DropNewest`
- **DeliveryRangeStore** (`delivery_range_store.go`) — in-memory circular buffer, TODO for real Timescale
- **Delivery contracts** — `envelope_policy.go` knows about marketdata, aggregation, insights subjects

**What's missing:**
1. **Hot snapshot on subscribe** — client subscribes → receives latest state before deltas
2. **GetRange with real Timescale** — `?start=<ts>&end=<ts>` queries the real database
3. **Backpressure policies** — only DropNewest exists; need DropOldest and priority-based drop
4. **Delivery routes for candle/stats/heatmap/VPVR** — contracts exist but WS routes don't
5. **Slow client detection** — sessions that fall behind must be managed

---

## Mandatory Patterns

### Errors: `*problem.Problem`
### Actor messages: typed structs through Hollywood mailbox
### WS wire format (from docs/contracts/delivery-ws.md):
```json
// Client → Server (subscribe)
{"action":"subscribe","subject":"aggregation.candle.v1.BINANCE.BTCPERP"}

// Server → Client (snapshot on subscribe)
{"type":"snapshot","subject":"aggregation.candle.v1.BINANCE.BTCPERP","payload":{...}}

// Server → Client (delta stream)
{"type":"delta","subject":"aggregation.candle.v1.BINANCE.BTCPERP","seq":42,"payload":{...}}

// Client → Server (getrange)
{"action":"getrange","subject":"aggregation.candle.v1.BINANCE.BTCPERP","start":1710000000000,"end":1710003600000,"limit":100}

// Server → Client (range response)
{"type":"range","subject":"aggregation.candle.v1.BINANCE.BTCPERP","items":[...]}
```

---

## Task: Complete Delivery Subsystem

### Step 1: Hot snapshot on subscribe

**File:** `internal/actors/delivery/runtime/session.go`

When a client sends `{"action":"subscribe","subject":"..."}`:

1. Parse subject and validate against delivery contracts
2. Register subscription in router
3. **NEW:** Query hot read model for latest state
4. Send snapshot message to client before starting delta stream

```go
func (s *SessionActor) handleSubscribe(c *actor.Context, sub SubscribeMsg) {
    // ... existing validation + router registration ...

    // NEW: emit hot snapshot
    if snapshot, ok := s.cfg.HotSnapshotProvider.GetLatest(sub.Subject); ok {
        s.sendToClient(c, WireMessage{
            Type:    "snapshot",
            Subject: sub.Subject.String(),
            Payload: snapshot,
        })
    }
}
```

Add `HotSnapshotProvider` interface to `SessionConfig`:
```go
type HotSnapshotProvider interface {
    GetLatest(subject domain.Subject) ([]byte, bool)
}
```

Implement using the existing hot read model stores (BoundedMap in aggregation).

### Step 2: GetRange with real Timescale

**File:** `internal/adapters/storage/timescale/delivery_range_store.go` (REWRITE)

Add a `PgRangeStore` that queries Timescale:

```go
type PgRangeStore struct {
    pool *Pool
    maxPerSubject int
}

func NewPgRangeStore(pool *Pool, maxPerSubject int) *PgRangeStore {
    if maxPerSubject <= 0 {
        maxPerSubject = 4096
    }
    return &PgRangeStore{pool: pool, maxPerSubject: maxPerSubject}
}

func (s *PgRangeStore) GetRange(ctx context.Context, subject domain.Subject, fromMs, toMs int64, limit int) ([]ports.RangeItem, *problem.Problem) {
    const querySQL = `
        SELECT seq, ts_ingest, payload
        FROM delivery_events
        WHERE subject = $1
          AND ($2 = 0 OR ts_ingest >= $2)
          AND ($3 = 0 OR ts_ingest <= $3)
        ORDER BY ts_ingest ASC, seq ASC
        LIMIT $4`

    if limit <= 0 || limit > s.maxPerSubject {
        limit = s.maxPerSubject
    }

    rows, err := s.pool.Raw().Query(ctx, querySQL,
        subject.String(), fromMs, toMs, limit)
    if err != nil {
        return nil, problem.Wrap(err, problem.Unavailable, "timescale getrange query failed")
    }
    defer rows.Close()

    var items []ports.RangeItem
    for rows.Next() {
        var item ports.RangeItem
        if err := rows.Scan(&item.Seq, &item.TsIngest, &item.Payload); err != nil {
            return nil, problem.Wrap(err, problem.Internal, "timescale scan failed")
        }
        items = append(items, item)
    }
    return items, nil
}
```

Keep the in-memory `DeliveryRangeStore` as fallback when Timescale is not enabled.

### Step 3: Backpressure policies

**File:** `internal/core/delivery/domain/backpressure_policy.go` (EXTEND)

Currently only `DropNewest`. Add:

```go
type BackpressurePolicy int

const (
    BackpressureDropNewest BackpressurePolicy = iota  // existing
    BackpressureDropOldest                             // NEW
    BackpressurePriorityDrop                           // NEW
)

// PriorityDropPolicy drops lower-priority event types first.
// Priority order (highest to lowest):
//   1. marketdata.trade (real-time critical)
//   2. marketdata.bookdelta (orderbook updates)
//   3. aggregation.candle (derived, can be reconstructed)
//   4. aggregation.stats (derived)
//   5. marketdata.markprice (periodic)
//   6. marketdata.liquidation (sporadic)
//   7. insights.* (computed, lowest priority)

type PriorityDropConfig struct {
    QueueCapacity int
    Priorities    map[string]int // event type → priority (higher = keep longer)
}

func DefaultPriorities() map[string]int {
    return map[string]int{
        "marketdata.trade":     100,
        "marketdata.bookdelta": 90,
        "aggregation.candle":   70,
        "aggregation.stats":    60,
        "marketdata.markprice": 50,
        "marketdata.liquidation": 40,
        "insights.crossvenue.trade_snapshot": 30,
        "insights.crossvenue.spread_signal":  20,
    }
}
```

### Step 4: Session bounded queue with policy

**File:** `internal/actors/delivery/runtime/session.go`

The session should have a bounded outgoing queue per client:

```go
type sessionQueue struct {
    items    []WireMessage
    capacity int
    policy   domain.BackpressurePolicy
    priorities map[string]int
    drops    int64
}

func (q *sessionQueue) Enqueue(msg WireMessage) bool {
    if len(q.items) < q.capacity {
        q.items = append(q.items, msg)
        return true
    }
    switch q.policy {
    case domain.BackpressureDropNewest:
        q.drops++
        metrics.IncWSDrops(msg.Subject, "drop_newest")
        return false
    case domain.BackpressureDropOldest:
        q.items = q.items[1:]
        q.items = append(q.items, msg)
        q.drops++
        metrics.IncWSDrops(msg.Subject, "drop_oldest")
        return true
    case domain.BackpressurePriorityDrop:
        return q.priorityDrop(msg)
    }
    return false
}

func (q *sessionQueue) priorityDrop(msg WireMessage) bool {
    msgPriority := q.priorities[msg.EventType]
    // Find lowest priority item in queue
    lowestIdx := -1
    lowestPri := msgPriority
    for i, item := range q.items {
        pri := q.priorities[item.EventType]
        if pri < lowestPri {
            lowestPri = pri
            lowestIdx = i
        }
    }
    if lowestIdx >= 0 {
        // Drop lowest priority, enqueue new
        q.items = append(q.items[:lowestIdx], q.items[lowestIdx+1:]...)
        q.items = append(q.items, msg)
        q.drops++
        metrics.IncWSDrops(msg.Subject, "priority_drop")
        return true
    }
    // New message is lowest priority — drop it
    q.drops++
    metrics.IncWSDrops(msg.Subject, "priority_drop_self")
    return false
}
```

### Step 5: WS delivery routes for all artifact types

**File:** `internal/actors/delivery/runtime/router.go` (VERIFY)

The router should already handle candle/stats subjects since they were added to `envelope_policy.go` in Prompt B. Verify:

1. Client subscribes to `aggregation.candle.v1.BINANCE.BTCPERP`
2. Router registers subscription
3. When candle envelope arrives on bus, router matches subject → forward to session
4. Session encodes and sends to WS client

If the router uses the delivery contracts map for subject validation, this should already work. If not, extend the routing table.

### Step 6: Tests

**File:** `internal/actors/delivery/runtime/session_snapshot_test.go` (NEW)

```go
func TestSession_SubscribeEmitsHotSnapshot(t *testing.T)
func TestSession_SubscribeNoSnapshot_WhenEmpty(t *testing.T)
func TestSession_GetRange_ReturnsItems(t *testing.T)
func TestSession_GetRange_EmptyRange(t *testing.T)
func TestSession_GetRange_LimitEnforced(t *testing.T)
```

**File:** `internal/core/delivery/domain/backpressure_policy_test.go` (EXTEND)

```go
func TestBackpressure_DropOldest(t *testing.T)
func TestBackpressure_PriorityDrop_LowPriorityEvicted(t *testing.T)
func TestBackpressure_PriorityDrop_HighPriorityKept(t *testing.T)
func TestBackpressure_PriorityDrop_SelfDropWhenLowest(t *testing.T)
```

**File:** `internal/interfaces/ws/delivery_snapshot_e2e_test.go` (NEW)

```go
func TestWSDelivery_SubscribeOrderbook_ReceivesSnapshot(t *testing.T)
func TestWSDelivery_GetRange_ReturnsHistoricalData(t *testing.T)
func TestWSDelivery_SlowClient_DropsLowPriority(t *testing.T)
```

---

## Reference Files

| File | Purpose |
|------|---------|
| `internal/actors/delivery/runtime/session.go` | Session actor to extend |
| `internal/actors/delivery/runtime/router.go` | Router to verify |
| `internal/core/delivery/domain/backpressure_policy.go` | Policies to extend |
| `internal/core/delivery/domain/envelope_policy.go` | Delivery contracts |
| `internal/adapters/storage/timescale/delivery_range_store.go` | RangeStore to rewrite |
| `docs/contracts/delivery-ws.md` | WS wire contract |
| `internal/interfaces/ws/server.go` | WS server |
| `internal/interfaces/ws/delivery_contract_e2e_test.go` | Existing E2E tests |

---

## Execution Rules

```bash
make test-workspace
make test-workspace-race
make docs-check
```

### STOP CONDITIONS:
- Snapshot sent AFTER deltas (must be before)
- GetRange returns unbounded results (must respect limit)
- Backpressure drops high-priority events before low-priority
- WS session leaks goroutines (must clean up on disconnect)

### Commit sequence:
```
feat(s3): add hot snapshot on subscribe for all delivery subjects
feat(s3): implement real Timescale GetRange query
feat(s3): add DropOldest and PriorityDrop backpressure policies
test(s3): add delivery snapshot, getrange, and backpressure E2E tests
```

---

## Important Constraints

1. **Snapshot before deltas** — client must receive snapshot THEN start getting deltas (no gap)
2. **GetRange bounded** — always enforce limit; default 4096
3. **Priority drop is deterministic** — same queue state + same input = same drop decision
4. **Metrics for every drop** — `ws_drops_total{reason}` counter must increment
5. **Graceful degradation** — if Timescale unavailable, GetRange returns empty (not error)
6. **No blocking WS writes** — use non-blocking send with deadline; drop on timeout
