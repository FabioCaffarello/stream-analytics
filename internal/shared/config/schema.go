// Package config provides structured configuration loading for market-raccoon.
//
// Configuration is loaded from a JSONC file (JSON with comments).  Comments
// (// ... and /* ... */) are stripped before JSON decoding so that operators
// can annotate fields inline.  Defaults are applied for every omitted field,
// and Validate must be called explicitly to fail fast before spawning actors.
package config

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// AppConfig is the root config envelope.  All fields are optional; absent
// fields receive safe defaults via applyDefaults.
type AppConfig struct {
	Log        LogConfig        `json:"log"`
	HTTP       HTTPConfig       `json:"http"`
	Bus        BusConfig        `json:"bus"`
	JetStream  JetStreamConfig  `json:"jetstream"`
	Consumer   ConsumerConfig   `json:"consumer"`
	MarketData MarketDataConfig `json:"marketdata"`
	Processor  ProcessorConfig  `json:"processor"`
}

// BusConfig controls runtime bus adapter selection.
type BusConfig struct {
	// Type selects event bus implementation.
	// Allowed: "inmemory" (default) | "jetstream".
	Type string `json:"type"`
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
	// ShutdownTimeout is the maximum time to wait for graceful shutdown.  Default: "10s".
	ShutdownTimeout string `json:"shutdown_timeout"`
}

// ConsumerConfig controls the market-data consumer binary.
type ConsumerConfig struct {
	// Exchange is the canonical exchange name.  Default: "binance".
	Exchange string `json:"exchange"`
	// MarketType classifies Binance market. Default: "SPOT".
	MarketType string `json:"market_type"`
	// Tickers is the list of canonical instrument names to subscribe to.  Default: ["BTC-USDT","ETH-USDT"].
	Tickers []string `json:"tickers"`
	// BinanceWSBaseURL overrides Binance websocket combined-stream base URL.
	// Default: "wss://stream.binance.com:9443/stream".
	BinanceWSBaseURL string `json:"binance_ws_base_url"`
	// StreamsPerTicker defines how many streams are opened per ticker in ws.Manager planning.
	// For W3 Binance adapter, default is 2 (aggTrade + depth).
	StreamsPerTicker int64 `json:"streams_per_ticker"`
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

// MarketDataConfig controls marketdata publish encoding policy.
type MarketDataConfig struct {
	// PublishContentType controls the wire payload format for produced marketdata envelopes.
	// Allowed: "application/json" (default) or "application/protobuf" (opt-in).
	PublishContentType string `json:"publish_content_type"`
}

// ProcessorConfig controls the aggregation processor binary.
type ProcessorConfig struct {
	// BusCapacity is the channel buffer size for the in-memory event bus.  Default: 1024.
	BusCapacity int `json:"bus_capacity"`
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

func mustParseDuration(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		panic(fmt.Sprintf("invalid duration %q: %v", s, err))
	}
	return d
}

func mustParseByteSize(s string) int64 {
	v, err := parseByteSize(s)
	if err != nil {
		panic(fmt.Sprintf("invalid byte size %q: %v", s, err))
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
