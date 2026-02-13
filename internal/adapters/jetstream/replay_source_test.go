package jetstream

import (
	"testing"
	"time"

	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/problem"
)

func TestReplaySourceDefaultsAndValidation(t *testing.T) {
	cfg := withReplaySourceDefaults(ReplaySourceConfig{
		URL:             "nats://127.0.0.1:4222",
		StreamName:      "MARKETDATA",
		SubjectFilter:   "marketdata.>",
		ConsumerDurable: "processor-replay-test",
		Window:          5 * time.Minute,
	})
	if cfg.DeliverPolicy != replayDeliverByStartTime {
		t.Fatalf("deliver policy=%q want=%q", cfg.DeliverPolicy, replayDeliverByStartTime)
	}
	if cfg.MaxMessages <= 0 {
		t.Fatalf("max_messages should be defaulted to positive value, got %d", cfg.MaxMessages)
	}
	if p := validateReplaySourceConfig(cfg); p != nil {
		t.Fatalf("validateReplaySourceConfig: %v", p)
	}
}

func TestReplaySourceValidation_MaxMessagesBounds(t *testing.T) {
	cfg := withReplaySourceDefaults(ReplaySourceConfig{
		URL:             "nats://127.0.0.1:4222",
		StreamName:      "MARKETDATA",
		SubjectFilter:   "marketdata.>",
		ConsumerDurable: "processor-replay-test",
		MaxMessages:     maxReplayMaxMessages + 1,
	})
	p := validateReplaySourceConfig(cfg)
	if p == nil {
		t.Fatal("expected validation failure for max_messages overflow")
	}
	if p.Code != problem.ValidationFailed {
		t.Fatalf("problem code=%s want=%s", p.Code, problem.ValidationFailed)
	}
}

func TestReplaySourceValidation_WindowRequiredForByStartTime(t *testing.T) {
	cfg := withReplaySourceDefaults(ReplaySourceConfig{
		URL:             "nats://127.0.0.1:4222",
		StreamName:      "MARKETDATA",
		SubjectFilter:   "marketdata.>",
		ConsumerDurable: "processor-replay-test",
		DeliverPolicy:   replayDeliverByStartTime,
		Window:          0,
	})
	p := validateReplaySourceConfig(cfg)
	if p == nil {
		t.Fatal("expected validation failure for missing window")
	}
	if p.Code != problem.ValidationFailed {
		t.Fatalf("problem code=%s want=%s", p.Code, problem.ValidationFailed)
	}
}

func TestReplaySourceValidation_InvalidSubjectFilter(t *testing.T) {
	cfg := withReplaySourceDefaults(ReplaySourceConfig{
		URL:             "nats://127.0.0.1:4222",
		StreamName:      "MARKETDATA",
		SubjectFilter:   "freeprefix.>",
		ConsumerDurable: "processor-replay-test",
	})
	p := validateReplaySourceConfig(cfg)
	if p == nil {
		t.Fatal("expected validation failure for invalid subject filter")
	}
	if p.Code != problem.ValidationFailed {
		t.Fatalf("problem code=%s want=%s", p.Code, problem.ValidationFailed)
	}
}

func TestEnvelopeLessDeterministicOrdering(t *testing.T) {
	a := envelope.Envelope{
		Type:           "marketdata.trade",
		Version:        1,
		Venue:          "binance",
		Instrument:     "BTCUSDT",
		TsIngest:       10,
		Seq:            1,
		IdempotencyKey: "a",
	}
	b := envelope.Envelope{
		Type:           "marketdata.trade",
		Version:        1,
		Venue:          "binance",
		Instrument:     "BTCUSDT",
		TsIngest:       10,
		Seq:            2,
		IdempotencyKey: "b",
	}
	c := envelope.Envelope{
		Type:           "marketdata.trade",
		Version:        1,
		Venue:          "kraken",
		Instrument:     "BTCUSDT",
		TsIngest:       10,
		Seq:            1,
		IdempotencyKey: "a",
	}

	if !envelopeLess(a, b) {
		t.Fatal("expected a < b by seq")
	}
	if !envelopeLess(a, c) {
		t.Fatal("expected a < c by venue tie-break")
	}
	if envelopeLess(b, a) {
		t.Fatal("expected b !< a")
	}
}
