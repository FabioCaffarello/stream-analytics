// Package config provides structured configuration loading for market-raccoon.
//
// Configuration is loaded from a JSONC file (JSON with comments).  Comments
// (// ... and /* ... */) are stripped before JSON decoding so that operators
// can annotate fields inline.  Defaults are applied for every omitted field,
// and Validate must be called explicitly to fail fast before spawning actors.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// AppConfig is the root config envelope.  All fields are optional; absent
// fields receive safe defaults via applyDefaults.
type AppConfig struct {
	Log          LogConfig          `json:"log"`
	HTTP         HTTPConfig         `json:"http"`
	WS           WSConfig           `json:"ws"`
	Delivery     DeliveryConfig     `json:"delivery"`
	Bus          BusConfig          `json:"bus"`
	ProtoRollout ProtoRolloutConfig `json:"proto_rollout"`
	Shard        ShardConfig        `json:"shard"`
	JetStream    JetStreamConfig    `json:"jetstream"`
	Consumer     ConsumerConfig     `json:"consumer"`
	MarketData   MarketDataConfig   `json:"marketdata"`
	Replay       ReplayConfig       `json:"replay"`
	Processor    ProcessorConfig    `json:"processor"`
	Store        StoreConfig        `json:"store"`
	Storage      StorageConfig      `json:"storage"`
	Backfill     BackfillConfig     `json:"backfill"`
	Markets      MarketsConfig      `json:"markets"`
}

// MarketsConfig declares available exchanges and symbols for client discovery.
type MarketsConfig struct {
	Exchanges []MarketsExchangeConfig `json:"exchanges"`
}

// MarketsExchangeConfig describes one exchange with its available symbols.
type MarketsExchangeConfig struct {
	Name    string                `json:"name"`
	Symbols []MarketsSymbolConfig `json:"symbols"`
}

// MarketsSymbolConfig describes one tradable symbol on an exchange.
type MarketsSymbolConfig struct {
	Ticker     string  `json:"ticker"`
	TickSize   float64 `json:"tick_size"`
	MarketType string  `json:"market_type"`
}

// ShardConfig controls deterministic shard assignment for horizontal scaling.
type ShardConfig struct {
	// Index is the 0-based shard index for this instance.
	Index int `json:"index"`
	// Count is the total number of shards. Default: 1 (sharding disabled).
	Count int `json:"count"`
	// MaxLag is the lag budget per shard.  When exceeded, a warning is logged.
	// 0 means no budget enforcement (default).
	MaxLag int `json:"max_lag"`
	// Registry controls optional NATS KV shard registry for lease-based
	// coordination.  When enabled, the processor acquires a shard lease
	// and waits for topology completeness before processing.
	Registry ShardRegistryConfig `json:"registry"`
}

// ShardRegistryConfig controls the optional shard registry (NATS KV lease coordination).
type ShardRegistryConfig struct {
	// Enabled toggles shard registry lease acquisition. Default: false.
	Enabled bool `json:"enabled"`
	// Strict fails startup if topology is incomplete after grace period. Default: false.
	Strict bool `json:"strict"`
	// TopologyGrace is the maximum time to wait for all shards to register. Default: "60s".
	TopologyGrace string `json:"topology_grace"`
}

// TopologyGraceDuration parses and returns ShardRegistryConfig.TopologyGrace.
func (s ShardRegistryConfig) TopologyGraceDuration() time.Duration {
	return mustParseDuration(s.TopologyGrace)
}

// BusConfig controls runtime bus adapter selection.
type BusConfig struct {
	// Type selects event bus implementation.
	// Allowed: "inmemory" (default) | "jetstream".
	Type string `json:"type"`
	// WireFormat selects runtime payload wire format for event envelopes.
	// Allowed: "json" | "proto" (default).
	WireFormat string `json:"wire_format"`
}

// ProtoRolloutConfig controls per-event-type protobuf rollout toggles.
// All flags default to false (JSON delivery).
type ProtoRolloutConfig struct {
	MarketData  ProtoRolloutMarketDataConfig  `json:"marketdata"`
	Aggregation ProtoRolloutAggregationConfig `json:"aggregation"`
	Insights    ProtoRolloutInsightsConfig    `json:"insights"`
}

// ProtoRolloutMarketDataConfig controls marketdata.* event rollout.
type ProtoRolloutMarketDataConfig struct {
	Trade       bool `json:"trade"`
	BookDelta   bool `json:"bookdelta"`
	MarkPrice   bool `json:"markprice"`
	Liquidation bool `json:"liquidation"`
}

// ProtoRolloutAggregationConfig controls aggregation.* event rollout.
type ProtoRolloutAggregationConfig struct {
	Candle   bool `json:"candle"`
	Stats    bool `json:"stats"`
	Snapshot bool `json:"snapshot"`
}

