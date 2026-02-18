package observability

import "sync/atomic"

// ShardStateSnapshot is a point-in-time view of shard topology and counters.
// Fields are populated by the JetStream consumer at runtime.
type ShardStateSnapshot struct {
	ShardIndex  int    `json:"shard_index"`
	ShardCount  int    `json:"shard_count"`
	Lag         int64  `json:"lag"`
	EventsTotal uint64 `json:"events_total"`
	SkipTotal   uint64 `json:"skip_total"`
	Budget      int64  `json:"budget"`
	BudgetOK    bool   `json:"budget_ok"`
}

type shardStateStore struct {
	shardIndex  int32
	shardCount  int32
	lag         int64
	eventsTotal uint64
	skipTotal   uint64
	budget      int64

	// configured is set to 1 when SetShardTopology is called, meaning
	// shard mode is active for this process.
	configured uint32
}

var globalShardStateStore shardStateStore

// SetShardTopology records static shard topology and budget.
// Called once at consumer startup.
func SetShardTopology(index, count int, maxLag int) {
	atomic.StoreInt32(&globalShardStateStore.shardIndex, int32(index)) // #nosec G115 -- shard index is bounded [0,999]
	atomic.StoreInt32(&globalShardStateStore.shardCount, int32(count)) // #nosec G115 -- shard count is bounded [0,999]
	atomic.StoreInt64(&globalShardStateStore.budget, int64(maxLag))
	atomic.StoreUint32(&globalShardStateStore.configured, 1)
}

// SetShardLag updates the current consumer lag for the shard endpoint.
func SetShardLag(lag int64) {
	atomic.StoreInt64(&globalShardStateStore.lag, lag)
}

// IncShardEventsTotal increments the processed events counter.
func IncShardEventsTotal() {
	atomic.AddUint64(&globalShardStateStore.eventsTotal, 1)
}

// IncShardSkipTotal increments the skipped events counter.
func IncShardSkipTotal() {
	atomic.AddUint64(&globalShardStateStore.skipTotal, 1)
}

// ShardConfigured returns true when SetShardTopology has been called,
// indicating this process is running in shard mode.
func ShardConfigured() bool {
	return atomic.LoadUint32(&globalShardStateStore.configured) == 1
}

// SnapshotShardState returns a consistent point-in-time snapshot.
func SnapshotShardState() ShardStateSnapshot {
	idx := int(atomic.LoadInt32(&globalShardStateStore.shardIndex))
	cnt := int(atomic.LoadInt32(&globalShardStateStore.shardCount))
	lag := atomic.LoadInt64(&globalShardStateStore.lag)
	events := atomic.LoadUint64(&globalShardStateStore.eventsTotal)
	skip := atomic.LoadUint64(&globalShardStateStore.skipTotal)
	budget := atomic.LoadInt64(&globalShardStateStore.budget)

	budgetOK := budget == 0 || lag <= budget

	return ShardStateSnapshot{
		ShardIndex:  idx,
		ShardCount:  cnt,
		Lag:         lag,
		EventsTotal: events,
		SkipTotal:   skip,
		Budget:      budget,
		BudgetOK:    budgetOK,
	}
}
