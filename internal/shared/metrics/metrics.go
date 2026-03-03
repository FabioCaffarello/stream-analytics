package metrics

import (
	"fmt"
	"hash/fnv"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/market-raccoon/internal/shared/observability"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	statusOK               = "ok"
	statusFailed           = "failed"
	statusDuplicate        = "duplicate"
	statusOutOfOrder       = "out_of_order"
	statusValidationFailed = "validation_failed"
	policyKitLatencyEveryN = uint64(8)
)

var (
	IngestMessagesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ingest_messages_total",
			Help: "Total ingest messages processed by status.",
		},
		[]string{"venue", "event_type", "status"},
	)
	IngestLatencySeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "ingest_latency_seconds",
			Help:    "Ingest pipeline latency in seconds.",
			Buckets: []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05},
		},
		[]string{"venue", "event_type"},
	)
	IngestStreamsActive = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "ingest_streams_active",
			Help: "Number of active ingest streams held in memory.",
		},
	)

	BackpressureQueueDepth = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "backpressure_queue_depth",
			Help: "Current backpressure queue depth.",
		},
		[]string{"venue"},
	)
	BackpressureDropsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "backpressure_drops_total",
			Help: "Total dropped messages in backpressure queue.",
		},
		[]string{"policy"},
	)

	BusPublishedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "bus_published_total",
			Help: "Total messages published successfully to the configured bus.",
		},
		[]string{"event_type", "venue"},
	)
	BusDroppedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "bus_dropped_total",
			Help: "Total dropped bus fanout messages per subscriber.",
		},
		[]string{"subscriber_id"},
	)
	BusPublishErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "bus_publish_errors_total",
			Help: "Total bus publish errors by error kind.",
		},
		[]string{"kind"},
	)
	BusPublishLatencySeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "bus_publish_latency_seconds",
			Help:    "Latency of publish calls to the configured bus backend.",
			Buckets: []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1},
		},
		[]string{"bus_type"},
	)
	BusConsumedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "bus_consumed_total",
			Help: "Total consumed bus messages by status.",
		},
		[]string{"bus_type", "status"},
	)
	BusRedeliveredTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "bus_redelivered_total",
			Help: "Total redelivered bus messages.",
		},
		[]string{"bus_type"},
	)
	BusAckLatencySeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "bus_ack_latency_seconds",
			Help:    "Time between processing start and Ack/Nak/Term operation.",
			Buckets: []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 2, 5},
		},
		[]string{"bus_type"},
	)
	BusConsumerLag = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "bus_consumer_lag",
			Help: "Estimated JetStream consumer lag (NumPending).",
		},
		[]string{"bus_type"},
	)
	IngestQuarantineTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ingest_quarantine_total",
			Help: "Total poison envelopes routed to quarantine by reason.",
		},
		[]string{"reason"},
	)
	IngestDropTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ingest_drop_total",
			Help: "Total explicitly dropped ingest envelopes by reason.",
		},
		[]string{"reason"},
	)
	IngestNakTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ingest_nak_total",
			Help: "Total ingest envelopes NAKed by reason.",
		},
		[]string{"reason"},
	)
	IngestTermTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ingest_term_total",
			Help: "Total ingest envelopes TERMed by reason.",
		},
		[]string{"reason"},
	)
	IngestBoundedMapEvictionsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ingest_bounded_map_evictions_total",
			Help: "Total ingest bounded map evictions by reason.",
		},
		[]string{"reason"},
	)
	ReplayMessagesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "replay_messages_total",
			Help: "Total replay messages by mode and status.",
		},
		[]string{"mode", "status"},
	)
	ReplayLatencySeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "replay_latency_seconds",
			Help:    "Replay processing latency from envelope ts_ingest.",
			Buckets: []float64{0.0001, 0.001, 0.01, 0.1, 1, 5, 15, 30, 60},
		},
		[]string{"mode"},
	)
	ReplayRedeliveriesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "replay_redeliveries_total",
			Help: "Total replay redelivered messages.",
		},
		[]string{"mode"},
	)

	WSConnectionsActive = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ws_connections_active",
			Help: "Active websocket connections.",
		},
		[]string{"venue"},
	)
	WSReconnectsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ws_reconnects_total",
			Help: "Total websocket reconnects.",
		},
		[]string{"venue", "status"},
	)
	WSMessagesReceivedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ws_messages_received_total",
			Help: "Total websocket messages received.",
		},
		[]string{"venue", "event_type"},
	)
	WSErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ws_errors_total",
			Help: "Total websocket errors.",
		},
		[]string{"venue", "status"},
	)
	WSQueueDepth = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "ws_queue_depth",
			Help: "Current outbound websocket queue depth across delivery sessions.",
		},
	)
	WSQueueLen = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "ws_queue_len",
			Help: "Current outbound websocket queue length across delivery sessions.",
		},
	)
	WSDropsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ws_drops_total",
			Help: "Total dropped websocket outbound events by reason.",
		},
		[]string{"reason"},
	)
	WSDroppedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ws_dropped_total",
			Help: "Total dropped websocket outbound events by reason, channel, and priority.",
		},
		[]string{"reason", "channel", "priority"},
	)
	WSCompressAppliedTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "ws_compress_applied_total",
			Help: "Total websocket outbound frames where compression was applied.",
		},
	)
	WSCompressBytesInTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "ws_compress_bytes_in_total",
			Help: "Total uncompressed websocket outbound bytes considered for compression.",
		},
	)
	WSCompressBytesOutTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "ws_compress_bytes_out_total",
			Help: "Total compressed websocket outbound bytes after compression.",
		},
	)
	WSBatchFramesTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "ws_batch_frames_total",
			Help: "Total websocket batched frames emitted.",
		},
	)
	WSBatchEventsTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "ws_batch_events_total",
			Help: "Total websocket events emitted inside batched frames.",
		},
	)
	WSMessagesOutTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ws_messages_out_total",
			Help: "Total websocket outbound messages by channel.",
		},
		[]string{"channel"},
	)
	WSBytesOutTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ws_bytes_out_total",
			Help: "Total websocket outbound bytes by channel.",
		},
		[]string{"channel"},
	)
	WSSendLatencyMilliseconds = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "ws_send_latency_ms",
			Help:    "(DEPRECATED; remove-by=2026-06-30: use ws_send_latency_seconds) Latency in milliseconds for websocket event frame writes.",
			Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 25, 50, 100, 250, 500, 1000},
		},
	)
	WSSendLatencySeconds = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "ws_send_latency_seconds",
			Help:    "Latency in seconds for websocket event frame writes.",
			Buckets: []float64{0.0001, 0.0005, 0.001, 0.002, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1},
		},
	)
	WSPublishToDeliverLatencyMilliseconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "ws_publish_to_deliver_latency_ms",
			Help:    "(DEPRECATED; remove-by=2026-06-30: use ws_publish_to_deliver_latency_seconds) Latency in milliseconds from event publish(ts_ingest) to websocket delivery.",
			Buckets: []float64{0.5, 1, 2, 5, 10, 25, 50, 100, 250, 500, 1000, 2000, 5000},
		},
		[]string{"channel"},
	)
	WSPublishToDeliverLatencySeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "ws_publish_to_deliver_latency_seconds",
			Help:    "Latency in seconds from event publish(ts_ingest) to websocket delivery.",
			Buckets: []float64{0.0005, 0.001, 0.002, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2, 5},
		},
		[]string{"channel"},
	)
	WSLagMilliseconds = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ws_lag_ms",
			Help: "(DEPRECATED; remove-by=2026-06-30: use ws_lag_seconds) Current websocket delivery lag in milliseconds by channel.",
		},
		[]string{"channel"},
	)
	WSLagSeconds = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ws_lag_seconds",
			Help: "Current websocket delivery lag in seconds by channel.",
		},
		[]string{"channel"},
	)
	WSClientsConnected = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "ws_clients_connected",
			Help: "Connected websocket delivery clients.",
		},
	)
	WSClientsConnectedByMode = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ws_clients_connected_by_mode",
			Help: "Connected websocket delivery clients by route compatibility mode.",
		},
		[]string{"mode"},
	)
	WSClientsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ws_clients_total",
			Help: "Total websocket client connections accepted by route compatibility mode.",
		},
		[]string{"mode"},
	)
	WSSubscriptionsActive = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "ws_subscriptions_active",
			Help: "Active websocket subscriptions across all sessions.",
		},
	)
	WSControlFramesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ws_control_frames_total",
			Help: "Total websocket control frames sent by frame type.",
		},
		[]string{"type"},
	)
	WSQueryTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ws_query_total",
			Help: "Total websocket read-path queries by operation and bounded category.",
		},
		[]string{"op", "bc"},
	)
	WSQueryRejectedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ws_query_rejected_total",
			Help: "Total rejected websocket read-path queries by reason.",
		},
		[]string{"reason"},
	)
	WSLimitRejectionsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ws_limit_rejections_total",
			Help: "Total websocket rejections caused by explicit session limits.",
		},
		[]string{"type"},
	)
	WSEffectiveLimits = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ws_effective_limits",
			Help: "Effective websocket session limits after tenant/global resolution.",
		},
		[]string{"limit_name"},
	)
	WSSerializeErrorsTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "serialize_errors_total",
			Help: "Total websocket serialization/transcoding errors.",
		},
	)
	WSAuthFailTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "auth_fail_total",
			Help: "Total websocket authentication/authorization failures.",
		},
	)
	WSResyncTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "resync_total",
			Help: "Total websocket resync requests handled.",
		},
	)
	WSResyncRejectedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ws_resync_rejected_total",
			Help: "Total websocket resync requests rejected by reason.",
		},
		[]string{"reason"},
	)
	WSContractViolationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ws_contract_violations_total",
			Help: "Total websocket contract violations fixed/rejected by reason.",
		},
		[]string{"reason"},
	)

	// F5: backpressure introspection gauges.
	WSBackpressureLevel = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "ws_backpressure_level",
			Help: "Current session backpressure level (0=ok, 1=elevated, 2=high, 3=critical).",
		},
	)
	WSQueueHighWatermark = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "ws_queue_high_watermark",
			Help: "Peak queue depth since last metrics emission.",
		},
	)

	// F6: tenant-labeled metrics.
	WSTenantDropsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ws_tenant_drops_total",
			Help: "Total dropped messages by tenant and reason.",
		},
		[]string{"tenant_id", "reason"},
	)
	WSTenantQueueDepth = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ws_tenant_queue_depth",
			Help: "Current queue depth by tenant.",
		},
		[]string{"tenant_id"},
	)
	WSTenantConnectionsActive = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ws_tenant_connections_active",
			Help: "Active connections by tenant.",
		},
		[]string{"tenant_id"},
	)
	WSTenantMessagesOutTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ws_tenant_messages_out_total",
			Help: "Total outbound messages by tenant and channel.",
		},
		[]string{"tenant_id", "channel"},
	)

	GuardianRestartsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "guardian_restarts_total",
			Help: "Total guardian restarts by subsystem.",
		},
		[]string{"subsystem", "status"},
	)
	GuardianDegradedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "guardian_degraded_total",
			Help: "Total guardian degraded transitions.",
		},
		[]string{"subsystem"},
	)
	GuardianSubsystemState = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "guardian_subsystem_state",
			Help: "Current subsystem state (0=stopped,1=running,2=degraded).",
		},
		[]string{"subsystem"},
	)
	GuardianRateLimitedTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "guardian_rate_limited_total",
			Help: "Total guardian restart attempts deferred by global limiter.",
		},
	)

	ProcessGoroutines = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "process_goroutines",
			Help: "Current goroutine count.",
		},
	)
	ProcessHeapAllocBytes = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "process_heap_alloc_bytes",
			Help: "Current heap allocations in bytes.",
		},
	)
	ProcessGCPauseSeconds = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "process_gc_pause_seconds",
			Help:    "Go GC pause durations in seconds.",
			Buckets: []float64{0.00005, 0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05},
		},
	)

	AggregationBooksActive = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "aggregation_books_active",
			Help: "Number of active order books held in memory.",
		},
	)
	StreamsEvictedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "streams_evicted_total",
			Help: "Total stream evictions by reason.",
		},
		[]string{"reason"},
	)
	BooksEvictedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "books_evicted_total",
			Help: "Total book evictions by reason.",
		},
		[]string{"reason"},
	)
	InsightsSnapshotsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "insights_snapshots_total",
			Help: "Total cross-venue insight snapshots emitted by bounded venue-count bucket.",
		},
		[]string{"venue_count_bucket"},
	)
	InsightsStateInstrumentsActive = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "insights_state_instruments_active",
			Help: "Number of active instrument states in cross-venue join cache.",
		},
	)
	InsightsStateEvictionsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "insights_state_evictions_total",
			Help: "Total cross-venue join cache evictions by reason.",
		},
		[]string{"reason"},
	)
	EvidenceEmittedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "evidence_emitted_total",
			Help: "Total evidence events emitted by kind and severity.",
		},
		[]string{"kind", "severity"},
	)
	EvidenceStateEntries = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "evidence_state_entries",
			Help: "Number of active per-stream state entries per rule.",
		},
		[]string{"rule"},
	)
	EvidenceStateEntriesTotal = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "evidence_state_entries_total",
			Help: "Total active per-stream state entries across all rules.",
		},
	)
	EvidenceStateEvictedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "evidence_state_evicted_total",
			Help: "Total per-stream state evictions by rule.",
		},
		[]string{"rule"},
	)
	EvidenceEngineEventsTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "evidence_engine_events_total",
			Help: "Total events processed by the evidence engine.",
		},
	)
	VPVRBuilderBucketCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "vpvr_builder_bucket_count",
			Help: "Current VPVR bucket count per active partition window.",
		},
		[]string{"venue", "instrument_bucket", "timeframe_bucket"},
	)
	VPVRBuilderWindowsOpen = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "vpvr_builder_windows_open",
			Help: "Current VPVR open windows per partition.",
		},
		[]string{"venue", "instrument_bucket", "timeframe_bucket"},
	)
	VPVRBuilderOverloadActionsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "vpvr_builder_overload_actions_total",
			Help: "Total VPVR builder overload actions by type.",
		},
		[]string{"action"},
	)
	VPVRBuilderDropTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "vpvr_builder_drop_total",
			Help: "Total VPVR builder dropped trades by reason.",
		},
		[]string{"reason"},
	)
	VPVRBuilderReplayMismatchTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "vpvr_builder_replay_mismatch_total",
			Help: "Total VPVR builder replay/out-of-order mismatches observed while updating buckets.",
		},
	)
	VPVRWriterUpsertOpsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "vpvr_writer_upsert_ops_total",
			Help: "Total VPVR writer upsert operations by status.",
		},
		[]string{"status"},
	)
	VPVRWriterUpsertDedupTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "vpvr_writer_upsert_dedup_total",
			Help: "Total VPVR writer deduplicated upsert operations.",
		},
	)
	VPVRWriterUpsertLatencySeconds = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "vpvr_writer_upsert_latency_seconds",
			Help:    "Latency in seconds for VPVR writer upsert operations.",
			Buckets: []float64{0, 0.0001, 0.0005, 0.001, 0.002, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5},
		},
	)
	VPVRWriterWriteFailTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "vpvr_writer_write_fail_total",
			Help: "Total VPVR writer failures by reason.",
		},
		[]string{"reason"},
	)
	VPVROverloadLevel = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "vpvr_overload_level",
			Help: "Current VPVR overload level per partition.",
		},
		[]string{"venue", "instrument_bucket", "timeframe_bucket"},
	)
	VPVRDropTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "vpvr_drop_total",
			Help: "Total VPVR emit-path drops by reason.",
		},
		[]string{"reason"},
	)
	VPVRDegradeTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "vpvr_degrade_total",
			Help: "Total VPVR emit-path degradations by action.",
		},
		[]string{"action"},
	)
	VPVRCompressRatio = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "vpvr_compress_ratio",
			Help:    "VPVR snapshot compression ratio (compressed buckets / original buckets).",
			Buckets: []float64{0, 0.1, 0.2, 0.25, 0.33, 0.5, 0.66, 0.75, 0.9, 1.0},
		},
	)
	VPVRProcessingLatencySeconds = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "vpvr_processing_latency_seconds",
			Help:    "Latency in seconds observed for VPVR processing in emit path.",
			Buckets: []float64{0, 0.0001, 0.0005, 0.001, 0.002, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5},
		},
	)
	PolicyKitOverloadLevel = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "policykit_overload_level",
			Help: "Current PolicyKit overload level per stream partition.",
		},
		[]string{"stream", "venue"},
	)
	PolicyKitDropTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "policykit_drop_total",
			Help: "Total PolicyKit drops by stream and venue.",
		},
		[]string{"stream", "venue"},
	)
	PolicyKitDegradeTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "policykit_degrade_total",
			Help: "Total PolicyKit degradations by stream and venue.",
		},
		[]string{"stream", "venue"},
	)
	PolicyKitCompressTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "policykit_compress_total",
			Help: "Total PolicyKit compress actions by stream.",
		},
		[]string{"stream"},
	)
	PolicyKitLatencySeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "policykit_latency_seconds",
			Help:    "Latency in seconds for PolicyKit decision+apply path.",
			Buckets: []float64{0, 0.00005, 0.0001, 0.0005, 0.001, 0.002, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25},
		},
		[]string{"stream"},
	)
	HeatmapBuildLatencySeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "heatmap_build_latency_seconds",
			Help:    "Latency in seconds to build one heatmap artifact window.",
			Buckets: []float64{0.0001, 0.0005, 0.001, 0.002, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1},
		},
		[]string{"venue", "instrument_bucket", "timeframe_bucket"},
	)
	HeatmapCellsTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "heatmap_cells_total",
			Help: "Current emitted heatmap cell count by partition.",
		},
		[]string{"venue", "instrument_bucket", "timeframe_bucket"},
	)
	HeatmapPayloadBytes = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "heatmap_payload_bytes",
			Help:    "Size in bytes of emitted heatmap payloads.",
			Buckets: []float64{64, 128, 256, 512, 1024, 2048, 4096, 8192, 16384, 32768, 65536, 131072, 262144},
		},
		[]string{"venue", "instrument_bucket", "timeframe_bucket"},
	)
	HeatmapDropTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "heatmap_drop_total",
			Help: "Total heatmap drops/degradations by reason.",
		},
		[]string{"reason"},
	)
	HeatmapQueueDepth = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "heatmap_queue_depth",
			Help: "Current heatmap pipeline queue depth by partition.",
		},
		[]string{"venue", "instrument_bucket"},
	)

	// ── Shard consumer observability ─────────────────────────────────────
	// These metrics carry a group_id label so each horizontal shard replica
	// can be monitored independently.  They are only populated when
	// jetstream.shard_group_count > 1; at count=1 they remain at zero.

	ShardConsumerLag = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "jetstream_shard_consumer_lag",
			Help: "Estimated JetStream consumer lag (NumPending) per shard group.",
		},
		[]string{"group_id"},
	)
	ShardRedeliveredTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "jetstream_shard_redelivered_total",
			Help: "Total redelivered messages per shard group.",
		},
		[]string{"group_id"},
	)
	ShardAckLatencySeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "jetstream_shard_ack_latency_seconds",
			Help:    "Time between processing start and Ack/Nak/Term per shard group.",
			Buckets: []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 2, 5},
		},
		[]string{"group_id"},
	)
	ShardSkipTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "jetstream_shard_skip_total",
			Help: "Total messages skipped (belong to a different shard group).",
		},
		[]string{"group_id"},
	)
	ShardEventsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "jetstream_shard_events_total",
			Help: "Total events successfully processed per shard group.",
		},
		[]string{"group_id"},
	)
	ShardInfo = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "jetstream_shard_info",
			Help: "Static info labels for shard topology. Value is always 1.",
		},
		[]string{"shard_index", "shard_count"},
	)
	ShardLagBudget = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "jetstream_shard_lag_budget",
			Help: "Configured maximum acceptable lag per shard group. 0 means no budget.",
		},
		[]string{"group_id"},
	)
	ShardTopologyComplete = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "shard_topology_complete",
			Help: "Shard topology completeness state. 1=all shard owners present, 0=incomplete.",
		},
	)
	ShardLeaseAgeSeconds = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "shard_lease_age_seconds",
			Help: "Age in seconds since the current process lease last heartbeat update.",
		},
	)
	ShardOwnerConflictsTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "shard_owner_conflicts_total",
			Help: "Total shard owner conflict detections (dual-owner or lease-lost).",
		},
	)
	ShardLeaseLostTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "shard_lease_lost_total",
			Help: "Total shard lease lost events that triggered processor shutdown.",
		},
	)
	ShardRegistryErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shard_registry_errors_total",
			Help: "Total shard registry operation errors by operation.",
		},
		[]string{"op"},
	)

	// ── Store observability ─────────────────────────────────────────────
	// Tracks envelope consumption and ClickHouse commit outcomes in
	// cmd/store so operators can prove the cold-path is alive.

	StoreConsumedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "store_consumed_total",
			Help: "Total envelopes consumed by the store pipeline by status and reason.",
		},
		[]string{"status", "reason"},
	)
	StoreCommitTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "store_commit_total",
			Help: "Total ClickHouse commit operations in the store by status.",
		},
		[]string{"status"},
	)
	StoreCommitLatencySeconds = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "store_commit_latency_seconds",
			Help:    "ClickHouse commit latency in the store pipeline in seconds.",
			Buckets: []float64{0.0005, 0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 2, 5, 10},
		},
	)
	StoreQuarantineTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "store_quarantine_total",
			Help: "Total store envelopes quarantined by reason.",
		},
		[]string{"reason"},
	)
	StoreBatchSize = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "store_batch_size",
			Help:    "Number of rows per flushed batch in the store pipeline.",
			Buckets: []float64{1, 2, 5, 10, 25, 50, 100, 250, 500, 1000},
		},
	)
	StoreFlushTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "store_flush_total",
			Help: "Total batch flush operations in the store by status.",
		},
		[]string{"status"},
	)
	StoreFlushLatencySeconds = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "store_flush_latency_seconds",
			Help:    "Store batch flush latency in seconds.",
			Buckets: []float64{0.0005, 0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 2, 5, 10},
		},
	)

	// ── Processor observability ──────────────────────────────────────────
	// Tracks envelope processing outcomes in the aggregation processor
	// actor so operators can prove the pipeline is alive.

	ProcessorProcessedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "processor_processed_total",
			Help: "Total envelopes processed by the aggregation processor actor.",
		},
		[]string{"event_type", "status"},
	)
	ProcessorCommitTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "processor_commit_total",
			Help: "Total snapshot commit operations by status.",
		},
		[]string{"status"},
	)
	ProcessorCommitLatencySeconds = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "processor_commit_latency_seconds",
			Help:    "Snapshot commit latency (hot+cold dual-write) in seconds.",
			Buckets: []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 2, 5},
		},
	)
	ProcessorAckAfterCommitTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "processor_ack_after_commit_total",
			Help: "Total processor ack decisions after commit boundary by status.",
		},
		[]string{"status"},
	)

	// ── Codec observability ──────────────────────────────────────────────

	CodecRegistrySize = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "codec_registry_size",
			Help: "Number of registered encoder entries in the payload codec registry.",
		},
	)
	CodecUnknownEventsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "codec_unknown_events_total",
			Help: "Total unknown event type lookups in the payload codec by format.",
		},
		[]string{"format"},
	)

	// ── BoundedMap observability ─────────────────────────────────────────

	BoundedMapSize = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "bounded_map_size",
			Help: "Current number of entries in a BoundedMap instance.",
		},
		[]string{"name"},
	)
	BoundedMapEvictionsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "bounded_map_evictions_total",
			Help: "Total BoundedMap evictions by instance and reason.",
		},
		[]string{"name", "reason"},
	)
	BoundedMapSweepsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "bounded_map_sweeps_total",
			Help: "Total BoundedMap sweep operations by instance.",
		},
		[]string{"name"},
	)

	// ── Delivery router observability ────────────────────────────────────

	DeliveryRouterSubscriptionsActive = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "delivery_router_subscriptions_active",
			Help: "Total active subject subscriptions across all delivery sessions.",
		},
	)
	DeliveryWSSnapshotCacheEntries = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "delivery_ws_snapshot_cache_entries",
			Help: "Total entries currently stored in bounded websocket snapshot cache.",
		},
	)
	DeliveryWSSnapshotCacheHits = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "delivery_ws_snapshot_cache_hits_total",
			Help: "Total cache hits in bounded websocket snapshot cache.",
		},
	)
	DeliveryWSSnapshotCacheMisses = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "delivery_ws_snapshot_cache_misses_total",
			Help: "Total cache misses in bounded websocket snapshot cache.",
		},
	)
	DeliveryRouterEventsRoutedTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "delivery_router_events_routed_total",
			Help: "Total events successfully routed to at least one delivery session.",
		},
	)
	DeliveryRouterEventsRejectedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "delivery_router_events_rejected_total",
			Help: "Total events rejected by the delivery router by reason.",
		},
		[]string{"reason"},
	)
	DeliveryRouterCoherenceMode = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "delivery_router_coherence_mode",
			Help: "Active delivery router stream coherence mode. Value is always 1 for active mode.",
		},
		[]string{"mode"},
	)
	DeliveryRouterCoherenceViolationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "delivery_router_coherence_violations_total",
			Help: "Total stream coherence violations detected by the delivery router.",
		},
		[]string{"type"},
	)
	DeliveryRouterStreamStateEntries = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "router_stream_state_entries",
			Help: "Current number of delivery router stream-state entries.",
		},
	)
	DeliveryRouterStreamStateEvictedTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "delivery_router_stream_state_evicted_total",
			Help: "Total delivery router stream-state entries evicted by TTL sweeps or hard-cap eviction.",
		},
	)
	DeliveryRouterStreamStateEvictedTotalDeprecated = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "router_stream_state_evicted_total",
			Help: "(DEPRECATED; remove-by=2026-06-30: use delivery_router_stream_state_evicted_total) Total delivery router stream-state entries evicted by TTL sweeps.",
		},
	)
	DeliveryRouterStreamStateActiveTotal = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "router_stream_state_active_total",
			Help: "Current number of active delivery router stream states observed during sweep.",
		},
	)
	DeliveryRouterSessionsActive = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "delivery_router_sessions_active",
			Help: "Current number of active sessions registered with the delivery router.",
		},
	)
	DeliveryRouterSessionsRejectedTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "delivery_router_sessions_rejected_total",
			Help: "Total sessions rejected by MaxActiveSessions cap.",
		},
	)
	WSQueueCapacity = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "ws_queue_capacity",
			Help: "Configured outbound websocket queue capacity per session.",
		},
	)
	DeliveryRangeAliasFallbackTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "delivery_range_alias_fallback_total",
			Help: "Total delivery getrange alias fallback attempts by outcome.",
		},
		[]string{"outcome"},
	)

	// ── Legacy strangler metrics ─────────────────────────────────────────

	// ── Transcode cache observability ────────────────────────────────────

	TranscodeCacheEntries = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "transcode_cache_entries",
			Help: "Current entries in the proto-to-JSON transcode cache.",
		},
	)
	TranscodeCacheHits = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "transcode_cache_hits",
			Help: "Cumulative transcode cache hits.",
		},
	)
	TranscodeCacheMisses = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "transcode_cache_misses",
			Help: "Cumulative transcode cache misses.",
		},
	)

	WSLegacyRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ws_legacy_requests_total",
			Help: "Total /ws/marketdata legacy route requests by status (accepted/rejected).",
		},
		[]string{"status"},
	)
)

