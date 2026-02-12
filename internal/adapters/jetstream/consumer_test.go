package jetstream

import (
	"testing"
	"time"

	"github.com/market-raccoon/internal/shared/problem"
)

func TestMapProblemToDisposition(t *testing.T) {
	tests := []struct {
		name       string
		prob       *problem.Problem
		wantDisp   Disposition
		wantStatus string
	}{
		{
			name:       "ok",
			prob:       nil,
			wantDisp:   DispositionAck,
			wantStatus: "ok",
		},
		{
			name:       "transient retryable",
			prob:       problem.WithRetryable(problem.New(problem.Internal, "tmp")),
			wantDisp:   DispositionNak,
			wantStatus: "nak",
		},
		{
			name:       "transient unavailable",
			prob:       problem.New(problem.Unavailable, "nats down"),
			wantDisp:   DispositionNak,
			wantStatus: "nak",
		},
		{
			name:       "poison validation",
			prob:       problem.New(problem.ValidationFailed, "bad payload"),
			wantDisp:   DispositionTerm,
			wantStatus: "term",
		},
		{
			name:       "poison out of order",
			prob:       problem.New(problem.OutOfOrder, "seq"),
			wantDisp:   DispositionTerm,
			wantStatus: "term",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotDisp, gotStatus := MapProblemToDisposition(tc.prob)
			if gotDisp != tc.wantDisp {
				t.Fatalf("disposition=%v want=%v", gotDisp, tc.wantDisp)
			}
			if gotStatus != tc.wantStatus {
				t.Fatalf("status=%q want=%q", gotStatus, tc.wantStatus)
			}
		})
	}
}

func TestConsumerConfigDefaultsAndValidation(t *testing.T) {
	cfg := withConsumerDefaults(ConsumerConfig{
		URL:         "nats://127.0.0.1:4222",
		StreamName:  "MARKETDATA",
		DedupWindow: 5 * time.Minute,
		MaxAge:      24 * time.Hour,
		MaxBytes:    1_000_000,
	})
	if cfg.ConsumerDurable == "" || cfg.AckWait <= 0 || cfg.MaxAckPending <= 0 || cfg.MaxDeliver <= 0 {
		t.Fatalf("defaults not applied: %+v", cfg)
	}
	if p := validateConsumerConfig(cfg); p != nil {
		t.Fatalf("validateConsumerConfig failed: %v", p)
	}
}

func TestToNATSConsumerConfig_FilterMapping(t *testing.T) {
	cfg := withConsumerDefaults(ConsumerConfig{
		URL:            "nats://127.0.0.1:4222",
		StreamName:     "MARKETDATA",
		DedupWindow:    5 * time.Minute,
		MaxAge:         24 * time.Hour,
		MaxBytes:       1_000_000,
		FilterSubjects: []string{"marketdata.bookdelta.v1.>", "marketdata.trade.v1.>"},
	})

	ccfg, p := toNATSConsumerConfig(cfg)
	if p != nil {
		t.Fatalf("toNATSConsumerConfig: %v", p)
	}
	if ccfg.FilterSubject != "" {
		t.Fatalf("FilterSubject should be empty for multiple filters, got %q", ccfg.FilterSubject)
	}
	if len(ccfg.FilterSubjects) != 2 {
		t.Fatalf("FilterSubjects len=%d want=2", len(ccfg.FilterSubjects))
	}
}
