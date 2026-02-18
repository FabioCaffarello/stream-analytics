package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/anthdm/hollywood/actor"
	actorruntime "github.com/market-raccoon/internal/actors/runtime"
	"github.com/market-raccoon/internal/shared/config"
)

func TestBuildServerFactories_DeliveryEnabledIncludesFactory(t *testing.T) {
	factory := func() actor.Receiver { return nil }
	factories := buildServerFactories(true, factory)
	if _, ok := factories[actorruntime.SubsystemDelivery]; !ok {
		t.Fatal("expected SubsystemDelivery factory when delivery is enabled")
	}
}

func TestBuildServerFactories_DeliveryDisabledExcludesFactory(t *testing.T) {
	factory := func() actor.Receiver { return nil }
	factories := buildServerFactories(false, factory)
	if _, ok := factories[actorruntime.SubsystemDelivery]; ok {
		t.Fatal("did not expect SubsystemDelivery factory when delivery is disabled")
	}
}

func TestConfigLoad_DeliveryEnabled(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "server.jsonc")
	raw := `{
  "delivery": {
    "enabled": true,
    "max_sessions": 128,
    "backpressure_policy": "drop_newest",
    "nats": {
      "consumer_durable": "delivery-test",
      "filter_subjects": ["marketdata.>"]
    }
  }
}`
	if err := os.WriteFile(cfgPath, []byte(raw), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, p := config.Load(cfgPath)
	if p != nil {
		t.Fatalf("config load failed: %v", p)
	}
	if !cfg.Delivery.Enabled {
		t.Fatal("expected delivery.enabled=true")
	}
	if cfg.Delivery.MaxSessions != 128 {
		t.Fatalf("max_sessions=%d want=128", cfg.Delivery.MaxSessions)
	}
	if p := cfg.Validate(); p != nil {
		t.Fatalf("config validate failed: %v", p)
	}
}