var (
	registerOnce sync.Once
	gcMu         sync.Mutex
	lastNumGC    uint32
	samplerMu    sync.Map

	venuePattern            = regexp.MustCompile(`^[a-z0-9_\-]{1,24}$`)
	eventTypePattern        = regexp.MustCompile(`^[a-z0-9_.]{1,64}$`)
	policyPattern           = regexp.MustCompile(`^[a-z_]{1,32}$`)
	kindPattern             = regexp.MustCompile(`^[a-z0-9_]{1,48}$`)
	busTypePattern          = regexp.MustCompile(`^[a-z0-9_]{1,24}$`)
	busStatusPattern        = regexp.MustCompile(`^[a-z_]{1,24}$`)
	instrumentBucketAliases = map[string]string{
		"btc":   "btc",
		"xbt":   "btc",
		"eth":   "eth",
		"usdt":  "stable",
		"usdc":  "stable",
		"dai":   "stable",
		"fdusd": "stable",
		"bnb":   "major",
		"sol":   "major",
		"xrp":   "major",
		"ada":   "major",
		"doge":  "major",
		"dot":   "major",
		"avax":  "major",
	}
	timeframeAllowedBuckets = map[string]struct{}{
		"1m":  {},
		"5m":  {},
		"15m": {},
		"1h":  {},
		"4h":  {},
		"1d":  {},
	}
	tenantLabelPolicy = struct {
		mu                    sync.RWMutex
		includeTenantLabel    bool
		whitelist             map[string]struct{}
		fallback              string
		hashBucketCardinality uint32
	}{
		includeTenantLabel:    true,
		whitelist:             nil,
		fallback:              "unknown",
		hashBucketCardinality: 64,
	}
)

