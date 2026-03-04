package metrics

import (
	"fmt"
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
	IncWSDropped("queue_full", "trade", "high_volume")
	IncWSCompressApplied()
	AddWSCompressBytesIn(2048)
	AddWSCompressBytesOut(512)
	IncWSBatchFrames()
	AddWSBatchEvents(4)
	IncWSMessagesOut("trade")
	AddWSBytesOut("trade", 128)
	SetWSLag("trade", 42)
	ObserveWSPublishToDeliverLatency("trade", 5*time.Millisecond)
	IncWSSerializeErrors()
	IncWSAuthFail()
	IncWSResync()
	ObserveWSSendLatency(5 * time.Millisecond)
	IncWSClientsConnected()
	DecWSClientsConnected()
	IncWSClientsConnectedByMode("v1")
	DecWSClientsConnectedByMode("v1")
	IncWSControlFrame("metrics")
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
	ObserveVPVRProcessingLatencySeconds(0.004)
	SetPolicyKitOverloadLevel("marketdata.bookdelta", "binance", "BTC-USDT", 2)
	IncPolicyKitDrop("marketdata.bookdelta", "binance", "delta_l3")
	IncPolicyKitDegrade("marketdata.bookdelta", "binance", "stride_2")
	IncPolicyKitCompress("insights.volume_profile_snapshot")
	ObservePolicyKitLatencySeconds("marketdata.bookdelta", 0.0015)
	ObserveHeatmapBuildLatency("binance", "BTC-USDT", "1m", 3*time.Millisecond)
	SetHeatmapCells("binance", "BTC-USDT", "1m", 42)
	ObserveHeatmapPayloadBytes("binance", "BTC-USDT", "1m", 2048)
	IncHeatmapDrop("queue_full")
	SetHeatmapQueueDepth("binance", "BTC-USDT", 7)
	IncDeliveryRangeAliasFallback("hit")
	SetDeliveryWSSnapshotCacheEntries(3)
	IncDeliveryWSSnapshotCacheHit()
	IncDeliveryWSSnapshotCacheMiss()
	SetDeliveryRouterCoherenceMode("sticky_session")
	IncDeliveryRouterCoherenceViolation("seq_non_monotonic")
	IncDeliveryRouterCoherenceViolation("seq_invalid")
	SetDeliveryRouterStreamStateEntries(5)
	AddDeliveryRouterStreamStateEvicted(2)
	SetDeliveryRouterStreamStateActive(3)
	IncWSResyncRejected("snapshot_unavailable")
	IncWSContractViolation("missing_ts_server")
	SetMROrderBookLevels("binance", "BTC-USDT", "bid", 42)
	SetMROrderBookSpreadBPS("binance", "BTC-USDT", 12.5)
	ObserveMROrderBookUpdateDuration("binance", 3*time.Millisecond)
	AddMROrderBookPruned("binance", "BTC-USDT", 2)
	IncMROrderBookCrossed("binance", "BTC-USDT")
	IncMROrderBookStale("binance", "BTC-USDT")
	SetMRWindowOpen("binance", "BTC-USDT", "1m", 1)
	IncMRWindowLateArrival("binance", "BTC-USDT", "1m")
	IncMRWindowForceClose("binance", "BTC-USDT", "1m")
	SetMRXVenueSpreadBPS("BTC-USDT", 3.2)
	SetMRXVenueDivergenceBPS("BTC-USDT", 1.1)
	ObserveMRXVenueMergeDuration("BTC-USDT", 2*time.Millisecond)
	SetMRXVenueVenuesActive("BTC-USDT", 2)
	SetEvidenceBufferEntries("sweep", 10)
	IncEvidenceBufferOverwrites("sweep")
	SetMRRegimeCurrent("binance", "BTC-USDT", "1m", "trending")
	SetMRRegimeStrength("binance", "BTC-USDT", "1m", 0.82)
	IncMRRegimeTransition("binance", "BTC-USDT", "1m", "ranging", "trending")
	ObserveMRRegimeDetectionDuration("binance", "BTC-USDT", "1m", 60*time.Second)
	IncMRSignalEmitted("absorption", "binance", "BTC-USDT", "high")
	IncMRSignalDeduplicated("absorption", "binance", "BTC-USDT")
	IncMRSignalRateLimited("binance", "BTC-USDT")
	ObserveMRSignalCompositionDuration("absorption", 3*time.Millisecond)
	ObserveMRSignalConfidence("absorption", 0.87)
	IncMRSignalCorrelationHit("absorption")
	IncMRSignalRegimeBoost("absorption", "trending")
	IncMRSignalWSDelivered("absorption", "binance", "BTC-USDT")
	IncMRSignalWSSubscriptionRejected("max_signal_subscriptions")
	IncMRSignalWSActiveSubscriptions()
	DecMRSignalWSActiveSubscriptions()

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
		"ws_queue_len",
		"ws_drops_total",
		"ws_dropped_total",
		"ws_compress_applied_total",
		"ws_compress_bytes_in_total",
		"ws_compress_bytes_out_total",
		"ws_batch_frames_total",
		"ws_batch_events_total",
		"ws_messages_out_total",
		"ws_bytes_out_total",
		"ws_lag_ms",
		"ws_lag_seconds",
		"ws_publish_to_deliver_latency_ms",
		"ws_publish_to_deliver_latency_seconds",
		"ws_send_latency_ms",
		"ws_send_latency_seconds",
		"ws_clients_connected",
		"ws_clients_connected_by_mode",
		"ws_clients_total",
		"ws_subscriptions_active",
		"ws_control_frames_total",
		"serialize_errors_total",
		"auth_fail_total",
		"resync_total",
		"ws_resync_rejected_total",
		"ws_contract_violations_total",
		"ws_limit_rejections_total",
		"ws_effective_limits",
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
		"vpvr_processing_latency_seconds",
		"policykit_overload_level",
		"policykit_drop_total",
		"policykit_degrade_total",
		"policykit_compress_total",
		"policykit_latency_seconds",
		"heatmap_build_latency_seconds",
		"heatmap_cells_total",
		"heatmap_payload_bytes",
		"heatmap_drop_total",
		"heatmap_queue_depth",
		"guardian_rate_limited_total",
		"process_heap_alloc_bytes",
		"mr_orderbook_levels_total",
		"mr_orderbook_spread_bps",
		"mr_orderbook_update_duration_seconds",
		"mr_orderbook_prune_total",
		"mr_orderbook_crossed_total",
		"mr_orderbook_stale_total",
		"mr_window_open_total",
		"mr_window_late_arrival_total",
		"mr_window_force_close_total",
		"mr_xvenue_spread_bps",
		"mr_xvenue_divergence_bps",
		"mr_xvenue_merge_duration_seconds",
		"mr_xvenue_venues_active",
		"evidence_buffer_entries",
		"evidence_buffer_overwrites_total",
		"mr_regime_current",
		"mr_regime_strength",
		"mr_regime_transition_total",
		"mr_regime_detection_duration_seconds",
		"mr_signal_emitted_total",
		"mr_signal_deduplicated_total",
		"mr_signal_rate_limited_total",
		"mr_signal_composition_duration_seconds",
		"mr_signal_confidence_distribution",
		"mr_signal_correlation_hit_total",
		"mr_signal_regime_boost_total",
		"mr_signal_ws_delivered_total",
		"mr_signal_ws_subscription_rejected_total",
		"mr_signal_ws_subscriptions_active",
		"signal_emitted_total",
		"signal_state_entries",
		"signal_evicted_total",
		"signal_eval_latency_seconds",
		"signal_dedup_total",
		"delivery_range_alias_fallback_total",
		"delivery_ws_snapshot_cache_entries",
		"delivery_ws_snapshot_cache_hits_total",
		"delivery_ws_snapshot_cache_misses_total",
		"delivery_router_coherence_mode",
		"delivery_router_coherence_violations_total",
		"router_stream_state_entries",
		"router_stream_state_active_total",
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

func TestCrossVenueMetrics_StableLabelsOnly(t *testing.T) {
	t.Parallel()
	SetMRXVenueSpreadBPS("BTC-USDT", 2.1)
	SetMRXVenueDivergenceBPS("BTC-USDT", 0.7)
	ObserveMRXVenueMergeDuration("BTC-USDT", time.Millisecond)
	SetMRXVenueVenuesActive("BTC-USDT", 2)

	assertMetricLabelNames(t, "mr_xvenue_spread_bps", []string{"instrument_bucket"})
	assertMetricLabelNames(t, "mr_xvenue_divergence_bps", []string{"instrument_bucket"})
	assertMetricLabelNames(t, "mr_xvenue_merge_duration_seconds", []string{"instrument_bucket"})
	assertMetricLabelNames(t, "mr_xvenue_venues_active", []string{"instrument_bucket"})
}

func TestEvidenceBufferMetrics_StableLabelsOnly(t *testing.T) {
	t.Parallel()
	SetEvidenceBufferEntries("sweep", 2)
	IncEvidenceBufferOverwrites("sweep")

	assertMetricLabelNames(t, "evidence_buffer_entries", []string{"kind"})
	assertMetricLabelNames(t, "evidence_buffer_overwrites_total", []string{"kind"})
}

func TestRegimeMetrics_StableLabelsOnly(t *testing.T) {
	t.Parallel()

	SetMRRegimeCurrent("binance", "BTC-USDT", "1m", "trending")
	SetMRRegimeStrength("binance", "BTC-USDT", "1m", 0.8)
	IncMRRegimeTransition("binance", "BTC-USDT", "1m", "ranging", "trending")
	ObserveMRRegimeDetectionDuration("binance", "BTC-USDT", "1m", time.Minute)

	assertMetricLabelNames(t, "mr_regime_current", []string{"instrument_bucket", "kind", "timeframe_bucket", "venue"})
	assertMetricLabelNames(t, "mr_regime_strength", []string{"instrument_bucket", "timeframe_bucket", "venue"})
	assertMetricLabelNames(t, "mr_regime_transition_total", []string{"from", "instrument_bucket", "timeframe_bucket", "to", "venue"})
	assertMetricLabelNames(t, "mr_regime_detection_duration_seconds", []string{"instrument_bucket", "timeframe_bucket", "venue"})
}

func TestRegimeCurrentMetric_BoundedCardinality(t *testing.T) {
	before := mrRegimeCurrentSeriesCount(t)
	for i := 0; i < 512; i++ {
		SetMRRegimeCurrent("binance", fmt.Sprintf("PAIR-%d", i), "weird_tf", "unsupported_kind")
	}
	after := mrRegimeCurrentSeriesCount(t)
	if growth := after - before; growth > 6 {
		t.Fatalf("mr_regime_current cardinality growth=%d want<=6", growth)
	}
}

func TestSignalMetrics_StableLabelsOnly(t *testing.T) {
	t.Parallel()

	IncMRSignalEmitted("absorption", "binance", "BTC-USDT", "high")
	IncMRSignalDeduplicated("absorption", "binance", "BTC-USDT")
	IncMRSignalRateLimited("binance", "BTC-USDT")
	ObserveMRSignalCompositionDuration("absorption", 2*time.Millisecond)
	ObserveMRSignalConfidence("absorption", 0.91)
	IncMRSignalCorrelationHit("absorption")
	IncMRSignalRegimeBoost("absorption", "trending")

	assertMetricLabelNames(t, "mr_signal_emitted_total", []string{"instrument_bucket", "kind", "severity", "venue"})
	assertMetricLabelNames(t, "mr_signal_deduplicated_total", []string{"instrument_bucket", "kind", "venue"})
	assertMetricLabelNames(t, "mr_signal_rate_limited_total", []string{"instrument_bucket", "venue"})
	assertMetricLabelNames(t, "mr_signal_composition_duration_seconds", []string{"kind"})
	assertMetricLabelNames(t, "mr_signal_confidence_distribution", []string{"kind"})
	assertMetricLabelNames(t, "mr_signal_correlation_hit_total", []string{"kind"})
	assertMetricLabelNames(t, "mr_signal_regime_boost_total", []string{"kind", "regime"})
}

func TestSignalEmitted_BoundedCardinality(t *testing.T) {
	before := signalEmittedSeriesCount(t)
	for i := 0; i < 512; i++ {
		IncMRSignalEmitted(
			"kind_"+string(rune('a'+(i%26))),
			"binance",
			fmt.Sprintf("PAIR-%d", i),
			"severity_"+string(rune('a'+(i%26))),
		)
	}
	after := signalEmittedSeriesCount(t)
	if growth := after - before; growth > 60 {
		t.Fatalf("mr_signal_emitted_total cardinality growth=%d want<=60", growth)
	}
}

func TestSignalWSMetrics_StableLabelsOnly(t *testing.T) {
	t.Parallel()

	IncMRSignalWSDelivered("absorption", "binance", "BTC-USDT")
	IncMRSignalWSSubscriptionRejected("max_signal_subscriptions")
	IncMRSignalWSActiveSubscriptions()
	DecMRSignalWSActiveSubscriptions()

	assertMetricLabelNames(t, "mr_signal_ws_delivered_total", []string{"instrument_bucket", "kind", "venue"})
	assertMetricLabelNames(t, "mr_signal_ws_subscription_rejected_total", []string{"reason"})
}

func TestSignalWSActiveSubscriptions_NonNegative(t *testing.T) {
	before := testutil.ToFloat64(MRSignalWSSubscriptionsActive)
	DecMRSignalWSActiveSubscriptions()
	mid := testutil.ToFloat64(MRSignalWSSubscriptionsActive)
	if mid < 0 {
		t.Fatalf("mr_signal_ws_subscriptions_active=%f want >= 0", mid)
	}
	IncMRSignalWSActiveSubscriptions()
	DecMRSignalWSActiveSubscriptions()
	after := testutil.ToFloat64(MRSignalWSSubscriptionsActive)
	if after < 0 {
		t.Fatalf("mr_signal_ws_subscriptions_active=%f want >= 0", after)
	}
	if before < 0 {
		t.Fatalf("initial mr_signal_ws_subscriptions_active=%f want >= 0", before)
	}
}

func TestSignalEngineMetrics_StableLabelsOnly(t *testing.T) {
	t.Parallel()

	IncSignalEmitted("liquidity_collapse", "high")
	SetSignalStateEntries(10)
	IncSignalEvicted("tenant_cap")
	ObserveSignalEvalLatency(5 * time.Millisecond)
	IncSignalDedup("liquidity_collapse")

	assertMetricLabelNames(t, "signal_emitted_total", []string{"severity", "type"})
	assertMetricLabelNames(t, "signal_state_entries", nil)
	assertMetricLabelNames(t, "signal_evicted_total", []string{"reason"})
	assertMetricLabelNames(t, "signal_eval_latency_seconds", nil)
	assertMetricLabelNames(t, "signal_dedup_total", []string{"type"})
}

func TestWSExtendedMetrics_StableLabelsOnly(t *testing.T) {
	t.Parallel()
	assertMetricLabelNames(t, "ws_dropped_total", []string{"reason", "channel", "priority"})
	assertMetricLabelNames(t, "ws_messages_out_total", []string{"channel"})
	assertMetricLabelNames(t, "ws_bytes_out_total", []string{"channel"})
	assertMetricLabelNames(t, "ws_lag_ms", []string{"channel"})
	assertMetricLabelNames(t, "ws_lag_seconds", []string{"channel"})
	assertMetricLabelNames(t, "ws_publish_to_deliver_latency_ms", []string{"channel"})
	assertMetricLabelNames(t, "ws_publish_to_deliver_latency_seconds", []string{"channel"})
	assertMetricLabelNames(t, "ws_clients_connected_by_mode", []string{"mode"})
	assertMetricLabelNames(t, "ws_clients_total", []string{"mode"})
	assertMetricLabelNames(t, "ws_control_frames_total", []string{"type"})
	assertMetricLabelNames(t, "ws_resync_rejected_total", []string{"reason"})
	assertMetricLabelNames(t, "ws_contract_violations_total", []string{"reason"})
	assertMetricLabelNames(t, "ws_limit_rejections_total", []string{"type"})
	assertMetricLabelNames(t, "ws_effective_limits", []string{"limit_name"})
}

func TestWSTenantMetrics_LabelPolicy_WhitelistAndFallbackUnknown(t *testing.T) {
	ConfigureWSTenantLabelPolicy(true, []string{"acme"}, "unknown")
	defer ConfigureWSTenantLabelPolicy(true, nil, "unknown")

	IncWSTenantDrop("acme", "queue_full")
	IncWSTenantDrop("globex", "queue_full")

	if got := testutil.ToFloat64(WSTenantDropsTotal.WithLabelValues("acme", "queue_full")); got < 1 {
		t.Fatalf("expected whitelisted tenant series increment, got=%f", got)
	}
	if got := testutil.ToFloat64(WSTenantDropsTotal.WithLabelValues("unknown", "queue_full")); got < 1 {
		t.Fatalf("expected unknown fallback series increment, got=%f", got)
	}
}

func TestWSTenantMetrics_LabelPolicy_HashBucketBoundsCardinality(t *testing.T) {
	ConfigureWSTenantLabelPolicy(true, []string{"acme"}, "hash_bucket")
	defer ConfigureWSTenantLabelPolicy(true, nil, "unknown")
	before := tenantDropsSeriesCount(t)
	for i := 0; i < 1024; i++ {
		IncWSTenantDrop("tenant-"+strings.Repeat("x", i%8)+string(rune('a'+(i%26))), "queue_full")
	}
	after := tenantDropsSeriesCount(t)
	if growth := after - before; growth > 70 {
		t.Fatalf("ws_tenant_drops_total cardinality growth=%d want<=70", growth)
	}
}

func tenantDropsSeriesCount(t *testing.T) int {
	t.Helper()
	mfs, err := Registry().Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() == "ws_tenant_drops_total" {
			return len(mf.GetMetric())
		}
	}
	t.Fatal("ws_tenant_drops_total metric family not found")
	return 0
}