// ProtoRolloutInsightsConfig controls insights.* event rollout.
type ProtoRolloutInsightsConfig struct {
	VolumeProfile bool `json:"volume_profile"`
	Heatmap       bool `json:"heatmap"`
	CrossVenue    bool `json:"crossvenue"`
}

// EventTypeFlags returns proto rollout enablement keyed by canonical event type.
func (c ProtoRolloutConfig) EventTypeFlags() map[string]bool {
	flags := make(map[string]bool, 14)
	flags["marketdata.trade"] = c.MarketData.Trade
	flags["marketdata.bookdelta"] = c.MarketData.BookDelta
	flags["marketdata.markprice"] = c.MarketData.MarkPrice
	flags["marketdata.liquidation"] = c.MarketData.Liquidation

	flags["aggregation.candle"] = c.Aggregation.Candle
	flags["aggregation.stats"] = c.Aggregation.Stats
	flags["aggregation.snapshot"] = c.Aggregation.Snapshot
	flags["aggregation.orderbook_inconsistency"] = c.Aggregation.Snapshot

	flags["insights.volume_profile_snapshot"] = c.Insights.VolumeProfile
	flags["insights.volume_profile_delta"] = c.Insights.VolumeProfile
	flags["insights.heatmap_snapshot"] = c.Insights.Heatmap
	flags["insights.heatmap_delta"] = c.Insights.Heatmap
	flags["insights.crossvenue.trade_snapshot"] = c.Insights.CrossVenue
	flags["insights.crossvenue.spread_signal"] = c.Insights.CrossVenue
	return flags
}

// JetStreamConfig controls JetStream connection and stream settings.
type JetStreamConfig struct {
	// URL is the nats-server URL.
	URL string `json:"url"`
	// StreamName is the JetStream stream name.
	StreamName string `json:"stream_name"`
	// ConsumerDurable is the processor durable consumer name.
	ConsumerDurable string `json:"consumer_durable"`
	// AckWait configures consumer ack timeout.
	AckWait string `json:"ack_wait"`
	// MaxAckPending limits in-flight unacked messages.
	MaxAckPending int `json:"max_ack_pending"`
	// MaxDeliver limits redelivery attempts before message is parked.
	MaxDeliver int `json:"max_deliver"`
	// DeliverPolicy controls initial delivery start point.
	// Supported: all|new|last.
	DeliverPolicy string `json:"deliver_policy"`
	// FilterSubjects controls the consumer subject filters.
	FilterSubjects []string `json:"filter_subjects"`
	// ShardGroupCount defines the total number of shard groups for horizontal
	// scaling.  Default: 1 (sharding disabled).  When > 1, each processor
	// instance handles only the subjects whose venue+instrument hash maps to
	// its ShardGroupID.  No external coordination is required; assignment is
	// purely deterministic (FNV-1a mod N).
	ShardGroupCount int `json:"shard_group_count"`
	// ShardGroupID is the 0-based group index for this processor instance.
	// Must be in [0, ShardGroupCount).  Default: 0.
	ShardGroupID int `json:"shard_group_id"`
	// DedupWindow configures JetStream duplicate tracking window.
	DedupWindow string `json:"dedup_window"`
	// MaxAge defines stream retention max age.
	MaxAge string `json:"max_age"`
	// MaxBytes is stream byte cap (supports e.g. "10GB").
	MaxBytes string `json:"max_bytes"`
}

// LogConfig controls structured logging output.
type LogConfig struct {
	// Level is one of "debug", "info", "warn", "error".  Default: "info".
	Level string `json:"level"`
	// Format is "text" (human-readable) or "json" (machine-readable).  Default: "text".
	Format string `json:"format"`
}

// HTTPConfig controls the HTTP server behaviour.
type HTTPConfig struct {
	// Addr is the TCP address to listen on.  Default: ":8080".
	Addr string `json:"addr"`
	// EnablePprof enables /debug/pprof/* endpoints. Default: false.
	EnablePprof bool `json:"enable_pprof"`
	// ReadTimeout is the maximum duration for reading the request.  Default: "10s".
	ReadTimeout string `json:"read_timeout"`
	// WriteTimeout is the maximum duration for writing the response.  Default: "15s".
	WriteTimeout string `json:"write_timeout"`
	// IdleTimeout is the maximum time to wait for the next request.  Default: "60s".
	IdleTimeout string `json:"idle_timeout"`
	// ShutdownTimeout is the legacy graceful shutdown timeout.  Default: "10s".
	ShutdownTimeout string `json:"shutdown_timeout"`
	// PublisherFlushTimeout is the maximum time to wait for publisher flush/close. Default: "3s".
	PublisherFlushTimeout string `json:"publisher_flush_timeout"`
	// GuardianShutdownTimeout is the maximum time to wait for guardian shutdown. Default: "10s".
	GuardianShutdownTimeout string `json:"guardian_shutdown_timeout"`
	// TLSCert is the optional filesystem path to the TLS certificate.
	TLSCert string `json:"tls_cert"`
	// TLSKey is the optional filesystem path to the TLS private key.
	TLSKey string `json:"tls_key"`
	// TLSRequired when true causes startup to fail if tls_cert and tls_key are empty.
	// Use this in production to prevent accidental plaintext deployments. Default: false.
	TLSRequired bool `json:"tls_required"`
}

