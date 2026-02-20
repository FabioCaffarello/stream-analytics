// Package main is the market-raccoon server binary.
//
// The server exposes runtime observability and control over HTTP.  It does NOT
// ingest market data or run any business logic — it only supervises the actor
// engine and proxies requests to the Guardian.
//
// Usage:
//
//	go run ./cmd/server [flags]
//	  -config     string   path to JSONC config file (default "config.jsonc")
//	  -addr       string   HTTP listen address override (default from config)
//	  -log-level  string   log level override: debug|info|warn|error
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
	flag.Parse()

	cfg, prob := bootstrap.LoadAndValidate(*configPath, func(c *config.AppConfig) {
		if *addrOverride != "" {
			c.HTTP.Addr = *addrOverride
		}
		if *logLevelOverride != "" {
			c.Log.Level = *logLevelOverride
		}
	})
	if prob != nil {
		fmt.Fprintf(os.Stderr, "server: config error: %v\n", prob)
		os.Exit(1)
	}

	if err := Run(context.Background(), cfg, *configPath); err != nil {
		fmt.Fprintf(os.Stderr, "server: %v\n", err)
		os.Exit(1)
	}
}
