// Package main is the market-raccoon processor binary.
//
// The processor subscribes to an InMemoryBus, reads normalised event envelopes,
// and applies them to the core aggregation use cases (order book, etc.).
//
// v1 wiring:
//
//	InMemoryBus (channel)
//	  └─ ProcessorSubsystemActor  → UpdateOrderBookFromEvents → LogArtifactPublisher
//	engine
//	  └─ Guardian (aggregation factory)
//
// In production, the InMemoryBus will be replaced by NATS JetStream
// (see ADR-0004).  The ProcessorSubsystemActor only depends on a
// <-chan envelope.Envelope, so the swap is a one-line change in this wiring.
//
// Usage:
//
//	go run ./cmd/processor [flags]
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
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/anthdm/hollywood/actor"
	aggruntime "github.com/market-raccoon/internal/actors/aggregation/runtime"
	actorruntime "github.com/market-raccoon/internal/actors/runtime"
	"github.com/market-raccoon/internal/adapters/bus"
	adapterjs "github.com/market-raccoon/internal/adapters/jetstream"
	adapterstorage "github.com/market-raccoon/internal/adapters/storage"
	"github.com/market-raccoon/internal/adapters/storage/clickhouse"
	"github.com/market-raccoon/internal/adapters/storage/timescale"
	aggapp "github.com/market-raccoon/internal/core/aggregation/app"
	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	insightsapp "github.com/market-raccoon/internal/core/insights/app"
	mddomain "github.com/market-raccoon/internal/core/marketdata/domain"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/config"
	"github.com/market-raccoon/internal/shared/contracts"
	"github.com/market-raccoon/internal/shared/envelope"
	sharedhash "github.com/market-raccoon/internal/shared/hash"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/problem"
	"github.com/market-raccoon/internal/shared/replay"
)

// ---------------------------------------------------------------------------
// v1 stub adapters for aggregation ports
// (will be replaced by real NATS/storage adapters in a future iteration)
// ---------------------------------------------------------------------------

type logArtifactPublisher struct{ logger *slog.Logger }

func (p *logArtifactPublisher) PublishSnapshot(_ context.Context, snap aggdomain.SnapshotProduced) *problem.Problem {
	p.logger.Debug("aggregation: snapshot published",
		"venue", snap.BookID.Venue,
		"instrument", snap.BookID.Instrument,
		"seq", snap.Seq,
		"bids", len(snap.Bids),
		"asks", len(snap.Asks),
	)
	return nil
}

func (p *logArtifactPublisher) PublishInconsistent(_ context.Context, evt aggdomain.OrderBookInconsistentDetected) *problem.Problem {
	p.logger.Warn("aggregation: inconsistent book detected",
		"venue", evt.BookID.Venue,
		"instrument", evt.BookID.Instrument,
		"seq", evt.Seq,
		"reason", evt.Reason,
	)
	return nil
}

type committedHotStore struct {
	committer *adapterstorage.SnapshotCommitter
}

func (s *committedHotStore) Save(ctx context.Context, snap aggdomain.SnapshotProduced) *problem.Problem {
	if s == nil || s.committer == nil {
		return problem.New(problem.ValidationFailed, "committed hot store is not configured")
	}
	return s.committer.Commit(ctx, snap)
}

type envelopeSource struct {
	envelopeCh <-chan envelope.Envelope
	consumeErr <-chan *problem.Problem
	shutdownFn func(context.Context)
	onResult   func(aggruntime.EnvelopeProcessResult)
}