func TestVPVRAndHeatmapMetrics_BoundedLabelsOnly(t *testing.T) {
	t.Parallel()

	SetVPVRBuilderBucketCount("binance", "BTC-USDT", "1m", 8)
	SetVPVRBuilderWindowsOpen("binance", "BTC-USDT", "1m", 2)
	SetVPVROverloadLevel("binance", "BTC-USDT", "1m", 1)
	ObserveHeatmapBuildLatency("binance", "BTC-USDT", "1m", 2*time.Millisecond)
	SetHeatmapCells("binance", "BTC-USDT", "1m", 50)
	ObserveHeatmapPayloadBytes("binance", "BTC-USDT", "1m", 1024)
	SetHeatmapQueueDepth("binance", "BTC-USDT", 3)

	for _, metricName := range []string{
		"vpvr_builder_bucket_count",
		"vpvr_builder_windows_open",
		"vpvr_overload_level",
		"heatmap_build_latency_seconds",
		"heatmap_cells_total",
		"heatmap_payload_bytes",
	} {
		assertMetricLabelNames(t, metricName, []string{"instrument_bucket", "timeframe_bucket", "venue"})
	}

	assertMetricLabelNames(t, "heatmap_queue_depth", []string{"instrument_bucket", "venue"})
}

func TestMROrderBookMetrics_BoundedLabelsOnly(t *testing.T) {
	t.Parallel()

	SetMROrderBookLevels("binance", "BTC-USDT", "bid", 10)
	SetMROrderBookSpreadBPS("binance", "BTC-USDT", 3.5)
	ObserveMROrderBookUpdateDuration("binance", 2*time.Millisecond)
	AddMROrderBookPruned("binance", "BTC-USDT", 1)
	IncMROrderBookCrossed("binance", "BTC-USDT")
	IncMROrderBookStale("binance", "BTC-USDT")

	assertMetricLabelNames(t, "mr_orderbook_levels_total", []string{"instrument_bucket", "side", "venue"})
	assertMetricLabelNames(t, "mr_orderbook_spread_bps", []string{"instrument_bucket", "venue"})
	assertMetricLabelNames(t, "mr_orderbook_update_duration_seconds", []string{"venue"})
	assertMetricLabelNames(t, "mr_orderbook_prune_total", []string{"instrument_bucket", "venue"})
	assertMetricLabelNames(t, "mr_orderbook_crossed_total", []string{"instrument_bucket", "venue"})
	assertMetricLabelNames(t, "mr_orderbook_stale_total", []string{"instrument_bucket", "venue"})
}

