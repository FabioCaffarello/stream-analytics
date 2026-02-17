package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// ── Load ──────────────────────────────────────────────────────────────────────

func TestLoad_EmptyPath_ReturnsDefaults(t *testing.T) {
	cfg, prob := Load("")
	if prob != nil {
		t.Fatalf("Load(\"\") unexpectedly failed: %v", prob)
	}
	assertChecks(t, []fieldCheck{
		{name: "log.level", got: cfg.Log.Level, want: "info"},
		{name: "http.addr", got: cfg.HTTP.Addr, want: ":8080"},
		{name: "http.publisher_flush_timeout", got: cfg.HTTP.PublisherFlushTimeoutDuration(), want: 3 * time.Second},
		{name: "http.guardian_shutdown_timeout", got: cfg.HTTP.GuardianShutdownTimeoutDuration(), want: 10 * time.Second},
		{name: "shard.index", got: cfg.Shard.Index, want: 0},
		{name: "shard.count", got: cfg.Shard.Count, want: 1},
		{name: "bus.type", got: cfg.Bus.Type, want: "inmemory"},
		{name: "jetstream.stream_name", got: cfg.JetStream.StreamName, want: "MARKETDATA"},
		{name: "jetstream.consumer_durable", got: cfg.JetStream.ConsumerDurable, want: "processor-v1"},
		{name: "jetstream.ack_wait", got: cfg.JetStream.AckWait, want: "30s"},
		{name: "jetstream.max_ack_pending", got: cfg.JetStream.MaxAckPending, want: 1024},
		{name: "jetstream.max_deliver", got: cfg.JetStream.MaxDeliver, want: 10},
		{name: "jetstream.deliver_policy", got: cfg.JetStream.DeliverPolicy, want: "all"},
		{name: "jetstream.filter_subjects", got: cfg.JetStream.FilterSubjects, want: []string{"marketdata.>"}},
		{name: "jetstream.dedup_window", got: cfg.JetStream.DedupWindow, want: "5m"},
		{name: "jetstream.max_age", got: cfg.JetStream.MaxAge, want: "24h"},
		{name: "jetstream.max_bytes", got: cfg.JetStream.MaxBytes, want: "10GB"},
		{name: "consumer.exchange", got: cfg.Consumer.Exchange, want: "binance"},
		{name: "consumer.tickers non-empty", got: len(cfg.Consumer.Tickers) > 0, want: true},
		{name: "consumer.exchanges has synthesized entry", got: len(cfg.Consumer.Exchanges), want: 1},
		{name: "consumer.exchanges[0].type", got: cfg.Consumer.Exchanges[0].Type, want: "binance"},
		{name: "consumer.streams_per_ticker", got: cfg.Consumer.StreamsPerTicker, want: int64(2)},
		{name: "consumer.max_streams_per_websocket", got: cfg.Consumer.MaxStreamsPerWebsocket, want: int64(200)},
		{name: "consumer.max_websockets", got: cfg.Consumer.MaxWebsockets, want: int64(5)},
		{name: "consumer.binance_ws_base_url non-empty", got: cfg.Consumer.BinanceWSBaseURL != "", want: true},
		{name: "marketdata.publish_content_type", got: cfg.MarketData.PublishContentType, want: "application/json"},
		{name: "marketdata.max_instruments", got: cfg.MarketData.MaxInstruments, want: 2048},
		{name: "marketdata.record_path", got: cfg.MarketData.RecordPath, want: ""},
		{name: "marketdata.replay_path", got: cfg.MarketData.ReplayPath, want: ""},
		{name: "processor.bus_capacity", got: cfg.Processor.BusCapacity, want: 1024},
		{name: "processor.max_instruments", got: cfg.Processor.MaxInstruments, want: 2048},
		{name: "processor.insights.enable_crossvenue_join", got: cfg.Processor.Insights.EnableCrossVenueJoin, want: false},
		{name: "processor.insights.enable_volume_profile_snapshot_proto", got: cfg.Processor.Insights.EnableVolumeProfileSnapshotProto, want: false},
		{name: "processor.insights.join_trades_subject", got: cfg.Processor.Insights.JoinTradesSubject, want: "marketdata.trade.v1.>"},
		{name: "processor.insights.snapshot_subject_prefix", got: cfg.Processor.Insights.SnapshotSubjectPrefix, want: ""},
		{name: "processor.insights.max_instruments", got: cfg.Processor.Insights.MaxInstruments, want: 10_000},
		{name: "processor.insights.ttl", got: cfg.Processor.Insights.TTL, want: "1h"},
		{name: "processor.insights.enable_spread_signal", got: cfg.Processor.Insights.EnableSpreadSignal, want: false},
		{name: "processor.insights.min_venues", got: cfg.Processor.Insights.MinVenues, want: 2},
		{name: "processor.insights.min_spread_bps", got: cfg.Processor.Insights.MinSpreadBPS, want: float64(0)},
		{name: "processor.insights.rounding_mode", got: cfg.Processor.Insights.RoundingMode, want: "half_even"},
		{name: "processor.insights.sweep_every_n", got: cfg.Processor.Insights.SweepEveryN, want: 1024},
		{name: "processor.insights.sweep_every", got: cfg.Processor.Insights.SweepEvery, want: "30s"},
		{name: "store.clickhouse.dsn", got: cfg.Store.ClickHouse.DSN, want: "clickhouse://default:password@localhost:9000/default"},
	})
}