func initEnvelopeSource(cfg config.AppConfig, logger *slog.Logger, e2e *e2eRuntime) envelopeSource {
	replayMode := strings.ToLower(strings.TrimSpace(cfg.Replay.Mode))
	if replayMode == "" {
		replayMode = "off"
	}
	if replayMode == "off" && strings.TrimSpace(cfg.MarketData.ReplayPath) != "" {
		replayMode = "file"
	}

	switch replayMode {
	case "file":
		replayPath := strings.TrimSpace(cfg.MarketData.ReplayPath)
		if replayPath == "" {
			logger.Error("processor: replay.mode=file requires marketdata.replay_path")
			os.Exit(1)
		}
		return initReplayEnvelopeSource(replayPath, cfg.Processor.BusCapacity, logger)
	case "jetstream":
		return initJetStreamReplayEnvelopeSource(cfg, logger)
	}

	switch strings.ToLower(strings.TrimSpace(cfg.Bus.Type)) {
	case "jetstream":
		ch := make(chan envelope.Envelope, cfg.Processor.BusCapacity)
		resultsCh := make(chan aggruntime.EnvelopeProcessResult, 1)
		filterSubjects := effectiveJetStreamFilters(cfg)

		onResult := func(res aggruntime.EnvelopeProcessResult) {
			res.Problem = e2e.maybeInjectTransient(res.Envelope, res.Problem)
			select {
			case resultsCh <- res:
			default:
				logger.Warn("processor: dropped processing result notification")
			}
		}

		durableName := shardAwareDurable(cfg.JetStream.ConsumerDurable, cfg.Shard.Index, cfg.Shard.Count)

		jetstreamConsumer, p := adapterjs.NewConsumer(context.Background(), adapterjs.ConsumerConfig{
			URL:             cfg.JetStream.URL,
			StreamName:      cfg.JetStream.StreamName,
			DedupWindow:     cfg.JetStream.DedupWindowDuration(),
			MaxAge:          cfg.JetStream.MaxAgeDuration(),
			MaxBytes:        cfg.JetStream.MaxBytesInt64(),
			ConsumerDurable: durableName,
			FilterSubjects:  filterSubjects,
			AckWait:         cfg.JetStream.AckWaitDuration(),
			MaxAckPending:   cfg.JetStream.MaxAckPending,
			MaxDeliver:      cfg.JetStream.MaxDeliver,
			DeliverPolicy:   cfg.JetStream.DeliverPolicy,
			ShardGroupCount: cfg.JetStream.ShardGroupCount,
			ShardGroupID:    cfg.JetStream.ShardGroupID,
		}, metrics.NewBusObserver())
		if p != nil {
			logger.Error("processor: jetstream consumer init failed", "err", p)
			os.Exit(1)
		}

		runCtx, cancelConsume := context.WithCancel(context.Background())
		errCh := make(chan *problem.Problem, 1)
		go func() {
			errCh <- jetstreamConsumer.Consume(runCtx, func(ctx context.Context, env envelope.Envelope) *problem.Problem {
				select {
				case ch <- env:
				case <-ctx.Done():
					return problem.WithRetryable(problem.Wrap(ctx.Err(), problem.Unavailable, "processor enqueue canceled"))
				}
				select {
				case res := <-resultsCh:
					return res.Problem
				case <-ctx.Done():
					return problem.WithRetryable(problem.Wrap(ctx.Err(), problem.Unavailable, "processor result wait canceled"))
				}
			})
		}()

		logger.Info("processor: subscribed to jetstream consumer",
			"url", cfg.JetStream.URL,
			"stream", cfg.JetStream.StreamName,
			"durable", durableName,
			"filters", filterSubjects,
			"shard", fmt.Sprintf("%d/%d", cfg.Shard.Index, cfg.Shard.Count),
		)

		return envelopeSource{
			envelopeCh: ch,
			consumeErr: errCh,
			onResult:   onResult,
			shutdownFn: func(ctx context.Context) {
				cancelConsume()
				if p := jetstreamConsumer.Close(ctx); p != nil {
					logger.Warn("processor: jetstream consumer close failed", "err", p)
				}
			},
		}
	default:
		eventBus := bus.NewInMemoryBus(cfg.Processor.BusCapacity, metrics.NewBusObserver())
		logger.Info("processor: subscribed to in-memory bus")
		return envelopeSource{
			envelopeCh: eventBus.Subscribe(),
			shutdownFn: func(context.Context) {
				eventBus.Close()
			},
		}
	}
}

