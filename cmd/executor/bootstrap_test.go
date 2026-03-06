package main

import (
	"log/slog"
	"testing"

	"github.com/market-raccoon/internal/shared/config"
)

func TestBuildIntentExecutor_DefaultBootstrapMode(t *testing.T) {
	cfg, prob := config.Load("")
	if prob != nil {
		t.Fatalf("config.Load defaults failed: %v", prob)
	}

	executor, err := buildIntentExecutor(cfg, slog.Default())
	if err != nil {
		t.Fatalf("buildIntentExecutor() error = %v", err)
	}
	info := executor.BoundaryInfo()
	if info.Adapter != "bootstrap.simulated" {
		t.Fatalf("adapter=%q want=bootstrap.simulated", info.Adapter)
	}
	if info.Mode != "bootstrap_simulated" {
		t.Fatalf("mode=%q want=bootstrap_simulated", info.Mode)
	}
}

func TestBuildIntentExecutor_RealSafeMode(t *testing.T) {
	cfg, prob := config.Load("")
	if prob != nil {
		t.Fatalf("config.Load defaults failed: %v", prob)
	}
	cfg.Execution.Mode = "real_adapter_safe"
	cfg.Execution.Adapter = "binance.spot"
	cfg.Execution.Real.Enabled = true
	cfg.Execution.AllowedVenues = []string{"binance"}
	cfg.Execution.AllowedSymbols = []string{"BTCUSDT"}

	executor, err := buildIntentExecutor(cfg, slog.Default())
	if err != nil {
		t.Fatalf("buildIntentExecutor() error = %v", err)
	}
	info := executor.BoundaryInfo()
	if info.Adapter != "binance.spot" {
		t.Fatalf("adapter=%q want=binance.spot", info.Adapter)
	}
	if info.Mode != "real_adapter_safe" {
		t.Fatalf("mode=%q want=real_adapter_safe", info.Mode)
	}
}

func TestBuildIntentExecutor_RealSafeModeRequiresBinanceAdapter(t *testing.T) {
	cfg, prob := config.Load("")
	if prob != nil {
		t.Fatalf("config.Load defaults failed: %v", prob)
	}
	cfg.Execution.Mode = "real_adapter_safe"
	cfg.Execution.Adapter = "bootstrap.simulated"
	cfg.Execution.Real.Enabled = true
	cfg.Execution.AllowedVenues = []string{"binance"}
	cfg.Execution.AllowedSymbols = []string{"BTCUSDT"}

	if _, err := buildIntentExecutor(cfg, slog.Default()); err == nil {
		t.Fatal("expected error when real mode does not use binance.spot adapter")
	}
}

func TestBuildIntentExecutor_RealSafeLifecycleMode(t *testing.T) {
	cfg, prob := config.Load("")
	if prob != nil {
		t.Fatalf("config.Load defaults failed: %v", prob)
	}
	cfg.Execution.Mode = "real_adapter_safe"
	cfg.Execution.Adapter = "binance.spot"
	cfg.Execution.Real.Enabled = true
	cfg.Execution.AllowedVenues = []string{"binance"}
	cfg.Execution.AllowedSymbols = []string{"BTCUSDT"}
	cfg.Execution.Real.Binance.TradeAPI.EndpointMode = "safe_order_lifecycle"
	cfg.Execution.Real.Binance.TradeAPI.ReconcileEnabled = true
	cfg.Execution.Real.Binance.TradeAPI.ReconcilePollInterval = "100ms"
	cfg.Execution.Real.Binance.TradeAPI.ReconcileMaxPolls = 3

	executor, err := buildIntentExecutor(cfg, slog.Default())
	if err != nil {
		t.Fatalf("buildIntentExecutor() error = %v", err)
	}
	info := executor.BoundaryInfo()
	if info.Adapter != "binance.spot" {
		t.Fatalf("adapter=%q want=binance.spot", info.Adapter)
	}
}

func TestEffectiveExecutorFilters_NarrowsToStrategyIntent(t *testing.T) {
	filters := effectiveExecutorFilters([]string{
		"strategy.>",
		"strategy.intent.>",
		"strategy.intent.v1.binance.BTCUSDT",
	})
	if len(filters) != 2 {
		t.Fatalf("filters=%v want 2 canonical entries", filters)
	}
	for _, filter := range filters {
		if filter != "strategy.intent.>" && filter != "strategy.intent.v1.binance.BTCUSDT" {
			t.Fatalf("unexpected filter retained: %q", filter)
		}
	}

	filters = effectiveExecutorFilters(nil)
	if len(filters) != 1 || filters[0] != "strategy.intent.>" {
		t.Fatalf("filters=%v want=[strategy.intent.>]", filters)
	}
}