func init() {
	registerAll()
}

func registerAll() {
	registerOnce.Do(func() {
		Registry().MustRegister(
			IngestMessagesTotal,
			IngestLatencySeconds,
			IngestStreamsActive,
			BackpressureQueueDepth,
			BackpressureDropsTotal,
			BusPublishedTotal,
			BusDroppedTotal,
			BusPublishErrorsTotal,
			BusPublishLatencySeconds,
			BusConsumedTotal,
			BusRedeliveredTotal,
			BusAckLatencySeconds,
			BusConsumerLag,
			IngestQuarantineTotal,
			IngestDropTotal,
			IngestNakTotal,
			IngestTermTotal,
			IngestBoundedMapEvictionsTotal,
			ReplayMessagesTotal,
			ReplayLatencySeconds,
			ReplayRedeliveriesTotal,
			WSConnectionsActive,
			WSReconnectsTotal,
			WSMessagesReceivedTotal,
			WSErrorsTotal,
			WSQueueDepth,
			WSQueueLen,
			WSDropsTotal,
			WSDroppedTotal,
			WSCompressAppliedTotal,
			WSCompressBytesInTotal,
			WSCompressBytesOutTotal,
			WSBatchFramesTotal,
			WSBatchEventsTotal,
			WSMessagesOutTotal,
			WSBytesOutTotal,
			WSSendLatencyMilliseconds,
			WSSendLatencySeconds,
			WSPublishToDeliverLatencyMilliseconds,
			WSPublishToDeliverLatencySeconds,
			WSLagMilliseconds,
			WSLagSeconds,
			WSClientsConnected,
			WSClientsConnectedByMode,
			WSClientsTotal,
			WSSubscriptionsActive,
			WSControlFramesTotal,
			WSQueryTotal,
			WSQueryRejectedTotal,
			WSLimitRejectionsTotal,
			WSEffectiveLimits,
			WSSerializeErrorsTotal,
			WSAuthFailTotal,
			WSResyncTotal,
			WSResyncRejectedTotal,
			WSContractViolationsTotal,
			WSBackpressureLevel,
			WSQueueHighWatermark,
			WSTenantDropsTotal,
			WSTenantQueueDepth,
			WSTenantConnectionsActive,
			WSTenantMessagesOutTotal,
			GuardianRestartsTotal,
			GuardianDegradedTotal,
			GuardianSubsystemState,
			GuardianRateLimitedTotal,
			ProcessGoroutines,
			ProcessHeapAllocBytes,
			ProcessGCPauseSeconds,
			AggregationBooksActive,
			StreamsEvictedTotal,
			BooksEvictedTotal,
			InsightsSnapshotsTotal,
			InsightsStateInstrumentsActive,
			InsightsStateEvictionsTotal,
			EvidenceEmittedTotal,
			EvidenceStateEntries,
			EvidenceStateEntriesTotal,
			EvidenceStateEvictedTotal,
			EvidenceEngineEventsTotal,
			VPVRBuilderBucketCount,
			VPVRBuilderWindowsOpen,
			VPVRBuilderOverloadActionsTotal,
			VPVRBuilderDropTotal,
			VPVRBuilderReplayMismatchTotal,
			VPVRWriterUpsertOpsTotal,
			VPVRWriterUpsertDedupTotal,
			VPVRWriterUpsertLatencySeconds,
			VPVRWriterWriteFailTotal,
			VPVROverloadLevel,
			VPVRDropTotal,
			VPVRDegradeTotal,
			VPVRCompressRatio,
			VPVRProcessingLatencySeconds,
			PolicyKitOverloadLevel,
			PolicyKitDropTotal,
			PolicyKitDegradeTotal,
			PolicyKitCompressTotal,
			PolicyKitLatencySeconds,
			HeatmapBuildLatencySeconds,
			HeatmapCellsTotal,
			HeatmapPayloadBytes,
			HeatmapDropTotal,
			HeatmapQueueDepth,
			ShardConsumerLag,
			ShardRedeliveredTotal,
			ShardAckLatencySeconds,
			ShardSkipTotal,
			ShardEventsTotal,
			ShardInfo,
			ShardLagBudget,
			ShardTopologyComplete,
			ShardLeaseAgeSeconds,
			ShardOwnerConflictsTotal,
			ShardLeaseLostTotal,
			ShardRegistryErrorsTotal,
			StoreConsumedTotal,
			StoreCommitTotal,
			StoreCommitLatencySeconds,
			StoreQuarantineTotal,
			StoreBatchSize,
			StoreFlushTotal,
			StoreFlushLatencySeconds,
			ProcessorProcessedTotal,
			ProcessorCommitTotal,
			ProcessorCommitLatencySeconds,
			ProcessorAckAfterCommitTotal,
			CodecRegistrySize,
			CodecUnknownEventsTotal,
			BoundedMapSize,
			BoundedMapEvictionsTotal,
			BoundedMapSweepsTotal,
			DeliveryRouterSubscriptionsActive,
			DeliveryWSSnapshotCacheEntries,
			DeliveryWSSnapshotCacheHits,
			DeliveryWSSnapshotCacheMisses,
			DeliveryRouterEventsRoutedTotal,
			DeliveryRouterEventsRejectedTotal,
			DeliveryRouterCoherenceMode,
			DeliveryRouterCoherenceViolationsTotal,
			DeliveryRouterStreamStateEntries,
			DeliveryRouterStreamStateEvictedTotal,
			DeliveryRouterStreamStateEvictedTotalDeprecated,
			DeliveryRouterStreamStateActiveTotal,
			DeliveryRouterSessionsActive,
			DeliveryRouterSessionsRejectedTotal,
			WSQueueCapacity,
			DeliveryRangeAliasFallbackTotal,
			TranscodeCacheEntries,
			TranscodeCacheHits,
			TranscodeCacheMisses,
			WSLegacyRequestsTotal,
		)

		// Pre-create one series for vector metrics so /metrics exposition is stable
		// even before the first domain event is observed.
		IngestMessagesTotal.WithLabelValues("unknown", "unknown", "unknown")
		IngestLatencySeconds.WithLabelValues("unknown", "unknown")
		BackpressureQueueDepth.WithLabelValues("unknown")
		BackpressureDropsTotal.WithLabelValues("unknown")
		BusPublishedTotal.WithLabelValues("unknown", "unknown")
		BusDroppedTotal.WithLabelValues("s256_plus")
		BusPublishErrorsTotal.WithLabelValues("unknown")
		BusPublishLatencySeconds.WithLabelValues("unknown")
		BusConsumedTotal.WithLabelValues("unknown", "unknown")
		BusRedeliveredTotal.WithLabelValues("unknown")
		BusAckLatencySeconds.WithLabelValues("unknown")
		BusConsumerLag.WithLabelValues("unknown")
		IngestQuarantineTotal.WithLabelValues("unknown")
		IngestDropTotal.WithLabelValues("unknown")
		IngestNakTotal.WithLabelValues("unknown")
		IngestTermTotal.WithLabelValues("unknown")
		IngestBoundedMapEvictionsTotal.WithLabelValues("unknown")
		ReplayMessagesTotal.WithLabelValues("unknown", "unknown")
		ReplayLatencySeconds.WithLabelValues("unknown")
		ReplayRedeliveriesTotal.WithLabelValues("unknown")
		WSConnectionsActive.WithLabelValues("unknown")
		WSReconnectsTotal.WithLabelValues("unknown", "unknown")
		WSMessagesReceivedTotal.WithLabelValues("unknown", "unknown")
		WSErrorsTotal.WithLabelValues("unknown", "unknown")
		WSDropsTotal.WithLabelValues("unknown")
		WSDroppedTotal.WithLabelValues("unknown", "unknown", "unknown")
		WSMessagesOutTotal.WithLabelValues("unknown")
		WSBytesOutTotal.WithLabelValues("unknown")
		WSLagMilliseconds.WithLabelValues("unknown")
		WSLagSeconds.WithLabelValues("unknown")
		WSPublishToDeliverLatencyMilliseconds.WithLabelValues("unknown")
		WSPublishToDeliverLatencySeconds.WithLabelValues("unknown")
		WSClientsConnectedByMode.WithLabelValues("v1")
		WSClientsConnectedByMode.WithLabelValues("legacy")
		WSClientsConnectedByMode.WithLabelValues("unknown")
		WSClientsTotal.WithLabelValues("v1")
		WSClientsTotal.WithLabelValues("legacy")
		WSClientsTotal.WithLabelValues("unknown")
		WSControlFramesTotal.WithLabelValues("hello")
		WSControlFramesTotal.WithLabelValues("pong")
		WSControlFramesTotal.WithLabelValues("metrics")
		WSControlFramesTotal.WithLabelValues("ack_resync")
		WSQueryTotal.WithLabelValues("unknown", "unknown")
		WSQueryRejectedTotal.WithLabelValues("unknown")
		WSLimitRejectionsTotal.WithLabelValues("max_subscriptions_per_connection")
		WSLimitRejectionsTotal.WithLabelValues("max_symbols_per_connection")
		WSLimitRejectionsTotal.WithLabelValues("max_frame_bytes")
		WSLimitRejectionsTotal.WithLabelValues("outbound_queue_size")
		WSLimitRejectionsTotal.WithLabelValues("rate_limit")
		WSEffectiveLimits.WithLabelValues("max_subscriptions_per_connection").Set(0)
		WSEffectiveLimits.WithLabelValues("max_symbols_per_connection").Set(0)
		WSEffectiveLimits.WithLabelValues("max_frame_bytes").Set(0)
		WSEffectiveLimits.WithLabelValues("outbound_queue_size").Set(0)
		WSEffectiveLimits.WithLabelValues("rate_limit").Set(0)
		WSResyncRejectedTotal.WithLabelValues("subject_invalid")
		WSResyncRejectedTotal.WithLabelValues("not_subscribed")
		WSResyncRejectedTotal.WithLabelValues("snapshot_unavailable")
		WSContractViolationsTotal.WithLabelValues("missing_ts_server")
		WSContractViolationsTotal.WithLabelValues("unknown_feature")
		GuardianRestartsTotal.WithLabelValues("unknown", "unknown")
		GuardianDegradedTotal.WithLabelValues("unknown")
		GuardianSubsystemState.WithLabelValues("unknown")
		InsightsSnapshotsTotal.WithLabelValues("2")
		InsightsSnapshotsTotal.WithLabelValues("3_4")
		InsightsSnapshotsTotal.WithLabelValues("5_8")
		InsightsSnapshotsTotal.WithLabelValues("9_plus")
		InsightsStateEvictionsTotal.WithLabelValues("unknown")
		EvidenceEmittedTotal.WithLabelValues("spread_explosion", "medium")
		EvidenceEmittedTotal.WithLabelValues("liquidity_thinning", "medium")
		EvidenceEmittedTotal.WithLabelValues("persistent_imbalance", "medium")
		EvidenceEmittedTotal.WithLabelValues("absorption", "medium")
		EvidenceStateEntries.WithLabelValues("spread_explosion")
		EvidenceStateEntries.WithLabelValues("liquidity_thinning")
		EvidenceStateEntries.WithLabelValues("persistent_imbalance")
		EvidenceStateEntries.WithLabelValues("absorption")
		EvidenceStateEvictedTotal.WithLabelValues("spread_explosion")
		EvidenceStateEvictedTotal.WithLabelValues("liquidity_thinning")
		EvidenceStateEvictedTotal.WithLabelValues("persistent_imbalance")
		EvidenceStateEvictedTotal.WithLabelValues("absorption")
		VPVRBuilderBucketCount.WithLabelValues("unknown", "unknown", "unknown")
		VPVRBuilderWindowsOpen.WithLabelValues("unknown", "unknown", "unknown")
		VPVRBuilderOverloadActionsTotal.WithLabelValues("unknown")
		VPVRBuilderDropTotal.WithLabelValues("unknown")
		VPVRWriterUpsertOpsTotal.WithLabelValues("unknown")
		VPVRWriterWriteFailTotal.WithLabelValues("unknown")
		VPVROverloadLevel.WithLabelValues("unknown", "unknown", "unknown")
		VPVRDropTotal.WithLabelValues("unknown")
		VPVRDegradeTotal.WithLabelValues("unknown")
		PolicyKitOverloadLevel.WithLabelValues("unknown", "unknown")
		PolicyKitDropTotal.WithLabelValues("unknown", "unknown")
		PolicyKitDegradeTotal.WithLabelValues("unknown", "unknown")
		PolicyKitCompressTotal.WithLabelValues("unknown")
		PolicyKitLatencySeconds.WithLabelValues("unknown")
		HeatmapBuildLatencySeconds.WithLabelValues("unknown", "unknown", "unknown")
		HeatmapCellsTotal.WithLabelValues("unknown", "unknown", "unknown")
		HeatmapPayloadBytes.WithLabelValues("unknown", "unknown", "unknown")
		HeatmapDropTotal.WithLabelValues("unknown")
		HeatmapQueueDepth.WithLabelValues("unknown", "unknown")
		ShardConsumerLag.WithLabelValues("0")
		ShardRedeliveredTotal.WithLabelValues("0")
		ShardAckLatencySeconds.WithLabelValues("0")
		ShardSkipTotal.WithLabelValues("0")
		ShardEventsTotal.WithLabelValues("0")
		ShardLagBudget.WithLabelValues("0")
		ShardTopologyComplete.Set(0)
		ShardLeaseAgeSeconds.Set(0)
		ShardRegistryErrorsTotal.WithLabelValues("unknown")
		StoreConsumedTotal.WithLabelValues("ok", "snapshot")
		StoreConsumedTotal.WithLabelValues("ok", "skipped")
		StoreConsumedTotal.WithLabelValues("failed", "decode")
		StoreConsumedTotal.WithLabelValues("failed", "commit")
		StoreCommitTotal.WithLabelValues("ok")
		StoreCommitTotal.WithLabelValues("failed")
		StoreQuarantineTotal.WithLabelValues("unknown")
		StoreFlushTotal.WithLabelValues("ok")
		StoreFlushTotal.WithLabelValues("failed")
		ProcessorProcessedTotal.WithLabelValues("unknown", "ok")
		ProcessorProcessedTotal.WithLabelValues("unknown", "failed")
		ProcessorCommitTotal.WithLabelValues("ok")
		ProcessorCommitTotal.WithLabelValues("failed")
		ProcessorAckAfterCommitTotal.WithLabelValues("ok")
		ProcessorAckAfterCommitTotal.WithLabelValues("failed")
		CodecUnknownEventsTotal.WithLabelValues("json")
		CodecUnknownEventsTotal.WithLabelValues("proto")
		BoundedMapSize.WithLabelValues("unknown")
		BoundedMapEvictionsTotal.WithLabelValues("unknown", "size")
		BoundedMapEvictionsTotal.WithLabelValues("unknown", "ttl")
		BoundedMapSweepsTotal.WithLabelValues("unknown")
		DeliveryRouterEventsRejectedTotal.WithLabelValues("contract_policy")
		DeliveryRouterEventsRejectedTotal.WithLabelValues("invalid_subject")
		DeliveryRouterEventsRejectedTotal.WithLabelValues("seq_non_monotonic")
		DeliveryRouterEventsRejectedTotal.WithLabelValues("seq_invalid")
		DeliveryRouterCoherenceMode.WithLabelValues("sticky_session")
		DeliveryRouterCoherenceMode.WithLabelValues("upstream_sequencer")
		DeliveryRouterCoherenceMode.WithLabelValues("unknown")
		DeliveryRouterCoherenceViolationsTotal.WithLabelValues("seq_non_monotonic")
		DeliveryRouterCoherenceViolationsTotal.WithLabelValues("seq_invalid")
		DeliveryRangeAliasFallbackTotal.WithLabelValues("hit")
		DeliveryRangeAliasFallbackTotal.WithLabelValues("miss")
		DeliveryRangeAliasFallbackTotal.WithLabelValues("error")
		WSLegacyRequestsTotal.WithLabelValues("accepted")
		WSLegacyRequestsTotal.WithLabelValues("rejected")
	})
}

