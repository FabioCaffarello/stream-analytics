// Package main is the market-raccoon strategist binary.
//
// The strategist service consumes evidence envelopes and emits composed
// `signal.composite` envelopes.
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
		bootstrap.ApplyShardOverrides(c, *shardIndex, *shardCount)
	})
	if prob != nil {
		fmt.Fprintf(os.Stderr, "strategist: config error: %v\n", prob)
		os.Exit(1)
	}

	if err := Run(context.Background(), cfg, *configPath); err != nil {
		fmt.Fprintf(os.Stderr, "strategist: %v\n", err)
		os.Exit(1)
	}
}
