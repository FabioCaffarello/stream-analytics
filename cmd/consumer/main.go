// Package main is the market-raccoon consumer binary.
//
// The consumer ingests real-time market data via WebSocket connections and
// publishes normalised events to the event bus.
//
// v1 wiring:
//
//	engine
//	  └─ Guardian  (runtime supervision)
//	       └─ MarketDataSubsystemActor[xN]  (one per configured exchange)
//
// Exchange connections are real and sourced from configured exchange adapters.
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
	"github.com/market-raccoon/internal/adapters/exchange/bybit"
	adapterjs "github.com/market-raccoon/internal/adapters/jetstream"
	mdapp "github.com/market-raccoon/internal/core/marketdata/app"
	"github.com/market-raccoon/internal/core/marketdata/ports"
	"github.com/market-raccoon/internal/shared/clock"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/config"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/problem"
	"github.com/market-raccoon/internal/shared/replay"
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
	recordPathOverride := flag.String("record", "", "optional fixture path to record published envelopes")
	recordPathLegacyOverride := flag.String("record-path", "", "deprecated: use -record")
	replayPathOverride := flag.String("replay", "", "optional fixture path to replay envelopes offline")
	flag.Parse()

	cfg := loadConsumerConfig(
		*configPath,
		*logLevelOverride,
		*busTypeOverride,
		*recordPathOverride,
		*recordPathLegacyOverride,
		*replayPathOverride,
	)

	// ── logger ───────────────────────────────────────────────────────────────
	logger := buildLogger(cfg.Log)
	slog.SetDefault(logger)
	if strings.TrimSpace(cfg.MarketData.ReplayPath) != "" {
		runConsumerReplay(cfg, logger)
		return
	}
	e2e, p := newE2ERuntime(logger)
	if p != nil {
		logger.Error("consumer: invalid e2e runtime posture", "err", p)
		os.Exit(1)
	}

	logger.Info("consumer starting",
		"bus_type", cfg.Bus.Type,
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
	pub, closePublisher = wrapWithRecorderPublisher(cfg, logger, pub, closePublisher)
	seq := newInMemSequencer()
	clk := clock.NewSystemClock()
	ingest := mdapp.NewIngestMarketDataWithConfig(clk, seq, pub, mdapp.IngestConfig{
		MaxStreams: cfg.MarketData.MaxInstruments,
	})

	runtimes, p := buildExchangeRuntimes(cfg, logger)
	if p != nil {
		logger.Error("consumer: exchange runtime build failed", "err", p)
		os.Exit(1)
	}
	for _, runtimeCfg := range runtimes {
		logger.Info("consumer exchange configured",
			"subsystem", runtimeCfg.Subsystem,
			"name", runtimeCfg.Exchange.Name,
			"type", runtimeCfg.Exchange.Type,
			"market_type", runtimeCfg.Exchange.MarketType,
			"tickers", runtimeCfg.Exchange.Tickers,
			"base_url", runtimeCfg.Exchange.BaseURL,
		)
	}

	// ── engine ───────────────────────────────────────────────────────────────
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		logger.Error("failed to create actor engine", "err", err)
		os.Exit(1)
	}
	e2e.bindEngine(e)

	// ── guardian ─────────────────────────────────────────────────────────────
	factories := make(map[actorruntime.Subsystem]actor.Producer, len(runtimes))
	expected := make([]actorruntime.Subsystem, 0, len(runtimes))
	for _, runtimeCfg := range runtimes {
		managerCfg := runtimeCfg.ManagerCfg
		if e2e.isEnabled() {
			managerCfg = nil // E2E mode injects fake feed directly; no external WS dependency.
		}
		subCfg := mdruntime.SubsystemConfig{
			Subsystem:              runtimeCfg.Subsystem,
			Logger:                 logger,
			Ingest:                 ingest,
			ParseMessage:           runtimeCfg.ParseV1,
			ParseMessageV2:         runtimeCfg.ParseV2,
			ManagerConfig:          managerCfg,
			OnStarted:              e2e.subsystemStartedHook(runtimeCfg.Exchange.Type, runtimeCfg.Exchange.Name),
			BackpressureBufferSize: cfg.Consumer.BackpressureBufferSize,
			BackpressurePolicy:     cfg.Consumer.BackpressurePolicy,
		}
		factories[runtimeCfg.Subsystem] = mdruntime.NewSubsystemActor(subCfg)
		expected = append(expected, runtimeCfg.Subsystem)
	}

	guardianPID := e.Spawn(
		actorruntime.NewGuardian(actorruntime.GuardianConfig{
			Logger:             logger,
			Factories:          factories,
			ExpectedSubsystems: expected,
		}),
		"guardian",
		actor.WithID("guardian"),
	)
	logger.Info("guardian spawned", "pid", guardianPID.String())
	e2e.bindGuardian(guardianPID)
	if p := e2e.startProbe(); p != nil {
		logger.Error("consumer: failed to start e2e probe", "err", p)
		os.Exit(1)
	}

	// ── signal handling ───────────────────────────────────────────────────────
	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("consumer: shutting down")
	cancel()

	shutdownConsumerRuntime(
		logger,
		consumerShutdownHooks{
			shutdownE2E: e2e.shutdown,
			closePublisher: func(ctx context.Context) *problem.Problem {
				return closePublisher(ctx)
			},
			stopGuardian: func() {
				e.Send(guardianPID, actorruntime.Stop{})
			},
			waitGuardianStopped: func(ctx context.Context) bool {
				select {
				case <-e.Poison(guardianPID).Done():
					return true
				case <-ctx.Done():
					return false
				}
			},
		},
		cfg.HTTP.PublisherFlushTimeoutDuration(),
		cfg.HTTP.GuardianShutdownTimeoutDuration(),
	)
}

