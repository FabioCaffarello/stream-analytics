// Package main is the market-raccoon store binary.
//
// The store is the cold-path (ClickHouse) authority for ack-on-commit.
//
// Usage:
//
//	go run ./cmd/store [flags]
//	  -config     string   path to JSONC config file (default "config.jsonc")
//	  -addr       string   HTTP listen address override (default from config)
//	  -log-level  string   log level override: debug|info|warn|error
//	  -bus        string   bus type override: inmemory|jetstream
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
	busOverride := flag.String("bus", "", "bus type override: inmemory|jetstream")
	flag.Parse()

	cfg, prob := bootstrap.LoadAndValidate(*configPath, func(c *config.AppConfig) {
		if *addrOverride != "" {
			c.HTTP.Addr = *addrOverride
		}
		if *logLevelOverride != "" {
			c.Log.Level = *logLevelOverride
		}
		if *busOverride != "" {
			c.Bus.Type = *busOverride
		}
	})
	if prob != nil {
		fmt.Fprintf(os.Stderr, "store: config error: %v\n", prob)
		os.Exit(1)
	}

	if err := Run(context.Background(), cfg); err != nil {
		fmt.Fprintf(os.Stderr, "store: %v\n", err)
		os.Exit(1)
	}
}