func ObserveIngest(venue, instrument, eventType, status string, latency time.Duration) {
	v := sanitizeVenue(venue)
	e := sanitizeEventType(eventType)
	s := sanitizeStatus(status)
	IngestMessagesTotal.WithLabelValues(v, e, s).Inc()
	if s == statusOK {
		IngestLatencySeconds.WithLabelValues(v, e).Observe(latency.Seconds())
	}
}

func SetBackpressureQueueDepth(venue string, depth int) {
	BackpressureQueueDepth.WithLabelValues(sanitizeVenue(venue)).Set(float64(max(depth, 0)))
}

func IncBackpressureDrops(policy string, drops int) {
	if drops <= 0 {
		return
	}
	BackpressureDropsTotal.WithLabelValues(sanitizePolicy(policy)).Add(float64(drops))
}

func IncBusPublished(eventType, venue string) {
	BusPublishedTotal.WithLabelValues(sanitizeEventType(eventType), sanitizeVenue(venue)).Inc()
}

func IncBusDropped(subscriberIndex int) {
	BusDroppedTotal.WithLabelValues(bucketSubscriberID(subscriberIndex)).Inc()
}

func IncBusPublishError(kind string) {
	BusPublishErrorsTotal.WithLabelValues(sanitizeKind(kind)).Inc()
}