func TestLoad_NonExistentFile_ReturnsNotFound(t *testing.T) {
	_, prob := Load("/tmp/does-not-exist-market-raccoon.jsonc")
	if prob == nil {
		t.Fatal("expected problem for non-existent file, got nil")
	}
	if prob.Code != codeNotFound {
		t.Errorf("problem code = %q, want %q", prob.Code, codeNotFound)
	}
}

func TestLoad_ValidJSONC_ParsesFields(t *testing.T) {
	src := `{
		// log settings
		"log": { "level": "debug", "format": "json" },
		"http": {
			"addr": ":9090",   /* custom port */
			"read_timeout": "5s",
			"write_timeout": "10s",
			"idle_timeout": "30s",
			"shutdown_timeout": "8s",
			"publisher_flush_timeout": "4s",
			"guardian_shutdown_timeout": "12s"
		},
		"bus": { "type": "jetstream" },
		"jetstream": {
			"url": "nats://127.0.0.1:4222",
			"stream_name": "MARKETDATA",
			"consumer_durable": "processor-v2",
			"ack_wait": "45s",
			"max_ack_pending": 2048,
			"max_deliver": 20,
			"deliver_policy": "new",
			"filter_subjects": ["marketdata.>"],
			"dedup_window": "5m",
			"max_age": "24h",
			"max_bytes": "2GB"
		},
		"consumer": {
			"exchange": "binance",
			"tickers": ["BTC-USD"],
			"binance_ws_base_url": "wss://stream.binance.com:9443/stream",
			"streams_per_ticker": 2,
			"max_streams_per_websocket": 200,
			"max_websockets": 3,
			"max_websocket_lifetime": "10m",
			"respawn_overlap": "2s"
		},
		"marketdata": {
			"publish_content_type": "application/protobuf",
			"max_instruments": 1536,
			"record_path": "  /tmp/consumer.record.jsonl ",
			"replay_path": " /tmp/replay.input.jsonl "
		},
		"processor": {
			"bus_capacity": 512,
			"max_instruments": 1024,
			"insights": {
				"enable_crossvenue_join": true,
				"enable_volume_profile_snapshot_proto": true,
				"enable_spread_signal": true,
				"join_trades_subject": "marketdata.trade.v1.>",
				"snapshot_subject_prefix": "insights.crossvenue.trade_snapshot.v1",
				"max_instruments": 4096,
				"ttl": "45m",
				"min_venues": 3,
				"min_spread_bps": 12.5,
				"rounding_mode": "floor",
				"sweep_every_n": 0,
				"sweep_every": "15s"
			}
		}
	}`
	path := writeTempFile(t, src)

	cfg, prob := Load(path)
	if prob != nil {
		t.Fatalf("Load failed: %v", prob)
	}
	assertChecks(t, []fieldCheck{
		{name: "log.level", got: cfg.Log.Level, want: "debug"},
		{name: "log.format", got: cfg.Log.Format, want: "json"},
		{name: "http.addr", got: cfg.HTTP.Addr, want: ":9090"},
		{name: "http.shutdown_timeout", got: cfg.HTTP.ShutdownTimeoutDuration(), want: 8 * time.Second},
		{name: "http.publisher_flush_timeout", got: cfg.HTTP.PublisherFlushTimeoutDuration(), want: 4 * time.Second},
		{name: "http.guardian_shutdown_timeout", got: cfg.HTTP.GuardianShutdownTimeoutDuration(), want: 12 * time.Second},
		{name: "consumer.exchange", got: cfg.Consumer.Exchange, want: "binance"},
		{name: "consumer.exchanges synthesized", got: len(cfg.Consumer.Exchanges), want: 1},
		{name: "bus.type", got: cfg.Bus.Type, want: "jetstream"},
		{name: "jetstream.url", got: cfg.JetStream.URL, want: "nats://127.0.0.1:4222"},
		{name: "jetstream.consumer_durable", got: cfg.JetStream.ConsumerDurable, want: "processor-v2"},
		{name: "jetstream.ack_wait", got: cfg.JetStream.AckWaitDuration(), want: 45 * time.Second},
		{name: "jetstream.max_ack_pending", got: cfg.JetStream.MaxAckPending, want: 2048},
		{name: "jetstream.max_deliver", got: cfg.JetStream.MaxDeliver, want: 20},
		{name: "jetstream.deliver_policy", got: cfg.JetStream.DeliverPolicy, want: "new"},
		{name: "jetstream.filter_subjects", got: cfg.JetStream.FilterSubjects, want: []string{"marketdata.>"}},
		{name: "jetstream.max_bytes", got: cfg.JetStream.MaxBytesInt64(), want: int64(2_000_000_000)},
		{name: "consumer.tickers", got: cfg.Consumer.Tickers, want: []string{"BTC-USD"}},
		{name: "consumer.max_websockets", got: cfg.Consumer.MaxWebsockets, want: int64(3)},
		{name: "consumer.respawn_overlap", got: cfg.Consumer.RespawnOverlapDuration(), want: 2 * time.Second},
		{name: "marketdata.publish_content_type", got: cfg.MarketData.PublishContentType, want: "application/protobuf"},
		{name: "marketdata.max_instruments", got: cfg.MarketData.MaxInstruments, want: 1536},
		{name: "marketdata.record_path", got: cfg.MarketData.RecordPath, want: "/tmp/consumer.record.jsonl"},
		{name: "marketdata.replay_path", got: cfg.MarketData.ReplayPath, want: "/tmp/replay.input.jsonl"},
		{name: "processor.bus_capacity", got: cfg.Processor.BusCapacity, want: 512},
		{name: "processor.max_instruments", got: cfg.Processor.MaxInstruments, want: 1024},
		{name: "processor.insights.enable_crossvenue_join", got: cfg.Processor.Insights.EnableCrossVenueJoin, want: true},
		{name: "processor.insights.enable_volume_profile_snapshot_proto", got: cfg.Processor.Insights.EnableVolumeProfileSnapshotProto, want: true},
		{name: "processor.insights.join_trades_subject", got: cfg.Processor.Insights.JoinTradesSubject, want: "marketdata.trade.v1.>"},
		{name: "processor.insights.snapshot_subject_prefix", got: cfg.Processor.Insights.SnapshotSubjectPrefix, want: "insights.crossvenue.trade_snapshot.v1"},
		{name: "processor.insights.max_instruments", got: cfg.Processor.Insights.MaxInstruments, want: 4096},
		{name: "processor.insights.ttl", got: cfg.Processor.Insights.TTL, want: "45m"},
		{name: "processor.insights.enable_spread_signal", got: cfg.Processor.Insights.EnableSpreadSignal, want: true},
		{name: "processor.insights.min_venues", got: cfg.Processor.Insights.MinVenues, want: 3},
		{name: "processor.insights.min_spread_bps", got: cfg.Processor.Insights.MinSpreadBPS, want: float64(12.5)},
		{name: "processor.insights.rounding_mode", got: cfg.Processor.Insights.RoundingMode, want: "floor"},
		{name: "processor.insights.sweep_every_n", got: cfg.Processor.Insights.SweepEveryN, want: 0},
		{name: "processor.insights.sweep_every", got: cfg.Processor.Insights.SweepEvery, want: "15s"},
	})
}

