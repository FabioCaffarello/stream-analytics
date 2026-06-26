package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/anthdm/hollywood/actor"

	actorruntime "github.com/FabioCaffarello/stream-analytics/internal/actors/runtime"
	adapterjs "github.com/FabioCaffarello/stream-analytics/internal/adapters/jetstream"
	"github.com/FabioCaffarello/stream-analytics/internal/adapters/storage/clickhouse"
	"github.com/FabioCaffarello/stream-analytics/internal/contracts"
	aggdomain "github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/domain"
	insightsdomain "github.com/FabioCaffarello/stream-analytics/internal/core/insights/domain"
	httpserver "github.com/FabioCaffarello/stream-analytics/internal/interfaces/http"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/bootstrap"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/codec"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/config"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/envelope"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/metrics"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

// storeHeartbeatEveryN controls the heartbeat log interval.
const storeHeartbeatEveryN = 1000

// storeConsumedCount tracks total consumed messages for heartbeat logging.
var storeConsumedCount atomic.Uint64

// storeWriters bundles all cold storage writers for the store pipeline.
type storeWriters struct {
	batcher *clickhouse.BatchWriter
	candle  *clickhouse.ChCandleWriter
	stats   *clickhouse.ChStatsWriter
	heatmap *clickhouse.ChHeatmapWriter
}