func ObserveBusPublishLatency(busType string, latency time.Duration) {
	if latency < 0 {
		latency = 0
	}
	BusPublishLatencySeconds.WithLabelValues(sanitizeBusType(busType)).Observe(latency.Seconds())
}

func IncBusConsumed(busType, status string) {
	BusConsumedTotal.WithLabelValues(sanitizeBusType(busType), sanitizeBusStatus(status)).Inc()
}

func IncBusRedelivered(busType string) {
	BusRedeliveredTotal.WithLabelValues(sanitizeBusType(busType)).Inc()
}

func ObserveBusAckLatency(busType string, latency time.Duration) {
	if latency < 0 {
		latency = 0
	}
	BusAckLatencySeconds.WithLabelValues(sanitizeBusType(busType)).Observe(latency.Seconds())
}

func SetBusConsumerLag(busType string, lag int64) {
	if lag < 0 {
		lag = 0
	}
	BusConsumerLag.WithLabelValues(sanitizeBusType(busType)).Set(float64(lag))
}

func IncIngestQuarantine(reason string) {
	IngestQuarantineTotal.WithLabelValues(sanitizeIngestReason(reason)).Inc()
}

func IncIngestDrop(reason string) {
	IngestDropTotal.WithLabelValues(sanitizeIngestReason(reason)).Inc()
}

func IncIngestNak(reason string) {
	IngestNakTotal.WithLabelValues(sanitizeIngestReason(reason)).Inc()
}

func IncIngestTerm(reason string) {
	IngestTermTotal.WithLabelValues(sanitizeIngestReason(reason)).Inc()
}

func IncIngestBoundedMapEvictions(reason string) {
	IngestBoundedMapEvictionsTotal.WithLabelValues(sanitizeIngestReason(reason)).Inc()
}

func IncReplayMessages(mode, status string) {
	ReplayMessagesTotal.WithLabelValues(sanitizeReplayMode(mode), sanitizeReplayStatus(status)).Inc()
}

func ObserveReplayLatency(mode string, latency time.Duration) {
	if latency < 0 {
		latency = 0
	}
	ReplayLatencySeconds.WithLabelValues(sanitizeReplayMode(mode)).Observe(latency.Seconds())
}

func IncReplayRedeliveries(mode string) {
	ReplayRedeliveriesTotal.WithLabelValues(sanitizeReplayMode(mode)).Inc()
}

func SetWSConnectionsActive(venue string, active int) {
	WSConnectionsActive.WithLabelValues(sanitizeVenue(venue)).Set(float64(max(active, 0)))
}

func IncWSReconnect(venue, status string) {
	WSReconnectsTotal.WithLabelValues(sanitizeVenue(venue), sanitizeStatus(status)).Inc()
}

func IncWSMessageReceived(venue, eventType string) {
	WSMessagesReceivedTotal.WithLabelValues(sanitizeVenue(venue), sanitizeEventType(eventType)).Inc()
}

func IncWSError(venue, status string) {
	WSErrorsTotal.WithLabelValues(sanitizeVenue(venue), sanitizeStatus(status)).Inc()
}

func SetWSQueueDepth(depth int) {
	WSQueueDepth.Set(float64(max(depth, 0)))
	WSQueueLen.Set(float64(max(depth, 0)))
}

func IncWSDrops(reason string) {
	WSDropsTotal.WithLabelValues(sanitizeKind(reason)).Inc()
}

func IncWSDropped(reason, channel, priority string) {
	WSDroppedTotal.WithLabelValues(
		sanitizeKind(reason),
		sanitizeEventType(channel),
		sanitizeKind(priority),
	).Inc()
}

func IncWSCompressApplied() {
	WSCompressAppliedTotal.Inc()
}

func AddWSCompressBytesIn(n int) {
	if n <= 0 {
		return
	}
	WSCompressBytesInTotal.Add(float64(n))
}

func AddWSCompressBytesOut(n int) {
	if n <= 0 {
		return
	}
	WSCompressBytesOutTotal.Add(float64(n))
}

func IncWSBatchFrames() {
	WSBatchFramesTotal.Inc()
}

func AddWSBatchEvents(n int) {
	if n <= 0 {
		return
	}
	WSBatchEventsTotal.Add(float64(n))
}

func IncWSMessagesOut(channel string) {
	WSMessagesOutTotal.WithLabelValues(sanitizeEventType(channel)).Inc()
}

func AddWSBytesOut(channel string, n int) {
	if n <= 0 {
		return
	}
	WSBytesOutTotal.WithLabelValues(sanitizeEventType(channel)).Add(float64(n))
}

func SetWSLag(channel string, lagMs int64) {
	if lagMs < 0 {
		lagMs = 0
	}
	ch := sanitizeEventType(channel)
	WSLagMilliseconds.WithLabelValues(ch).Set(float64(lagMs))
	WSLagSeconds.WithLabelValues(ch).Set(float64(lagMs) / 1000)
}

func ObserveWSSendLatency(latency time.Duration) {
	if latency < 0 {
		latency = 0
	}
	WSSendLatencyMilliseconds.Observe(float64(latency) / float64(time.Millisecond))
	WSSendLatencySeconds.Observe(latency.Seconds())
}

func ObserveWSPublishToDeliverLatency(channel string, latency time.Duration) {
	if latency < 0 {
		latency = 0
	}
	ch := sanitizeEventType(channel)
	WSPublishToDeliverLatencyMilliseconds.WithLabelValues(ch).
		Observe(float64(latency) / float64(time.Millisecond))
	WSPublishToDeliverLatencySeconds.WithLabelValues(ch).
		Observe(latency.Seconds())
}

func IncWSClientsConnected() {
	WSClientsConnected.Inc()
}

func DecWSClientsConnected() {
	WSClientsConnected.Dec()
}

func IncWSClientsConnectedByMode(mode string) {
	m := sanitizeWSClientMode(mode)
	WSClientsConnectedByMode.WithLabelValues(m).Inc()
	WSClientsTotal.WithLabelValues(m).Inc()
}

func DecWSClientsConnectedByMode(mode string) {
	WSClientsConnectedByMode.WithLabelValues(sanitizeWSClientMode(mode)).Dec()
}

func IncWSQuery(op, boundedCategory string) {
	WSQueryTotal.WithLabelValues(sanitizeWSQueryOp(op), sanitizeWSQueryCategory(boundedCategory)).Inc()
}