type fieldCheck struct {
	name string
	got  any
	want any
}

func assertChecks(t *testing.T, checks []fieldCheck) {
	t.Helper()
	for _, c := range checks {
		if !reflect.DeepEqual(c.got, c.want) {
			t.Errorf("%s = %#v, want %#v", c.name, c.got, c.want)
		}
	}
}

func TestLoad_InvalidJSON_ReturnsParseError(t *testing.T) {
	path := writeTempFile(t, `{ "log": { "level": "debug" `)
	_, prob := Load(path)
	if prob == nil {
		t.Fatal("expected parse error for invalid JSON, got nil")
	}
	if prob.Code != codeParseError {
		t.Errorf("problem code = %q, want %q", prob.Code, codeParseError)
	}
}

func TestLoad_PartialFile_FillsRemainingDefaults(t *testing.T) {
	path := writeTempFile(t, `{"log": {"level": "warn"}}`)
	cfg, prob := Load(path)
	if prob != nil {
		t.Fatalf("Load failed: %v", prob)
	}
	if cfg.Log.Level != "warn" {
		t.Errorf("log.level = %q, want warn", cfg.Log.Level)
	}
	// Unspecified field should be defaulted.
	if cfg.HTTP.Addr != ":8080" {
		t.Errorf("http.addr = %q, want :8080 (default)", cfg.HTTP.Addr)
	}
}

func TestLoad_HTTPGuardianTimeout_FallsBackToLegacyShutdownTimeout(t *testing.T) {
	path := writeTempFile(t, `{"http": {"shutdown_timeout": "8s"}}`)
	cfg, prob := Load(path)
	if prob != nil {
		t.Fatalf("Load failed: %v", prob)
	}
	if got := cfg.HTTP.GuardianShutdownTimeoutDuration(); got != 8*time.Second {
		t.Fatalf("guardian_shutdown_timeout=%s want 8s", got)
	}
	if got := cfg.HTTP.PublisherFlushTimeoutDuration(); got != 3*time.Second {
		t.Fatalf("publisher_flush_timeout=%s want 3s", got)
	}
}

// ── Validate ─────────────────────────────────────────────────────────────────

func TestValidate_Defaults_Passes(t *testing.T) {
	cfg, _ := Load("")
	if prob := cfg.Validate(); prob != nil {
		t.Fatalf("default config should pass validation, got: %v", prob)
	}
}

func TestValidate_InvalidLogLevel(t *testing.T) {
	cfg, _ := Load("")
	cfg.Log.Level = "VERBOSE"
	prob := cfg.Validate()
	if prob == nil {
		t.Fatal("expected validation error for invalid log level")
	}
	if prob.Code != codeInvalid {
		t.Errorf("problem code = %q, want %q", prob.Code, codeInvalid)
	}
}

func TestValidate_InvalidLogFormat(t *testing.T) {
	cfg, _ := Load("")
	cfg.Log.Format = "yaml"
	prob := cfg.Validate()
	if prob == nil {
		t.Fatal("expected validation error for invalid log format")
	}
}

func TestValidate_EmptyHTTPAddr(t *testing.T) {
	cfg, _ := Load("")
	cfg.HTTP.Addr = "   "
	prob := cfg.Validate()
	if prob == nil {
		t.Fatal("expected validation error for empty http.addr")
	}
}

func TestValidate_InvalidDuration(t *testing.T) {
	cfg, _ := Load("")
	cfg.HTTP.ReadTimeout = "not-a-duration"
	prob := cfg.Validate()
	if prob == nil {
		t.Fatal("expected validation error for invalid duration")
	}
}

