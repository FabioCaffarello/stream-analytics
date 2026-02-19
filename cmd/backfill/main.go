package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/market-raccoon/internal/shared/bootstrap"
	"github.com/market-raccoon/internal/shared/config"
)

func main() {
	configPath := flag.String("config", "config.jsonc", "path to JSONC config file")
	mode := flag.String("mode", "", "download|gaps")
	exchange := flag.String("exchange", "", "exchange: binance|bybit")
	symbol := flag.String("symbol", "", "trading symbol")
	fromDate := flag.String("from", "", "start date YYYY-MM-DD")
	toDate := flag.String("to", "", "end date YYYY-MM-DD")
	marketType := flag.String("market-type", "", "SPOT or USD_M_FUTURES")
	outputDir := flag.String("output-dir", "", "directory for downloaded files and fixtures")
	fixtureFile := flag.String("fixture", "", "output JSONL fixture path")
	timeframe := flag.String("timeframe", "", "candle timeframe for gaps mode (e.g. 1m)")
	flag.Parse()

	appCfg, prob := bootstrap.LoadAndValidate(*configPath)
	if prob != nil {
		fmt.Fprintf(os.Stderr, "backfill: config error: %v\n", prob)
		os.Exit(1)
	}

	runCfg := runConfigFromAppConfig(appCfg)
	if v := strings.TrimSpace(*mode); v != "" {
		runCfg.Mode = v
	}
	if v := strings.TrimSpace(*exchange); v != "" {
		runCfg.Exchange = v
	}
	if v := strings.TrimSpace(*symbol); v != "" {
		runCfg.Symbol = v
	}
	if v := strings.TrimSpace(*fromDate); v != "" {
		runCfg.From = v
	}
	if v := strings.TrimSpace(*toDate); v != "" {
		runCfg.To = v
	}
	if v := strings.TrimSpace(*marketType); v != "" {
		runCfg.MarketType = v
	}
	if v := strings.TrimSpace(*outputDir); v != "" {
		runCfg.OutputDir = v
	}
	if v := strings.TrimSpace(*fixtureFile); v != "" {
		runCfg.Fixture = v
	}
	if v := strings.TrimSpace(*timeframe); v != "" {
		runCfg.Timeframe = v
	}

	exitCode, err := Run(context.Background(), appCfg, runCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "backfill: %v\n", err)
		os.Exit(1)
	}
	if exitCode != 0 {
		os.Exit(exitCode)
	}
}

func runConfigFromAppConfig(cfg config.AppConfig) runConfig {
	return runConfig{
		Mode:       cfg.Backfill.Mode,
		Exchange:   cfg.Backfill.Exchange,
		Symbol:     cfg.Backfill.Symbol,
		From:       cfg.Backfill.FromDate,
		To:         cfg.Backfill.ToDate,
		MarketType: cfg.Backfill.MarketType,
		OutputDir:  cfg.Backfill.OutputDir,
		Timeframe:  cfg.Backfill.Timeframe,
	}
}