// Run is the store composition root.  It wires ClickHouse, JetStream consumer,
// Guardian (observer mode), and HTTP server, then blocks until signal or error.
//
//nolint:gocyclo // composition root wires lifecycle branches by design.
func Run(ctx context.Context, cfg config.AppConfig) error {
	logger := bootstrap.BuildLogger(cfg.Log)
	slog.SetDefault(logger)
	logger.Info("store starting", "addr", cfg.HTTP.Addr, "bus", cfg.Bus.Type)

	// ── engine + guardian (observer mode) ─────────────────────────────────
	e, err := actorruntime.NewDefaultEngine()
	if err != nil {
		return err
	}

	guardianPID := actorruntime.SpawnGuardian(e, actorruntime.GuardianConfig{
		Logger:             logger,
		Factories:          map[actorruntime.Subsystem]actor.Producer{},
		ExpectedSubsystems: []actorruntime.Subsystem{},
	})
	logger.Info("store: guardian spawned", "pid", guardianPID.String())

	// ── payload codec registry (enables proto decode for candle/stats) ───
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		return fmt.Errorf("payload codec registry bootstrap: %v", p)
	}

	// ── schema contract validation (fail fast) ───────────────────────────
	if p := ValidateSchemaContract("sql/clickhouse/migrations"); p != nil {
		return fmt.Errorf("schema contract validation: %v", p)
	}
	logger.Info("store: schema contract validated")

	// ── ClickHouse writer + batcher ─────────────────────────────────────
	snapshotWriter := clickhouse.SnapshotWriter(clickhouse.NewWriter())
	var chPool *clickhouse.Pool
	if cfg.Storage.ClickHouse.Enabled {
		pool, p := clickhouse.NewPool(ctx, clickhouse.PoolConfig{
			Addrs:           cfg.Storage.ClickHouse.Addrs,
			Database:        cfg.Storage.ClickHouse.Database,
			Username:        cfg.Storage.ClickHouse.Username,
			Password:        cfg.Storage.ClickHouse.Password,
			MaxOpenConns:    cfg.Storage.ClickHouse.MaxOpenConns,
			MaxIdleConns:    cfg.Storage.ClickHouse.MaxIdleConns,
			ConnMaxLifetime: cfg.Storage.ClickHouse.ConnMaxLifetimeDuration(),
			DialTimeout:     cfg.Storage.ClickHouse.DialTimeoutDuration(),
			ReadTimeout:     cfg.Storage.ClickHouse.ReadTimeoutDuration(),
		})
		if p != nil {
			return fmt.Errorf("clickhouse pool init failed: %v", p)
		}
		chPool = pool
		defer func() {
			if p := chPool.Close(); p != nil {
				logger.Warn("store: clickhouse pool close failed", "err", p)
			}
		}()
		snapshotWriter = clickhouse.NewChWriter(chPool)
		logger.Info("store: using ClickHouse writer")
	} else {
		logger.Warn("store: using in-memory ClickHouse writer (storage.clickhouse.enabled=false)")
	}

	batcher := clickhouse.NewBatchWriter(snapshotWriter, cfg.Store.Batch)
	writers := &storeWriters{
		batcher: batcher,
		candle:  clickhouse.NewChCandleWriter(chPool),
		stats:   clickhouse.NewChStatsWriter(chPool),
		heatmap: clickhouse.NewChHeatmapWriter(chPool),
	}
	logger.Info("store: batcher configured",
		"max_rows", cfg.Store.Batch.MaxRows,
		"max_bytes", cfg.Store.Batch.MaxBytes,
		"flush_interval", cfg.Store.Batch.FlushInterval,
	)

	// ── readiness gate ───────────────────────────────────────────────────
	var storeReady atomic.Bool

	// ── JetStream consumer (when bus.type=jetstream) ─────────────────────
	var consumeErr <-chan *problem.Problem
	var shutdownConsumer func(context.Context)
	if strings.EqualFold(strings.TrimSpace(cfg.Bus.Type), "jetstream") {
		consumeErr, shutdownConsumer = initStoreConsumer(cfg, writers, logger)
	} else {
		logger.Info("store: bus.type is not jetstream, running in observer mode")
	}

	storeReady.Store(true)
	logger.Info("store: ready")

	// ── HTTP server ──────────────────────────────────────────────────────
	srv := httpserver.NewServer(
		e,
		guardianPID,
		cfg.HTTP.Addr,
		cfg.HTTP.EnablePprof,
		logger,
		httpserver.WithTLS(cfg.HTTP.TLSCert, cfg.HTTP.TLSKey),
	)
	srv.SetReadyGate(func() bool { return storeReady.Load() })

	serverErr := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	// ── wait for signal or error ─────────────────────────────────────────
	quit := bootstrap.SignalChannel()
	select {
	case sig := <-quit:
		logger.Info("store: received signal", "signal", sig)
	case err := <-serverErr:
		logger.Error("store: HTTP server error", "err", err)
	case p := <-consumeErr:
		if p != nil {
			logger.Error("store: consume loop failed", "err", p)
		}
	case <-ctx.Done():
		logger.Info("store: context canceled")
	}

	// ── shutdown ─────────────────────────────────────────────────────────
	logger.Info("store: shutting down")

	shutCtx, cancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeoutDuration())
	defer cancel()

	if err := srv.Shutdown(shutCtx); err != nil {
		logger.Warn("store: HTTP shutdown error", "err", err)
	}
	if shutdownConsumer != nil {
		shutdownConsumer(shutCtx)
	}
	if p := batcher.Close(shutCtx); p != nil {
		logger.Warn("store: batcher close error", "err", p)
	}

	actorruntime.ShutdownGuardian(shutCtx, e, guardianPID, logger)
	logger.Info("store: shutdown complete")
	return nil
}

