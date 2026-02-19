package config

import (
	"testing"
)

func TestReplayDefaults_OffByDefault(t *testing.T) {
	cfg, p := Load("")
	if p != nil {
		t.Fatalf("Load defaults failed: %v", p)
	}
	if cfg.Replay.Mode != "off" {
		t.Fatalf("replay.mode=%q want=off", cfg.Replay.Mode)
	}
	if cfg.Replay.JetStream.MaxMessages <= 0 {
		t.Fatalf("replay.jetstream.max_messages=%d want>0", cfg.Replay.JetStream.MaxMessages)
	}
	if cfg.Replay.JetStream.MergeBuffer <= 0 {
		t.Fatalf("replay.jetstream.merge_buffer=%d want>0", cfg.Replay.JetStream.MergeBuffer)
	}
	if p := cfg.Validate(); p != nil {
		t.Fatalf("Validate defaults failed: %v", p)
	}
}

func TestReplayValidate_JetStreamRequiresBusType(t *testing.T) {
	cfg, p := Load("")
	if p != nil {
		t.Fatalf("Load defaults failed: %v", p)
	}
	cfg.Replay.Mode = "jetstream"
	cfg.Bus.Type = "inmemory"

	p = cfg.Validate()
	if p == nil {
		t.Fatal("expected validation failure for replay.mode=jetstream with bus.type=inmemory")
	}
}

func TestReplayValidate_WindowRequiredForByStartTime(t *testing.T) {
	cfg, p := Load("")
	if p != nil {
		t.Fatalf("Load defaults failed: %v", p)
	}
	cfg.Bus.Type = "jetstream"
	cfg.Replay.Mode = "jetstream"
	cfg.Replay.JetStream.DeliverPolicy = "by_start_time"
	cfg.Replay.JetStream.Window = ""

	p = cfg.Validate()
	if p == nil {
		t.Fatal("expected validation failure for replay.jetstream.deliver_policy=by_start_time without window")
	}
}

func TestReplayValidate_MaxMessagesBounds(t *testing.T) {
	cfg, p := Load("")
	if p != nil {
		t.Fatalf("Load defaults failed: %v", p)
	}
	cfg.Bus.Type = "jetstream"
	cfg.Replay.Mode = "jetstream"
	cfg.Replay.JetStream.MaxMessages = 10_000_001

	p = cfg.Validate()
	if p == nil {
		t.Fatal("expected validation failure for replay.jetstream.max_messages overflow")
	}
}

func TestReplayValidate_FileModeRequiresPath(t *testing.T) {
	cfg, p := Load("")
	if p != nil {
		t.Fatalf("Load defaults failed: %v", p)
	}
	cfg.Replay.Mode = "file"
	cfg.MarketData.ReplayPath = ""

	p = cfg.Validate()
	if p == nil {
		t.Fatal("expected validation failure for replay.mode=file with empty marketdata.replay_path")
	}
}
