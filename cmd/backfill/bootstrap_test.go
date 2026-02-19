package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/market-raccoon/internal/adapters/exchange/binance"
	"github.com/market-raccoon/internal/adapters/storage/clickhouse"
	aggapp "github.com/market-raccoon/internal/core/aggregation/app"
	aggports "github.com/market-raccoon/internal/core/aggregation/ports"
	"github.com/market-raccoon/internal/shared/config"
	"github.com/market-raccoon/internal/shared/problem"
)

func TestBackfill_ProducesValidFixture(t *testing.T) {
	appCfg, prob := config.Load("")
	if prob != nil {
		t.Fatalf("config.Load defaults: %v", prob)
	}

	tmp := t.TempDir()
	downloaded := filepath.Join(tmp, "fixture.jsonl")
	if err := os.WriteFile(downloaded, []byte("{\"fixture\":\"ok\"}\n"), 0o600); err != nil {
		t.Fatalf("WriteFile fixture: %v", err)
	}
	target := filepath.Join(tmp, "moved", "fixture.jsonl")

	restore := patchBackfillDeps(
		func(_ context.Context, cfg binance.BackfillConfig) (binance.BackfillResult, *problem.Problem) {
			if cfg.Symbol != "BTCUSDT" {
				t.Fatalf("symbol=%q want=BTCUSDT", cfg.Symbol)
			}
			return binance.BackfillResult{
				DatesDownloaded: 1,
				TradesParsed:    2,
				OutputPath:      downloaded,
			}, nil
		},
		nil,
		nil,
	)
	defer restore()

	exitCode, err := Run(context.Background(), appCfg, runConfig{
		Mode:       "download",
		Exchange:   "binance",
		Symbol:     "BTCUSDT",
		From:       "2025-01-01",
		To:         "2025-01-01",
		MarketType: "USD_M_FUTURES",
		OutputDir:  tmp,
		Fixture:    target,
	})
	if err != nil {
		t.Fatalf("Run(download): %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("exitCode=%d want=0", exitCode)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("fixture target not found: %v", err)
	}
}

func TestGapDetector_ReturnsGaps(t *testing.T) {
	appCfg, prob := config.Load("")
	if prob != nil {
		t.Fatalf("config.Load defaults: %v", prob)
	}
	appCfg.Storage.ClickHouse.Enabled = true
	// Ensure duration parsing in Run() never panics in tests.
	appCfg.Storage.ClickHouse.ConnMaxLifetime = "30m"
	appCfg.Storage.ClickHouse.DialTimeout = "5s"
	appCfg.Storage.ClickHouse.ReadTimeout = "30s"

	restore := patchBackfillDeps(
		nil,
		func(_ context.Context, _ clickhouse.PoolConfig) (*clickhouse.Pool, *problem.Problem) {
			return nil, nil
		},
		func(_ context.Context, _ aggports.CandleReader, cfg aggapp.GapDetectorConfig) ([]aggapp.GapReport, *problem.Problem) {
			if cfg.Timeframe != "1m" {
				t.Fatalf("timeframe=%q want=1m", cfg.Timeframe)
			}
			return []aggapp.GapReport{{
				Venue:      cfg.Venue,
				Instrument: cfg.Instrument,
				Timeframe:  cfg.Timeframe,
				GapStartMs: time.Date(2025, 1, 1, 0, 1, 0, 0, time.UTC).UnixMilli(),
				GapEndMs:   time.Date(2025, 1, 1, 0, 3, 0, 0, time.UTC).UnixMilli(),
				Missing:    2,
			}}, nil
		},
	)
	defer restore()

	exitCode, err := Run(context.Background(), appCfg, runConfig{
		Mode:      "gaps",
		Exchange:  "binance",
		Symbol:    "BTCUSDT",
		Timeframe: "1m",
		From:      "2025-01-01",
		To:        "2025-01-02",
	})
	if err != nil {
		t.Fatalf("Run(gaps): %v", err)
	}
	if exitCode != 1 {
		t.Fatalf("exitCode=%d want=1 when gaps exist", exitCode)
	}
}

func patchBackfillDeps(
	download func(ctx context.Context, cfg binance.BackfillConfig) (binance.BackfillResult, *problem.Problem),
	newPool func(ctx context.Context, cfg clickhouse.PoolConfig) (*clickhouse.Pool, *problem.Problem),
	detect func(ctx context.Context, reader aggports.CandleReader, cfg aggapp.GapDetectorConfig) ([]aggapp.GapReport, *problem.Problem),
) func() {
	prevDownload := runDownloadAggTrades
	prevNewPool := runNewClickHousePool
	prevDetect := runDetectCandleGaps
	if download != nil {
		runDownloadAggTrades = download
	}
	if newPool != nil {
		runNewClickHousePool = newPool
	}
	if detect != nil {
		runDetectCandleGaps = detect
	}
	return func() {
		runDownloadAggTrades = prevDownload
		runNewClickHousePool = prevNewPool
		runDetectCandleGaps = prevDetect
	}
}
