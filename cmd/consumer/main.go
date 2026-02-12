// Package main is the market-raccoon consumer binary.
//
// The consumer ingests real-time market data via WebSocket connections and
// publishes normalised events to the event bus.
//
// v1 wiring:
//
//	engine
//	  └─ Guardian  (runtime supervision)
//	       └─ MarketDataSubsystemActor  (ws.Manager → IngestMarketData → LogPublisher)
//
// Exchange connections are real and sourced from Binance WebSocket streams.
//
// Usage:
//
//	go run ./cmd/consumer [flags]
//	  -config     string  path to JSONC config file (default "config.jsonc")
//	  -log-level  string  log level override: debug|info|warn|error
//	  -bus        string  bus adapter override: inmemory|jetstream
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/anthdm/hollywood/actor"
	mdruntime "github.com/market-raccoon/internal/actors/marketdata/runtime"
	ws "github.com/market-raccoon/internal/actors/marketdata/ws"
	actorruntime "github.com/market-raccoon/internal/actors/runtime"
	"github.com/market-raccoon/internal/adapters/bus"
	"github.com/market-raccoon/internal/adapters/exchange/binance"
	adapterjs "github.com/market-raccoon/internal/adapters/jetstream"
	mdapp "github.com/market-raccoon/internal/core/marketdata/app"
	"github.com/market-raccoon/internal/core/marketdata/ports"
	"github.com/market-raccoon/internal/shared/clock"
	"github.com/market-raccoon/internal/shared/config"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/problem"
)

// ---------------------------------------------------------------------------
// in-memory sequencer (v1 — replaced by a distributed sequencer in v2)
// ---------------------------------------------------------------------------

type inMemSequencer struct {
	mu  sync.Mutex
	seq map[string]int64
}

func newInMemSequencer() *inMemSequencer {
	return &inMemSequencer{seq: make(map[string]int64)}
}

func (s *inMemSequencer) Next(venue, instrument string) (int64, *problem.Problem) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := venue + ":" + instrument
	s.seq[key]++
	return s.seq[key], nil
}

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------

func main() {
	configPath := flag.String("config", "config.jsonc", "path to JSONC config file")
	logLevelOverride := flag.String("log-level", "", "log level override: debug|info|warn|error")
	busTypeOverride := flag.String("bus", "", "bus adapter override: inmemory|jetstream")
	flag.Parse()

	cfg := loadConsumerConfig(*configPath, *logLevelOverride, *busTypeOverride)

	// ── logger ───────────────────────────────────────────────────────────────
	logger := buildLogger(cfg.Log)
	slog.SetDefault(logger)

	exchange := cfg.Consumer.Exchange
	tickers := cfg.Consumer.Tickers

	logger.Info("consumer starting",
		"bus_type", cfg.Bus.Type,
		"exchange", exchange,
		"market_type", cfg.Consumer.MarketType,
		"tickers", tickers,
		"streams_per_ticker", cfg.Consumer.StreamsPerTicker,
		"max_streams_per_websocket", cfg.Consumer.MaxStreamsPerWebsocket,
		"max_websockets", cfg.Consumer.MaxWebsockets,
		"backpressure_buffer_size", cfg.Consumer.BackpressureBufferSize,
		"backpressure_policy", cfg.Consumer.BackpressurePolicy,
		"reconnect_base_backoff", cfg.Consumer.ReconnectBaseBackoff,
		"reconnect_max_backoff", cfg.Consumer.ReconnectMaxBackoff,
		"reconnect_budget_window", cfg.Consumer.ReconnectBudgetWindow,
		"reconnect_retry_budget", cfg.Consumer.ReconnectRetryBudget,
		"reconnect_cooldown", cfg.Consumer.ReconnectCooldown,
	)

	// ── dependencies ─────────────────────────────────────────────────────────
	pub, closePublisher := buildPublisher(cfg, logger)
	seq := newInMemSequencer()
	clk := clock.NewSystemClock()
	ingest := mdapp.NewIngestMarketData(clk, seq, pub)

	if len(tickers) == 0 {
		logger.Error("consumer: no tickers configured")
		os.Exit(1)
	}

	parseFunc, managerCfg := buildParseFuncAndManagerCfg(cfg, logger, exchange, tickers)

	subCfg := mdruntime.SubsystemConfig{
		Logger:                 logger,
		Ingest:                 ingest,
		ParseMessage:           parseFunc,
		ParseMessageV2:         buildParseFuncV2(logger),
		ManagerConfig:          managerCfg,
		BackpressureBufferSize: cfg.Consumer.BackpressureBufferSize,
		BackpressurePolicy:     cfg.Consumer.BackpressurePolicy,
	}

	// ── engine ───────────────────────────────────────────────────────────────
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		logger.Error("failed to create actor engine", "err", err)
		os.Exit(1)
	}

	// ── guardian ─────────────────────────────────────────────────────────────
	guardianPID := e.Spawn(
		actorruntime.NewGuardian(actorruntime.GuardianConfig{
			Logger: logger,
			Factories: map[actorruntime.Subsystem]actor.Producer{
				actorruntime.SubsystemMarketData: mdruntime.NewSubsystemActor(subCfg),
			},
		}),
		"guardian",
		actor.WithID("guardian"),
	)
	logger.Info("guardian spawned", "pid", guardianPID.String())

	// ── signal handling ───────────────────────────────────────────────────────
	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("consumer: shutting down")
	cancel()

	shutCtx, shutCancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeoutDuration())
	defer shutCancel()
	if p := closePublisher(shutCtx); p != nil {
		logger.Warn("consumer: publisher close failed", "err", p)
	}

	e.Send(guardianPID, actorruntime.Stop{})
	select {
	case <-e.Poison(guardianPID).Done():
	case <-shutCtx.Done():
		logger.Warn("consumer: guardian did not stop in time")
	}
	logger.Info("consumer: shutdown complete")
}