func TestValidate_FlushTimeoutLessThanGuardianTimeout(t *testing.T) {
	t.Run("invalid when flush is greater", func(t *testing.T) {
		cfg, _ := Load("")
		cfg.HTTP.PublisherFlushTimeout = "10s"
		cfg.HTTP.GuardianShutdownTimeout = "3s"

		prob := cfg.Validate()
		if prob == nil {
			t.Fatal("expected validation error when publisher_flush_timeout > guardian_shutdown_timeout")
		}
		if prob.Code != codeInvalid {
			t.Fatalf("problem code = %q, want %q", prob.Code, codeInvalid)
		}
		if !strings.Contains(prob.Message, "http.publisher_flush_timeout (10s)") {
			t.Fatalf("expected publisher_flush_timeout in message, got %q", prob.Message)
		}
		if !strings.Contains(prob.Message, "http.guardian_shutdown_timeout (3s)") {
			t.Fatalf("expected guardian_shutdown_timeout in message, got %q", prob.Message)
		}
	})

	t.Run("invalid when flush equals guardian", func(t *testing.T) {
		cfg, _ := Load("")
		cfg.HTTP.PublisherFlushTimeout = "3s"
		cfg.HTTP.GuardianShutdownTimeout = "3s"

		prob := cfg.Validate()
		if prob == nil {
			t.Fatal("expected validation error when publisher_flush_timeout == guardian_shutdown_timeout")
		}
		if prob.Code != codeInvalid {
			t.Fatalf("problem code = %q, want %q", prob.Code, codeInvalid)
		}
	})

	t.Run("valid when flush is less", func(t *testing.T) {
		cfg, _ := Load("")
		cfg.HTTP.PublisherFlushTimeout = "3s"
		cfg.HTTP.GuardianShutdownTimeout = "10s"

		if prob := cfg.Validate(); prob != nil {
			t.Fatalf("expected config to be valid when publisher_flush_timeout < guardian_shutdown_timeout, got %v", prob)
		}
	})
}

func TestValidate_ConsumerExchangeUnknownType(t *testing.T) {
	cfg, _ := Load("")
	cfg.Consumer.Exchanges = nil
	cfg.Consumer.Exchange = "kraken"
	prob := cfg.Validate()
	if prob == nil {
		t.Fatal("expected validation error for unknown legacy exchange type")
	}
}

func TestValidate_ConsumerExchangeBaseURLEmpty(t *testing.T) {
	cfg, _ := Load("")
	cfg.Consumer.Exchanges = []ConsumerExchangeConfig{
		{
			Name:       "binance",
			Type:       "binance",
			BaseURL:    "",
			Tickers:    []string{"BTC-USDT"},
			MarketType: "SPOT",
		},
	}
	prob := cfg.Validate()
	if prob == nil {
		t.Fatal("expected validation error for empty consumer.exchanges[0].base_url")
	}
}

func TestValidate_ExchangeNameMatchesMetricsPattern(t *testing.T) {
	t.Run("invalid long and chars", func(t *testing.T) {
		cfg, _ := Load("")
		cfg.Consumer.Exchanges = []ConsumerExchangeConfig{
			{
				Name:       "bybit-super-long-exchange-name-123!",
				Type:       "bybit",
				BaseURL:    "wss://stream.bybit.com/v5/public/spot",
				Tickers:    []string{"ETH-USDT"},
				MarketType: "SPOT",
			},
		}
		if prob := cfg.Validate(); prob == nil {
			t.Fatal("expected validation error for exchange name outside metrics label pattern")
		}
	})

	t.Run("valid names", func(t *testing.T) {
		cfg, _ := Load("")
		cfg.Consumer.Exchanges = []ConsumerExchangeConfig{
			{
				Name:       "binance",
				Type:       "binance",
				BaseURL:    "wss://stream.binance.com:9443/stream",
				Tickers:    []string{"BTC-USDT"},
				MarketType: "SPOT",
			},
			{
				Name:       "bybit",
				Type:       "bybit",
				BaseURL:    "wss://stream.bybit.com/v5/public/spot",
				Tickers:    []string{"ETH-USDT"},
				MarketType: "SPOT",
			},
		}
		if prob := cfg.Validate(); prob != nil {
			t.Fatalf("expected valid exchange names, got error: %v", prob)
		}
	})
}

func TestLoad_MultiExchangeNormalization_SortsDeterministically(t *testing.T) {
	src := `{
		"consumer": {
			"exchanges": [
				{"name":"zeta","type":"bybit","base_url":"wss://stream.bybit.com/v5/public/spot","tickers":["ETH-USDT"],"market_type":"spot"},
				{"name":"alpha","type":"binance","base_url":"wss://stream.binance.com:9443/stream","tickers":["BTC-USDT"],"market_type":"spot"}
			]
		}
	}`
	path := writeTempFile(t, src)
	cfg, prob := Load(path)
	if prob != nil {
		t.Fatalf("Load failed: %v", prob)
	}
	if len(cfg.Consumer.Exchanges) != 2 {
		t.Fatalf("consumer.exchanges len = %d, want 2", len(cfg.Consumer.Exchanges))
	}
	if cfg.Consumer.Exchanges[0].Name != "alpha" || cfg.Consumer.Exchanges[1].Name != "zeta" {
		t.Fatalf("consumer.exchanges order = %+v, want alpha then zeta", cfg.Consumer.Exchanges)
	}
}