type consumerShutdownHooks struct {
	shutdownE2E         func(context.Context) *problem.Problem
	closePublisher      func(context.Context) *problem.Problem
	stopGuardian        func()
	waitGuardianStopped func(context.Context) bool
}

func shutdownConsumerRuntime(
	logger *slog.Logger,
	hooks consumerShutdownHooks,
	publisherFlushTimeout time.Duration,
	guardianShutdownTimeout time.Duration,
) {
	depsCtx, depsCancel := context.WithTimeout(context.Background(), guardianShutdownTimeout)
	defer depsCancel()
	if p := hooks.shutdownE2E(depsCtx); p != nil {
		logger.Warn("consumer: e2e probe shutdown failed", "err", p)
	}

	flushCtx, flushCancel := context.WithTimeout(context.Background(), publisherFlushTimeout)
	if p := hooks.closePublisher(flushCtx); p != nil {
		logger.Warn("consumer: publisher close failed", "err", p)
	}
	flushCancel()

	guardianCtx, guardianCancel := context.WithTimeout(context.Background(), guardianShutdownTimeout)
	defer guardianCancel()
	hooks.stopGuardian()
	if !hooks.waitGuardianStopped(guardianCtx) {
		logger.Warn("consumer: guardian did not stop in time")
	}
	logger.Info("consumer: shutdown complete")
}

func loadConsumerConfig(
	configPath,
	logLevelOverride,
	busTypeOverride,
	recordPathOverride,
	recordPathLegacyOverride,
	replayPathOverride string,
) config.AppConfig {
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
	recordPath := strings.TrimSpace(recordPathOverride)
	if recordPath == "" {
		recordPath = strings.TrimSpace(recordPathLegacyOverride)
	}
	if recordPath != "" {
		cfg.MarketData.RecordPath = recordPath
	}
	if strings.TrimSpace(replayPathOverride) != "" {
		cfg.MarketData.ReplayPath = strings.TrimSpace(replayPathOverride)
	}
	if prob = cfg.Validate(); prob != nil {
		slog.Error("consumer: config validation failed", "err", prob)
		os.Exit(1)
	}
	return cfg
}