func initJetStreamReplayEnvelopeSource(cfg config.AppConfig, logger *slog.Logger) envelopeSource {
	capacity := cfg.Processor.BusCapacity
	if capacity <= 0 {
		capacity = 1024
	}

	replayDurable := strings.TrimSpace(cfg.JetStream.ConsumerDurable) + "-replay"
	src, p := adapterjs.NewJetStreamReplaySource(adapterjs.ReplaySourceConfig{
		URL:             cfg.JetStream.URL,
		StreamName:      cfg.JetStream.StreamName,
		SubjectFilter:   cfg.Replay.JetStream.SubjectFilter,
		ConsumerDurable: replayDurable,
		DedupWindow:     cfg.JetStream.DedupWindowDuration(),
		MaxAge:          cfg.JetStream.MaxAgeDuration(),
		MaxBytes:        cfg.JetStream.MaxBytesInt64(),
		AckWait:         cfg.JetStream.AckWaitDuration(),
		MaxAckPending:   cfg.JetStream.MaxAckPending,
		MaxDeliver:      cfg.JetStream.MaxDeliver,
		DeliverPolicy:   cfg.Replay.JetStream.DeliverPolicy,
		Window:          cfg.Replay.JetStream.WindowDuration(),
		MaxMessages:     cfg.Replay.JetStream.MaxMessages,
		MergeBufferSize: cfg.Replay.JetStream.MergeBuffer,
		OutputBufferSize: func() int {
			if capacity < 256 {
				return capacity
			}
			return 256
		}(),
		DecodeErrorMode: cfg.Replay.OnDecodeError,
	})
	if p != nil {
		logger.Error("processor: jetstream replay source init failed", "err", p)
		os.Exit(1)
	}

	runCtx, cancel := context.WithCancel(context.Background())
	sourceCh, closeFn, p := src.Read(runCtx)
	if p != nil {
		logger.Error("processor: jetstream replay source read init failed", "err", p)
		os.Exit(1)
	}

	outCh := make(chan envelope.Envelope, capacity)
	errCh := make(chan *problem.Problem, 1)
	var closeOnce sync.Once
	var closeProb *problem.Problem
	safeClose := func() *problem.Problem {
		closeOnce.Do(func() {
			if err := closeFn(); err != nil {
				closeProb = problem.WithRetryable(problem.Wrap(err, problem.Unavailable, "close jetstream replay source failed"))
			}
		})
		return closeProb
	}

	go func() {
		defer close(outCh)
		defer close(errCh)

		for {
			select {
			case <-runCtx.Done():
				errCh <- safeClose()
				return
			case env, ok := <-sourceCh:
				if !ok {
					errCh <- safeClose()
					return
				}

				select {
				case <-runCtx.Done():
					errCh <- safeClose()
					return
				case outCh <- env:
				}
			}
		}
	}()

	logger.Info("processor: replay source mode jetstream enabled",
		"url", cfg.JetStream.URL,
		"stream", cfg.JetStream.StreamName,
		"subject_filter", cfg.Replay.JetStream.SubjectFilter,
		"deliver_policy", cfg.Replay.JetStream.DeliverPolicy,
		"window", cfg.Replay.JetStream.Window,
		"max_messages", cfg.Replay.JetStream.MaxMessages,
		"merge_buffer", cfg.Replay.JetStream.MergeBuffer,
		"durable", replayDurable,
	)

	return envelopeSource{
		envelopeCh: outCh,
		consumeErr: errCh,
		shutdownFn: func(context.Context) {
			cancel()
			_ = safeClose()
		},
	}
}