// WSConfig controls websocket auth and per-session read-path rate limiting.
type WSConfig struct {
	Auth         WSAuthConfig                   `json:"auth"`
	RateLimit    WSRateLimitConfig              `json:"rate_limit"`
	Limits       WSLimitsConfig                 `json:"limits"`
	TenantLimits map[string]WSTenantLimitConfig `json:"tenant_limits,omitempty"`
}

// WSTenantLimitConfig defines per-tenant connection and rate limit overrides.
type WSTenantLimitConfig struct {
	MaxConnectionsPerKey int               `json:"max_connections_per_key"`
	MaxSubsPerConnection int               `json:"max_subs_per_connection"`
	RateLimit            WSRateLimitConfig `json:"rate_limit"`
}

// WSAuthConfig controls API-key authentication for websocket connections.
type WSAuthConfig struct {
	Enabled      bool                `json:"enabled"`
	APIKeys      map[string]string   `json:"api_keys"`
	APIKeyScopes map[string][]string `json:"api_key_scopes"`
	JWT          WSJWTAuthConfig     `json:"jwt"`
}

// WSJWTAuthConfig controls optional HMAC JWT authentication.
type WSJWTAuthConfig struct {
	Enabled     bool   `json:"enabled"`
	HS256Secret string `json:"hs256_secret"`
	Issuer      string `json:"issuer"`
	Audience    string `json:"audience"`
}

// WSRateLimitConfig controls per-session token bucket settings for websocket
// read-path commands.
type WSRateLimitConfig struct {
	Enabled       bool `json:"enabled"`
	MaxPerSecond  int  `json:"max_per_second"`
	BurstCapacity int  `json:"burst_capacity"`
}

// WSLimitsConfig controls connection and subscription hard-limits.
type WSLimitsConfig struct {
	MaxConnectionsPerIP  int `json:"max_connections_per_ip"`
	MaxConnectionsPerKey int `json:"max_connections_per_key"`
	MaxSubsPerConnection int `json:"max_subs_per_connection"`
	MaxSymbolsPerConn    int `json:"max_symbols_per_connection"`
}

// DeliveryConfig controls delivery subsystem runtime wiring in server binary.
type DeliveryConfig struct {
	Enabled                  bool   `json:"enabled"`
	MaxSessions              int    `json:"max_sessions"`
	SessionOutboundQueueSize int    `json:"session_outbound_queue_size"`
	BackpressurePolicy       string `json:"backpressure_policy"`
	SlowClientDropThreshold  int    `json:"slow_client_drop_threshold"`
	// MaxFrameBytes is the maximum outbound frame size in bytes.
	// 0 defaults to readLimitBytes (64KB).
	MaxFrameBytes int `json:"max_frame_bytes,omitempty"`
	// RouterReadyTimeout is the maximum time to wait for the delivery router PID.  Default: "2s".
	RouterReadyTimeout string `json:"router_ready_timeout"`
	// SubsystemReadyTimeout is the maximum time to wait for the delivery subsystem PID.  Default: "500ms".
	SubsystemReadyTimeout string `json:"subsystem_ready_timeout"`
	// SessionSpawnTimeout is the request timeout for spawning a new WS session.  Default: "2s".
	SessionSpawnTimeout string             `json:"session_spawn_timeout"`
	NATS                DeliveryNATSConfig `json:"nats"`
}

// RouterReadyTimeoutDuration parses and returns DeliveryConfig.RouterReadyTimeout.
func (d DeliveryConfig) RouterReadyTimeoutDuration() time.Duration {
	return mustParseDuration(d.RouterReadyTimeout)
}

// SubsystemReadyTimeoutDuration parses and returns DeliveryConfig.SubsystemReadyTimeout.
func (d DeliveryConfig) SubsystemReadyTimeoutDuration() time.Duration {
	return mustParseDuration(d.SubsystemReadyTimeout)
}

// SessionSpawnTimeoutDuration parses and returns DeliveryConfig.SessionSpawnTimeout.
func (d DeliveryConfig) SessionSpawnTimeoutDuration() time.Duration {
	return mustParseDuration(d.SessionSpawnTimeout)
}

// DeliveryNATSConfig controls delivery consumer behavior for NATS/JetStream.
type DeliveryNATSConfig struct {
	ConsumerDurable string   `json:"consumer_durable"`
	FilterSubjects  []string `json:"filter_subjects"`
}