func TestMROrderBookLevels_BoundedCardinality(t *testing.T) {
	before := mrOrderbookLevelsSeriesCount(t)
	for i := 0; i < 512; i++ {
		SetMROrderBookLevels("binance", "PAIR-"+string(rune('A'+(i%26))), "invalid_side", i)
	}
	after := mrOrderbookLevelsSeriesCount(t)
	if growth := after - before; growth > 8 {
		t.Fatalf("mr_orderbook_levels_total cardinality growth=%d want<=8", growth)
	}
}

func TestMRWindowMetrics_BoundedLabelsOnly(t *testing.T) {
	t.Parallel()

	SetMRWindowOpen("binance", "BTC-USDT", "1m", 1)
	IncMRWindowLateArrival("binance", "BTC-USDT", "1m")
	IncMRWindowForceClose("binance", "BTC-USDT", "1m")

	assertMetricLabelNames(t, "mr_window_open_total", []string{"instrument_bucket", "timeframe_bucket", "venue"})
	assertMetricLabelNames(t, "mr_window_late_arrival_total", []string{"instrument_bucket", "timeframe_bucket", "venue"})
	assertMetricLabelNames(t, "mr_window_force_close_total", []string{"instrument_bucket", "timeframe_bucket", "venue"})
}

func TestPolicyKitLatencyDeterministicSampling(t *testing.T) {
	t.Parallel()

	stream := "marketdata.bookdelta_sampling"
	venue := "binance"
	beforeSeconds := policyKitLatencySampleCount(t, "policykit_latency_seconds", sanitizeEventType(stream))
	for i := 0; i < int(policyKitLatencyEveryN*2); i++ {
		ObservePolicyKitLatencySeconds(stream, 0.0015, venue)
	}
	afterSeconds := policyKitLatencySampleCount(t, "policykit_latency_seconds", sanitizeEventType(stream))
	if got := afterSeconds - beforeSeconds; got != 2 {
		t.Fatalf("expected deterministic sampling count delta=2 on seconds metric, got %d", got)
	}
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

func TestInstrumentBucketAllowList(t *testing.T) {
	t.Parallel()

	allowed := map[string]struct{}{
		"unknown": {},
		"btc":     {},
		"eth":     {},
		"stable":  {},
		"major":   {},
		"fiat":    {},
		"other":   {},
	}
	inputs := []string{
		"", "BTC-USDT", "ETHUSDT", "USDCUSD", "BNB-USDT", "SOLUSDT", "EURUSD", "WEIRD-PAIR",
	}
	for _, input := range inputs {
		got := bucketInstrument(input)
		if _, ok := allowed[got]; !ok {
			t.Fatalf("bucketInstrument(%q)=%q not in allow-list", input, got)
		}
	}
}

func TestTimeframeBucketAllowList(t *testing.T) {
	t.Parallel()

	allowed := map[string]struct{}{
		"1s":      {},
		"5s":      {},
		"1m":      {},
		"5m":      {},
		"15m":     {},
		"30m":     {},
		"1h":      {},
		"4h":      {},
		"1d":      {},
		"unknown": {},
	}
	inputs := []string{"1s", "5s", "1m", "5m", "15m", "30m", "1h", "4h", "1d", "7d", ""}
	for _, input := range inputs {
		got := bucketTimeframe(input)
		if _, ok := allowed[got]; !ok {
			t.Fatalf("bucketTimeframe(%q)=%q not in allow-list", input, got)
		}
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

func mrOrderbookLevelsSeriesCount(t *testing.T) int {
	t.Helper()

	mfs, err := Registry().Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() == "mr_orderbook_levels_total" {
			return len(mf.GetMetric())
		}
	}

	t.Fatal("mr_orderbook_levels_total metric family not found")
	return 0
}

func mrRegimeCurrentSeriesCount(t *testing.T) int {
	t.Helper()

	mfs, err := Registry().Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() == "mr_regime_current" {
			return len(mf.GetMetric())
		}
	}

	t.Fatal("mr_regime_current metric family not found")
	return 0
}

func signalEmittedSeriesCount(t *testing.T) int {
	t.Helper()

	mfs, err := Registry().Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() == "mr_signal_emitted_total" {
			return len(mf.GetMetric())
		}
	}

	t.Fatal("mr_signal_emitted_total metric family not found")
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
			if label == "instrument" || label == "timeframe" {
				t.Fatalf("metric %s must not expose unbounded labels instrument/timeframe", metricName)
			}
		}
		return
	}

	t.Fatalf("metric family %s not found", metricName)
}