func initReplayEnvelopeSource(path string, capacity int, logger *slog.Logger) envelopeSource {
	if capacity <= 0 {
		capacity = 1024
	}

	ch := make(chan envelope.Envelope, capacity)
	errCh := make(chan *problem.Problem, 1)
	runCtx, cancel := context.WithCancel(context.Background())

	go func() {
		defer close(ch)
		defer close(errCh)

		reader, p := replay.NewReader(path)
		if p != nil {
			errCh <- p
			return
		}
		defer func() {
			_ = reader.Close()
		}()

		count := 0
		hashes := make([]string, 0, 1024)
		for {
			rec, ok, p := reader.Next()
			if p != nil {
				errCh <- p
				return
			}
			if !ok {
				break
			}

			select {
			case <-runCtx.Done():
				errCh <- nil
				return
			case ch <- rec.Envelope:
				count++
				hashes = append(hashes, rec.SHA256)
			}
		}

		logger.Info("processor: replay fixture loaded",
			"replay_path", path,
			"records", count,
			"sha256", sharedhash.HashFields(hashes...),
		)
		errCh <- nil
	}()

	return envelopeSource{
		envelopeCh: ch,
		consumeErr: errCh,
		shutdownFn: func(context.Context) {
			cancel()
		},
	}
}

func effectiveJetStreamFilters(cfg config.AppConfig) []string {
	base := append([]string(nil), cfg.JetStream.FilterSubjects...)
	if cfg.Processor.Insights.EnableCrossVenueJoin {
		if joinSubject := strings.TrimSpace(cfg.Processor.Insights.JoinTradesSubject); joinSubject != "" {
			covered := false
			for _, existing := range base {
				if subjectMatchesFilter(joinSubject, strings.TrimSpace(existing)) {
					covered = true
					break
				}
			}
			if !covered {
				base = append(base, joinSubject)
			}
		}
	}
	if len(base) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(base))
	out := make([]string, 0, len(base))
	for _, raw := range base {
		subject := strings.TrimSpace(raw)
		if subject == "" {
			continue
		}
		if _, exists := seen[subject]; exists {
			continue
		}
		seen[subject] = struct{}{}
		out = append(out, subject)
	}
	return out
}

func subjectMatchesFilter(subject, filter string) bool {
	subject = strings.TrimSpace(subject)
	filter = strings.TrimSpace(filter)
	if subject == "" || filter == "" {
		return false
	}
	if filter == ">" {
		return true
	}
	if subject == filter {
		return true
	}
	if strings.HasSuffix(filter, ".>") {
		prefix := strings.TrimSuffix(filter, ">")
		return strings.HasPrefix(subject, prefix)
	}
	return false
}

func buildEnvelopePublisher(cfg config.AppConfig, logger *slog.Logger) (aggruntime.EventPublisher, func(context.Context) *problem.Problem) {
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
			logger.Error("processor: jetstream publisher init failed", "err", p)
			os.Exit(1)
		}
		logger.Info("processor: using jetstream publisher",
			"url", cfg.JetStream.URL,
			"stream", cfg.JetStream.StreamName,
		)
		return pub, pub.Close
	default:
		logger.Info("processor: using in-memory/log publisher")
		return bus.NewLogPublisher(logger), func(context.Context) *problem.Problem { return nil }
	}
}