// ConsumerConfig controls the market-data consumer binary.
type ConsumerConfig struct {
	// Exchange is the canonical exchange name.  Default: "binance".
	// Legacy single-exchange field; kept for backward compatibility.
	Exchange string `json:"exchange"`
	// MarketType classifies market type. Default: "SPOT".
	// Legacy single-exchange field; kept for backward compatibility.
	MarketType string `json:"market_type"`
	// Tickers is the list of canonical instrument names to subscribe to.  Default: ["BTC-USDT","ETH-USDT"].
	// Legacy single-exchange field; kept for backward compatibility.
	Tickers []string `json:"tickers"`
	// BinanceWSBaseURL overrides Binance websocket combined-stream base URL.
	// Default: "wss://stream.binance.com:9443/stream".
	// Legacy single-exchange field; kept for backward compatibility.
	BinanceWSBaseURL string `json:"binance_ws_base_url"`
	// Exchanges is the normalized multi-exchange runtime configuration.
	// If omitted, applyDefaults synthesizes it from legacy single-exchange fields.
	Exchanges []ConsumerExchangeConfig `json:"exchanges"`
	// StreamsPerTicker defines how many streams are opened per ticker in ws.Manager planning.
	// For W3 Binance adapter, default is 2 (aggTrade + depth).
	StreamsPerTicker int64 `json:"streams_per_ticker"`
	// EnableMarkPriceLiquidation enables markprice/liquidation streams on supported exchanges.
	// Default: false (trade + bookdelta only).
	EnableMarkPriceLiquidation bool `json:"enable_markprice_liquidation"`
	// MaxStreamsPerWebsocket is ws.Manager upper bound per websocket.
	MaxStreamsPerWebsocket int64 `json:"max_streams_per_websocket"`
	// MaxWebsockets is max parallel websocket consumers in ws.Manager.
	MaxWebsockets int64 `json:"max_websockets"`
	// MaxWebsocketLifetime defines rolling restart horizon per websocket.
	MaxWebsocketLifetime string `json:"max_websocket_lifetime"`
	// RespawnOverlap defines overlap duration while swapping old/new websocket.
	RespawnOverlap string `json:"respawn_overlap"`
	// BackpressureBufferSize caps queued WS messages before ingest.
	BackpressureBufferSize int `json:"backpressure_buffer_size"`
	// BackpressurePolicy defines the drop strategy when queue is full.
	BackpressurePolicy string `json:"backpressure_policy"`
	// ReconnectBaseBackoff defines initial reconnect delay.
	ReconnectBaseBackoff string `json:"reconnect_base_backoff"`
	// ReconnectMaxBackoff defines reconnect delay cap.
	ReconnectMaxBackoff string `json:"reconnect_max_backoff"`
	// ReconnectJitter is jitter ratio [0,1].
	ReconnectJitter float64 `json:"reconnect_jitter"`
	// ReconnectRetryBudget limits retries per budget window.
	ReconnectRetryBudget int `json:"reconnect_retry_budget"`
	// ReconnectBudgetWindow defines retry budget window.
	ReconnectBudgetWindow string `json:"reconnect_budget_window"`
	// ReconnectCooldown applies when retry budget is exhausted.
	ReconnectCooldown string `json:"reconnect_cooldown"`
}

// ConsumerExchangeConfig defines one exchange runtime in consumer.exchanges.
type ConsumerExchangeConfig struct {
	// Name is a unique logical key for this exchange runtime.
	Name string `json:"name"`
	// Type selects adapter implementation (e.g. "binance", "bybit").
	Type string `json:"type"`
	// BaseURL overrides exchange websocket base URL.
	BaseURL string `json:"base_url"`
	// Tickers is the list of instrument symbols for this exchange runtime.
	Tickers []string `json:"tickers"`
	// MarketType classifies instrument stream partitioning.
	MarketType string `json:"market_type"`
	// Buckets optionally pins websocket bucket allocation.
	// Reserved for future bucket override support.
	Buckets [][]string `json:"buckets"`
}

// MarketDataConfig controls marketdata publish encoding policy.
type MarketDataConfig struct {
	// PublishContentType controls the wire payload format for produced marketdata envelopes.
	// Allowed: "application/json" or "application/protobuf".
	// When omitted, defaults from bus.wire_format (proto => application/protobuf).
	PublishContentType string `json:"publish_content_type"`
	// MaxInstruments bounds in-memory instrument stream state for ingest.
	MaxInstruments int `json:"max_instruments"`
	// RecordPath enables opt-in fixture recording of published envelopes.
	// Empty disables recording (default behavior).
	RecordPath string `json:"record_path"`
	// ReplayPath enables opt-in fixture replay mode for processor runtime.
	// Empty disables replay (default behavior).
	ReplayPath string `json:"replay_path"`
}

// ReplayConfig controls opt-in replay runtime.
type ReplayConfig struct {
	// Mode selects replay behavior:
	// - off (default)
	// - file (fixture replay)
	// - jetstream (JetStream replay source)
	Mode string `json:"mode"`
	// OnDecodeError controls replay behavior for invalid envelope payloads.
	// Allowed: fail (default) | skip.
	OnDecodeError string `json:"on_decode_error"`
	// JetStream contains replay settings for jetstream mode.
	JetStream ReplayJetStreamConfig `json:"jetstream"`
}

