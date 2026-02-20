package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/market-raccoon/internal/adapters/exchange/binance"
	"github.com/market-raccoon/internal/adapters/exchange/bybit"
	"github.com/market-raccoon/internal/adapters/exchange/coinbase"
	"github.com/market-raccoon/internal/adapters/exchange/hyperliquid"
	"github.com/market-raccoon/internal/adapters/exchange/kraken"
	"github.com/market-raccoon/internal/adapters/exchange/krakenf"
	"github.com/market-raccoon/internal/adapters/storage/clickhouse"
	"github.com/market-raccoon/internal/core/aggregation/app"
	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	"github.com/market-raccoon/internal/shared/config"
	"github.com/market-raccoon/internal/shared/naming"
)

type runConfig struct {
	Mode       string
	Exchange   string
	Symbol     string
	From       string
	To         string
	MarketType string
	OutputDir  string
	Fixture    string
	Timeframe  string
}

var (
	runDownloadAggTrades         = binance.DownloadAggTrades
	runDownloadBybitTrades       = bybit.DownloadTrades
	runDownloadCoinbaseTrades    = coinbase.DownloadTrades
	runDownloadHyperLiquidTrades = hyperliquid.DownloadTrades
	runDownloadKrakenTrades      = kraken.DownloadTrades
	runDownloadKrakenFTrades     = krakenf.DownloadTrades
	runNewClickHousePool         = clickhouse.NewPool
	runDetectCandleGaps          = app.DetectCandleGaps
)