func TestValidate_ConsumerExchangesDuplicateName(t *testing.T) {
	cfg, _ := Load("")
	cfg.Consumer.Exchanges = []ConsumerExchangeConfig{
		{Name: "binance-a", Type: "binance", BaseURL: "wss://stream.binance.com:9443/stream", Tickers: []string{"BTC-USDT"}, MarketType: "SPOT"},
		{Name: "BINANCE-A", Type: "bybit", BaseURL: "wss://stream.bybit.com/v5/public/spot", Tickers: []string{"BTC-USDT"}, MarketType: "SPOT"},
	}
	if prob := cfg.Validate(); prob == nil {
		t.Fatal("expected validation error for duplicate exchange names")
	}
}

func TestValidate_ConsumerExchangesEmptyTickers(t *testing.T) {
	cfg, _ := Load("")
	cfg.Consumer.Exchanges = []ConsumerExchangeConfig{
		{Name: "binance-a", Type: "binance", BaseURL: "wss://stream.binance.com:9443/stream", Tickers: nil, MarketType: "SPOT"},
	}
	if prob := cfg.Validate(); prob == nil {
		t.Fatal("expected validation error for empty exchange tickers")
	}
}

func TestValidate_ConsumerExchangesUnknownType(t *testing.T) {
	cfg, _ := Load("")
	cfg.Consumer.Exchanges = []ConsumerExchangeConfig{
		{Name: "x", Type: "kraken", BaseURL: "wss://example.invalid/ws", Tickers: []string{"BTC-USDT"}, MarketType: "SPOT"},
	}
	if prob := cfg.Validate(); prob == nil {
		t.Fatal("expected validation error for unknown exchange type")
	}
}

func TestValidate_ConsumerInvalidRespawnOverlap(t *testing.T) {
	cfg, _ := Load("")
	cfg.Consumer.RespawnOverlap = "nope"
	prob := cfg.Validate()
	if prob == nil {
		t.Fatal("expected validation error for invalid respawn overlap")
	}
}

func TestValidate_InvalidMarketDataPublishContentType(t *testing.T) {
	cfg, _ := Load("")
	cfg.MarketData.PublishContentType = "application/xml"
	prob := cfg.Validate()
	if prob == nil {
		t.Fatal("expected validation error for invalid marketdata.publish_content_type")
	}
}

func TestValidate_InvalidMarketDataRecordPathDot(t *testing.T) {
	cfg, _ := Load("")
	cfg.MarketData.RecordPath = "."
	prob := cfg.Validate()
	if prob == nil {
		t.Fatal("expected validation error for invalid marketdata.record_path")
	}
}

func TestValidate_InvalidMarketDataReplayPathDot(t *testing.T) {
	cfg, _ := Load("")
	cfg.MarketData.ReplayPath = "."
	prob := cfg.Validate()
	if prob == nil {
		t.Fatal("expected validation error for invalid marketdata.replay_path")
	}
}

func TestValidate_InvalidMarketDataMaxInstruments(t *testing.T) {
	cfg, _ := Load("")
	cfg.MarketData.MaxInstruments = 0
	prob := cfg.Validate()
	if prob == nil {
		t.Fatal("expected validation error for invalid marketdata.max_instruments")
	}
}

func TestValidate_InvalidBusType(t *testing.T) {
	cfg, _ := Load("")
	cfg.Bus.Type = "kafka"
	prob := cfg.Validate()
	if prob == nil {
		t.Fatal("expected validation error for invalid bus.type")
	}
}

func TestValidate_JetStreamConfig_RequiredWhenEnabled(t *testing.T) {
	cfg, _ := Load("")
	cfg.Bus.Type = "jetstream"
	cfg.JetStream.URL = ""
	prob := cfg.Validate()
	if prob == nil {
		t.Fatal("expected validation error for empty jetstream.url when bus.type=jetstream")
	}
}

func TestValidate_JetStreamMaxBytes_Invalid(t *testing.T) {
	cfg, _ := Load("")
	cfg.Bus.Type = "jetstream"
	cfg.JetStream.MaxBytes = "ten-gigabytes"
	prob := cfg.Validate()
	if prob == nil {
		t.Fatal("expected validation error for invalid jetstream.max_bytes")
	}
}

func TestValidate_JetStreamDeliverPolicy_Invalid(t *testing.T) {
	cfg, _ := Load("")
	cfg.Bus.Type = "jetstream"
	cfg.JetStream.DeliverPolicy = "by_sequence"
	prob := cfg.Validate()
	if prob == nil {
		t.Fatal("expected validation error for invalid jetstream.deliver_policy")
	}
}

func TestValidate_JetStreamFilterSubjects_Empty(t *testing.T) {
	cfg, _ := Load("")
	cfg.Bus.Type = "jetstream"
	cfg.JetStream.FilterSubjects = nil
	prob := cfg.Validate()
	if prob == nil {
		t.Fatal("expected validation error for empty jetstream.filter_subjects")
	}
}

func TestValidate_ProcessorInsightsInvalidTTL(t *testing.T) {
	cfg, _ := Load("")
	cfg.Processor.Insights.TTL = "nope"
	if prob := cfg.Validate(); prob == nil {
		t.Fatal("expected validation error for invalid processor.insights.ttl")
	}
}