// initStoreConsumer creates a JetStream consumer for the store pipeline and
// starts consuming in a background goroutine.
func initStoreConsumer(cfg config.AppConfig, writers *storeWriters, logger *slog.Logger) (<-chan *problem.Problem, func(context.Context)) {
	jsConsumer, p := adapterjs.NewConsumer(context.Background(), adapterjs.ConsumerConfig{
		URL:             cfg.JetStream.URL,
		StreamName:      cfg.JetStream.StreamName,
		DedupWindow:     cfg.JetStream.DedupWindowDuration(),
		MaxAge:          cfg.JetStream.MaxAgeDuration(),
		MaxBytes:        cfg.JetStream.MaxBytesInt64(),
		ConsumerDurable: cfg.JetStream.ConsumerDurable,
		FilterSubjects:  cfg.JetStream.FilterSubjects,
		AckWait:         cfg.JetStream.AckWaitDuration(),
		MaxAckPending:   cfg.JetStream.MaxAckPending,
		MaxDeliver:      cfg.JetStream.MaxDeliver,
		DeliverPolicy:   cfg.JetStream.DeliverPolicy,
	}, metrics.NewBusObserver())
	if p != nil {
		logger.Error("store: jetstream consumer init failed", "err", p)
		// Return a closed error channel so the caller's select doesn't block.
		errCh := make(chan *problem.Problem, 1)
		errCh <- p
		return errCh, func(context.Context) {}
	}

	consumeCtx, cancelConsume := context.WithCancel(context.Background())
	errCh := make(chan *problem.Problem, 1)

	go func() {
		errCh <- jsConsumer.Consume(consumeCtx, func(ctx context.Context, env envelope.Envelope) *problem.Problem {
			return handleStoreEnvelope(ctx, env, writers, logger)
		})
	}()

	logger.Info("store: subscribed to jetstream consumer",
		"url", cfg.JetStream.URL,
		"stream", cfg.JetStream.StreamName,
		"durable", cfg.JetStream.ConsumerDurable,
		"filters", cfg.JetStream.FilterSubjects,
	)

	return errCh, func(ctx context.Context) {
		cancelConsume()
		if p := jsConsumer.Close(ctx); p != nil {
			logger.Warn("store: jetstream consumer close failed", "err", p)
		}
	}
}

// handleStoreEnvelope routes an envelope to the appropriate write handler.
func handleStoreEnvelope(ctx context.Context, env envelope.Envelope, writers *storeWriters, logger *slog.Logger) *problem.Problem {
	eventKey := fmt.Sprintf("%s.v%d", env.Type, env.Version)

	n := storeConsumedCount.Add(1)
	if n%storeHeartbeatEveryN == 0 {
		logger.Info("store: heartbeat", "consumed", n)
	}

	switch {
	case env.Type == "aggregation.snapshot" && env.Version == 1:
		p := handleAggregationSnapshot(ctx, env, writers.batcher, logger)
		if p != nil {
			metrics.IncStoreConsumed("failed", "commit")
		} else {
			metrics.IncStoreConsumed("ok", "snapshot")
		}
		return p
	case env.Type == "aggregation.candle" && env.Version == 1:
		p := handleAggregationCandle(ctx, env, writers.candle, logger)
		if p != nil {
			metrics.IncStoreConsumed("failed", "candle")
		} else {
			metrics.IncStoreConsumed("ok", "candle")
		}
		return p
	case env.Type == "aggregation.stats" && env.Version == 1:
		p := handleAggregationStats(ctx, env, writers.stats, logger)
		if p != nil {
			metrics.IncStoreConsumed("failed", "stats")
		} else {
			metrics.IncStoreConsumed("ok", "stats")
		}
		return p
	case env.Type == insightsdomain.HeatmapSnapshotType && env.Version == insightsdomain.HeatmapSnapshotVersion:
		p := handleInsightsHeatmapSnapshot(ctx, env, writers.heatmap, logger)
		if p != nil {
			metrics.IncStoreConsumed("failed", "heatmap")
		} else {
			metrics.IncStoreConsumed("ok", "heatmap")
		}
		return p
	default:
		metrics.IncStoreConsumed("ok", "skipped")
		logger.Debug("store: skipping unhandled event", "type", eventKey,
			"venue", env.Venue, "instrument", env.Instrument)
		return nil
	}
}

