// Package main is the market-raccoon executor binary.
//
// The executor service consumes `strategy.intent` and emits canonical
// `execution.event` lifecycle transitions. Stage 7 keeps bootstrap mode as
// default while enabling an opt-in real adapter in restricted safe mode behind
// the execution adapter boundary. Stage 8 expands lifecycle/reconciliation
// under the same safe boundary controls.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/market-raccoon/internal/shared/bootstrap"
	"github.com/market-raccoon/internal/shared/config"
)

func main() {
	configPath := flag.String("config", "config.jsonc", "path to JSONC config file")
	addrOverride := flag.String("addr", "", "HTTP listen address (overrides config)")
	logLevelOverride := flag.String("log-level", "", "log level override: debug|info|warn|error")
	busTypeOverride := flag.String("bus", "", "bus adapter override: inmemory|jetstream")
	executionModeOverride := flag.String("execution-mode", "", "execution mode override: bootstrap_simulated|real_adapter_safe")
	shardIndex := flag.Int("shard-index", -1, "shard index override (0-based); env: SHARD_INDEX")
	shardCount := flag.Int("shard-count", -1, "total shard count override; env: SHARD_COUNT")
	flag.Parse()

	cfg, prob := bootstrap.LoadAndValidate(*configPath, func(c *config.AppConfig) {
		if *addrOverride != "" {
			c.HTTP.Addr = *addrOverride
		}
		if *logLevelOverride != "" {
			c.Log.Level = *logLevelOverride
		}
		if *busTypeOverride != "" {
			c.Bus.Type = *busTypeOverride
		}
		if *executionModeOverride != "" {
			c.Execution.Mode = *executionModeOverride
		}
		bootstrap.ApplyShardOverrides(c, *shardIndex, *shardCount)
	})
	if prob != nil {
		fmt.Fprintf(os.Stderr, "executor: config error: %v\n", prob)
		os.Exit(1)
	}

	if err := Run(context.Background(), cfg, *configPath); err != nil {
		fmt.Fprintf(os.Stderr, "executor: %v\n", err)
		os.Exit(1)
	}
}
