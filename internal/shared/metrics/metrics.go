package metrics

import (
	"regexp"
	"runtime"
	"strings"
	"sync"
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
)

var (
	IngestMessagesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ingest_messages_total",
			Help: "Total ingest messages processed by status.",
		},
		[]string{"venue", "instrument", "event_type", "status"},
	)
	IngestLatencySeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "ingest_latency_seconds",
			Help:    "Ingest pipeline latency in seconds.",
			Buckets: []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05},
		},
		[]string{"venue", "instrument", "event_type"},
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
)

var (
	registerOnce sync.Once
	gcMu         sync.Mutex
	lastNumGC    uint32

	venuePattern      = regexp.MustCompile(`^[a-z0-9_\-]{1,24}$`)
	instrumentPattern = regexp.MustCompile(`^[A-Z0-9_\-]{1,32}$`)
	eventTypePattern  = regexp.MustCompile(`^[a-z0-9_.]{1,64}$`)
	policyPattern     = regexp.MustCompile(`^[a-z_]{1,32}$`)
	kindPattern       = regexp.MustCompile(`^[a-z0-9_]{1,48}$`)
	busTypePattern    = regexp.MustCompile(`^[a-z0-9_]{1,24}$`)
	busStatusPattern  = regexp.MustCompile(`^[a-z_]{1,24}$`)
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
			ReplayMessagesTotal,
			ReplayLatencySeconds,
			ReplayRedeliveriesTotal,
			WSConnectionsActive,
			WSReconnectsTotal,
			WSMessagesReceivedTotal,
			WSErrorsTotal,
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
		)

		// Pre-create one series for vector metrics so /metrics exposition is stable
		// even before the first domain event is observed.
		IngestMessagesTotal.WithLabelValues("unknown", "UNKNOWN", "unknown", "unknown")
		IngestLatencySeconds.WithLabelValues("unknown", "UNKNOWN", "unknown")
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
		ReplayMessagesTotal.WithLabelValues("unknown", "unknown")
		ReplayLatencySeconds.WithLabelValues("unknown")
		ReplayRedeliveriesTotal.WithLabelValues("unknown")
		WSConnectionsActive.WithLabelValues("unknown")
		WSReconnectsTotal.WithLabelValues("unknown", "unknown")
		WSMessagesReceivedTotal.WithLabelValues("unknown", "unknown")
		WSErrorsTotal.WithLabelValues("unknown", "unknown")
		GuardianRestartsTotal.WithLabelValues("unknown", "unknown")
		GuardianDegradedTotal.WithLabelValues("unknown")
		GuardianSubsystemState.WithLabelValues("unknown")
		InsightsSnapshotsTotal.WithLabelValues("2")
		InsightsSnapshotsTotal.WithLabelValues("3_4")
		InsightsSnapshotsTotal.WithLabelValues("5_8")
		InsightsSnapshotsTotal.WithLabelValues("9_plus")
		InsightsStateEvictionsTotal.WithLabelValues("unknown")
	})
}

func ObserveIngest(venue, instrument, eventType, status string, latency time.Duration) {
	v := sanitizeVenue(venue)
	i := sanitizeInstrument(instrument)
	e := sanitizeEventType(eventType)
	s := sanitizeStatus(status)
	IngestMessagesTotal.WithLabelValues(v, i, e, s).Inc()
	if s == statusOK {
		IngestLatencySeconds.WithLabelValues(v, i, e).Observe(latency.Seconds())
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

func sanitizeInstrument(v string) string {
	v = strings.ToUpper(strings.TrimSpace(v))
	if instrumentPattern.MatchString(v) {
		return v
	}
	return "UNKNOWN"
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
