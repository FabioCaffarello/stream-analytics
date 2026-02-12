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
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/anthdm/hollywood/actor"
	mdruntime "github.com/market-raccoon/internal/actors/marketdata/runtime"
	ws "github.com/market-raccoon/internal/actors/marketdata/ws"
	actorruntime "github.com/market-raccoon/internal/actors/runtime"
	"github.com/market-raccoon/internal/adapters/bus"
	"github.com/market-raccoon/internal/adapters/exchange/binance"
	mdapp "github.com/market-raccoon/internal/core/marketdata/app"
	"github.com/market-raccoon/internal/shared/clock"
	"github.com/market-raccoon/internal/shared/config"
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
	flag.Parse()

	cfg := loadConsumerConfig(*configPath, *logLevelOverride)

	// ── logger ───────────────────────────────────────────────────────────────
	logger := buildLogger(cfg.Log)
	slog.SetDefault(logger)

	exchange := cfg.Consumer.Exchange
	tickers := cfg.Consumer.Tickers

	logger.Info("consumer starting",
		"exchange", exchange,
		"tickers", tickers,
	)

	// ── dependencies ─────────────────────────────────────────────────────────
	pub := bus.NewLogPublisher(logger)
	seq := newInMemSequencer()
	clk := clock.NewSystemClock()
	ingest := mdapp.NewIngestMarketData(clk, seq, pub)

	if len(tickers) == 0 {
		logger.Error("consumer: no tickers configured")
		os.Exit(1)
	}

	parseFunc, managerCfg := buildParseFuncAndManagerCfg(cfg, logger, exchange, tickers)

	subCfg := mdruntime.SubsystemConfig{
		Logger:         logger,
		Ingest:         ingest,
		ParseMessage:   parseFunc,
		ParseMessageV2: buildParseFuncV2(logger),
		ManagerConfig:  managerCfg,
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

	e.Send(guardianPID, actorruntime.Stop{})
	select {
	case <-e.Poison(guardianPID).Done():
	case <-shutCtx.Done():
		logger.Warn("consumer: guardian did not stop in time")
	}
	logger.Info("consumer: shutdown complete")
}

func loadConsumerConfig(configPath, logLevelOverride string) config.AppConfig {
	cfg, prob := config.Load(configPath)
	if prob != nil {
		slog.Error("consumer: config load failed", "err", prob)
		os.Exit(1)
	}
	if logLevelOverride != "" {
		cfg.Log.Level = logLevelOverride
	}
	if prob = cfg.Validate(); prob != nil {
		slog.Error("consumer: config validation failed", "err", prob)
		os.Exit(1)
	}
	return cfg
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
		out := mdruntime.ParseMeta{
			EventType:  meta.EventType,
			SkipReason: meta.SkipReason,
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
