package metrics

import (
	"sort"
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
	before := testutil.ToFloat64(IngestMessagesTotal.WithLabelValues("unknown", "unknown", "unknown"))

	ObserveIngest("binance", "BTCUSDT", "marketdata.trade", "ok", 2*time.Millisecond)
	ObserveIngest("BINANCE", "trade_id=123", "marketdata.trade", "wild_status", 2*time.Millisecond)

	if got := testutil.ToFloat64(IngestMessagesTotal.WithLabelValues("binance", "marketdata.trade", "ok")); got < 1 {
		t.Fatalf("expected ingest counter increment, got %f", got)
	}

	// wild_status is sanitized to "unknown"; instrument label is no longer present
	after := testutil.ToFloat64(IngestMessagesTotal.WithLabelValues("binance", "marketdata.trade", "unknown"))
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
	SetWSQueueDepth(2)
	IncWSDrops("queue_full")
	ObserveWSSendLatency(5 * time.Millisecond)
	IncWSClientsConnected()
	DecWSClientsConnected()
	IncBusPublishError("timeout")
	ObserveBusPublishLatency("jetstream", 2*time.Millisecond)
	IncBusConsumed("jetstream", "ok")
	IncBusRedelivered("jetstream")
	ObserveBusAckLatency("jetstream", 3*time.Millisecond)
	SetBusConsumerLag("jetstream", 42)
	IncIngestQuarantine("decode_failed")
	IncIngestDrop("unknown_event_type")
	IncIngestNak("transient_failure")
	IncIngestTerm("validation_failed")
	IncIngestBoundedMapEvictions("max_instruments")
	IncReplayMessages("jetstream", "ok")
	ObserveReplayLatency("jetstream", 4*time.Millisecond)
	IncReplayRedeliveries("jetstream")
	IncInsightsSnapshots(2)
	SetInsightsStateInstrumentsActive(3)
	IncInsightsStateEvictions("ttl")
	SetVPVROverloadLevel("binance", "BTC-USDT", "1m", 2)
	IncVPVRDrop("delta_l3")
	IncVPVRDegrade("compress")
	ObserveVPVRCompressRatio(0.5)
	ObserveVPVRProcessingLatencyMilliseconds(4)
	SetPolicyKitOverloadLevel("marketdata.bookdelta", "binance", "BTC-USDT", 2)
	IncPolicyKitDrop("marketdata.bookdelta", "binance", "delta_l3")
	IncPolicyKitDegrade("marketdata.bookdelta", "binance", "stride_2")
	IncPolicyKitCompress("insights.volume_profile_snapshot")
	ObservePolicyKitLatencyMilliseconds("marketdata.bookdelta", 1.5)
	ObserveHeatmapBuildLatency("binance", "BTC-USDT", "1m", 3*time.Millisecond)
	SetHeatmapCells("binance", "BTC-USDT", "1m", 42)
	ObserveHeatmapPayloadBytes("binance", "BTC-USDT", "1m", 2048)
	IncHeatmapDrop("queue_full")
	SetHeatmapQueueDepth("binance", "BTC-USDT", 7)

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
		"ws_queue_depth",
		"ws_drops_total",
		"ws_send_latency_ms",
		"ws_clients_connected",
		"bus_dropped_total",
		"bus_publish_errors_total",
		"bus_publish_latency_seconds",
		"bus_consumed_total",
		"bus_redelivered_total",
		"bus_ack_latency_seconds",
		"bus_consumer_lag",
		"ingest_quarantine_total",
		"ingest_drop_total",
		"ingest_nak_total",
		"ingest_term_total",
		"ingest_bounded_map_evictions_total",
		"replay_messages_total",
		"replay_latency_seconds",
		"replay_redeliveries_total",
		"insights_snapshots_total",
		"insights_state_instruments_active",
		"insights_state_evictions_total",
		"vpvr_overload_level",
		"vpvr_drop_total",
		"vpvr_degrade_total",
		"vpvr_compress_ratio",
		"vpvr_processing_latency_ms",
		"policykit_overload_level",
		"policykit_drop_total",
		"policykit_degrade_total",
		"policykit_compress_total",
		"policykit_latency_ms",
		"heatmap_build_latency_ms",
		"heatmap_cells_total",
		"heatmap_payload_bytes",
		"heatmap_drop_total",
		"heatmap_queue_depth",
		"guardian_rate_limited_total",
		"process_heap_alloc_bytes",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected metric %s in registry", want)
		}
	}
}