func loadConsumerConfig(configPath, logLevelOverride, busTypeOverride string) config.AppConfig {
	cfg, prob := config.Load(configPath)
	if prob != nil {
		slog.Error("consumer: config load failed", "err", prob)
		os.Exit(1)
	}
	if logLevelOverride != "" {
		cfg.Log.Level = logLevelOverride
	}
	if busTypeOverride != "" {
		cfg.Bus.Type = busTypeOverride
	}
	if prob = cfg.Validate(); prob != nil {
		slog.Error("consumer: config validation failed", "err", prob)
		os.Exit(1)
	}
	return cfg
}

func buildPublisher(cfg config.AppConfig, logger *slog.Logger) (ports.EventPublisher, func(context.Context) *problem.Problem) {
	switch strings.ToLower(strings.TrimSpace(cfg.Bus.Type)) {
	case "jetstream":
		pub, p := adapterjs.NewPublisher(context.Background(), adapterjs.PublisherConfig{
			URL:            cfg.JetStream.URL,
			StreamName:     cfg.JetStream.StreamName,
			DedupWindow:    cfg.JetStream.DedupWindowDuration(),
			MaxAge:         cfg.JetStream.MaxAgeDuration(),
			MaxBytes:       cfg.JetStream.MaxBytesInt64(),
			PublishTimeout: 5 * time.Second,
		}, metrics.NewBusObserver())
		if p != nil {
			logger.Error("consumer: jetstream publisher init failed", "err", p)
			os.Exit(1)
		}
		logger.Info("consumer: using jetstream publisher",
			"url", cfg.JetStream.URL,
			"stream", cfg.JetStream.StreamName,
			"dedup_window", cfg.JetStream.DedupWindow,
			"max_age", cfg.JetStream.MaxAge,
			"max_bytes", cfg.JetStream.MaxBytes,
		)
		return pub, pub.Close
	default:
		logger.Info("consumer: using in-memory/log publisher")
		return bus.NewLogPublisher(logger), func(context.Context) *problem.Problem { return nil }
	}
}

