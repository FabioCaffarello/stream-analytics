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
// Exchange connections are real when fake=false and a valid exchange is
// configured.  In fake=true mode (default) a synthetic feed goroutine
// generates WsMessages directly so the ingest pipeline can be exercised
// without a live network connection.
//
// Usage:
//
//	go run ./cmd/consumer [flags]
//	  -config     string  path to JSONC config file (default "config.jsonc")
//	  -log-level  string  log level override: debug|info|warn|error
//	  -binance-real       enable real Binance websocket source mode (overrides config)
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
	binanceRealOverride := flag.Bool("binance-real", false, "enable real Binance websocket source mode")
	flag.Parse()

	// ── config ───────────────────────────────────────────────────────────────
	cfg, prob := config.Load(*configPath)
	if prob != nil {
		slog.Error("consumer: config load failed", "err", prob)
		os.Exit(1)
	}
	if *logLevelOverride != "" {
		cfg.Log.Level = *logLevelOverride
	}
	if *binanceRealOverride {
		cfg.Consumer.BinanceReal = true
		cfg.Consumer.Fake = false
	}
	if prob = cfg.Validate(); prob != nil {
		slog.Error("consumer: config validation failed", "err", prob)
		os.Exit(1)
	}

	// ── logger ───────────────────────────────────────────────────────────────
	logger := buildLogger(cfg.Log)
	slog.SetDefault(logger)

	exchange := cfg.Consumer.Exchange
	tickers := cfg.Consumer.Tickers

	logger.Info("consumer starting",
		"exchange", exchange,
		"tickers", tickers,
		"fake", cfg.Consumer.Fake,
		"binance_real", cfg.Consumer.BinanceReal,
	)

	// ── dependencies ─────────────────────────────────────────────────────────
	pub := bus.NewLogPublisher(logger)
	seq := newInMemSequencer()
	clk := clock.NewSystemClock()
	ingest := mdapp.NewIngestMarketData(clk, seq, pub)

	// ── subsystem PID capture (used by fake feeder) ───────────────────────────
	subsystemPIDCh := make(chan *actor.PID, 1)

	if len(tickers) == 0 {
		logger.Error("consumer: no tickers configured")
		os.Exit(1)
	}
	realMode := cfg.Consumer.BinanceReal

	var parseFunc mdruntime.ParseFunc
	var managerCfg *ws.ManagerConfig
	if realMode {
		parseFunc = func(msg *ws.WsMessage) (mdapp.IngestRequest, bool) {
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
		managerCfg = &ws.ManagerConfig{
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
				return endpoint
			},
		}
	} else {
		parseFunc = mdruntime.MakeRawParseFunc(exchange, tickers[0])
	}

	subCfg := mdruntime.SubsystemConfig{
		Logger:        logger,
		Ingest:        ingest,
		ParseMessage:  parseFunc,
		ManagerConfig: managerCfg,
		OnStarted: func(pid *actor.PID) {
			select {
			case subsystemPIDCh <- pid:
			default:
			}
		},
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

	// ── fake feed ─────────────────────────────────────────────────────────────
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if cfg.Consumer.Fake && !realMode {
		go runFakeFeeder(ctx, e, subsystemPIDCh, exchange, tickers, cfg.Consumer.FakeRate(), logger)
	}

	// ── signal handling ───────────────────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("consumer: shutting down")
	cancel() // stop fake feeder

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

// runFakeFeeder generates synthetic *ws.WsMessage at the given rate and sends
// them directly to the subsystem actor.  It runs until ctx is cancelled.
//
// This is a development/testing aid — NOT production code.  In production,
// real WsMessages come from ws.Consumer actors managed by ws.Manager.
func runFakeFeeder(
	ctx context.Context,
	e *actor.Engine,
	pidCh <-chan *actor.PID,
	exchange string,
	tickers []string,
	rate time.Duration,
	logger *slog.Logger,
) {
	// Wait for the subsystem actor to report its PID.
	var pid *actor.PID
	select {
	case pid = <-pidCh:
	case <-time.After(5 * time.Second):
		logger.Warn("fake feeder: timeout waiting for subsystem PID; not starting")
		return
	case <-ctx.Done():
		return
	}

	logger.Info("fake feeder: started",
		"exchange", exchange,
		"tickers", tickers,
		"rate", rate,
		"target", pid.String(),
	)

	ticker := time.NewTicker(rate)
	defer ticker.Stop()

	var seq int64
	for {
		select {
		case <-ctx.Done():
			logger.Info("fake feeder: stopped")
			return
		case t := <-ticker.C:
			seq++
			for _, sym := range tickers {
				data := []byte(fmt.Sprintf(
					`{"price":%.2f,"size":0.001,"side":"buy","trade_id":"%d","timestamp_ms":%d,"symbol":"%s"}`,
					42000+float64(seq)*0.01,
					seq,
					t.UnixMilli(),
					sym,
				))
				e.Send(pid, &ws.WsMessage{
					Exchange:   exchange,
					BucketID:   0,
					ConsumerID: "fake-feeder",
					Endpoint:   "fake://",
					Data:       data,
					RecvAt:     t,
				})
			}
		}
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
