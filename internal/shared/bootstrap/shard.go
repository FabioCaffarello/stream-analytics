package bootstrap

import (
	"os"
	"strconv"

	"github.com/market-raccoon/internal/shared/config"
)

// ApplyShardOverrides resolves shard index/count from flag > env > JSONC and
// propagates the result to JetStream shard fields.  Flag values of -1 mean
// "not set" and fall through to environment variables.
func ApplyShardOverrides(cfg *config.AppConfig, flagIndex, flagCount int) {
	if flagIndex >= 0 {
		cfg.Shard.Index = flagIndex
	} else if v := os.Getenv("SHARD_INDEX"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Shard.Index = n
		}
	}
	if flagCount >= 0 {
		cfg.Shard.Count = flagCount
	} else if v := os.Getenv("SHARD_COUNT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Shard.Count = n
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
}