func maybeInjectJoinFixture(cfg config.AppConfig, e2e *e2eRuntime, pub aggruntime.EventPublisher, logger *slog.Logger) *problem.Problem {
	if e2e == nil || !e2e.shouldInjectJoinFixture() {
		return nil
	}
	if !cfg.Processor.Insights.EnableCrossVenueJoin {
		logger.Warn("processor: skipping e2e join fixture injection because cross-venue join is disabled")
		return nil
	}
	if pub == nil {
		return problem.New(problem.ValidationFailed, "e2e join fixture injection requires publisher")
	}

	buildTrade := func(venue string, seq int64, tsIngest int64, price float64, side string, tradeID string) (envelope.Envelope, *problem.Problem) {
		payload, p := codec.EncodePayload("marketdata.trade", 1, envelope.ContentTypeJSON, mddomain.TradeTickV1{
			Price:     price,
			Size:      1.0,
			Side:      side,
			TradeID:   tradeID,
			Timestamp: tsIngest - 10,
		})
		if p != nil {
			return envelope.Envelope{}, p
		}
		env := envelope.Envelope{
			Type:           "marketdata.trade",
			Version:        1,
			Venue:          venue,
			Instrument:     e2e.joinInstrument,
			TsExchange:     tsIngest - 10,
			TsIngest:       tsIngest,
			Seq:            seq,
			IdempotencyKey: sharedhash.HashFields("e2e_join_fixture", venue, e2e.joinInstrument, side, tradeID),
			ContentType:    envelope.ContentTypeJSON,
			Meta: map[string]string{
				"instrument_market_type": "SPOT",
			},
			Payload: payload,
		}
		if p := env.Validate(); p != nil {
			return envelope.Envelope{}, p
		}
		return env, nil
	}

	trades := []struct {
		venue string
		seq   int64
		ts    int64
		price float64
		side  string
		id    string
	}{
		{venue: "BINANCE", seq: 1, ts: 1_710_000_001_000, price: 100.25, side: "buy", id: "e2e-b-1"},
		{venue: "BYBIT", seq: 1, ts: 1_710_000_001_010, price: 100.35, side: "sell", id: "e2e-y-1"},
	}
	for _, trade := range trades {
		env, p := buildTrade(trade.venue, trade.seq, trade.ts, trade.price, trade.side, trade.id)
		if p != nil {
			return p
		}
		if p := pub.Publish(context.Background(), env); p != nil {
			return p
		}
	}
	logger.Info("processor: injected deterministic e2e join fixture", "instrument", e2e.joinInstrument, "venues", len(trades))
	return nil
}

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------