// ReplayJetStreamConfig controls JetStream replay source behavior.
type ReplayJetStreamConfig struct {
	// Window bounds replay input when deliver_policy=by_start_time.
	// Empty means disabled.
	Window string `json:"window"`
	// MaxMessages hard-limits replay envelopes to prevent accidental infinite runs.
	MaxMessages int `json:"max_messages"`
	// SubjectFilter selects stream subjects to replay.
	SubjectFilter string `json:"subject_filter"`
	// DeliverPolicy controls replay start position.
	// Supported: all|by_start_time.
	DeliverPolicy string `json:"deliver_policy"`
	// MergeBuffer controls bounded reordering window for deterministic global ordering.
	MergeBuffer int `json:"merge_buffer"`
}

// BackfillConfig controls operational backfill and gap-detection tooling.
type BackfillConfig struct {
	Mode       string `json:"mode"`
	Exchange   string `json:"exchange"`
	Symbol     string `json:"symbol"`
	MarketType string `json:"market_type"`
	FromDate   string `json:"from_date"`
	ToDate     string `json:"to_date"`
	OutputDir  string `json:"output_dir"`
	Timeframe  string `json:"timeframe"`
}

// StoreConfig controls the store cold-path binary.
type StoreConfig struct {
	// ClickHouse controls ClickHouse connection for cold-path writes.
	ClickHouse StoreClickHouseConfig `json:"clickhouse"`
	// Batch controls write batching policy for the cold-path pipeline.
	Batch StoreBatchConfig `json:"batch"`
}

// StorageConfig controls opt-in production storage drivers.
type StorageConfig struct {
	Timescale  StorageTimescaleConfig  `json:"timescale"`
	ClickHouse StorageClickHouseConfig `json:"clickhouse"`
}

// StorageTimescaleConfig controls Timescale connection options.
type StorageTimescaleConfig struct {
	Enabled           bool   `json:"enabled"`
	DSN               string `json:"dsn"`
	MaxConns          int    `json:"max_conns"`
	MinConns          int    `json:"min_conns"`
	MaxConnLifetime   string `json:"max_conn_lifetime"`
	MaxConnIdleTime   string `json:"max_conn_idle_time"`
	HealthCheckPeriod string `json:"health_check_period"`
}

// StorageClickHouseConfig controls ClickHouse connection options.
type StorageClickHouseConfig struct {
	Enabled         bool     `json:"enabled"`
	Addrs           []string `json:"addrs"`
	Database        string   `json:"database"`
	Username        string   `json:"username"`
	Password        string   `json:"password"`
	MaxOpenConns    int      `json:"max_open_conns"`
	MaxIdleConns    int      `json:"max_idle_conns"`
	ConnMaxLifetime string   `json:"conn_max_lifetime"`
	DialTimeout     string   `json:"dial_timeout"`
	ReadTimeout     string   `json:"read_timeout"`
}

// StoreBatchConfig controls write batching thresholds for the store pipeline.
type StoreBatchConfig struct {
	// MaxRows triggers a flush when the batch reaches this many rows.
	// Default: 1 (write-through; increase when concurrent dispatch is enabled).
	MaxRows int `json:"max_rows"`
	// MaxBytes triggers a flush when accumulated payload bytes reach this limit.
	// Default: 0 (disabled).
	MaxBytes int `json:"max_bytes"`
	// FlushInterval triggers a time-based flush regardless of batch size.
	// Default: "100ms".
	FlushInterval string `json:"flush_interval"`
}

// FlushIntervalDuration parses and returns StoreBatchConfig.FlushInterval.
func (b StoreBatchConfig) FlushIntervalDuration() time.Duration {
	return mustParseDuration(b.FlushInterval)
}

// MaxConnLifetimeDuration parses and returns StorageTimescaleConfig.MaxConnLifetime.
func (s StorageTimescaleConfig) MaxConnLifetimeDuration() time.Duration {
	return mustParseDuration(s.MaxConnLifetime)
}

// MaxConnIdleTimeDuration parses and returns StorageTimescaleConfig.MaxConnIdleTime.
func (s StorageTimescaleConfig) MaxConnIdleTimeDuration() time.Duration {
	return mustParseDuration(s.MaxConnIdleTime)
}

// HealthCheckPeriodDuration parses and returns StorageTimescaleConfig.HealthCheckPeriod.
func (s StorageTimescaleConfig) HealthCheckPeriodDuration() time.Duration {
	return mustParseDuration(s.HealthCheckPeriod)
}

// ConnMaxLifetimeDuration parses and returns StorageClickHouseConfig.ConnMaxLifetime.
func (s StorageClickHouseConfig) ConnMaxLifetimeDuration() time.Duration {
	return mustParseDuration(s.ConnMaxLifetime)
}

