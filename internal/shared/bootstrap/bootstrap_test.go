package bootstrap

import (
	"context"
	"log/slog"
	"testing"

	"github.com/market-raccoon/internal/shared/config"
)

func TestBuildLogger_TextFormat(t *testing.T) {
	logger := BuildLogger(config.LogConfig{Level: "debug", Format: "text"})
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}
	if !logger.Enabled(context.TODO(), slog.LevelDebug) {
		t.Error("expected debug level to be enabled")
	}
}

func TestBuildLogger_JSONFormat(t *testing.T) {
	logger := BuildLogger(config.LogConfig{Level: "warn", Format: "json"})
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}
	if logger.Enabled(context.TODO(), slog.LevelInfo) {
		t.Error("expected info level to be disabled at warn level")
	}
}

func TestBuildLogger_InvalidLevel_DefaultsToInfo(t *testing.T) {
	logger := BuildLogger(config.LogConfig{Level: "invalid", Format: "text"})
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}
	if !logger.Enabled(context.TODO(), slog.LevelInfo) {
		t.Error("expected info level to be enabled as default fallback")
	}
	if logger.Enabled(context.TODO(), slog.LevelDebug) {
		t.Error("expected debug level to be disabled at default info level")
	}
}

func TestLoadAndValidate_EmptyPath_ReturnsDefaults(t *testing.T) {
	cfg, prob := LoadAndValidate("")
	if prob != nil {
		t.Fatalf("unexpected problem: %v", prob)
	}
	if cfg.Log.Level != "info" {
		t.Errorf("expected default log level 'info', got %q", cfg.Log.Level)
	}
	if cfg.HTTP.Addr != ":8080" {
		t.Errorf("expected default HTTP addr ':8080', got %q", cfg.HTTP.Addr)
	}
}

func TestLoadAndValidate_WithOverrides(t *testing.T) {
	cfg, prob := LoadAndValidate("", func(c *config.AppConfig) {
		c.HTTP.Addr = ":9090"
		c.Log.Level = "debug"
	})
	if prob != nil {
		t.Fatalf("unexpected problem: %v", prob)
	}
	if cfg.HTTP.Addr != ":9090" {
		t.Errorf("expected overridden addr ':9090', got %q", cfg.HTTP.Addr)
	}
	if cfg.Log.Level != "debug" {
		t.Errorf("expected overridden log level 'debug', got %q", cfg.Log.Level)
	}
}

func TestLoadAndValidate_InvalidConfig_ReturnsProblem(t *testing.T) {
	_, prob := LoadAndValidate("", func(c *config.AppConfig) {
		c.Log.Level = "invalid_level"
	})
	if prob == nil {
		t.Fatal("expected validation problem for invalid log level")
	}
}

func TestApplyShardOverrides_FlagsOverrideConfig(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.Shard.Index = 0
	cfg.Shard.Count = 1

	ApplyShardOverrides(&cfg, 2, 4)

	if cfg.Shard.Index != 2 {
		t.Errorf("expected shard index 2, got %d", cfg.Shard.Index)
	}
	if cfg.Shard.Count != 4 {
		t.Errorf("expected shard count 4, got %d", cfg.Shard.Count)
	}
	if cfg.JetStream.ShardGroupID != 2 {
		t.Errorf("expected jetstream shard group id 2, got %d", cfg.JetStream.ShardGroupID)
	}
	if cfg.JetStream.ShardGroupCount != 4 {
		t.Errorf("expected jetstream shard group count 4, got %d", cfg.JetStream.ShardGroupCount)
	}
}

func TestApplyShardOverrides_NegativeFlags_NoOverride(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.Shard.Index = 1
	cfg.Shard.Count = 1

	ApplyShardOverrides(&cfg, -1, -1)

	if cfg.Shard.Index != 0 {
		t.Errorf("expected shard index to default to 0 for single shard, got %d", cfg.Shard.Index)
	}
	if cfg.Shard.Count != 1 {
		t.Errorf("expected shard count 1 (unchanged), got %d", cfg.Shard.Count)
	}
}

func TestApplyShardOverrides_HostnameFallbackWhenIndexUnset(t *testing.T) {
	t.Setenv("SHARD_INDEX", "")
	t.Setenv("SHARD_COUNT", "2")
	t.Setenv("MR_ENV", "dev")
	hostnameProvider = func() (string, error) { return "market-raccoon-processor-2", nil }
	composeContainerNumberProvider = defaultComposeContainerNumberProvider
	t.Cleanup(func() {
		hostnameProvider = defaultHostnameProvider
		composeContainerNumberProvider = defaultComposeContainerNumberProvider
	})

	cfg := defaultTestConfig()
	cfg.Shard.Index = 0
	cfg.Shard.Count = 1

	ApplyShardOverrides(&cfg, -1, -1)

	if cfg.Shard.Index != 1 {
		t.Fatalf("expected shard index from hostname=1, got %d", cfg.Shard.Index)
	}
	if cfg.Shard.Count != 2 {
		t.Fatalf("expected shard count from env=2, got %d", cfg.Shard.Count)
	}
	if cfg.JetStream.ShardGroupCount != 2 {
		t.Fatalf("expected jetstream shard group count 2, got %d", cfg.JetStream.ShardGroupCount)
	}
}