func runConsumerReplay(cfg config.AppConfig, logger *slog.Logger) {
	replayPath := strings.TrimSpace(cfg.MarketData.ReplayPath)
	if replayPath == "" {
		logger.Error("consumer: replay path must not be empty")
		os.Exit(1)
	}

	logger.Info("consumer: replay mode enabled",
		"replay_path", replayPath,
		"record_path", cfg.MarketData.RecordPath,
	)

	// Replay is intentionally offline: no WS and no remote bus side effects.
	pub := ports.EventPublisher(bus.NewLogPublisher(logger))
	closePublisher := func(context.Context) *problem.Problem { return nil }
	pub, closePublisher = wrapWithRecorderPublisher(cfg, logger, pub, closePublisher)

	fakeClock := clock.NewFakeClock(time.UnixMilli(0))
	replaySeq := replay.NewReplaySequencer()
	ingest := mdapp.NewIngestMarketDataWithConfig(fakeClock, replaySeq, pub, mdapp.IngestConfig{
		MaxStreams: cfg.MarketData.MaxInstruments,
	})

	player, p := replay.NewPlayer(replayPath, fakeClock)
	if p != nil {
		logger.Error("consumer: replay player init failed", "err", p)
		os.Exit(1)
	}
	player.SetReplaySequencer(replaySeq)

	summary, p := player.Replay(context.Background(), func(ctx context.Context, env envelope.Envelope) *problem.Problem {
		req, pp := replayEnvelopeToIngestRequest(env)
		if pp != nil {
			return pp
		}
		res := ingest.Execute(ctx, req)
		if res.IsFail() {
			return res.Problem()
		}
		return nil
	})
	if p != nil {
		logger.Error("consumer: replay failed", "err", p)
		os.Exit(1)
	}

	shutCtx, shutCancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeoutDuration())
	defer shutCancel()
	if p := closePublisher(shutCtx); p != nil {
		logger.Warn("consumer: replay publisher close failed", "err", p)
	}

	logger.Info("consumer: replay complete",
		"input_count", summary.InputCount,
		"input_sha", summary.InputSHA,
		"active_streams", ingest.ActiveStreams(),
	)
}

