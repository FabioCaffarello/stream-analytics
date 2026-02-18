// Package main is the market-raccoon consumer binary.
//
// The consumer ingests real-time market data via WebSocket connections and
// publishes normalised events to the event bus.
//
// Usage:
//
//	go run ./cmd/consumer [flags]
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
	recordPathOverride := flag.String("record", "", "optional fixture path to record published envelopes")
	replayPathOverride := flag.String("replay", "", "optional fixture path to replay envelopes offline")
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
		if strings.TrimSpace(*recordPathOverride) != "" {
			c.MarketData.RecordPath = strings.TrimSpace(*recordPathOverride)
		}
		if strings.TrimSpace(*replayPathOverride) != "" {
			c.MarketData.ReplayPath = strings.TrimSpace(*replayPathOverride)
		}
		bootstrap.ApplyShardOverrides(c, *shardIndex, *shardCount)
	})
	if prob != nil {
		fmt.Fprintf(os.Stderr, "consumer: config error: %v\n", prob)
		os.Exit(1)
	}

	if err := Run(context.Background(), cfg); err != nil {
		fmt.Fprintf(os.Stderr, "consumer: %v\n", err)
		os.Exit(1)
	}
}