// handleAggregationSnapshot decodes an aggregation snapshot envelope and
// commits to the ClickHouse writer.
func handleAggregationSnapshot(ctx context.Context, env envelope.Envelope, batcher *clickhouse.BatchWriter, logger *slog.Logger) *problem.Problem {
	var snap aggdomain.SnapshotProduced
	if err := json.Unmarshal(env.Payload, &snap); err != nil {
		metrics.IncStoreQuarantine("decode")
		return problem.Wrap(err, problem.ValidationFailed, "store: decode aggregation.snapshot payload failed")
	}

	if snap.BookID.Venue == "" {
		snap.BookID.Venue = env.Venue
	}
	if snap.BookID.Instrument == "" {
		snap.BookID.Instrument = env.Instrument
	}
	if snap.Seq == 0 {
		snap.Seq = env.Seq
	}

	started := time.Now()
	if p := batcher.Write(ctx, snap, env.IdempotencyKey); p != nil {
		metrics.IncStoreCommit("failed")
		metrics.ObserveStoreCommitLatency(time.Since(started))
		return p
	}

	metrics.IncStoreCommit("ok")
	metrics.ObserveStoreCommitLatency(time.Since(started))

	logger.Debug("store: snapshot committed",
		"venue", snap.BookID.Venue,
		"instrument", snap.BookID.Instrument,
		"seq", snap.Seq,
	)
	return nil
}

// handleAggregationCandle decodes a candle envelope via the codec registry
// and writes to ClickHouse cold storage.
func handleAggregationCandle(ctx context.Context, env envelope.Envelope, writer *clickhouse.ChCandleWriter, logger *slog.Logger) *problem.Problem {
	decoded, p := codec.DecodePayload(env.Type, env.Version, env.ContentType, env.Payload)
	if p != nil {
		metrics.IncStoreQuarantine("decode")
		return p
	}
	wireDTO, ok := decoded.(contracts.AggregationCandleClosedV1)
	if !ok {
		metrics.IncStoreQuarantine("decode")
		return problem.Newf(problem.ValidationFailed, "store: candle payload type mismatch: got %T", decoded)
	}

	domainEvt := aggdomain.CandleClosed{
		Candle: aggdomain.CandleV1{
			Venue:         wireDTO.Candle.Venue,
			Instrument:    wireDTO.Candle.Instrument,
			Timeframe:     wireDTO.Candle.Timeframe,
			WindowStartTs: wireDTO.Candle.WindowStartTs,
			WindowEndTs:   wireDTO.Candle.WindowEndTs,
			Open:          wireDTO.Candle.Open,
			High:          wireDTO.Candle.High,
			Low:           wireDTO.Candle.Low,
			ClosePrice:    wireDTO.Candle.ClosePrice,
			Volume:        wireDTO.Candle.Volume,
			BuyVolume:     wireDTO.Candle.BuyVolume,
			SellVolume:    wireDTO.Candle.SellVolume,
			TradeCount:    wireDTO.Candle.TradeCount,
			SeqFirst:      wireDTO.Candle.SeqFirst,
			SeqLast:       wireDTO.Candle.SeqLast,
			IsClosed:      wireDTO.Candle.IsClosed,
		},
	}

	started := time.Now()
	if p := writer.SaveCandle(ctx, domainEvt); p != nil {
		metrics.IncStoreCommit("failed")
		metrics.ObserveStoreCommitLatency(time.Since(started))
		return p
	}
	metrics.IncStoreCommit("ok")
	metrics.ObserveStoreCommitLatency(time.Since(started))

	logger.Debug("store: candle committed",
		"venue", wireDTO.Candle.Venue,
		"instrument", wireDTO.Candle.Instrument,
		"timeframe", wireDTO.Candle.Timeframe,
	)
	return nil
}

