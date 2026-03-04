package config

import "testing"

func TestMustParseDuration_Valid(t *testing.T) {
	c := ConsumerConfig{MaxWebsocketLifetime: "1m", RespawnOverlap: "5s"}
	if got := c.MaxWebsocketLifetimeDuration().String(); got != "1m0s" {
		t.Fatalf("MaxWebsocketLifetimeDuration = %s, want 1m0s", got)
	}
	if got := c.RespawnOverlapDuration().String(); got != "5s" {
		t.Fatalf("RespawnOverlapDuration = %s, want 5s", got)
	}
}

func TestJetStreamHelpers_Valid(t *testing.T) {
	js := JetStreamConfig{
		DedupWindow: "5m",
		MaxAge:      "24h",
		MaxBytes:    "10GB",
		AckWait:     "30s",
	}

	if got := js.DedupWindowDuration().String(); got != "5m0s" {
		t.Fatalf("DedupWindowDuration = %s, want 5m0s", got)
	}
	if got := js.MaxAgeDuration().String(); got != "24h0m0s" {
		t.Fatalf("MaxAgeDuration = %s, want 24h0m0s", got)
	}
	if got := js.MaxBytesInt64(); got != 10_000_000_000 {
		t.Fatalf("MaxBytesInt64 = %d, want %d", got, int64(10_000_000_000))
	}
	if got := js.AckWaitDuration().String(); got != "30s" {
		t.Fatalf("AckWaitDuration = %s, want 30s", got)
	}
}

func TestMustParseDuration_ReturnsZeroOnInvalid(t *testing.T) {
	c := ConsumerConfig{MaxWebsocketLifetime: "invalid"}
	got := c.MaxWebsocketLifetimeDuration()
	if got != 0 {
		t.Fatalf("MaxWebsocketLifetimeDuration(%q) = %s, want 0", "invalid", got)
	}
}

func TestProcessorInsightsSweepEveryDuration_EmptyIsZero(t *testing.T) {
	cfg := ProcessorInsightsConfig{}
	if got := cfg.SweepEveryDuration(); got != 0 {
		t.Fatalf("SweepEveryDuration=%s want=0", got)
	}
}

func TestProcessorInsightsDefaultsAndDurationHelpers(t *testing.T) {
	cfg, prob := Load("")
	if prob != nil {
		t.Fatalf("Load defaults failed: %v", prob)
	}
	if got := cfg.Processor.Insights.TTLDuration().String(); got != "1h0m0s" {
		t.Fatalf("TTLDuration=%s want=1h0m0s", got)
	}
	if cfg.Processor.Insights.MinVenues != 2 {
		t.Fatalf("MinVenues=%d want=2", cfg.Processor.Insights.MinVenues)
	}
	if cfg.Processor.Insights.RoundingMode != "half_even" {
		t.Fatalf("RoundingMode=%q want=half_even", cfg.Processor.Insights.RoundingMode)
	}
}

func TestProtoRolloutEventTypeFlags(t *testing.T) {
	flags := ProtoRolloutConfig{
		MarketData: ProtoRolloutMarketDataConfig{
			Trade: true,
		},
		Aggregation: ProtoRolloutAggregationConfig{
			Tape:     true,
			Snapshot: true,
		},
		Insights: ProtoRolloutInsightsConfig{
			Heatmap: true,
		},
	}.EventTypeFlags()

	if !flags["marketdata.trade"] {
		t.Fatal("marketdata.trade should be enabled")
	}
	if !flags["aggregation.orderbook_inconsistency"] {
		t.Fatal("aggregation.orderbook_inconsistency should follow aggregation.snapshot")
	}
	if !flags["aggregation.tape"] {
		t.Fatal("aggregation.tape should be enabled")
	}
	if !flags["insights.heatmap_delta"] {
		t.Fatal("insights.heatmap_delta should follow insights.heatmap")
	}
	if flags["insights.crossvenue.trade_snapshot"] {
		t.Fatal("insights.crossvenue.trade_snapshot should be disabled")
	}
}