func TestValidate_ProcessorInvalidMaxInstruments(t *testing.T) {
	cfg, _ := Load("")
	cfg.Processor.MaxInstruments = 0
	if prob := cfg.Validate(); prob == nil {
		t.Fatal("expected validation error for invalid processor.max_instruments")
	}
}

func TestValidate_ProcessorInsightsInvalidMaxInstruments(t *testing.T) {
	cfg, _ := Load("")
	cfg.Processor.Insights.MaxInstruments = 0
	if prob := cfg.Validate(); prob == nil {
		t.Fatal("expected validation error for invalid processor.insights.max_instruments")
	}
}

func TestValidate_ProcessorInsightsInvalidSweepEveryN(t *testing.T) {
	cfg, _ := Load("")
	cfg.Processor.Insights.SweepEveryN = -1
	if prob := cfg.Validate(); prob == nil {
		t.Fatal("expected validation error for invalid processor.insights.sweep_every_n")
	}
}

func TestValidate_ProcessorInsightsInvalidSweepEvery(t *testing.T) {
	cfg, _ := Load("")
	cfg.Processor.Insights.SweepEveryN = 0
	cfg.Processor.Insights.SweepEvery = "not-a-duration"
	if prob := cfg.Validate(); prob == nil {
		t.Fatal("expected validation error for invalid processor.insights.sweep_every")
	}
}

func TestValidate_ProcessorInsightsInvalidMinVenues(t *testing.T) {
	cfg, _ := Load("")
	cfg.Processor.Insights.MinVenues = 1
	if prob := cfg.Validate(); prob == nil {
		t.Fatal("expected validation error for invalid processor.insights.min_venues")
	}
}

func TestValidate_ProcessorInsightsInvalidMinSpreadBPS(t *testing.T) {
	cfg, _ := Load("")
	cfg.Processor.Insights.MinSpreadBPS = -1
	if prob := cfg.Validate(); prob == nil {
		t.Fatal("expected validation error for invalid processor.insights.min_spread_bps")
	}
}

func TestValidate_ProcessorInsightsInvalidRoundingMode(t *testing.T) {
	cfg, _ := Load("")
	cfg.Processor.Insights.RoundingMode = "bankers_plus"
	if prob := cfg.Validate(); prob == nil {
		t.Fatal("expected validation error for invalid processor.insights.rounding_mode")
	}
}

func TestJoinEnabled_MissingSubjects_Fails(t *testing.T) {
	cfg, _ := Load("")
	cfg.Bus.Type = "jetstream"
	cfg.Consumer.Exchanges = testConsumerExchanges()
	cfg.Processor.Insights.EnableCrossVenueJoin = true
	cfg.Processor.Insights.JoinTradesSubject = "marketdata.trade.v1.binance.BTCUSDT"
	cfg.JetStream.FilterSubjects = []string{"marketdata.bookdelta.v1.binance.>", "marketdata.bookdelta.v1.bybit.>"}

	prob := cfg.Validate()
	if prob == nil {
		t.Fatal("expected validation error for missing join subject coverage")
	}
	if !strings.Contains(prob.Message, "jetstream.filter_subjects + processor.insights.join_trades_subject") {
		t.Fatalf("unexpected error message: %q", prob.Message)
	}
}

func TestJoinEnabled_SubjectsPresent_Passes(t *testing.T) {
	cfg, _ := Load("")
	cfg.Bus.Type = "jetstream"
	cfg.Consumer.Exchanges = testConsumerExchanges()
	cfg.Processor.Insights.EnableCrossVenueJoin = true
	cfg.Processor.Insights.JoinTradesSubject = "marketdata.trade.v1.>"
	cfg.JetStream.FilterSubjects = []string{"marketdata.bookdelta.v1.>"}

	if prob := cfg.Validate(); prob != nil {
		t.Fatalf("expected join-enabled config to pass, got: %v", prob)
	}
}

func TestReplayJetStream_MissingSubjects_Fails(t *testing.T) {
	cfg, _ := Load("")
	cfg.Bus.Type = "jetstream"
	cfg.Consumer.Exchanges = testConsumerExchanges()
	cfg.Replay.Mode = "jetstream"
	cfg.Replay.JetStream.SubjectFilter = "insights.>"

	prob := cfg.Validate()
	if prob == nil {
		t.Fatal("expected validation error for replay subject filter not covering marketdata")
	}
	if !strings.Contains(prob.Message, "replay.jetstream.subject_filter") {
		t.Fatalf("unexpected error message: %q", prob.Message)
	}
}

func TestDefaults_NoBehaviorChange(t *testing.T) {
	cfg, _ := Load("")
	cfg.Bus.Type = "jetstream"
	cfg.Processor.Insights.EnableCrossVenueJoin = false
	cfg.Replay.Mode = "off"

	if prob := cfg.Validate(); prob != nil {
		t.Fatalf("default feature-off config should still pass, got: %v", prob)
	}
}

// ── Store config validation ───────────────────────────────────────────────────

func TestValidate_StoreClickHouseDSN_EmptyFails(t *testing.T) {
	cfg, _ := Load("")
	cfg.Store.ClickHouse.DSN = "   "
	prob := cfg.Validate()
	if prob == nil {
		t.Fatal("expected validation error for empty store.clickhouse.dsn")
	}
	if prob.Code != codeInvalid {
		t.Errorf("problem code = %q, want %q", prob.Code, codeInvalid)
	}
}