// DialTimeoutDuration parses and returns StorageClickHouseConfig.DialTimeout.
func (s StorageClickHouseConfig) DialTimeoutDuration() time.Duration {
	return mustParseDuration(s.DialTimeout)
}

// ReadTimeoutDuration parses and returns StorageClickHouseConfig.ReadTimeout.
func (s StorageClickHouseConfig) ReadTimeoutDuration() time.Duration {
	return mustParseDuration(s.ReadTimeout)
}

// StoreClickHouseConfig controls ClickHouse connection for the store binary.
type StoreClickHouseConfig struct {
	// DSN is the ClickHouse connection string.
	// Format: clickhouse://user:password@host:port/database
	// Default: "clickhouse://default:password@localhost:9000/default".
	DSN string `json:"dsn"`
}

// ProcessorConfig controls the aggregation processor binary.
type ProcessorConfig struct {
	// BusCapacity is the channel buffer size for the in-memory event bus.  Default: 1024.
	BusCapacity int `json:"bus_capacity"`
	// MaxInstruments bounds in-memory order book state for aggregation.
	MaxInstruments int `json:"max_instruments"`
	// PublisherTimeout is the JetStream publish timeout per envelope.  Default: "5s".
	PublisherTimeout string `json:"publisher_timeout"`
	// Insights controls optional processor-side insight derivations.
	Insights ProcessorInsightsConfig `json:"insights"`
	// Candle controls candle aggregation runtime options.
	Candle ProcessorCandleConfig `json:"candle"`
	// Stats controls stats aggregation runtime options.
	Stats ProcessorStatsConfig `json:"stats"`
	// SubMinuteRollout controls canary/rollback for 1s and 5s outputs.
	SubMinuteRollout ProcessorSubMinuteRolloutConfig `json:"subminute_rollout"`
	// RTPublish controls timer-driven publishing intervals for real-time snapshots.
	RTPublish ProcessorRTPublishConfig `json:"rt_publish"`
	// CatchUpSkipBookDeltaSkewMs skips stale bookdelta envelopes while the
	// processor is catching up. 0 disables this behavior (default).
	CatchUpSkipBookDeltaSkewMs int `json:"catchup_skip_bookdelta_skew_ms"`
	// CatchUpSkipTradeSkewMs skips stale trade envelopes while the processor
	// is catching up. 0 disables this behavior (default).
	CatchUpSkipTradeSkewMs int `json:"catchup_skip_trade_skew_ms"`
	// CatchUpSkipStatsSkewMs skips stale liquidation/markprice envelopes while
	// the processor is catching up. 0 disables this behavior (default).
	CatchUpSkipStatsSkewMs int `json:"catchup_skip_stats_skew_ms"`
}

// PublisherTimeoutDuration parses and returns ProcessorConfig.PublisherTimeout.
func (p ProcessorConfig) PublisherTimeoutDuration() time.Duration {
	return mustParseDuration(p.PublisherTimeout)
}

// ProcessorCandleConfig controls candle aggregation options in processor runtime.
type ProcessorCandleConfig struct {
	// Enabled toggles candle aggregation route handling.
	Enabled bool `json:"enabled"`
	// MaxCandles bounds active candle windows in memory.
	MaxCandles int `json:"max_candles"`
}

// ProcessorStatsConfig controls stats aggregation options in processor runtime.
type ProcessorStatsConfig struct {
	// Enabled toggles stats aggregation route handling.
	Enabled bool `json:"enabled"`
	// MaxWindows bounds active stats windows in memory.
	MaxWindows int `json:"max_windows"`
}

// ProcessorSubMinuteRolloutConfig controls rollout for sub-minute outputs.
type ProcessorSubMinuteRolloutConfig struct {
	// Enabled toggles sub-minute (`1s`,`5s`) output publish/persist.
	// Default: true.
	Enabled bool `json:"enabled"`
	// Venues optionally limits sub-minute outputs to a venue allow-list.
	// Empty means all venues.
	Venues []string `json:"venues"`
	// Instruments optionally limits sub-minute outputs to an instrument allow-list.
	// Empty means all instruments.
	Instruments []string `json:"instruments"`

	enabledSet bool `json:"-"`
}

