package aggruntime

// SnapshotTickKind identifies a timer-driven snapshot publish kind.
type SnapshotTickKind string

const (
	SnapshotTickOrderBook SnapshotTickKind = "orderbook"
	SnapshotTickHeatmap   SnapshotTickKind = "heatmap"
	SnapshotTickVolume    SnapshotTickKind = "volume"
)

// SnapshotTick is emitted by TickerPublisherActor on each timer tick.
type SnapshotTick struct {
	Kind SnapshotTickKind
}