func TestValidate_StoreClickHouseDSN_DefaultPasses(t *testing.T) {
	cfg, _ := Load("")
	if prob := cfg.Validate(); prob != nil {
		t.Fatalf("default store config should pass, got: %v", prob)
	}
	if cfg.Store.ClickHouse.DSN == "" {
		t.Fatal("store.clickhouse.dsn should have a non-empty default")
	}
}

func TestLoad_StoreConfigFromJSONC(t *testing.T) {
	src := `{
		"store": {
			"clickhouse": {
				"dsn": "clickhouse://user:pass@remote:9000/mydb"
			}
		}
	}`
	path := writeTempFile(t, src)
	cfg, prob := Load(path)
	if prob != nil {
		t.Fatalf("Load failed: %v", prob)
	}
	if cfg.Store.ClickHouse.DSN != "clickhouse://user:pass@remote:9000/mydb" {
		t.Errorf("store.clickhouse.dsn = %q, want custom DSN", cfg.Store.ClickHouse.DSN)
	}
}

// ── stripComments ─────────────────────────────────────────────────────────────

func TestStripComments_LineComment(t *testing.T) {
	in := `{"a": 1} // comment`
	out := string(stripComments([]byte(in)))
	if strings.Contains(out, "comment") {
		t.Errorf("line comment not stripped; got: %s", out)
	}
	if !strings.Contains(out, `"a": 1`) {
		t.Errorf("JSON content removed unexpectedly; got: %s", out)
	}
}

func TestStripComments_BlockComment(t *testing.T) {
	in := `{"a": /* the value */ 1}`
	out := string(stripComments([]byte(in)))
	if strings.Contains(out, "the value") {
		t.Errorf("block comment not stripped; got: %s", out)
	}
	if !strings.Contains(out, `"a":`) {
		t.Errorf("JSON content removed unexpectedly; got: %s", out)
	}
}

func TestStripComments_URLInsideString_NotStripped(t *testing.T) {
	in := `{"url": "https://example.com/api"}`
	out := string(stripComments([]byte(in)))
	if !strings.Contains(out, "https://example.com/api") {
		t.Errorf("URL inside string was incorrectly stripped; got: %s", out)
	}
}

func TestStripComments_PreservesNewlines(t *testing.T) {
	in := "{\n// comment\n\"a\": 1\n}"
	out := string(stripComments([]byte(in)))
	// Should have at least 3 newlines (before comment, replacing comment, after)
	count := strings.Count(out, "\n")
	if count < 3 {
		t.Errorf("expected newlines preserved, got %d newline(s) in: %q", count, out)
	}
}

func TestStripComments_EscapedQuoteInsideString(t *testing.T) {
	in := `{"msg": "say \"hello\" // not a comment"}`
	out := string(stripComments([]byte(in)))
	if !strings.Contains(out, `say \"hello\"`) {
		t.Errorf("escaped quote handling broken; got: %s", out)
	}
	if !strings.Contains(out, "not a comment") {
		t.Errorf("content after escaped quote in string was stripped; got: %s", out)
	}
}

func TestStripComments_BlockCommentPreservesNewlines(t *testing.T) {
	in := "{\n/*\nmulti\nline\n*/\n\"a\": 1\n}"
	out := string(stripComments([]byte(in)))
	// Block comment's internal newlines should be preserved
	count := strings.Count(out, "\n")
	if count < 4 {
		t.Errorf("expected block comment newlines preserved, got %d in %q", count, out)
	}
}

// ── Top-level Shard config validation ─────────────────────────────────────────

func TestLoad_ShardDefaults(t *testing.T) {
	cfg, prob := Load("")
	if prob != nil {
		t.Fatalf("Load: %v", prob)
	}
	if cfg.Shard.Count != 1 {
		t.Errorf("default Shard.Count = %d; want 1", cfg.Shard.Count)
	}
	if cfg.Shard.Index != 0 {
		t.Errorf("default Shard.Index = %d; want 0", cfg.Shard.Index)
	}
}

func TestValidate_ShardCount_Zero_Fails(t *testing.T) {
	cfg, _ := Load("")
	cfg.Shard.Count = 0
	prob := cfg.Validate()
	if prob == nil {
		t.Fatal("shard.count=0 should fail validation")
	}
	if !strings.Contains(prob.Message, "shard.count") {
		t.Fatalf("error message should mention shard.count, got: %q", prob.Message)
	}
}

func TestValidate_ShardCount_Negative_Fails(t *testing.T) {
	cfg, _ := Load("")
	cfg.Shard.Count = -1
	prob := cfg.Validate()
	if prob == nil {
		t.Fatal("shard.count=-1 should fail validation")
	}
}

func TestValidate_ShardIndex_EqualCount_Fails(t *testing.T) {
	cfg, _ := Load("")
	cfg.Shard.Count = 3
	cfg.Shard.Index = 3
	prob := cfg.Validate()
	if prob == nil {
		t.Fatal("shard.index=3 with count=3 should fail validation")
	}
	if !strings.Contains(prob.Message, "shard.index") {
		t.Fatalf("error message should mention shard.index, got: %q", prob.Message)
	}
}

func TestValidate_ShardIndex_Negative_Fails(t *testing.T) {
	cfg, _ := Load("")
	cfg.Shard.Count = 2
	cfg.Shard.Index = -1
	prob := cfg.Validate()
	if prob == nil {
		t.Fatal("shard.index=-1 should fail validation")
	}
}

