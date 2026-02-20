// Package main is the market-raccoon processor binary.
//
// The processor subscribes to an event bus, reads normalised event envelopes,
// and applies them to the core aggregation use cases (order book, etc.).
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
	"os"
	"strings"

	"github.com/market-raccoon/internal/shared/bootstrap"
	"github.com/market-raccoon/internal/shared/config"
)

func main() {
	configPath := flag.String("config", "config.jsonc", "path to JSONC config file")
	logLevelOverride := flag.String("log-level", "", "log level override: debug|info|warn|error")
	busTypeOverride := flag.String("bus", "", "bus adapter override: inmemory|jetstream")
	replayModeOverride := flag.String("replay-mode", "", "replay mode override: off|file|jetstream")
	replayPathOverride := flag.String("replay-path", "", "optional fixture path to replay envelopes")
	shardIndex := flag.Int("shard-index", -1, "shard index override (0-based); env: SHARD_INDEX")
	shardCount := flag.Int("shard-count", -1, "total shard count override; env: SHARD_COUNT")
	flag.Parse()

	cfg, prob := bootstrap.LoadAndValidate(*configPath, func(c *config.AppConfig) {
		if *logLevelOverride != "" {
			c.Log.Level = *logLevelOverride
		}
		if *busTypeOverride != "" {
			c.Bus.Type = *busTypeOverride
		}
		if strings.TrimSpace(*replayModeOverride) != "" {
			c.Replay.Mode = strings.TrimSpace(*replayModeOverride)
		}
		if strings.TrimSpace(*replayPathOverride) != "" {
			c.MarketData.ReplayPath = strings.TrimSpace(*replayPathOverride)
			c.Replay.Mode = "file"
		}
		bootstrap.ApplyShardOverrides(c, *shardIndex, *shardCount)
	})
	if prob != nil {
		fmt.Fprintf(os.Stderr, "processor: config error: %v\n", prob)
		os.Exit(1)
	}

	if err := Run(context.Background(), cfg, *configPath); err != nil {
		fmt.Fprintf(os.Stderr, "processor: %v\n", err)
		os.Exit(1)
	}
}