func policyKitLatencySampleCount(t *testing.T, metricName, stream string) uint64 {
	t.Helper()

	mfs, err := Registry().Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() != metricName {
			continue
		}
		for _, metric := range mf.GetMetric() {
			labels := metric.GetLabel()
			for _, label := range labels {
				if label.GetName() == "stream" && label.GetValue() == stream {
					if h := metric.GetHistogram(); h != nil {
						return h.GetSampleCount()
					}
				}
			}
		}
	}
	return 0
}

// ── ws_legacy_requests_total ─────────────────────────────────────────────────

func TestIncWSLegacyRequest_AcceptedAndRejected(t *testing.T) {
	before := testutil.ToFloat64(WSLegacyRequestsTotal.WithLabelValues("accepted"))
	IncWSLegacyRequest("accepted")
	after := testutil.ToFloat64(WSLegacyRequestsTotal.WithLabelValues("accepted"))
	if after-before != 1 {
		t.Fatalf("accepted: delta=%f want=1", after-before)
	}

	beforeR := testutil.ToFloat64(WSLegacyRequestsTotal.WithLabelValues("rejected"))
	IncWSLegacyRequest("rejected")
	afterR := testutil.ToFloat64(WSLegacyRequestsTotal.WithLabelValues("rejected"))
	if afterR-beforeR != 1 {
		t.Fatalf("rejected: delta=%f want=1", afterR-beforeR)
	}
}

