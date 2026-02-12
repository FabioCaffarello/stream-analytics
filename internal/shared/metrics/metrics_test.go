package metrics

import (
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestRegistryAndMetricsInitialized(t *testing.T) {
	if Registry() == nil {
		t.Fatal("registry must not be nil")
	}
	if Handler() == nil {
		t.Fatal("handler must not be nil")
	}
}

func TestObserveIngestAndCardinalityGuard(t *testing.T) {
	before := testutil.ToFloat64(IngestMessagesTotal.WithLabelValues("unknown", "UNKNOWN", "unknown", "unknown"))

	ObserveIngest("binance", "BTCUSDT", "marketdata.trade", "ok", 2*time.Millisecond)
	ObserveIngest("BINANCE", "trade_id=123", "marketdata.trade", "wild_status", 2*time.Millisecond)

	if got := testutil.ToFloat64(IngestMessagesTotal.WithLabelValues("binance", "BTCUSDT", "marketdata.trade", "ok")); got < 1 {
		t.Fatalf("expected ingest counter increment, got %f", got)
	}

	after := testutil.ToFloat64(IngestMessagesTotal.WithLabelValues("binance", "UNKNOWN", "marketdata.trade", "unknown"))
	if after < 1 {
		t.Fatalf("expected sanitized label series increment, got %f", after)
	}
	if after+before < 1 {
		t.Fatalf("unexpected sanitized counter state")
	}
}

func TestBusDropSubscriberLabelBounded(t *testing.T) {
	before := busDroppedSeriesCount(t)

	IncBusDropped(7)
	if got := testutil.ToFloat64(BusDroppedTotal.WithLabelValues("s4_15")); got < 1 {
		t.Fatalf("expected subscriber id metric increment, got %f", got)
	}

	IncBusDropped(10001)
	if got := testutil.ToFloat64(BusDroppedTotal.WithLabelValues("s256_plus")); got < 1 {
		t.Fatalf("expected overflow metric increment, got %f", got)
	}

	for i := 0; i < 1000; i++ {
		IncBusDropped(i)
	}
	after := busDroppedSeriesCount(t)
	if growth := after - before; growth > 5 {
		t.Fatalf("expected bounded bus_dropped_total cardinality growth <= 5, got %d", growth)
	}
}

func TestProcessMetricsUpdate(t *testing.T) {
	UpdateProcessMetrics()
	if g := testutil.ToFloat64(ProcessGoroutines); g <= 0 {
		t.Fatalf("expected goroutines > 0, got %f", g)
	}
	if heap := testutil.ToFloat64(ProcessHeapAllocBytes); heap < 0 {
		t.Fatalf("expected heap metric >= 0, got %f", heap)
	}
}

func busDroppedSeriesCount(t *testing.T) int {
	t.Helper()

	mfs, err := Registry().Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() == "bus_dropped_total" {
			return len(mf.GetMetric())
		}
	}

	t.Fatal("bus_dropped_total metric family not found")
	return 0
}

func TestMetricsNamesPresent(t *testing.T) {
	SetWSConnectionsActive("binance", 1)
	IncBusPublishError("timeout")
	ObserveBusPublishLatency("jetstream", 2*time.Millisecond)
	IncBusConsumed("jetstream", "ok")
	IncBusRedelivered("jetstream")
	ObserveBusAckLatency("jetstream", 3*time.Millisecond)
	SetBusConsumerLag("jetstream", 42)
	IncReplayMessages("jetstream", "ok")
	ObserveReplayLatency("jetstream", 4*time.Millisecond)
	IncReplayRedeliveries("jetstream")
	IncInsightsSnapshots(2)
	SetInsightsStateInstrumentsActive(3)
	IncInsightsStateEvictions("ttl")

	mfs, err := Registry().Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	joined := ""
	for _, mf := range mfs {
		joined += mf.GetName() + "\n"
	}
	for _, want := range []string{
		"ingest_messages_total",
		"ingest_latency_seconds",
		"ws_connections_active",
		"bus_dropped_total",
		"bus_publish_errors_total",
		"bus_publish_latency_seconds",
		"bus_consumed_total",
		"bus_redelivered_total",
		"bus_ack_latency_seconds",
		"bus_consumer_lag",
		"replay_messages_total",
		"replay_latency_seconds",
		"replay_redeliveries_total",
		"insights_snapshots_total",
		"insights_state_instruments_active",
		"insights_state_evictions_total",
		"guardian_rate_limited_total",
		"process_heap_alloc_bytes",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected metric %s in registry", want)
		}
	}
}

func TestInsightsSnapshotBucketsBounded(t *testing.T) {
	before := insightsSnapshotSeriesCount(t)
	for i := 1; i <= 512; i++ {
		IncInsightsSnapshots(i)
	}
	after := insightsSnapshotSeriesCount(t)
	if growth := after - before; growth > 4 {
		t.Fatalf("expected bounded insights_snapshots_total cardinality growth <= 4, got %d", growth)
	}
}

func insightsSnapshotSeriesCount(t *testing.T) int {
	t.Helper()

	mfs, err := Registry().Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() == "insights_snapshots_total" {
			return len(mf.GetMetric())
		}
	}

	t.Fatal("insights_snapshots_total metric family not found")
	return 0
}