func TestApplyShardOverrides_UnresolvedIndexFailsForMultiShard(t *testing.T) {
	t.Setenv("SHARD_INDEX", "")
	t.Setenv("SHARD_COUNT", "3")
	t.Setenv("MR_ENV", "dev")
	hostnameProvider = func() (string, error) { return "processor", nil }
	composeContainerNumberProvider = defaultComposeContainerNumberProvider
	t.Cleanup(func() {
		hostnameProvider = defaultHostnameProvider
		composeContainerNumberProvider = defaultComposeContainerNumberProvider
	})

	cfg := defaultTestConfig()
	cfg.Shard.Index = 0
	cfg.Shard.Count = 1

	ApplyShardOverrides(&cfg, -1, -1)

	if cfg.Shard.Index != -1 {
		t.Fatalf("expected unresolved shard index=-1, got %d", cfg.Shard.Index)
	}
	if cfg.Shard.Count != 3 {
		t.Fatalf("expected shard count from env=3, got %d", cfg.Shard.Count)
	}
}

func TestApplyShardOverrides_ComposeContainerIDFallbackWhenIndexUnset(t *testing.T) {
	t.Setenv("SHARD_INDEX", "")
	t.Setenv("SHARD_COUNT", "3")
	t.Setenv("MR_ENV", "dev")
	hostnameProvider = func() (string, error) { return "13d5d7951762", nil }
	composeContainerNumberProvider = func(containerID string) (int, bool) {
		if containerID != "13d5d7951762" {
			return 0, false
		}
		return 3, true
	}
	t.Cleanup(func() {
		hostnameProvider = defaultHostnameProvider
		composeContainerNumberProvider = defaultComposeContainerNumberProvider
	})

	cfg := defaultTestConfig()
	cfg.Shard.Index = 0
	cfg.Shard.Count = 1

	ApplyShardOverrides(&cfg, -1, -1)

	if cfg.Shard.Index != 2 {
		t.Fatalf("expected shard index 2 from compose container-number, got %d", cfg.Shard.Index)
	}
	if cfg.Shard.Count != 3 {
		t.Fatalf("expected shard count from env=3, got %d", cfg.Shard.Count)
	}
}

func TestApplyShardOverrides_DevComposeContainerIDFallback_NormalizesOutOfRangeIndex(t *testing.T) {
	t.Setenv("SHARD_INDEX", "")
	t.Setenv("SHARD_COUNT", "2")
	t.Setenv("MR_ENV", "dev")
	hostnameProvider = func() (string, error) { return "13d5d7951762", nil }
	composeContainerNumberProvider = func(containerID string) (int, bool) {
		if containerID != "13d5d7951762" {
			return 0, false
		}
		// Docker Compose may keep incrementing container-number after recreates
		// even when service scale is only 2 replicas.
		return 3, true
	}
	t.Cleanup(func() {
		hostnameProvider = defaultHostnameProvider
		composeContainerNumberProvider = defaultComposeContainerNumberProvider
	})

	cfg := defaultTestConfig()
	cfg.Shard.Index = 0
	cfg.Shard.Count = 1

	ApplyShardOverrides(&cfg, -1, -1)

	if cfg.Shard.Index != 0 {
		t.Fatalf("expected normalized shard index 0 from compose container-number=3 with count=2, got %d", cfg.Shard.Index)
	}
	if cfg.Shard.Count != 2 {
		t.Fatalf("expected shard count from env=2, got %d", cfg.Shard.Count)
	}
}

func TestApplyShardOverrides_ProdRequiresExplicitIndexForMultiShard(t *testing.T) {
	t.Setenv("SHARD_INDEX", "")
	t.Setenv("SHARD_COUNT", "2")
	t.Setenv("MR_ENV", "prod")
	hostnameProvider = func() (string, error) { return "processor-1", nil }
	t.Cleanup(func() {
		hostnameProvider = defaultHostnameProvider
	})

	cfg := defaultTestConfig()
	cfg.Shard.Index = 0
	cfg.Shard.Count = 1

	ApplyShardOverrides(&cfg, -1, -1)

	if cfg.Shard.Index != -1 {
		t.Fatalf("expected shard index=-1 in prod when env is absent, got %d", cfg.Shard.Index)
	}
}

func TestApplyShardOverrides_SingleShardDefaultsToZeroWithoutEnv(t *testing.T) {
	t.Setenv("SHARD_INDEX", "")
	t.Setenv("SHARD_COUNT", "1")
	t.Setenv("MR_ENV", "prod")
	hostnameProvider = func() (string, error) { return "anything", nil }
	t.Cleanup(func() {
		hostnameProvider = defaultHostnameProvider
	})

	cfg := defaultTestConfig()
	cfg.Shard.Index = 0
	cfg.Shard.Count = 1

	ApplyShardOverrides(&cfg, -1, -1)

	if cfg.Shard.Index != 0 {
		t.Fatalf("expected shard index default 0 for single shard, got %d", cfg.Shard.Index)
	}
}

func TestSignalChannel_ReturnsNonNil(t *testing.T) {
	ch := SignalChannel()
	if ch == nil {
		t.Fatal("expected non-nil signal channel")
	}
}

func defaultTestConfig() config.AppConfig {
	cfg, _ := config.Load("")
	return cfg
}