func main() {
	configPath := flag.String("config", "config.jsonc", "path to JSONC config file")
	logLevelOverride := flag.String("log-level", "", "log level override: debug|info|warn|error")
	busTypeOverride := flag.String("bus", "", "bus adapter override: inmemory|jetstream")
	replayModeOverride := flag.String("replay-mode", "", "replay mode override: off|file|jetstream")
	replayPathOverride := flag.String("replay-path", "", "optional fixture path to replay envelopes")
	shardIndex := flag.Int("shard-index", -1, "shard index override (0-based); env: SHARD_INDEX")
	shardCount := flag.Int("shard-count", -1, "total shard count override; env: SHARD_COUNT")
	flag.Parse()

	cfg := loadProcessorConfig(*configPath, *logLevelOverride, *busTypeOverride, *replayModeOverride, *replayPathOverride, *shardIndex, *shardCount)

	// ── logger ─────────────────────────────────────────────────────────────
	logger := buildLogger(cfg.Log)
	slog.SetDefault(logger)
	e2e, p := newE2ERuntime(logger)
	if p != nil {
		logger.Error("processor: invalid e2e runtime posture", "err", p)
		os.Exit(1)
	}
	if p := e2e.startProbe(); p != nil {
		logger.Error("processor: failed to start e2e probe", "err", p)
		os.Exit(1)
	}

	logger.Info("processor starting",
		"shard", fmt.Sprintf("%d/%d", cfg.Shard.Index, cfg.Shard.Count),
		"bus_type", cfg.Bus.Type,
	)

	// Ensure payload codec registry is bootstrapped unconditionally so that
	// content-type-aware decoding (e.g. protobuf) works even when
	// EnableCrossVenueJoin is disabled. Options remain controlled by config.
	if p := contracts.BootstrapPayloadCodecRegistryWithOptions(contracts.PayloadRegistryOptions{
		EnableInsightsVolumeProfileSnapshotProto: cfg.Processor.Insights.EnableVolumeProfileSnapshotProto,
	}); p != nil {
		logger.Error("processor: payload codec registry bootstrap failed", "err", p)
		os.Exit(1)
	}

	// ── aggregation use case ────────────────────────────────────────────────
	artifactPub := &logArtifactPublisher{logger: logger}
	hotStore := &committedHotStore{
		committer: adapterstorage.NewSnapshotCommitter(timescale.NewWriter(), clickhouse.NewWriter()),
	}
	aggSvc := &aggapp.AggregationService{
		UpdateBook: aggapp.NewUpdateOrderBookFromEventsWithConfig(artifactPub, hotStore, aggapp.UpdateConfig{
			MaxBooks: cfg.Processor.MaxInstruments,
		}),
	}
	var joinTrades *insightsapp.JoinCrossVenueTrades
	var publishEnvelope aggruntime.EventPublisher
	closePublisher := func(context.Context) *problem.Problem { return nil }
	if cfg.Processor.Insights.EnableCrossVenueJoin {
		publishEnvelope, closePublisher = buildEnvelopePublisher(cfg, logger)
		joinTrades = insightsapp.NewJoinCrossVenueTradesWithConfig(insightsapp.JoinCrossVenueTradesConfig{
			MaxInstruments:     cfg.Processor.Insights.MaxInstruments,
			TTL:                cfg.Processor.Insights.TTLDuration(),
			EnableSpreadSignal: cfg.Processor.Insights.EnableSpreadSignal,
			MinVenues:          cfg.Processor.Insights.MinVenues,
			MinSpreadBPS:       cfg.Processor.Insights.MinSpreadBPS,
			RoundingMode:       cfg.Processor.Insights.RoundingMode,
			SweepEveryN:        cfg.Processor.Insights.SweepEveryN,
			SweepEvery:         cfg.Processor.Insights.SweepEveryDuration(),
		})
		logger.Info("processor: cross-venue trade join enabled",
			"join_subject", cfg.Processor.Insights.JoinTradesSubject,
			"snapshot_subject_prefix", cfg.Processor.Insights.SnapshotSubjectPrefix,
			"max_instruments", cfg.Processor.Insights.MaxInstruments,
			"ttl", cfg.Processor.Insights.TTL,
			"enable_spread_signal", cfg.Processor.Insights.EnableSpreadSignal,
			"min_venues", cfg.Processor.Insights.MinVenues,
			"min_spread_bps", cfg.Processor.Insights.MinSpreadBPS,
			"rounding_mode", cfg.Processor.Insights.RoundingMode,
			"sweep_every_n", cfg.Processor.Insights.SweepEveryN,
			"sweep_every", cfg.Processor.Insights.SweepEvery,
		)
	}

	// ── envelope source wiring ───────────────────────────────────────────────
	source := initEnvelopeSource(cfg, logger, e2e)

	// ── processor subsystem config ──────────────────────────────────────────
	processorCfg := aggruntime.ProcessorConfig{
		Logger:                logger,
		EnvelopeCh:            source.envelopeCh,
		Service:               aggSvc,
		JoinTrades:            joinTrades,
		PublishEnvelope:       publishEnvelope,
		SnapshotSubjectPrefix: cfg.Processor.Insights.SnapshotSubjectPrefix,
		OnEnvelopeProcessed:   source.onResult,
	}

	// ── engine ──────────────────────────────────────────────────────────────
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		logger.Error("failed to create actor engine", "err", err)
		os.Exit(1)
	}

	// ── guardian with aggregation factory ──────────────────────────────────
	guardianPID := e.Spawn(
		actorruntime.NewGuardian(actorruntime.GuardianConfig{
			Logger: logger,
			Factories: map[actorruntime.Subsystem]actor.Producer{
				actorruntime.SubsystemAggregation: aggruntime.NewProcessorSubsystemActor(processorCfg),
			},
		}),
		"guardian",
		actor.WithID("guardian"),
	)
	logger.Info("processor: guardian spawned", "pid", guardianPID.String())
	logger.Info("processor: waiting for envelopes (use cmd/consumer or inject via InMemoryBus)")
	if p := maybeInjectJoinFixture(cfg, e2e, publishEnvelope, logger); p != nil {
		logger.Error("processor: failed to inject e2e join fixture", "err", p)
		os.Exit(1)
	}
	e2e.markReady()

	// ── signal handling ─────────────────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-quit:
	case p := <-source.consumeErr:
		if p != nil {
			logger.Error("processor: jetstream consume loop failed", "err", p)
		}
	}

	logger.Info("processor: shutting down")

	depsCtx, depsCancel := context.WithTimeout(context.Background(), cfg.HTTP.GuardianShutdownTimeoutDuration())
	defer depsCancel()
	if p := e2e.shutdown(depsCtx); p != nil {
		logger.Warn("processor: e2e probe shutdown failed", "err", p)
	}
	source.shutdownFn(depsCtx)

	flushCtx, flushCancel := context.WithTimeout(context.Background(), cfg.HTTP.PublisherFlushTimeoutDuration())
	if p := closePublisher(flushCtx); p != nil {
		logger.Warn("processor: publisher shutdown failed", "err", p)
	}
	flushCancel()

	guardianCtx, guardianCancel := context.WithTimeout(context.Background(), cfg.HTTP.GuardianShutdownTimeoutDuration())
	defer guardianCancel()
	e.Send(guardianPID, actorruntime.Stop{})
	select {
	case <-e.Poison(guardianPID).Done():
	case <-guardianCtx.Done():
		logger.Warn("processor: guardian did not stop in time")
	}
	logger.Info("processor: shutdown complete")
}