func (c *ProcessorSubMinuteRolloutConfig) UnmarshalJSON(data []byte) error {
	type Alias ProcessorSubMinuteRolloutConfig
	aux := struct {
		*Alias
	}{
		Alias: (*Alias)(c),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if _, ok := raw["enabled"]; ok {
		c.enabledSet = true
	}
	return nil
}

func (c ProcessorSubMinuteRolloutConfig) enabledConfigured() bool {
	return c.enabledSet
}

// ProcessorRTPublishConfig controls timer-driven publish intervals in milliseconds.
type ProcessorRTPublishConfig struct {
	// OrderbookIntervalMs controls periodic orderbook snapshot publish cadence.
	// Default: 200ms. 0 disables timer-driven orderbook publishing.
	OrderbookIntervalMs int `json:"orderbook_interval_ms"`
	// HeatmapIntervalMs controls periodic heatmap snapshot publish cadence.
	// Default: 200ms. 0 disables timer-driven heatmap publishing.
	HeatmapIntervalMs int `json:"heatmap_interval_ms"`
	// VolumeIntervalMs controls periodic volume profile snapshot publish cadence.
	// Default: 250ms. 0 disables timer-driven volume profile publishing.
	VolumeIntervalMs int `json:"volume_interval_ms"`

	orderbookIntervalSet bool `json:"-"`
	heatmapIntervalSet   bool `json:"-"`
	volumeIntervalSet    bool `json:"-"`
}

func (c *ProcessorRTPublishConfig) UnmarshalJSON(data []byte) error {
	type Alias ProcessorRTPublishConfig
	aux := struct {
		*Alias
	}{
		Alias: (*Alias)(c),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if _, ok := raw["orderbook_interval_ms"]; ok {
		c.orderbookIntervalSet = true
	}
	if _, ok := raw["heatmap_interval_ms"]; ok {
		c.heatmapIntervalSet = true
	}
	if _, ok := raw["volume_interval_ms"]; ok {
		c.volumeIntervalSet = true
	}
	return nil
}

func (c ProcessorRTPublishConfig) orderbookConfigured() bool {
	return c.orderbookIntervalSet
}

func (c ProcessorRTPublishConfig) heatmapConfigured() bool {
	return c.heatmapIntervalSet
}

func (c ProcessorRTPublishConfig) volumeConfigured() bool {
	return c.volumeIntervalSet
}

// ProcessorInsightsConfig controls optional cross-venue join derivation in processor runtime.
type ProcessorInsightsConfig struct {
	// EnableCrossVenueJoin toggles cross-venue trade join processing.
	EnableCrossVenueJoin bool `json:"enable_crossvenue_join"`
	// EnableVolumeProfileSnapshotProto enables opt-in protobuf payload codec
	// for insights.volume_profile_snapshot publish path. Default: false.
	EnableVolumeProfileSnapshotProto bool `json:"enable_volume_profile_snapshot_proto"`
	// EnableSpreadSignal toggles optional spread-signal emission.
	EnableSpreadSignal bool `json:"enable_spread_signal"`
	// JoinTradesSubject is the JetStream filter subject required when join is enabled.
	JoinTradesSubject string `json:"join_trades_subject"`
	// SnapshotSubjectPrefix optionally overrides publish subject prefix for snapshots.
	// Empty means default SubjectFromEnvelope output.
	SnapshotSubjectPrefix string `json:"snapshot_subject_prefix"`
	// MaxInstruments bounds tracked instruments in join state.
	MaxInstruments int `json:"max_instruments"`
	// TTL is per-instrument state lifetime.
	TTL string `json:"ttl"`
	// SweepEveryN triggers one explicit TTL sweep every N join updates.
	// When >0, it takes precedence over SweepEvery.
	SweepEveryN int `json:"sweep_every_n"`
	// SweepEvery triggers one explicit TTL sweep by elapsed time.
	// Used only when SweepEveryN==0.
	SweepEvery string `json:"sweep_every"`
	// MinVenues is the minimum venue count required to emit spread-signal events.
	MinVenues int `json:"min_venues"`
	// MinSpreadBPS is the minimum spread threshold required to emit spread-signal events.
	MinSpreadBPS float64 `json:"min_spread_bps"`
	// RoundingMode controls deterministic spread rounding.
	// Supported: half_even (default) | floor.
	RoundingMode string `json:"rounding_mode"`
}

// ReadTimeout parses and returns HTTPConfig.ReadTimeout as a time.Duration.
func (h HTTPConfig) ReadTimeoutDuration() time.Duration { return mustParseDuration(h.ReadTimeout) }

// WriteTimeoutDuration parses and returns HTTPConfig.WriteTimeout as a time.Duration.
func (h HTTPConfig) WriteTimeoutDuration() time.Duration { return mustParseDuration(h.WriteTimeout) }

// IdleTimeoutDuration parses and returns HTTPConfig.IdleTimeout as a time.Duration.
func (h HTTPConfig) IdleTimeoutDuration() time.Duration { return mustParseDuration(h.IdleTimeout) }

// ShutdownTimeoutDuration parses and returns HTTPConfig.ShutdownTimeout as a time.Duration.
func (h HTTPConfig) ShutdownTimeoutDuration() time.Duration {
	return mustParseDuration(h.ShutdownTimeout)
}

// PublisherFlushTimeoutDuration parses and returns HTTPConfig.PublisherFlushTimeout.
func (h HTTPConfig) PublisherFlushTimeoutDuration() time.Duration {
	return mustParseDuration(h.PublisherFlushTimeout)
}

// GuardianShutdownTimeoutDuration parses and returns HTTPConfig.GuardianShutdownTimeout.
func (h HTTPConfig) GuardianShutdownTimeoutDuration() time.Duration {
	return mustParseDuration(h.GuardianShutdownTimeout)
}

// MaxWebsocketLifetimeDuration parses and returns ConsumerConfig.MaxWebsocketLifetime.
func (c ConsumerConfig) MaxWebsocketLifetimeDuration() time.Duration {
	return mustParseDuration(c.MaxWebsocketLifetime)
}

// RespawnOverlapDuration parses and returns ConsumerConfig.RespawnOverlap.
func (c ConsumerConfig) RespawnOverlapDuration() time.Duration {
	return mustParseDuration(c.RespawnOverlap)
}

func (c ConsumerConfig) ReconnectBaseBackoffDuration() time.Duration {
	return mustParseDuration(c.ReconnectBaseBackoff)
}

func (c ConsumerConfig) ReconnectMaxBackoffDuration() time.Duration {
	return mustParseDuration(c.ReconnectMaxBackoff)
}

func (c ConsumerConfig) ReconnectBudgetWindowDuration() time.Duration {
	return mustParseDuration(c.ReconnectBudgetWindow)
}

func (c ConsumerConfig) ReconnectCooldownDuration() time.Duration {
	return mustParseDuration(c.ReconnectCooldown)
}

// DedupWindowDuration parses and returns JetStreamConfig.DedupWindow.
func (j JetStreamConfig) DedupWindowDuration() time.Duration {
	return mustParseDuration(j.DedupWindow)
}

// MaxAgeDuration parses and returns JetStreamConfig.MaxAge.
func (j JetStreamConfig) MaxAgeDuration() time.Duration {
	return mustParseDuration(j.MaxAge)
}

// MaxBytesInt64 parses and returns JetStreamConfig.MaxBytes.
func (j JetStreamConfig) MaxBytesInt64() int64 {
	return mustParseByteSize(j.MaxBytes)
}

// AckWaitDuration parses and returns JetStreamConfig.AckWait.
func (j JetStreamConfig) AckWaitDuration() time.Duration {
	return mustParseDuration(j.AckWait)
}

// WindowDuration parses and returns ReplayJetStreamConfig.Window.
func (r ReplayJetStreamConfig) WindowDuration() time.Duration {
	if strings.TrimSpace(r.Window) == "" {
		return 0
	}
	return mustParseDuration(r.Window)
}

// TTLDuration parses and returns ProcessorInsightsConfig.TTL.
func (i ProcessorInsightsConfig) TTLDuration() time.Duration {
	return mustParseDuration(i.TTL)
}

// SweepEveryDuration parses and returns ProcessorInsightsConfig.SweepEvery.
// Empty values are interpreted as disabled (0).
func (i ProcessorInsightsConfig) SweepEveryDuration() time.Duration {
	if strings.TrimSpace(i.SweepEvery) == "" {
		return 0
	}
	return mustParseDuration(i.SweepEvery)
}

// mustParseDuration parses s as a Go duration. Returns 0 on invalid input
// instead of panicking. Validate() catches invalid durations at startup; this
// fallback only fires if Duration() is called without prior validation.
func mustParseDuration(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0
	}
	return d
}

// mustParseByteSize parses s as a byte size. Returns 0 on invalid input
// instead of panicking.
func mustParseByteSize(s string) int64 {
	v, err := parseByteSize(s)
	if err != nil {
		return 0
	}
	return v
}

func parseByteSize(s string) (int64, error) {
	raw := strings.ToUpper(strings.TrimSpace(s))
	if raw == "" {
		return 0, errors.New("empty byte size")
	}

	// Allow plain bytes without unit.
	if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
		if n <= 0 {
			return 0, fmt.Errorf("byte size must be > 0, got %d", n)
		}
		return n, nil
	}

	units := []struct {
		unit string
		mul  int64
	}{
		{"TB", 1_000_000_000_000},
		{"GB", 1_000_000_000},
		{"MB", 1_000_000},
		{"KB", 1_000},
		{"B", 1},
	}
	for _, u := range units {
		unit := u.unit
		mul := u.mul
		if strings.HasSuffix(raw, unit) {
			numPart := strings.TrimSpace(strings.TrimSuffix(raw, unit))
			n, err := strconv.ParseInt(numPart, 10, 64)
			if err != nil {
				return 0, fmt.Errorf("invalid numeric value %q", numPart)
			}
			if n <= 0 {
				return 0, fmt.Errorf("byte size must be > 0, got %d", n)
			}
			if n > (int64(^uint64(0)>>1))/mul {
				return 0, errors.New("byte size overflows int64")
			}
			return n * mul, nil
		}
	}

	return 0, fmt.Errorf("unsupported size unit in %q", s)
}
