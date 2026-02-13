package jetstream

import (
	"slices"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

func TestBuildStreamConfig_QuarantineAndBounds(t *testing.T) {
	cfg := buildStreamConfig(PublisherConfig{
		URL:         "nats://127.0.0.1:4222",
		StreamName:  "MARKETDATA",
		DedupWindow: 5 * time.Minute,
		MaxAge:      24 * time.Hour,
		MaxBytes:    50_000_000,
	})

	if cfg.MaxAge <= 0 {
		t.Fatal("stream MaxAge must be bounded (>0)")
	}
	if cfg.MaxBytes <= 0 {
		t.Fatal("stream MaxBytes must be bounded (>0)")
	}
	if cfg.Retention != nats.LimitsPolicy {
		t.Fatalf("retention=%v want=%v", cfg.Retention, nats.LimitsPolicy)
	}
	if cfg.Storage != nats.FileStorage {
		t.Fatalf("storage=%v want=%v", cfg.Storage, nats.FileStorage)
	}
	if !slices.Contains(cfg.Subjects, "quarantine.>") {
		t.Fatalf("subjects=%v: expected quarantine wildcard", cfg.Subjects)
	}
}