func IncWSQueryRejected(reason string) {
	WSQueryRejectedTotal.WithLabelValues(sanitizeKind(reason)).Inc()
}

func IncWSLimitRejection(limitType string) {
	WSLimitRejectionsTotal.WithLabelValues(sanitizeKind(limitType)).Inc()
}

func SetWSEffectiveLimit(limitName string, value int) {
	if value < 0 {
		value = 0
	}
	WSEffectiveLimits.WithLabelValues(sanitizeKind(limitName)).Set(float64(value))
}

func SetWSSubscriptionsActive(count int) {
	WSSubscriptionsActive.Set(float64(max(count, 0)))
}

func IncWSControlFrame(frameType string) {
	WSControlFramesTotal.WithLabelValues(sanitizeWSControlFrameType(frameType)).Inc()
}

func IncWSSerializeErrors() {
	WSSerializeErrorsTotal.Inc()
}

func IncWSAuthFail() {
	WSAuthFailTotal.Inc()
}

func IncWSResync() {
	WSResyncTotal.Inc()
}

func IncWSResyncRejected(reason string) {
	WSResyncRejectedTotal.WithLabelValues(sanitizeWSResyncRejectReason(reason)).Inc()
}

func IncWSContractViolation(reason string) {
	WSContractViolationsTotal.WithLabelValues(sanitizeWSContractViolationReason(reason)).Inc()
}

// F5: backpressure introspection helpers.

func SetWSBackpressureLevel(level int) {
	if level < 0 {
		level = 0
	}
	WSBackpressureLevel.Set(float64(level))
}

func SetWSQueueHighWatermark(watermark int) {
	if watermark < 0 {
		watermark = 0
	}
	WSQueueHighWatermark.Set(float64(watermark))
}

// F6: tenant-labeled metric helpers.

var tenantIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

const maxTenantIDLen = 64

// ConfigureWSTenantLabelPolicy controls ws_tenant_* metric tenant label behavior.
func ConfigureWSTenantLabelPolicy(include bool, whitelist []string, fallback string) {
	policyFallback := strings.ToLower(strings.TrimSpace(fallback))
	switch policyFallback {
	case "", "unknown", "hash_bucket":
	default:
		policyFallback = "unknown"
	}
	wl := make(map[string]struct{}, len(whitelist))
	for _, tenant := range whitelist {
		id := strings.TrimSpace(tenant)
		if id == "" {
			continue
		}
		if len(id) > maxTenantIDLen {
			id = id[:maxTenantIDLen]
		}
		if !tenantIDPattern.MatchString(id) {
			continue
		}
		wl[id] = struct{}{}
	}
	tenantLabelPolicy.mu.Lock()
	tenantLabelPolicy.includeTenantLabel = include
	if len(wl) == 0 {
		tenantLabelPolicy.whitelist = nil
	} else {
		tenantLabelPolicy.whitelist = wl
	}
	tenantLabelPolicy.fallback = policyFallback
	tenantLabelPolicy.mu.Unlock()
}

func sanitizeTenantID(tenantID string) string {
	id := strings.TrimSpace(tenantID)
	tenantLabelPolicy.mu.RLock()
	include := tenantLabelPolicy.includeTenantLabel
	whitelist := tenantLabelPolicy.whitelist
	fallback := tenantLabelPolicy.fallback
	buckets := tenantLabelPolicy.hashBucketCardinality
	tenantLabelPolicy.mu.RUnlock()

	if !include {
		return "unknown"
	}
	if id == "" {
		if len(whitelist) == 0 {
			return "default"
		}
		return "unknown"
	}
	if len(id) > maxTenantIDLen {
		id = id[:maxTenantIDLen]
	}
	if tenantIDPattern.MatchString(id) {
		if len(whitelist) == 0 {
			return id
		}
		if _, ok := whitelist[id]; ok {
			return id
		}
	} else {
		if len(whitelist) == 0 {
			return "invalid"
		}
		id = strings.TrimSpace(tenantID)
		if id == "" {
			id = "unknown"
		}
	}
	if fallback == "hash_bucket" && buckets > 0 {
		h := fnv.New32a()
		_, _ = h.Write([]byte(id))
		return fmt.Sprintf("tenant_bucket_%02d", h.Sum32()%buckets)
	}
	return "unknown"
}

func IncWSTenantDrop(tenantID, reason string) {
	WSTenantDropsTotal.WithLabelValues(sanitizeTenantID(tenantID), sanitizeKind(reason)).Inc()
}

func SetWSTenantQueueDepth(tenantID string, depth int) {
	if depth < 0 {
		depth = 0
	}
	WSTenantQueueDepth.WithLabelValues(sanitizeTenantID(tenantID)).Set(float64(depth))
}

func IncWSTenantConnectionsActive(tenantID string) {
	WSTenantConnectionsActive.WithLabelValues(sanitizeTenantID(tenantID)).Inc()
}

func DecWSTenantConnectionsActive(tenantID string) {
	WSTenantConnectionsActive.WithLabelValues(sanitizeTenantID(tenantID)).Dec()
}

func IncWSTenantMessagesOut(tenantID, channel string) {
	WSTenantMessagesOutTotal.WithLabelValues(sanitizeTenantID(tenantID), sanitizeEventType(channel)).Inc()
}

func IncGuardianRestart(subsystem, status string) {
	GuardianRestartsTotal.WithLabelValues(sanitizeSubsystem(subsystem), sanitizeStatus(status)).Inc()
}

func IncGuardianDegraded(subsystem string) {
	GuardianDegradedTotal.WithLabelValues(sanitizeSubsystem(subsystem)).Inc()
}

func SetGuardianSubsystemState(subsystem string, state float64) {
	if state < 0 {
		state = 0
	}
	if state > 2 {
		state = 2
	}
	GuardianSubsystemState.WithLabelValues(sanitizeSubsystem(subsystem)).Set(state)
}

func IncStreamsEvicted(reason string) {
	StreamsEvictedTotal.WithLabelValues(sanitizeReason(reason)).Inc()
}

func IncBooksEvicted(reason string) {
	BooksEvictedTotal.WithLabelValues(sanitizeReason(reason)).Inc()
}

func IncInsightsSnapshots(venueCount int) {
	InsightsSnapshotsTotal.WithLabelValues(bucketVenueCount(venueCount)).Inc()
}

func SetInsightsStateInstrumentsActive(active float64) {
	if active < 0 {
		active = 0
	}
	InsightsStateInstrumentsActive.Set(active)
}

func IncInsightsStateEvictions(reason string) {
	InsightsStateEvictionsTotal.WithLabelValues(sanitizeReason(reason)).Inc()
}

func IncEvidenceEmitted(kind, severity string) {
	EvidenceEmittedTotal.WithLabelValues(kind, severity).Inc()
}

func SetEvidenceStateEntries(rule string, count int) {
	if count < 0 {
		count = 0
	}
	EvidenceStateEntries.WithLabelValues(rule).Set(float64(count))
}

func SetEvidenceStateEntriesTotal(count int) {
	if count < 0 {
		count = 0
	}
	EvidenceStateEntriesTotal.Set(float64(count))
}

func IncEvidenceStateEvicted(rule string) {
	EvidenceStateEvictedTotal.WithLabelValues(rule).Inc()
}

func IncEvidenceEngineEvents() {
	EvidenceEngineEventsTotal.Inc()
}

func SetVPVRBuilderBucketCount(venue, instrument, timeframe string, count int) {
	if count < 0 {
		count = 0
	}
	VPVRBuilderBucketCount.WithLabelValues(
		sanitizeVenue(venue),
		bucketInstrument(instrument),
		bucketTimeframe(timeframe),
	).Set(float64(count))
}

func SetVPVRBuilderWindowsOpen(venue, instrument, timeframe string, count int) {
	if count < 0 {
		count = 0
	}
	VPVRBuilderWindowsOpen.WithLabelValues(
		sanitizeVenue(venue),
		bucketInstrument(instrument),
		bucketTimeframe(timeframe),
	).Set(float64(count))
}

func IncVPVRBuilderOverloadAction(action string) {
	VPVRBuilderOverloadActionsTotal.WithLabelValues(sanitizeKind(action)).Inc()
}

func IncVPVRBuilderDrop(reason string) {
	VPVRBuilderDropTotal.WithLabelValues(sanitizeIngestReason(reason)).Inc()
}

func IncVPVRBuilderReplayMismatch() {
	VPVRBuilderReplayMismatchTotal.Inc()
}

func IncVPVRWriterUpsertOps(status string) {
	VPVRWriterUpsertOpsTotal.WithLabelValues(sanitizeStatus(status)).Inc()
}

func IncVPVRWriterUpsertDedup() {
	VPVRWriterUpsertDedupTotal.Inc()
}

func ObserveVPVRWriterUpsertLatencySeconds(latencySeconds float64) {
	if latencySeconds < 0 {
		latencySeconds = 0
	}
	VPVRWriterUpsertLatencySeconds.Observe(latencySeconds)
}

func IncVPVRWriterWriteFail(reason string) {
	VPVRWriterWriteFailTotal.WithLabelValues(sanitizeKind(reason)).Inc()
}

func SetVPVROverloadLevel(venue, instrument, timeframe string, level int) {
	if level < 0 {
		level = 0
	}
	if level > 3 {
		level = 3
	}
	VPVROverloadLevel.WithLabelValues(
		sanitizeVenue(venue),
		bucketInstrument(instrument),
		bucketTimeframe(timeframe),
	).Set(float64(level))
}

func IncVPVRDrop(reason string) {
	VPVRDropTotal.WithLabelValues(sanitizeIngestReason(reason)).Inc()
}

func IncVPVRDegrade(action string) {
	VPVRDegradeTotal.WithLabelValues(sanitizeKind(action)).Inc()
}

func ObserveVPVRCompressRatio(ratio float64) {
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	VPVRCompressRatio.Observe(ratio)
}

func ObserveVPVRProcessingLatencySeconds(latencySeconds float64) {
	if latencySeconds < 0 {
		latencySeconds = 0
	}
	VPVRProcessingLatencySeconds.Observe(latencySeconds)
}

func SetPolicyKitOverloadLevel(stream, venue, instrument string, level int) {
	if level < 0 {
		level = 0
	}
	if level > 3 {
		level = 3
	}
	_ = instrument
	PolicyKitOverloadLevel.WithLabelValues(
		sanitizeEventType(stream),
		sanitizeVenue(venue),
	).Set(float64(level))
}

func IncPolicyKitDrop(stream string, args ...string) {
	PolicyKitDropTotal.WithLabelValues(
		sanitizeEventType(stream),
		policyKitVenueFromArgs(args...),
	).Inc()
}

