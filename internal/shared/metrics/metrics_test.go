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
	IncBusDropped(7)
	if got := testutil.ToFloat64(BusDroppedTotal.WithLabelValues("7")); got < 1 {
		t.Fatalf("expected subscriber id metric increment, got %f", got)
	}

	IncBusDropped(10001)
	if got := testutil.ToFloat64(BusDroppedTotal.WithLabelValues("overflow")); got < 1 {
		t.Fatalf("expected overflow metric increment, got %f", got)
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

func TestMetricsNamesPresent(t *testing.T) {
	SetWSConnectionsActive("binance", 1)

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
		"process_heap_alloc_bytes",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected metric %s in registry", want)
		}
	}
}