func TestIngestOutcomeMetrics_ReasonOnlyNoInstrumentLabel(t *testing.T) {
	t.Parallel()

	assertMetricLabelNames(t, "ingest_quarantine_total", []string{"reason"})
	assertMetricLabelNames(t, "ingest_drop_total", []string{"reason"})
	assertMetricLabelNames(t, "ingest_nak_total", []string{"reason"})
	assertMetricLabelNames(t, "ingest_term_total", []string{"reason"})
	assertMetricLabelNames(t, "ingest_bounded_map_evictions_total", []string{"reason"})
}

func TestPolicyKitMetrics_StableLabelsOnly(t *testing.T) {
	t.Parallel()

	SetPolicyKitOverloadLevel("marketdata.bookdelta", "binance", "BTC-USDT", 2)
	IncPolicyKitDrop("marketdata.bookdelta", "binance", "policy_drop")
	IncPolicyKitDegrade("marketdata.bookdelta", "binance", "stride_2")

	assertMetricLabelNames(t, "policykit_overload_level", []string{"stream", "venue"})
	assertMetricLabelNames(t, "policykit_drop_total", []string{"stream", "venue"})
	assertMetricLabelNames(t, "policykit_degrade_total", []string{"stream", "venue"})
}

func TestSanitizeSubsystemMultiExchange(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"marketdata", "marketdata"},
		{"aggregation", "aggregation"},
		{"delivery", "delivery"},
		{"insights", "insights"},
		{"marketdata:binance", "marketdata"},
		{"marketdata:bybit", "marketdata"},
		{"MARKETDATA:BINANCE", "marketdata"},
		{" marketdata:bybit ", "marketdata"},
		{"unknown_subsystem", "unknown"},
		{":leading_colon", "unknown"},
		{"", "unknown"},
	}
	for _, tt := range tests {
		got := sanitizeSubsystem(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeSubsystem(%q) = %q; want %q", tt.input, got, tt.want)
		}
	}
}

func TestGuardianMetricsMultiExchangeSubsystem(t *testing.T) {
	// Verify that multi-exchange subsystem keys produce correct metric labels
	// instead of "unknown".
	IncGuardianRestart("marketdata:binance", "ok")
	IncGuardianDegraded("marketdata:bybit")
	SetGuardianSubsystemState("marketdata:binance", 1)

	if got := testutil.ToFloat64(GuardianRestartsTotal.WithLabelValues("marketdata", "ok")); got < 1 {
		t.Fatalf("expected guardian restart increment for marketdata:binance, got %f", got)
	}
	if got := testutil.ToFloat64(GuardianDegradedTotal.WithLabelValues("marketdata")); got < 1 {
		t.Fatalf("expected guardian degraded increment for marketdata:bybit, got %f", got)
	}
	if got := testutil.ToFloat64(GuardianSubsystemState.WithLabelValues("marketdata")); got != 1 {
		t.Fatalf("expected guardian subsystem state 1 for marketdata:binance, got %f", got)
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

func assertMetricLabelNames(t *testing.T, metricName string, want []string) {
	t.Helper()

	mfs, err := Registry().Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() != metricName {
			continue
		}
		if len(mf.GetMetric()) == 0 {
			t.Fatalf("metric %s has no samples", metricName)
		}

		got := make([]string, 0, len(mf.GetMetric()[0].GetLabel()))
		for _, lp := range mf.GetMetric()[0].GetLabel() {
			got = append(got, lp.GetName())
		}
		sort.Strings(got)
		wantSorted := append([]string(nil), want...)
		sort.Strings(wantSorted)
		if strings.Join(got, ",") != strings.Join(wantSorted, ",") {
			t.Fatalf("metric %s labels=%v want=%v", metricName, got, wantSorted)
		}
		for _, label := range got {
			if label == "instrument" {
				t.Fatalf("metric %s must not expose instrument label", metricName)
			}
		}
		return
	}

	t.Fatalf("metric family %s not found", metricName)
}
