package metrics

import (
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

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
			Help: "Total messages published to in-memory bus.",
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
)

var (
	registerOnce sync.Once
	gcMu         sync.Mutex
	lastNumGC    uint32

	venuePattern      = regexp.MustCompile(`^[a-z0-9_\-]{1,24}$`)
	instrumentPattern = regexp.MustCompile(`^[A-Z0-9_\-]{1,32}$`)
	eventTypePattern  = regexp.MustCompile(`^[a-z0-9_.]{1,64}$`)
	policyPattern     = regexp.MustCompile(`^[a-z_]{1,32}$`)
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
			WSConnectionsActive,
			WSReconnectsTotal,
			WSMessagesReceivedTotal,
			WSErrorsTotal,
			GuardianRestartsTotal,
			GuardianDegradedTotal,
			GuardianSubsystemState,
			ProcessGoroutines,
			ProcessHeapAllocBytes,
			ProcessGCPauseSeconds,
			AggregationBooksActive,
			StreamsEvictedTotal,
			BooksEvictedTotal,
		)

		// Pre-create one series for vector metrics so /metrics exposition is stable
		// even before the first domain event is observed.
		IngestMessagesTotal.WithLabelValues("unknown", "UNKNOWN", "unknown", "unknown")
		IngestLatencySeconds.WithLabelValues("unknown", "UNKNOWN", "unknown")
		BackpressureQueueDepth.WithLabelValues("unknown")
		BackpressureDropsTotal.WithLabelValues("unknown")
		BusPublishedTotal.WithLabelValues("unknown", "unknown")
		BusDroppedTotal.WithLabelValues("overflow")
		WSConnectionsActive.WithLabelValues("unknown")
		WSReconnectsTotal.WithLabelValues("unknown", "unknown")
		WSMessagesReceivedTotal.WithLabelValues("unknown", "unknown")
		WSErrorsTotal.WithLabelValues("unknown", "unknown")
		GuardianRestartsTotal.WithLabelValues("unknown", "unknown")
		GuardianDegradedTotal.WithLabelValues("unknown")
		GuardianSubsystemState.WithLabelValues("unknown")
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
	id := "overflow"
	if subscriberIndex >= 0 && subscriberIndex <= 9999 {
		id = strconv.Itoa(subscriberIndex)
	}
	BusDroppedTotal.WithLabelValues(id).Inc()
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

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