func IncPolicyKitDegrade(stream string, args ...string) {
	PolicyKitDegradeTotal.WithLabelValues(
		sanitizeEventType(stream),
		policyKitVenueFromArgs(args...),
	).Inc()
}

func IncPolicyKitCompress(stream string) {
	PolicyKitCompressTotal.WithLabelValues(sanitizeEventType(stream)).Inc()
}

func ObservePolicyKitLatencySeconds(stream string, latencySeconds float64, args ...string) {
	if latencySeconds < 0 {
		latencySeconds = 0
	}
	venue := policyKitVenueFromArgs(args...)
	if !shouldSampleDeterministically(
		"policykit_latency:"+sanitizeEventType(stream)+":"+venue,
		policyKitLatencyEveryN,
	) {
		return
	}
	sanitizedStream := sanitizeEventType(stream)
	PolicyKitLatencySeconds.WithLabelValues(sanitizedStream).Observe(latencySeconds)
}

func policyKitVenueFromArgs(args ...string) string {
	switch len(args) {
	case 0:
		return "unknown"
	case 1:
		// Backward compatible shape: (stream, reason|action).
		return "unknown"
	default:
		// Preferred shape: (stream, venue, reason|action).
		return sanitizeVenue(args[0])
	}
}

func shouldSampleDeterministically(partition string, everyN uint64) bool {
	if everyN <= 1 {
		return true
	}
	counterAny, _ := samplerMu.LoadOrStore(partition, &atomic.Uint64{})
	counter := counterAny.(*atomic.Uint64)
	// First observation passes, then every Nth.
	return (counter.Add(1)-1)%everyN == 0
}

func ObserveHeatmapBuildLatency(venue, instrument, timeframe string, latency time.Duration) {
	if latency < 0 {
		latency = 0
	}
	sanitizedVenue := sanitizeVenue(venue)
	sanitizedInstrument := bucketInstrument(instrument)
	sanitizedTimeframe := bucketTimeframe(timeframe)
	HeatmapBuildLatencySeconds.WithLabelValues(
		sanitizedVenue,
		sanitizedInstrument,
		sanitizedTimeframe,
	).Observe(latency.Seconds())
}

func SetHeatmapCells(venue, instrument, timeframe string, cells int) {
	HeatmapCellsTotal.WithLabelValues(
		sanitizeVenue(venue),
		bucketInstrument(instrument),
		bucketTimeframe(timeframe),
	).Set(float64(max(cells, 0)))
}

func ObserveHeatmapPayloadBytes(venue, instrument, timeframe string, payloadBytes int) {
	if payloadBytes < 0 {
		payloadBytes = 0
	}
	HeatmapPayloadBytes.WithLabelValues(
		sanitizeVenue(venue),
		bucketInstrument(instrument),
		bucketTimeframe(timeframe),
	).Observe(float64(payloadBytes))
}

func IncHeatmapDrop(reason string) {
	HeatmapDropTotal.WithLabelValues(sanitizeIngestReason(reason)).Inc()
}

func SetHeatmapQueueDepth(venue, instrument string, depth int) {
	HeatmapQueueDepth.WithLabelValues(
		sanitizeVenue(venue),
		bucketInstrument(instrument),
	).Set(float64(max(depth, 0)))
}

// ── Shard consumer observability ─────────────────────────────────────────────

// SetShardConsumerLag sets the NumPending lag for the given shard group.
func SetShardConsumerLag(groupID string, lag int64) {
	if lag < 0 {
		lag = 0
	}
	ShardConsumerLag.WithLabelValues(sanitizeGroupID(groupID)).Set(float64(lag))
}

// IncShardRedelivered increments the redelivery counter for the given shard group.
func IncShardRedelivered(groupID string) {
	ShardRedeliveredTotal.WithLabelValues(sanitizeGroupID(groupID)).Inc()
}

// ObserveShardAckLatency records an ack latency observation for the shard group.
func ObserveShardAckLatency(groupID string, latency time.Duration) {
	if latency < 0 {
		latency = 0
	}
	ShardAckLatencySeconds.WithLabelValues(sanitizeGroupID(groupID)).Observe(latency.Seconds())
}

// IncShardSkip increments the skip counter for the given shard group.
func IncShardSkip(groupID string) {
	ShardSkipTotal.WithLabelValues(sanitizeGroupID(groupID)).Inc()
}

// IncShardEvents increments the processed-events counter for the shard group.
func IncShardEvents(groupID string) {
	ShardEventsTotal.WithLabelValues(sanitizeGroupID(groupID)).Inc()
}

// SetShardInfo sets the static shard topology info gauge to 1.
func SetShardInfo(shardIndex, shardCount string) {
	ShardInfo.WithLabelValues(sanitizeGroupID(shardIndex), sanitizeGroupID(shardCount)).Set(1)
}

// SetShardLagBudget records the configured max-lag budget for a shard group.
func SetShardLagBudget(groupID string, maxLag int) {
	ShardLagBudget.WithLabelValues(sanitizeGroupID(groupID)).Set(float64(maxLag))
}

// SetShardTopologyComplete records whether all shard leases are present.
func SetShardTopologyComplete(complete bool) {
	if complete {
		ShardTopologyComplete.Set(1)
		return
	}
	ShardTopologyComplete.Set(0)
}

// SetShardLeaseAgeSeconds records lease heartbeat age.
func SetShardLeaseAgeSeconds(age float64) {
	if age < 0 {
		age = 0
	}
	ShardLeaseAgeSeconds.Set(age)
}

// IncShardOwnerConflicts increments shard owner conflict counter.
func IncShardOwnerConflicts() {
	ShardOwnerConflictsTotal.Inc()
}

// IncShardLeaseLost increments shard lease lost counter.
func IncShardLeaseLost() {
	ShardLeaseLostTotal.Inc()
}

// IncShardRegistryError increments shard registry errors for the given operation.
func IncShardRegistryError(op string) {
	op = strings.TrimSpace(op)
	if op == "" {
		op = "unknown"
	}
	ShardRegistryErrorsTotal.WithLabelValues(op).Inc()
}

// sanitizeGroupID normalises a group ID string for use as a Prometheus label.
// Group IDs are always small non-negative integers; values outside [0,999]
// are collapsed to "unknown" to prevent label explosion.
func sanitizeGroupID(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "unknown"
	}
	for _, c := range v {
		if c < '0' || c > '9' {
			return "unknown"
		}
	}
	if len(v) > 3 {
		return "unknown"
	}
	return v
}

// ── Processor observability ──────────────────────────────────────────────────

// IncProcessorProcessed increments the processor_processed_total counter.
func IncProcessorProcessed(eventType, status string) {
	ProcessorProcessedTotal.WithLabelValues(sanitizeEventType(eventType), sanitizeStatus(status)).Inc()
}

// IncProcessorCommit increments processor_commit_total with the given status.
func IncProcessorCommit(status string) {
	ProcessorCommitTotal.WithLabelValues(sanitizeStatus(status)).Inc()
}

// ObserveProcessorCommitLatency records a commit latency observation.
func ObserveProcessorCommitLatency(latency time.Duration) {
	if latency < 0 {
		latency = 0
	}
	ProcessorCommitLatencySeconds.Observe(latency.Seconds())
}

// IncProcessorAckAfterCommit increments processor_ack_after_commit_total.
func IncProcessorAckAfterCommit(status string) {
	ProcessorAckAfterCommitTotal.WithLabelValues(sanitizeStatus(status)).Inc()
}

// ── Store observability ──────────────────────────────────────────────────────

// IncStoreConsumed increments store_consumed_total with sanitized labels.
func IncStoreConsumed(status, reason string) {
	StoreConsumedTotal.WithLabelValues(sanitizeStatus(status), sanitizeIngestReason(reason)).Inc()
}

// IncStoreCommit increments store_commit_total with the given status.
func IncStoreCommit(status string) {
	StoreCommitTotal.WithLabelValues(sanitizeStatus(status)).Inc()
}

// ObserveStoreCommitLatency records a ClickHouse commit latency observation.
func ObserveStoreCommitLatency(latency time.Duration) {
	if latency < 0 {
		latency = 0
	}
	StoreCommitLatencySeconds.Observe(latency.Seconds())
}

// IncStoreQuarantine increments store_quarantine_total with the given reason.
func IncStoreQuarantine(reason string) {
	StoreQuarantineTotal.WithLabelValues(sanitizeIngestReason(reason)).Inc()
}

// ObserveStoreBatchSize records the number of rows in a flushed batch.
func ObserveStoreBatchSize(size int) {
	StoreBatchSize.Observe(float64(size))
}

// IncStoreFlush increments store_flush_total with the given status.
func IncStoreFlush(status string) {
	StoreFlushTotal.WithLabelValues(sanitizeStatus(status)).Inc()
}

// ObserveStoreFlushLatency records a batch flush latency observation.
func ObserveStoreFlushLatency(latency time.Duration) {
	if latency < 0 {
		latency = 0
	}
	StoreFlushLatencySeconds.Observe(latency.Seconds())
}

type busObserver struct{}

func (busObserver) IncPublished(eventType, venue string) {
	IncBusPublished(eventType, venue)
}

func (busObserver) IncDropped(subscriberIndex int) {
	IncBusDropped(subscriberIndex)
}

func (busObserver) IncPublishError(kind string) {
	IncBusPublishError(kind)
}

func (busObserver) ObservePublishLatency(busType string, latency time.Duration) {
	ObserveBusPublishLatency(busType, latency)
}

func (busObserver) IncConsumed(busType, status string) {
	IncBusConsumed(busType, status)
}

func (busObserver) IncRedelivered(busType string) {
	IncBusRedelivered(busType)
}

func (busObserver) ObserveAckLatency(busType string, latency time.Duration) {
	ObserveBusAckLatency(busType, latency)
}

func (busObserver) SetConsumerLag(busType string, lag int64) {
	SetBusConsumerLag(busType, lag)
}

// NewBusObserver returns the default metrics-backed bus observer.
func NewBusObserver() observability.BusObserver {
	return busObserver{}
}

// UpdateProcessMetrics refreshes runtime process gauges and appends new GC pauses.
func UpdateProcessMetrics() {
	ProcessGoroutines.Set(float64(runtime.NumGoroutine()))

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	ProcessHeapAllocBytes.Set(float64(m.HeapAlloc))

	gcMu.Lock()
	defer gcMu.Unlock()

	start := lastNumGC
	if m.NumGC-start > 256 {
		start = m.NumGC - 256
	}
	for gc := start; gc < m.NumGC; gc++ {
		idx := gc % 256
		ProcessGCPauseSeconds.Observe(float64(m.PauseNs[idx]) / float64(time.Second))
	}
	lastNumGC = m.NumGC
}

