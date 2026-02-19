package bootstrap

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/market-raccoon/internal/shared/config"
)

var shardOrdinalSuffixPattern = regexp.MustCompile(`-(\d+)$`)
var containerIDPattern = regexp.MustCompile(`^[a-f0-9]{12,64}$`)
var defaultHostnameProvider = os.Hostname
var hostnameProvider = defaultHostnameProvider
var defaultComposeContainerNumberProvider = composeContainerNumberFromDockerAPI
var composeContainerNumberProvider = defaultComposeContainerNumberProvider

const (
	runtimeEnvVar     = "MR_ENV"
	runtimeEnvDev     = "dev"
	runtimeEnvProd    = "prod"
	defaultRuntimeEnv = runtimeEnvDev
)

// ApplyShardOverrides resolves shard index/count from flag > env > JSONC and
// propagates the result to JetStream shard fields.  Flag values of -1 mean
// "not set" and fall through to environment variables.
//
//nolint:gocyclo // configuration precedence + dev/prod fallback rules.
func ApplyShardOverrides(cfg *config.AppConfig, flagIndex, flagCount int) {
	if flagCount >= 0 {
		cfg.Shard.Count = flagCount
	} else if v := os.Getenv("SHARD_COUNT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Shard.Count = n
		}
	}

	mode := runtimeMode()
	indexSource := "default"
	resolvedHost := ""
	indexSet := false
	if flagIndex >= 0 {
		cfg.Shard.Index = flagIndex
		indexSource = "env"
		indexSet = true
	} else if v := os.Getenv("SHARD_INDEX"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Shard.Index = n
			indexSource = "env"
			indexSet = true
		}
	}

	if !indexSet && cfg.Shard.Count <= 1 {
		cfg.Shard.Index = 0
		indexSource = "default"
		indexSet = true
	}

	if !indexSet && cfg.Shard.Count > 1 {
		if mode == runtimeEnvProd {
			cfg.Shard.Index = -1
			slog.Error("shard index resolution failed",
				"shard_index_source", "default",
				"hostname", resolvedHost,
				"shard_count", cfg.Shard.Count,
				"mode", mode,
				"reason", "prod requires explicit SHARD_INDEX when SHARD_COUNT>1",
			)
		} else if host, err := hostnameProvider(); err == nil {
			resolvedHost = host
			if n, ok := deriveShardIndexFromHostname(host); ok {
				cfg.Shard.Index = n
				indexSource = "hostname"
			} else if n, ok := deriveShardIndexFromComposeMetadata(host); ok {
				cfg.Shard.Index = n
				indexSource = "hostname"
			} else {
				cfg.Shard.Index = -1
				slog.Error("shard index resolution failed",
					"shard_index_source", "hostname",
					"hostname", resolvedHost,
					"shard_count", cfg.Shard.Count,
					"mode", mode,
					"reason", "unable to derive shard index from hostname",
				)
			}
		} else {
			cfg.Shard.Index = -1
			slog.Error("shard index resolution failed",
				"shard_index_source", "hostname",
				"hostname", resolvedHost,
				"shard_count", cfg.Shard.Count,
				"mode", mode,
				"reason", "unable to read hostname",
			)
		}
	}

	if v := os.Getenv("SHARD_MAX_LAG"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Shard.MaxLag = n
		}
	}
	// Propagate top-level shard to JetStream shard fields.
	cfg.JetStream.ShardGroupCount = cfg.Shard.Count
	cfg.JetStream.ShardGroupID = cfg.Shard.Index

	slog.Info("shard resolution applied",
		"shard_index_source", indexSource,
		"hostname", resolvedHost,
		"shard_index", cfg.Shard.Index,
		"shard_count", cfg.Shard.Count,
		"mode", mode,
	)
}

func runtimeMode() string {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(runtimeEnvVar)))
	switch raw {
	case runtimeEnvProd:
		return runtimeEnvProd
	case runtimeEnvDev:
		return runtimeEnvDev
	default:
		return defaultRuntimeEnv
	}
}

func deriveShardIndexFromHostname(host string) (int, bool) {
	host = strings.TrimSpace(host)
	if host == "" {
		return 0, false
	}
	matches := shardOrdinalSuffixPattern.FindStringSubmatch(host)
	if len(matches) != 2 {
		return 0, false
	}
	ordinal, err := strconv.Atoi(matches[1])
	if err != nil || ordinal < 0 {
		return 0, false
	}
	if ordinal == 0 {
		return 0, true
	}
	return ordinal - 1, true
}

func deriveShardIndexFromComposeMetadata(host string) (int, bool) {
	host = strings.TrimSpace(host)
	if !containerIDPattern.MatchString(host) {
		return 0, false
	}
	ordinal, ok := composeContainerNumberProvider(host)
	if !ok || ordinal < 1 {
		return 0, false
	}
	return ordinal - 1, true
}

func composeContainerNumberFromDockerAPI(containerID string) (int, bool) {
	dialer := &net.Dialer{Timeout: 500 * time.Millisecond}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", "/var/run/docker.sock")
		},
	}
	client := &http.Client{Timeout: time.Second, Transport: transport}
	req, err := http.NewRequest(http.MethodGet, "http://docker/containers/"+containerID+"/json", http.NoBody)
	if err != nil {
		return 0, false
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, false
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		return 0, false
	}
	var payload struct {
		Config struct {
			Labels map[string]string `json:"Labels"`
		} `json:"Config"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return 0, false
	}
	raw := strings.TrimSpace(payload.Config.Labels["com.docker.compose.container-number"])
	if raw == "" {
		return 0, false
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}
	return n, true
}