// handleAggregationStats decodes a stats envelope via the codec registry
// and writes to ClickHouse cold storage.
func handleAggregationStats(ctx context.Context, env envelope.Envelope, writer *clickhouse.ChStatsWriter, logger *slog.Logger) *problem.Problem {
	decoded, p := codec.DecodePayload(env.Type, env.Version, env.ContentType, env.Payload)
	if p != nil {
		metrics.IncStoreQuarantine("decode")
		return p
	}
	wireDTO, ok := decoded.(contracts.AggregationStatsWindowClosedV1)
	if !ok {
		metrics.IncStoreQuarantine("decode")
		return problem.Newf(problem.ValidationFailed, "store: stats payload type mismatch: got %T", decoded)
	}

	domainEvt := aggdomain.StatsWindowClosed{
		Stats: aggdomain.StatsWindowV1{
			Venue:           wireDTO.Stats.Venue,
			Instrument:      wireDTO.Stats.Instrument,
			Timeframe:       wireDTO.Stats.Timeframe,
			WindowStartTs:   wireDTO.Stats.WindowStartTs,
			WindowEndTs:     wireDTO.Stats.WindowEndTs,
			WindowMs:        wireDTO.Stats.WindowMs,
			TsIngestMs:      wireDTO.Stats.TsIngestMs,
			QualityFlags:    wireDTO.Stats.QualityFlags,
			LiqBuyVolume:    wireDTO.Stats.LiqBuyVolume,
			LiqSellVolume:   wireDTO.Stats.LiqSellVolume,
			LiqTotalVolume:  wireDTO.Stats.LiqTotalVolume,
			LiqCount:        wireDTO.Stats.LiqCount,
			MarkPriceOpen:   wireDTO.Stats.MarkPriceOpen,
			MarkPriceHigh:   wireDTO.Stats.MarkPriceHigh,
			MarkPriceLow:    wireDTO.Stats.MarkPriceLow,
			MarkPriceClose:  wireDTO.Stats.MarkPriceClose,
			FundingRateAvg:  wireDTO.Stats.FundingRateAvg,
			FundingRateLast: wireDTO.Stats.FundingRateLast,
			SeqFirst:        wireDTO.Stats.SeqFirst,
			SeqLast:         wireDTO.Stats.SeqLast,
			IsClosed:        wireDTO.Stats.IsClosed,
		},
	}

	started := time.Now()
	if p := writer.SaveStats(ctx, domainEvt); p != nil {
		metrics.IncStoreCommit("failed")
		metrics.ObserveStoreCommitLatency(time.Since(started))
		return p
	}
	metrics.IncStoreCommit("ok")
	metrics.ObserveStoreCommitLatency(time.Since(started))

	logger.Debug("store: stats committed",
		"venue", wireDTO.Stats.Venue,
		"instrument", wireDTO.Stats.Instrument,
		"timeframe", wireDTO.Stats.Timeframe,
	)
	return nil
}

// handleInsightsHeatmapSnapshot decodes an insights heatmap snapshot envelope
// and writes all artifact cells to ClickHouse cold storage.
func handleInsightsHeatmapSnapshot(ctx context.Context, env envelope.Envelope, writer *clickhouse.ChHeatmapWriter, logger *slog.Logger) *problem.Problem {
	decoded, p := codec.DecodePayload(env.Type, env.Version, env.ContentType, env.Payload)
	if p != nil {
		metrics.IncStoreQuarantine("decode")
		return p
	}
	artifact, ok := decoded.(insightsdomain.HeatmapArtifactV1)
	if !ok {
		metrics.IncStoreQuarantine("decode")
		return problem.Newf(problem.ValidationFailed, "store: heatmap payload type mismatch: got %T", decoded)
	}

	started := time.Now()
	if p := writer.Save(ctx, artifact, env.IdempotencyKey); p != nil {
		metrics.IncStoreCommit("failed")
		metrics.ObserveStoreCommitLatency(time.Since(started))
		return p
	}
	metrics.IncStoreCommit("ok")
	metrics.ObserveStoreCommitLatency(time.Since(started))

	logger.Debug("store: heatmap snapshot committed",
		"venue", artifact.Venue,
		"instrument", artifact.Instrument,
		"timeframe", artifact.Timeframe,
		"cells", len(artifact.Cells),
	)
	return nil
}