func sanitizeVenue(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if venuePattern.MatchString(v) {
		return v
	}
	return "unknown"
}

func sanitizeEventType(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if eventTypePattern.MatchString(v) {
		return v
	}
	return "unknown"
}

func sanitizeStatus(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case statusOK, statusFailed, statusDuplicate, statusOutOfOrder, statusValidationFailed, "reconnecting", "dial", "read", "subscribe", "heartbeat", "closed", "error", "degraded":
		return v
	default:
		return "unknown"
	}
}

func sanitizePolicy(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if policyPattern.MatchString(v) {
		return v
	}
	return "unknown"
}

func sanitizeSubsystem(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	// Strip colon suffix for multi-exchange keys (e.g. "marketdata:binance" → "marketdata").
	if idx := strings.IndexByte(v, ':'); idx > 0 {
		v = v[:idx]
	}
	switch v {
	case "marketdata", "aggregation", "delivery", "insights":
		return v
	default:
		return "unknown"
	}
}

func sanitizeReason(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "ttl", "size", "unknown":
		return v
	default:
		return "unknown"
	}
}

func sanitizeKind(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if kindPattern.MatchString(v) {
		return v
	}
	return "unknown"
}

func sanitizeBusType(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if busTypePattern.MatchString(v) {
		return v
	}
	return "unknown"
}

func sanitizeBusStatus(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	switch {
	case busStatusPattern.MatchString(v):
		return v
	default:
		return "unknown"
	}
}

func sanitizeReplayMode(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "file", "jetstream", "off":
		return v
	default:
		return "unknown"
	}
}

func sanitizeReplayStatus(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if busStatusPattern.MatchString(v) {
		return v
	}
	return "unknown"
}

func sanitizeIngestReason(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if kindPattern.MatchString(v) {
		return v
	}
	return "unknown"
}

func bucketInstrument(v string) string {
	v = strings.ToUpper(strings.TrimSpace(v))
	if v == "" {
		return "unknown"
	}
	var b strings.Builder
	b.Grow(len(v))
	for _, r := range v {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	sanitized := b.String()
	if sanitized == "" {
		return "unknown"
	}
	for alias, bucket := range instrumentBucketAliases {
		if strings.Contains(strings.ToLower(sanitized), alias) {
			return bucket
		}
	}
	if strings.HasSuffix(strings.ToLower(sanitized), "usd") {
		return "fiat"
	}
	return "other"
}

func bucketTimeframe(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if _, ok := timeframeAllowedBuckets[v]; ok {
		return v
	}
	return "unknown"
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func bucketSubscriberID(subscriberIndex int) string {
	switch {
	case subscriberIndex < 0:
		return "unknown"
	case subscriberIndex == 0:
		return "s0"
	case subscriberIndex <= 3:
		return "s1_3"
	case subscriberIndex <= 15:
		return "s4_15"
	case subscriberIndex <= 63:
		return "s16_63"
	case subscriberIndex <= 255:
		return "s64_255"
	default:
		return "s256_plus"
	}
}

func sanitizeWSQueryOp(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "getlast", "getrange":
		return v
	default:
		return "unknown"
	}
}

func sanitizeWSQueryCategory(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "marketdata", "aggregation", "insights":
		return v
	default:
		return "unknown"
	}
}

func sanitizeWSClientMode(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "v1", "legacy":
		return v
	default:
		return "unknown"
	}
}

func sanitizeWSControlFrameType(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "hello", "pong", "metrics", "ack_resync":
		return v
	default:
		return "unknown"
	}
}

func sanitizeWSResyncRejectReason(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "subject_invalid", "not_subscribed", "snapshot_unavailable":
		return v
	default:
		return "unknown"
	}
}

func sanitizeWSContractViolationReason(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "missing_ts_server", "unknown_feature":
		return v
	default:
		return "unknown"
	}
}

func bucketVenueCount(venueCount int) string {
	switch {
	case venueCount <= 2:
		return "2"
	case venueCount <= 4:
		return "3_4"
	case venueCount <= 8:
		return "5_8"
	default:
		return "9_plus"
	}
}

// ── Codec observability helpers ──────────────────────────────────────────────

// SetCodecRegistrySize sets the codec_registry_size gauge.
func SetCodecRegistrySize(size int) {
	CodecRegistrySize.Set(float64(max(size, 0)))
}

// IncCodecUnknownEvent increments the unknown event counter for the given format.
func IncCodecUnknownEvent(format string) {
	f := strings.ToLower(strings.TrimSpace(format))
	switch f {
	case "json", "proto":
	default:
		f = "unknown"
	}
	CodecUnknownEventsTotal.WithLabelValues(f).Inc()
}

// ── BoundedMap observability helpers ─────────────────────────────────────────

// SetBoundedMapSize sets the bounded_map_size gauge for the named instance.
func SetBoundedMapSize(name string, size int) {
	BoundedMapSize.WithLabelValues(sanitizeBoundedMapName(name)).Set(float64(max(size, 0)))
}

// IncBoundedMapEviction increments evictions for the named instance and reason.
func IncBoundedMapEviction(name, reason string) {
	BoundedMapEvictionsTotal.WithLabelValues(sanitizeBoundedMapName(name), sanitizeReason(reason)).Inc()
}

// IncBoundedMapSweep increments the sweep counter for the named instance.
func IncBoundedMapSweep(name string) {
	BoundedMapSweepsTotal.WithLabelValues(sanitizeBoundedMapName(name)).Inc()
}

func sanitizeBoundedMapName(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" {
		return "unknown"
	}
	if kindPattern.MatchString(v) {
		return v
	}
	return "unknown"
}

// ── Delivery router observability helpers ────────────────────────────────────

// SetDeliveryRouterSubscriptionsActive sets the active subscriptions gauge.
func SetDeliveryRouterSubscriptionsActive(count int) {
	DeliveryRouterSubscriptionsActive.Set(float64(max(count, 0)))
	SetWSSubscriptionsActive(count)
}

// SetDeliveryWSSnapshotCacheEntries sets the bounded ws snapshot cache size gauge.
func SetDeliveryWSSnapshotCacheEntries(count int) {
	DeliveryWSSnapshotCacheEntries.Set(float64(max(count, 0)))
}

// IncDeliveryWSSnapshotCacheHit increments the snapshot cache hit counter.
func IncDeliveryWSSnapshotCacheHit() {
	DeliveryWSSnapshotCacheHits.Inc()
}

// IncDeliveryWSSnapshotCacheMiss increments the snapshot cache miss counter.
func IncDeliveryWSSnapshotCacheMiss() {
	DeliveryWSSnapshotCacheMisses.Inc()
}

// IncDeliveryRouterEventsRouted increments the routed events counter.
func IncDeliveryRouterEventsRouted() {
	DeliveryRouterEventsRoutedTotal.Inc()
}

// IncDeliveryRouterEventsRejected increments the rejected events counter.
func IncDeliveryRouterEventsRejected(reason string) {
	DeliveryRouterEventsRejectedTotal.WithLabelValues(sanitizeKind(reason)).Inc()
}

// SetDeliveryRouterCoherenceMode sets the active delivery stream coherence mode info gauge.
func SetDeliveryRouterCoherenceMode(mode string) {
	DeliveryRouterCoherenceMode.WithLabelValues(sanitizeDeliveryRouterCoherenceMode(mode)).Set(1)
}

// IncDeliveryRouterCoherenceViolation increments the coherence violation counter.
func IncDeliveryRouterCoherenceViolation(violationType string) {
	label := "seq_non_monotonic"
	switch violationType {
	case "seq_non_monotonic", "seq_invalid":
		label = violationType
	}
	DeliveryRouterCoherenceViolationsTotal.WithLabelValues(label).Inc()
}

// SetDeliveryRouterStreamStateEntries sets the current stream-state entry count.
func SetDeliveryRouterStreamStateEntries(count int) {
	DeliveryRouterStreamStateEntries.Set(float64(max(count, 0)))
}

// AddDeliveryRouterStreamStateEvicted increments stream-state evictions by n.
func AddDeliveryRouterStreamStateEvicted(n int) {
	if n <= 0 {
		return
	}
	DeliveryRouterStreamStateEvictedTotal.Add(float64(n))
	DeliveryRouterStreamStateEvictedTotalDeprecated.Add(float64(n))
}

// SetDeliveryRouterStreamStateActive sets the active stream-state count from the latest sweep.
func SetDeliveryRouterStreamStateActive(count int) {
	DeliveryRouterStreamStateActiveTotal.Set(float64(max(count, 0)))
}

// SetDeliveryRouterSessionsActive sets the active sessions gauge for the delivery router.
func SetDeliveryRouterSessionsActive(count int) {
	DeliveryRouterSessionsActive.Set(float64(max(count, 0)))
}

// IncDeliveryRouterSessionsRejected increments the sessions-rejected counter.
func IncDeliveryRouterSessionsRejected() {
	DeliveryRouterSessionsRejectedTotal.Inc()
}

// SetWSQueueCapacity sets the per-session outbound queue capacity gauge.
func SetWSQueueCapacity(capacity int) {
	WSQueueCapacity.Set(float64(max(capacity, 0)))
}

// IncDeliveryRangeAliasFallback increments getrange alias fallback attempts by outcome.
func IncDeliveryRangeAliasFallback(outcome string) {
	DeliveryRangeAliasFallbackTotal.WithLabelValues(sanitizeKind(outcome)).Inc()
}

// SetTranscodeCacheEntries sets the current transcode cache size gauge.
func SetTranscodeCacheEntries(n int) {
	TranscodeCacheEntries.Set(float64(max(n, 0)))
}

// SetTranscodeCacheHits sets the cumulative hit gauge.
func SetTranscodeCacheHits(n int64) {
	if n < 0 {
		n = 0
	}
	TranscodeCacheHits.Set(float64(n))
}

// SetTranscodeCacheMisses sets the cumulative miss gauge.
func SetTranscodeCacheMisses(n int64) {
	if n < 0 {
		n = 0
	}
	TranscodeCacheMisses.Set(float64(n))
}

// IncWSLegacyRequest increments the legacy route request counter.
func IncWSLegacyRequest(status string) {
	s := strings.ToLower(strings.TrimSpace(status))
	switch s {
	case "accepted", "rejected":
	default:
		s = "rejected"
	}
	WSLegacyRequestsTotal.WithLabelValues(s).Inc()
}

func sanitizeDeliveryRouterCoherenceMode(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "sticky_session", "upstream_sequencer":
		return v
	default:
		return "unknown"
	}
}
