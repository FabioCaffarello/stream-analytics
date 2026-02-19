package jetstream

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/problem"
	"github.com/nats-io/nats.go"
)

func TestValidateConfig(t *testing.T) {
	cfg := PublisherConfig{
		URL:            "nats://127.0.0.1:4222",
		StreamName:     "MARKETDATA",
		DedupWindow:    5 * time.Minute,
		MaxAge:         24 * time.Hour,
		MaxBytes:       1_000,
		PublishTimeout: time.Second,
	}
	if p := validateConfig(cfg); p != nil {
		t.Fatalf("validateConfig returned problem: %v", p)
	}
}

func TestValidateConfig_Invalid(t *testing.T) {
	cfg := PublisherConfig{
		URL:            "",
		StreamName:     "MARKETDATA",
		DedupWindow:    5 * time.Minute,
		MaxAge:         24 * time.Hour,
		MaxBytes:       1_000,
		PublishTimeout: time.Second,
	}
	if p := validateConfig(cfg); p == nil {
		t.Fatal("expected validation failure for empty URL")
	}
}

func TestClassifyPublishError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "deadline", err: context.DeadlineExceeded, want: "timeout"},
		{name: "bad subject", err: nats.ErrBadSubject, want: "validation"},
		{name: "disconnected", err: nats.ErrDisconnected, want: "unavailable"},
		{name: "generic", err: errors.New("boom"), want: "publish_failed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyPublishError(tc.err); got != tc.want {
				t.Fatalf("classifyPublishError=%q want=%q", got, tc.want)
			}
		})
	}
}

func TestWrapUnavailable(t *testing.T) {
	p := wrapUnavailable("unavailable", errors.New("nats down"), "publish failed")
	if p == nil {
		t.Fatal("expected non-nil problem")
	}
	if p.Code != problem.Unavailable {
		t.Fatalf("problem code=%s want=%s", p.Code, problem.Unavailable)
	}
	if !p.Retryable {
		t.Fatal("expected retryable problem")
	}
}

func TestSubjectFromEnvelopeWithPrefix_Default(t *testing.T) {
	env := envelope.Envelope{
		Type:       "insights.crossvenue.trade_snapshot",
		Version:    1,
		Venue:      "GLOBAL",
		Instrument: "BTCUSDT",
	}
	got := subjectFromEnvelopeWithPrefix(env)
	want := "insights.crossvenue.trade_snapshot.v1.global.BTCUSDT"
	if got != want {
		t.Fatalf("subject=%q want=%q", got, want)
	}
}

func TestSubjectFromEnvelopeWithPrefix_Override(t *testing.T) {
	env := envelope.Envelope{
		Type:       "insights.crossvenue.trade_snapshot",
		Version:    1,
		Venue:      "GLOBAL",
		Instrument: "BTCUSDT",
		Meta: map[string]string{
			subjectPrefixMetaKey: "insights.custom.snapshot.v1",
		},
	}
	got := subjectFromEnvelopeWithPrefix(env)
	want := "insights.custom.snapshot.v1.global.BTCUSDT"
	if got != want {
		t.Fatalf("subject=%q want=%q", got, want)
	}
}