func loadProcessorConfig(configPath, logLevelOverride, busTypeOverride, replayModeOverride, replayPathOverride string, shardIndex, shardCount int) config.AppConfig {
	cfg, prob := config.Load(configPath)
	if prob != nil {
		slog.Error("processor: config load failed", "err", prob)
		os.Exit(1)
	}
	if logLevelOverride != "" {
		cfg.Log.Level = logLevelOverride
	}
	if busTypeOverride != "" {
		cfg.Bus.Type = busTypeOverride
	}
	if strings.TrimSpace(replayModeOverride) != "" {
		cfg.Replay.Mode = strings.TrimSpace(replayModeOverride)
	}
	if strings.TrimSpace(replayPathOverride) != "" {
		cfg.MarketData.ReplayPath = strings.TrimSpace(replayPathOverride)
		cfg.Replay.Mode = "file"
	}
	applyShardOverrides(&cfg, shardIndex, shardCount)
	if prob = cfg.Validate(); prob != nil {
		slog.Error("processor: config validation failed", "err", prob)
		os.Exit(1)
	}
	return cfg
}

// shardAwareDurable appends a shard suffix to the durable consumer name when
// sharding is active (count > 1).  This ensures each shard instance creates its
// own NATS durable consumer (e.g. "processor-v1-s0", "processor-v1-s1").
func shardAwareDurable(base string, index, count int) string {
	if count <= 1 {
		return base
	}
	return fmt.Sprintf("%s-s%d", base, index)
}

// applyShardOverrides resolves shard index/count from flag > env > JSONC.
// It also propagates the top-level Shard config to JetStream shard fields.
func applyShardOverrides(cfg *config.AppConfig, flagIndex, flagCount int) {
	if flagIndex >= 0 {
		cfg.Shard.Index = flagIndex
	} else if v := os.Getenv("SHARD_INDEX"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Shard.Index = n
		}
	}
	if flagCount >= 0 {
		cfg.Shard.Count = flagCount
	} else if v := os.Getenv("SHARD_COUNT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Shard.Count = n
		}
	}
	// Propagate top-level shard to JetStream shard fields.
	cfg.JetStream.ShardGroupCount = cfg.Shard.Count
	cfg.JetStream.ShardGroupID = cfg.Shard.Index
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