func replayEnvelopeToIngestRequest(env envelope.Envelope) (mdapp.IngestRequest, *problem.Problem) {
	payload, p := codec.DecodePayload(env.Type, env.Version, env.ContentType, env.Payload)
	if p != nil {
		return mdapp.IngestRequest{}, p
	}

	meta := make(map[string]string, len(env.Meta)+1)
	for k, v := range env.Meta {
		meta[k] = v
	}
	marketType := strings.ToUpper(strings.TrimSpace(meta["instrument_market_type"]))
	if marketType == "" {
		marketType = "SPOT"
		meta["instrument_market_type"] = marketType
	}

	return mdapp.IngestRequest{
		Venue:          env.Venue,
		Instrument:     env.Instrument,
		MarketType:     marketType,
		EventType:      env.Type,
		Version:        env.Version,
		TsExchange:     env.TsExchange,
		IdempotencyKey: env.IdempotencyKey,
		Payload:        payload,
		Metadata:       meta,
	}, nil
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

func wrapWithRecorderPublisher(
	cfg config.AppConfig,
	logger *slog.Logger,
	pub ports.EventPublisher,
	closePublisher func(context.Context) *problem.Problem,
) (ports.EventPublisher, func(context.Context) *problem.Problem) {
	recordPath := strings.TrimSpace(cfg.MarketData.RecordPath)
	if recordPath == "" {
		return pub, closePublisher
	}

	recPub, p := replay.NewRecorderPublisher(pub, recordPath)
	if p != nil {
		logger.Error("consumer: recorder init failed", "record_path", recordPath, "err", p)
		os.Exit(1)
	}
	logger.Info("consumer: fixture recording enabled", "record_path", recordPath)

	return recPub, func(ctx context.Context) *problem.Problem {
		var first *problem.Problem
		if p := recPub.Close(); p != nil {
			first = p
		}
		if p := closePublisher(ctx); p != nil && first == nil {
			first = p
		}
		return first
	}
}

type consumerExchangeRuntime struct {
	Subsystem  actorruntime.Subsystem
	Exchange   config.ConsumerExchangeConfig
	ParseV1    mdruntime.ParseFunc
	ParseV2    mdruntime.ParseFuncV2
	ManagerCfg *ws.ManagerConfig
}

func buildExchangeRuntimes(cfg config.AppConfig, logger *slog.Logger) ([]consumerExchangeRuntime, *problem.Problem) {
	exchanges := configuredExchanges(cfg)
	if len(exchanges) == 0 {
		return nil, problem.New(problem.ValidationFailed, "consumer: no exchange configured")
	}

	multi := len(exchanges) > 1
	out := make([]consumerExchangeRuntime, 0, len(exchanges))
	for _, exchange := range exchanges {
		ex := exchange
		if p := validateRuntimeExchange(ex); p != nil {
			return nil, p
		}
		if strings.TrimSpace(ex.MarketType) == "" {
			ex.MarketType = "SPOT"
		}
		runtimeCfg, p := buildExchangeRuntime(cfg, logger, ex, multi)
		if p != nil {
			return nil, p
		}
		out = append(out, runtimeCfg)
	}

	return out, nil
}

func configuredExchanges(cfg config.AppConfig) []config.ConsumerExchangeConfig {
	exchanges := cfg.Consumer.Exchanges
	if len(exchanges) != 0 {
		return exchanges
	}
	exchange := strings.ToLower(strings.TrimSpace(cfg.Consumer.Exchange))
	if exchange == "" {
		exchange = "binance"
	}
	return []config.ConsumerExchangeConfig{
		{
			Name:       exchange,
			Type:       exchange,
			BaseURL:    strings.TrimSpace(cfg.Consumer.BinanceWSBaseURL),
			Tickers:    append([]string(nil), cfg.Consumer.Tickers...),
			MarketType: strings.ToUpper(strings.TrimSpace(cfg.Consumer.MarketType)),
		},
	}
}

func validateRuntimeExchange(ex config.ConsumerExchangeConfig) *problem.Problem {
	if strings.TrimSpace(ex.Name) == "" {
		return problem.New(problem.ValidationFailed, "consumer: exchange.name must not be empty")
	}
	if len(ex.Tickers) == 0 {
		return problem.Newf(problem.ValidationFailed, "consumer: exchange %q has no tickers", ex.Name)
	}
	return nil
}

func buildExchangeRuntime(
	cfg config.AppConfig,
	logger *slog.Logger,
	ex config.ConsumerExchangeConfig,
	multi bool,
) (consumerExchangeRuntime, *problem.Problem) {
	subsystem := marketDataSubsystemKey(ex.Name, multi)
	switch strings.ToLower(strings.TrimSpace(ex.Type)) {
	case "binance":
		return buildBinanceRuntime(cfg, logger, ex, subsystem), nil
	case "bybit":
		return buildBybitRuntime(cfg, logger, ex, subsystem), nil
	default:
		return consumerExchangeRuntime{}, problem.Newf(problem.ValidationFailed, "consumer: unsupported exchange type %q", ex.Type)
	}
}

func buildBinanceRuntime(
	cfg config.AppConfig,
	logger *slog.Logger,
	ex config.ConsumerExchangeConfig,
	subsystem actorruntime.Subsystem,
) consumerExchangeRuntime {
	managerCfg := baseManagerConfig(cfg, ex)
	managerCfg.SubscriptionBuilder = func([]string) [][]byte { return nil }
	managerCfg.EndpointBuilder = func(bucket []string) string {
		endpoint, p := binance.BuildEndpoint(ex.BaseURL, bucket)
		if p != nil {
			logger.Error("consumer: binance endpoint build failed", "err", p, "exchange", ex.Name, "bucket", bucket)
			return ""
		}
		logger.Info("consumer: ws endpoint planned", "exchange", ex.Name, "endpoint", endpoint, "bucket", bucket)
		return endpoint
	}

	parseV1 := func(msg *ws.WsMessage) (mdapp.IngestRequest, bool) {
		req, skip, p := binance.ParseMessageForMarketType(msg.Data, msg.RecvAt, ex.MarketType)
		enrichRequestMetadata(&req, msg, ex.MarketType, "")
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
	parseV2 := func(msg *ws.WsMessage) (mdapp.IngestRequest, bool, mdruntime.ParseMeta) {
		req, skip, meta := binance.ParseMessageWithMetaForMarketType(msg.Data, msg.RecvAt, ex.MarketType)
		enrichRequestMetadata(&req, msg, ex.MarketType, meta.WSStream)
		outMeta := toRuntimeParseMeta(meta.EventType, meta.SkipReason, meta.WSStream, meta.Ticker, meta.Problem)
		if meta.Problem != nil {
			logger.Warn("consumer: binance parse skipped message",
				"code", meta.Problem.Code,
				"message", meta.Problem.Message,
				"exchange", msg.Exchange,
				"endpoint", msg.Endpoint,
				"bucket_id", msg.BucketID,
			)
			return req, true, outMeta
		}
		return req, skip, outMeta
	}
	return consumerExchangeRuntime{
		Subsystem:  subsystem,
		Exchange:   ex,
		ParseV1:    parseV1,
		ParseV2:    parseV2,
		ManagerCfg: &managerCfg,
	}
}

func buildBybitRuntime(
	cfg config.AppConfig,
	logger *slog.Logger,
	ex config.ConsumerExchangeConfig,
	subsystem actorruntime.Subsystem,
) consumerExchangeRuntime {
	managerCfg := baseManagerConfig(cfg, ex)
	managerCfg.SubscriptionBuilder = func(bucket []string) [][]byte {
		msgs, p := bybit.BuildSubscriptions(bucket)
		if p != nil {
			logger.Error("consumer: bybit subscription build failed", "err", p, "exchange", ex.Name, "bucket", bucket)
			return nil
		}
		return msgs
	}
	managerCfg.EndpointBuilder = func(bucket []string) string {
		endpoint, p := bybit.BuildEndpoint(ex.BaseURL, bucket, ex.MarketType)
		if p != nil {
			logger.Error("consumer: bybit endpoint build failed", "err", p, "exchange", ex.Name, "bucket", bucket)
			return ""
		}
		logger.Info("consumer: ws endpoint planned", "exchange", ex.Name, "endpoint", endpoint, "bucket", bucket)
		return endpoint
	}

	parseV1 := func(msg *ws.WsMessage) (mdapp.IngestRequest, bool) {
		req, skip, p := bybit.ParseMessageForMarketType(msg.Data, msg.RecvAt, ex.MarketType)
		enrichRequestMetadata(&req, msg, ex.MarketType, "")
		if p != nil {
			logger.Warn("consumer: bybit parse skipped message",
				"code", p.Code,
				"message", p.Message,
				"exchange", msg.Exchange,
				"endpoint", msg.Endpoint,
				"bucket_id", msg.BucketID,
			)
		}
		return req, skip || p != nil
	}
	parseV2 := func(msg *ws.WsMessage) (mdapp.IngestRequest, bool, mdruntime.ParseMeta) {
		req, skip, meta := bybit.ParseMessageWithMetaForMarketType(msg.Data, msg.RecvAt, ex.MarketType)
		enrichRequestMetadata(&req, msg, ex.MarketType, meta.WSStream)
		outMeta := toRuntimeParseMeta(meta.EventType, meta.SkipReason, meta.WSStream, meta.Ticker, meta.Problem)
		if meta.Problem != nil {
			logger.Warn("consumer: bybit parse skipped message",
				"code", meta.Problem.Code,
				"message", meta.Problem.Message,
				"exchange", msg.Exchange,
				"endpoint", msg.Endpoint,
				"bucket_id", msg.BucketID,
			)
			return req, true, outMeta
		}
		return req, skip, outMeta
	}
	return consumerExchangeRuntime{
		Subsystem:  subsystem,
		Exchange:   ex,
		ParseV1:    parseV1,
		ParseV2:    parseV2,
		ManagerCfg: &managerCfg,
	}
}

func baseManagerConfig(cfg config.AppConfig, ex config.ConsumerExchangeConfig) ws.ManagerConfig {
	return ws.ManagerConfig{
		Exchange:               ex.Name,
		Tickers:                ex.Tickers,
		StreamsPerTicker:       cfg.Consumer.StreamsPerTicker,
		MaxStreamsPerWebsocket: cfg.Consumer.MaxStreamsPerWebsocket,
		FillStrategy:           ws.FillStrategyAuto,
		MaxWebsockets:          cfg.Consumer.MaxWebsockets,
		MaxWebsocketLifetime:   cfg.Consumer.MaxWebsocketLifetimeDuration(),
		RespawnOverlap:         cfg.Consumer.RespawnOverlapDuration(),
		Heartbeat:              func() ws.Heartbeat { return ws.Heartbeat{} },
		Reconnect: ws.ReconnectPolicy{
			BaseBackoff:  cfg.Consumer.ReconnectBaseBackoffDuration(),
			MaxBackoff:   cfg.Consumer.ReconnectMaxBackoffDuration(),
			Jitter:       cfg.Consumer.ReconnectJitter,
			RetryBudget:  cfg.Consumer.ReconnectRetryBudget,
			BudgetWindow: cfg.Consumer.ReconnectBudgetWindowDuration(),
			Cooldown:     cfg.Consumer.ReconnectCooldownDuration(),
		},
	}
}

func marketDataSubsystemKey(exchangeName string, multi bool) actorruntime.Subsystem {
	if !multi {
		return actorruntime.SubsystemMarketData
	}
	name := strings.ToLower(strings.TrimSpace(exchangeName))
	if name == "" {
		name = "exchange"
	}
	return actorruntime.Subsystem("marketdata:" + name)
}

func enrichRequestMetadata(req *mdapp.IngestRequest, msg *ws.WsMessage, defaultMarketType, wsStream string) {
	if req.Metadata == nil {
		req.Metadata = make(map[string]string, 8)
	}
	if strings.TrimSpace(req.MarketType) == "" {
		req.MarketType = strings.ToUpper(strings.TrimSpace(defaultMarketType))
		if req.MarketType == "" {
			req.MarketType = "SPOT"
		}
	}
	if req.Metadata["instrument_market_type"] == "" {
		req.Metadata["instrument_market_type"] = req.MarketType
	}
	req.Metadata["exchange"] = msg.Exchange
	req.Metadata["endpoint"] = msg.Endpoint
	req.Metadata["bucket_id"] = fmt.Sprintf("%d", msg.BucketID)
	req.Metadata["consumer_id"] = msg.ConsumerID
	req.Metadata["recv_at"] = fmt.Sprintf("%d", msg.RecvAt.UnixMilli())
	if wsStream != "" {
		req.Metadata["ws_stream"] = wsStream
	}
}

func toRuntimeParseMeta(eventType, skipReason, wsStream, ticker string, p *problem.Problem) mdruntime.ParseMeta {
	out := mdruntime.ParseMeta{
		EventType:  eventType,
		SkipReason: skipReason,
		WSStream:   wsStream,
		Ticker:     ticker,
	}
	if p != nil {
		out.ProblemCode = string(p.Code)
		out.ProblemMessage = p.Message
	}
	return out
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