func TestIncWSLegacyRequest_UnknownDefaultsToRejected(t *testing.T) {
	before := testutil.ToFloat64(WSLegacyRequestsTotal.WithLabelValues("rejected"))
	IncWSLegacyRequest("bogus")
	after := testutil.ToFloat64(WSLegacyRequestsTotal.WithLabelValues("rejected"))
	if after-before != 1 {
		t.Fatalf("bogus→rejected: delta=%f want=1", after-before)
	}
}

func TestWSLegacyRequestsTotal_Registered(t *testing.T) {
	count := testutil.CollectAndCount(WSLegacyRequestsTotal)
	if count == 0 {
		t.Fatal("WSLegacyRequestsTotal not registered or has no series")
	}
}

// ── Transcode cache metrics ──────────────────────────────────────────────────

func TestTranscodeCacheMetrics_Registered(t *testing.T) {
	if testutil.ToFloat64(TranscodeCacheEntries) < 0 {
		t.Fatal("TranscodeCacheEntries should be >= 0")
	}
	SetTranscodeCacheEntries(42)
	if got := testutil.ToFloat64(TranscodeCacheEntries); got != 42 {
		t.Fatalf("entries=%f want=42", got)
	}

	SetTranscodeCacheHits(100)
	if got := testutil.ToFloat64(TranscodeCacheHits); got != 100 {
		t.Fatalf("hits=%f want=100", got)
	}

	SetTranscodeCacheMisses(5)
	if got := testutil.ToFloat64(TranscodeCacheMisses); got != 5 {
		t.Fatalf("misses=%f want=5", got)
	}
}

func TestSanitizeTenantID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "valid", input: "acme", want: "acme"},
		{name: "empty", input: "", want: "default"},
		{name: "whitespace", input: "  ", want: "default"},
		{name: "dash_underscore", input: "my-tenant_01", want: "my-tenant_01"},
		{name: "invalid_chars", input: "tenant;drop table", want: "invalid"},
		{name: "special_chars", input: "foo@bar.com", want: "invalid"},
		{name: "truncated_valid", input: "aaaaaaaaaa" + "aaaaaaaaaa" + "aaaaaaaaaa" + "aaaaaaaaaa" + "aaaaaaaaaa" + "aaaaaaaaaa" + "aaaaa", want: "aaaaaaaaaa" + "aaaaaaaaaa" + "aaaaaaaaaa" + "aaaaaaaaaa" + "aaaaaaaaaa" + "aaaaaaaaaa" + "aaaa"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeTenantID(tc.input)
			if got != tc.want {
				t.Fatalf("sanitizeTenantID(%q)=%q want=%q", tc.input, got, tc.want)
			}
		})
	}
}