//nolint:gocyclo // CLI mode branching is explicit to keep operational flow easy to audit.
func Run(ctx context.Context, appCfg config.AppConfig, cfg runConfig) (int, error) {
	mode := strings.ToLower(strings.TrimSpace(cfg.Mode))
	if mode == "" {
		mode = "download"
	}
	exchange := strings.ToLower(strings.TrimSpace(cfg.Exchange))
	if exchange == "" {
		exchange = "binance"
	}

	switch mode {
	case "download":
		from, err := parseDateRequired(cfg.From, "from")
		if err != nil {
			return 1, err
		}
		to, err := parseDateRequired(cfg.To, "to")
		if err != nil {
			return 1, err
		}

		var outputPath string
		var datesDownloaded, datesSkipped int
		var tradesParsed int64

		switch exchange {
		case "binance":
			result, p := runDownloadAggTrades(ctx, binance.BackfillConfig{
				Symbol:     cfg.Symbol,
				From:       from,
				To:         to,
				OutputDir:  cfg.OutputDir,
				MarketType: cfg.MarketType,
			})
			if p != nil {
				return 1, fmt.Errorf("backfill download failed: %v", p)
			}
			outputPath = result.OutputPath
			datesDownloaded = result.DatesDownloaded
			datesSkipped = result.DatesSkipped
			tradesParsed = result.TradesParsed
		case "bybit":
			result, p := runDownloadBybitTrades(ctx, bybit.BackfillConfig{
				Symbol:     cfg.Symbol,
				From:       from,
				To:         to,
				OutputDir:  cfg.OutputDir,
				MarketType: cfg.MarketType,
			})
			if p != nil {
				return 1, fmt.Errorf("backfill download failed: %v", p)
			}
			outputPath = result.OutputPath
			datesDownloaded = result.DatesDownloaded
			datesSkipped = result.DatesSkipped
			tradesParsed = result.TradesParsed
		case "coinbase":
			result, p := runDownloadCoinbaseTrades(ctx, coinbase.BackfillConfig{
				Symbol:     cfg.Symbol,
				From:       from,
				To:         to,
				OutputDir:  cfg.OutputDir,
				MarketType: cfg.MarketType,
			})
			if p != nil {
				return 1, fmt.Errorf("backfill download failed: %v", p)
			}
			outputPath = result.OutputPath
			datesDownloaded = result.DatesDownloaded
			datesSkipped = result.DatesSkipped
			tradesParsed = result.TradesParsed
		case "hyperliquid":
			result, p := runDownloadHyperLiquidTrades(ctx, hyperliquid.BackfillConfig{
				Symbol:     cfg.Symbol,
				From:       from,
				To:         to,
				OutputDir:  cfg.OutputDir,
				MarketType: cfg.MarketType,
			})
			if p != nil {
				return 1, fmt.Errorf("backfill download failed: %v", p)
			}
			outputPath = result.OutputPath
			datesDownloaded = result.DatesDownloaded
			datesSkipped = result.DatesSkipped
			tradesParsed = result.TradesParsed
		case "kraken":
			result, p := runDownloadKrakenTrades(ctx, kraken.BackfillConfig{
				Symbol:     cfg.Symbol,
				From:       from,
				To:         to,
				OutputDir:  cfg.OutputDir,
				MarketType: cfg.MarketType,
			})
			if p != nil {
				return 1, fmt.Errorf("backfill download failed: %v", p)
			}
			outputPath = result.OutputPath
			datesDownloaded = result.DatesDownloaded
			datesSkipped = result.DatesSkipped
			tradesParsed = result.TradesParsed
		case "krakenf":
			result, p := runDownloadKrakenFTrades(ctx, krakenf.BackfillConfig{
				Symbol:     cfg.Symbol,
				From:       from,
				To:         to,
				OutputDir:  cfg.OutputDir,
				MarketType: cfg.MarketType,
			})
			if p != nil {
				return 1, fmt.Errorf("backfill download failed: %v", p)
			}
			outputPath = result.OutputPath
			datesDownloaded = result.DatesDownloaded
			datesSkipped = result.DatesSkipped
			tradesParsed = result.TradesParsed
		default:
			return 1, fmt.Errorf("unsupported exchange %q (allowed: binance|bybit|coinbase|hyperliquid|kraken|krakenf)", exchange)
		}

		if strings.TrimSpace(cfg.Fixture) != "" {
			target := strings.TrimSpace(cfg.Fixture)
			if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
				return 1, fmt.Errorf("create fixture directory: %w", err)
			}
			if outputPath != target {
				if err := os.Rename(outputPath, target); err != nil {
					return 1, fmt.Errorf("move fixture to target path: %w", err)
				}
				outputPath = target
			}
		}

		fmt.Printf("download complete: downloaded=%d skipped=%d trades=%d fixture=%s\n",
			datesDownloaded,
			datesSkipped,
			tradesParsed,
			outputPath,
		)
		return 0, nil

	case "gaps":
		if strings.TrimSpace(cfg.Timeframe) == "" {
			return 1, fmt.Errorf("timeframe is required in gaps mode")
		}
		expectedStepMs, p := aggdomain.TimeframeToMs(cfg.Timeframe)
		if p != nil {
			return 1, fmt.Errorf("invalid timeframe: %v", p)
		}
		if !appCfg.Storage.ClickHouse.Enabled {
			return 1, fmt.Errorf("storage.clickhouse.enabled must be true for gaps mode")
		}

		pool, p := runNewClickHousePool(ctx, clickhouse.PoolConfig{
			Addrs:           appCfg.Storage.ClickHouse.Addrs,
			Database:        appCfg.Storage.ClickHouse.Database,
			Username:        appCfg.Storage.ClickHouse.Username,
			Password:        appCfg.Storage.ClickHouse.Password,
			MaxOpenConns:    appCfg.Storage.ClickHouse.MaxOpenConns,
			MaxIdleConns:    appCfg.Storage.ClickHouse.MaxIdleConns,
			ConnMaxLifetime: appCfg.Storage.ClickHouse.ConnMaxLifetimeDuration(),
			DialTimeout:     appCfg.Storage.ClickHouse.DialTimeoutDuration(),
			ReadTimeout:     appCfg.Storage.ClickHouse.ReadTimeoutDuration(),
		})
		if p != nil {
			return 1, fmt.Errorf("clickhouse pool init failed: %v", p)
		}
		defer func() {
			_ = pool.Close()
		}()

		fromMs, err := parseOptionalDateMs(cfg.From)
		if err != nil {
			return 1, err
		}
		toMs, err := parseOptionalDateMs(cfg.To)
		if err != nil {
			return 1, err
		}

		venue := naming.CanonicalVenue(cfg.Exchange)
		if venue == "" {
			venue = naming.CanonicalVenue(binance.VenueBinance)
		}

		reports, p := runDetectCandleGaps(ctx, clickhouse.NewChCandleReader(pool), app.GapDetectorConfig{
			Venue:          venue,
			Instrument:     naming.CanonicalInstrument(cfg.Symbol),
			Timeframe:      strings.TrimSpace(cfg.Timeframe),
			FromMs:         fromMs,
			ToMs:           toMs,
			ExpectedStepMs: expectedStepMs,
		})
		if p != nil {
			return 1, fmt.Errorf("detect candle gaps failed: %v", p)
		}

		totalMissing := 0
		for i, gap := range reports {
			totalMissing += gap.Missing
			fmt.Printf(
				"gap %d: %s -> %s (%d missing %s candles)\n",
				i+1,
				time.UnixMilli(gap.GapStartMs).UTC().Format(time.RFC3339),
				time.UnixMilli(gap.GapEndMs).UTC().Format(time.RFC3339),
				gap.Missing,
				gap.Timeframe,
			)
		}
		fmt.Printf("total: %d gaps, %d missing candles\n", len(reports), totalMissing)
		if len(reports) > 0 {
			return 1, nil
		}
		return 0, nil

	default:
		return 1, fmt.Errorf("unsupported mode %q (allowed: download|gaps)", mode)
	}
}

func parseDateRequired(raw, field string) (time.Time, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return time.Time{}, fmt.Errorf("%s is required (YYYY-MM-DD)", field)
	}
	ts, err := time.ParseInLocation("2006-01-02", v, time.UTC)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid %s date %q: %w", field, raw, err)
	}
	return ts, nil
}

func parseOptionalDateMs(raw string) (int64, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return 0, nil
	}
	ts, err := time.ParseInLocation("2006-01-02", v, time.UTC)
	if err != nil {
		return 0, fmt.Errorf("invalid date %q: %w", raw, err)
	}
	return ts.UnixMilli(), nil
}
