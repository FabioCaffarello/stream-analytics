package main

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/market-raccoon/internal/adapters/bus"
	"github.com/market-raccoon/internal/contracts"
	mdapp "github.com/market-raccoon/internal/core/marketdata/app"
	"github.com/market-raccoon/internal/core/marketdata/ports"
	"github.com/market-raccoon/internal/shared/clock"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/config"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/problem"
	"github.com/market-raccoon/internal/shared/replay"
)

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

	player, p := replay.NewPlayer(replayPath, fakeClock, contracts.BootstrapPayloadCodecRegistry)
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