func TestValidate_ShardIndex_ValidRange_Passes(t *testing.T) {
	for count := 1; count <= 4; count++ {
		for idx := 0; idx < count; idx++ {
			cfg, _ := Load("")
			cfg.Shard.Count = count
			cfg.Shard.Index = idx
			if prob := cfg.Validate(); prob != nil {
				t.Errorf("count=%d index=%d should pass validation, got: %v", count, idx, prob)
			}
		}
	}
}

func TestLoad_ShardFromJSONC(t *testing.T) {
	src := `{"shard": {"index": 1, "count": 4}}`
	path := writeTempFile(t, src)
	cfg, prob := Load(path)
	if prob != nil {
		t.Fatalf("Load failed: %v", prob)
	}
	if cfg.Shard.Index != 1 {
		t.Errorf("Shard.Index = %d; want 1", cfg.Shard.Index)
	}
	if cfg.Shard.Count != 4 {
		t.Errorf("Shard.Count = %d; want 4", cfg.Shard.Count)
	}
}

// ── JetStream Shard config validation ─────────────────────────────────────────

func jetStreamShardBaseConfig() AppConfig {
	cfg, _ := Load("")
	cfg.Bus.Type = "jetstream"
	// Provide the minimum valid JetStream fields so validation reaches shard checks.
	cfg.JetStream.URL = "nats://localhost:4222"
	cfg.JetStream.StreamName = "MARKETDATA"
	cfg.JetStream.ConsumerDurable = "processor-v1"
	return cfg
}

func TestValidate_ShardGroupCount_DefaultOne_Passes(t *testing.T) {
	cfg := jetStreamShardBaseConfig()
	// Default is ShardGroupCount=1, ShardGroupID=0 — sharding disabled.
	if prob := cfg.Validate(); prob != nil {
		t.Fatalf("default shard config should pass validation, got: %v", prob)
	}
}

func TestValidate_ShardGroupCount_Zero_Fails(t *testing.T) {
	cfg := jetStreamShardBaseConfig()
	cfg.JetStream.ShardGroupCount = 0
	prob := cfg.Validate()
	if prob == nil {
		t.Fatal("shard_group_count=0 should fail validation")
	}
	if !strings.Contains(prob.Message, "shard_group_count") {
		t.Fatalf("error message should mention shard_group_count, got: %q", prob.Message)
	}
}

func TestValidate_ShardGroupCount_Negative_Fails(t *testing.T) {
	cfg := jetStreamShardBaseConfig()
	cfg.JetStream.ShardGroupCount = -1
	prob := cfg.Validate()
	if prob == nil {
		t.Fatal("shard_group_count=-1 should fail validation")
	}
}

func TestValidate_ShardGroupID_EqualCount_Fails(t *testing.T) {
	cfg := jetStreamShardBaseConfig()
	cfg.JetStream.ShardGroupCount = 3
	cfg.JetStream.ShardGroupID = 3 // must be in [0, 3)
	prob := cfg.Validate()
	if prob == nil {
		t.Fatal("shard_group_id=3 with count=3 should fail validation")
	}
	if !strings.Contains(prob.Message, "shard_group_id") {
		t.Fatalf("error message should mention shard_group_id, got: %q", prob.Message)
	}
}

func TestValidate_ShardGroupID_Negative_Fails(t *testing.T) {
	cfg := jetStreamShardBaseConfig()
	cfg.JetStream.ShardGroupCount = 2
	cfg.JetStream.ShardGroupID = -1
	prob := cfg.Validate()
	if prob == nil {
		t.Fatal("shard_group_id=-1 should fail validation")
	}
}

func TestValidate_ShardGroupID_ValidRange_Passes(t *testing.T) {
	for count := 1; count <= 4; count++ {
		for id := 0; id < count; id++ {
			cfg := jetStreamShardBaseConfig()
			cfg.JetStream.ShardGroupCount = count
			cfg.JetStream.ShardGroupID = id
			if prob := cfg.Validate(); prob != nil {
				t.Errorf("count=%d id=%d should pass validation, got: %v", count, id, prob)
			}
		}
	}
}

func TestLoad_ShardGroupDefaults(t *testing.T) {
	cfg, prob := Load("")
	if prob != nil {
		t.Fatalf("Load: %v", prob)
	}
	if cfg.JetStream.ShardGroupCount != 1 {
		t.Errorf("default ShardGroupCount = %d; want 1", cfg.JetStream.ShardGroupCount)
	}
	if cfg.JetStream.ShardGroupID != 0 {
		t.Errorf("default ShardGroupID = %d; want 0", cfg.JetStream.ShardGroupID)
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func testConsumerExchanges() []ConsumerExchangeConfig {
	return []ConsumerExchangeConfig{
		{
			Name:       "binance",
			Type:       "binance",
			BaseURL:    "wss://stream.binance.com:9443/stream",
			Tickers:    []string{"BTC-USDT"},
			MarketType: "SPOT",
		},
		{
			Name:       "bybit",
			Type:       "bybit",
			BaseURL:    "wss://stream.bybit.com/v5/public/spot",
			Tickers:    []string{"BTC-USDT"},
			MarketType: "SPOT",
		},
	}
}

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "config-*.jsonc")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close temp file: %v", err)
	}
	return filepath.Clean(f.Name())
}