func buildParseFuncAndManagerCfg(
	cfg config.AppConfig,
	logger *slog.Logger,
	exchange string,
	tickers []string,
) (mdruntime.ParseFunc, *ws.ManagerConfig) {
	parseFunc := func(msg *ws.WsMessage) (mdapp.IngestRequest, bool) {
		req, skip, p := binance.ParseMessage(msg.Data, msg.RecvAt)
		if p != nil {
			logger.Warn("consumer: binance parse skipped message",
				"code", p.Code,
				"message", p.Message,
				"exchange", msg.Exchange,
				"endpoint", msg.Endpoint,
				"bucket_id", msg.BucketID,
			)
		}
		return req, skip || p != nil
	}

	managerCfg := &ws.ManagerConfig{
		Exchange:               exchange,
		Tickers:                tickers,
		StreamsPerTicker:       cfg.Consumer.StreamsPerTicker,
		MaxStreamsPerWebsocket: cfg.Consumer.MaxStreamsPerWebsocket,
		FillStrategy:           ws.FillStrategyAuto,
		MaxWebsockets:          cfg.Consumer.MaxWebsockets,
		MaxWebsocketLifetime:   cfg.Consumer.MaxWebsocketLifetimeDuration(),
		RespawnOverlap:         cfg.Consumer.RespawnOverlapDuration(),
		SubscriptionBuilder:    func([]string) [][]byte { return nil }, // combined stream URL encodes subscriptions
		Heartbeat:              func() ws.Heartbeat { return ws.Heartbeat{} },
		Reconnect: ws.ReconnectPolicy{
			BaseBackoff:  cfg.Consumer.ReconnectBaseBackoffDuration(),
			MaxBackoff:   cfg.Consumer.ReconnectMaxBackoffDuration(),
			Jitter:       cfg.Consumer.ReconnectJitter,
			RetryBudget:  cfg.Consumer.ReconnectRetryBudget,
			BudgetWindow: cfg.Consumer.ReconnectBudgetWindowDuration(),
			Cooldown:     cfg.Consumer.ReconnectCooldownDuration(),
		},
		EndpointBuilder: func(bucket []string) string {
			endpoint, p := binance.BuildEndpoint(cfg.Consumer.BinanceWSBaseURL, bucket)
			if p != nil {
				logger.Error("consumer: binance endpoint build failed", "err", p, "bucket", bucket)
				return ""
			}
			logger.Info("consumer: ws endpoint planned", "endpoint", endpoint, "bucket", bucket)
			return endpoint
		},
	}

	return parseFunc, managerCfg
}

func buildParseFuncV2(logger *slog.Logger) mdruntime.ParseFuncV2 {
	return func(msg *ws.WsMessage) (mdapp.IngestRequest, bool, mdruntime.ParseMeta) {
		req, skip, meta := binance.ParseMessageWithMeta(msg.Data, msg.RecvAt)
		if req.Metadata == nil {
			req.Metadata = make(map[string]string, 8)
		}
		req.Metadata["exchange"] = msg.Exchange
		req.Metadata["endpoint"] = msg.Endpoint
		req.Metadata["bucket_id"] = fmt.Sprintf("%d", msg.BucketID)
		req.Metadata["consumer_id"] = msg.ConsumerID
		req.Metadata["recv_at"] = fmt.Sprintf("%d", msg.RecvAt.UnixMilli())
		if meta.WSStream != "" {
			req.Metadata["ws_stream"] = meta.WSStream
		}

		out := mdruntime.ParseMeta{
			EventType:  meta.EventType,
			SkipReason: meta.SkipReason,
			WSStream:   meta.WSStream,
			Ticker:     meta.Ticker,
		}
		if meta.Problem != nil {
			out.ProblemCode = string(meta.Problem.Code)
			out.ProblemMessage = meta.Problem.Message
			logger.Warn("consumer: binance parse skipped message",
				"code", meta.Problem.Code,
				"message", meta.Problem.Message,
				"exchange", msg.Exchange,
				"endpoint", msg.Endpoint,
				"bucket_id", msg.BucketID,
			)
			return req, true, out
		}
		return req, skip, out
	}
}

func buildLogger(cfg config.LogConfig) *slog.Logger {
	var level slog.Level
	if err := level.UnmarshalText([]byte(strings.ToLower(cfg.Level))); err != nil {
		level = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: level}
	if strings.ToLower(cfg.Format) == "json" {
		return slog.New(slog.NewJSONHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, opts))
}
