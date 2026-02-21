//go:build integration

package jetstream

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/observability"
	"github.com/market-raccoon/internal/shared/problem"
	"github.com/nats-io/nats.go"
)

func TestIngestConformance_AckNakTermGoldenTable(t *testing.T) {
	t.Parallel()

	knownEnv := envelope.Envelope{Type: "marketdata.trade", Version: 1}
	tests := []struct {
		name            string
		decision        ingestDecision
		wantDisposition Disposition
		wantStatus      string
		wantReason      string
		wantQuarantine  bool
	}{
		{
			name: "DECODE_FAILED",
			decision: ClassifyIngestError(
				problem.WithDetail(problem.New(problem.Internal, "decode failed"), "reason_code", ingestReasonDecodeFailed),
				envelope.Envelope{},
			),
			wantDisposition: DispositionTerm,
			wantStatus:      "term",
			wantReason:      ingestReasonDecodeFailed,
			wantQuarantine:  true,
		},
		{
			name: "VALIDATION_FAILED",
			decision: ClassifyIngestError(
				problem.New(problem.ValidationFailed, "bad payload"),
				knownEnv,
			),
			wantDisposition: DispositionTerm,
			wantStatus:      "term",
			wantReason:      ingestReasonValidationFailed,
			wantQuarantine:  true,
		},
		{
			name: "UNKNOWN_EVENT_TYPE",
			decision: ClassifyIngestError(
				problem.WithDetail(problem.New(problem.ValidationFailed, "unhandled envelope type"), "reason_code", ingestReasonUnknownEventType),
				envelope.Envelope{Type: "unknown.type", Version: 1},
			),
			wantDisposition: DispositionTerm,
			wantStatus:      "term",
			wantReason:      ingestReasonUnknownEventType,
			wantQuarantine:  true,
		},
		{
			name: "UNKNOWN_EVENT_VERSION",
			decision: ClassifyIngestError(
				problem.New(problem.ValidationFailed, "unsupported envelope version"),
				envelope.Envelope{Type: "marketdata.trade", Version: 2},
			),
			wantDisposition: DispositionTerm,
			wantStatus:      "term",
			wantReason:      ingestReasonUnknownEventVersion,
			wantQuarantine:  true,
		},
		{
			name: "TRANSIENT_FAILURE",
			decision: ClassifyIngestError(
				problem.WithRetryable(problem.New(problem.Unavailable, "temporary unavailable")),
				knownEnv,
			),
			wantDisposition: DispositionNak,
			wantStatus:      "nak",
			wantReason:      ingestReasonTransientFailure,
			wantQuarantine:  false,
		},
		{
			name:            "QUARANTINE_PUBLISH_FAILED_FORBIDDEN",
			decision:        quarantineFailureDecision(errors.New("nats: Authorization Violation")),
			wantDisposition: DispositionTerm,
			wantStatus:      "term",
			wantReason:      ingestReasonQuarantinePublishError,
			wantQuarantine:  false,
		},
		{
			name:            "QUARANTINE_PUBLISH_FAILED_TIMEOUT",
			decision:        quarantineFailureDecision(context.DeadlineExceeded),
			wantDisposition: DispositionNak,
			wantStatus:      "nak",
			wantReason:      ingestReasonQuarantinePublishError,
			wantQuarantine:  false,
		},
		{
			name:            "OK",
			decision:        ClassifyIngestError(nil, knownEnv),
			wantDisposition: DispositionAck,
			wantStatus:      "ok",
			wantReason:      ingestReasonOK,
			wantQuarantine:  false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if tc.decision.Disposition != tc.wantDisposition {
				t.Fatalf("disposition=%v want=%v", tc.decision.Disposition, tc.wantDisposition)
			}
			if tc.decision.Status != tc.wantStatus {
				t.Fatalf("status=%q want=%q", tc.decision.Status, tc.wantStatus)
			}
			if tc.decision.ReasonCode != tc.wantReason {
				t.Fatalf("reason=%q want=%q", tc.decision.ReasonCode, tc.wantReason)
			}
			if tc.decision.Quarantine != tc.wantQuarantine {
				t.Fatalf("quarantine=%v want=%v", tc.decision.Quarantine, tc.wantQuarantine)
			}
		})
	}
}

func TestIngestConformance_ForbiddenQuarantinePublishDoesNotNak(t *testing.T) {
	t.Parallel()

	decision := quarantineFailureDecision(errors.New("permission denied: forbidden"))
	if decision.Disposition != DispositionTerm {
		t.Fatalf("disposition=%v want=%v", decision.Disposition, DispositionTerm)
	}

	fake := &fakeAckMessage{}
	consumer := &Consumer{observer: observability.NopBusObserver()}
	if p := consumer.ackWithDisposition(context.Background(), fake, decision.Disposition, decision.Status, decision.ReasonCode, time.Now()); p != nil {
		t.Fatalf("ackWithDisposition failed: %v", p)
	}

	if fake.nakCalls != 0 {
		t.Fatalf("nak calls=%d want=0", fake.nakCalls)
	}
	if fake.termCalls != 1 {
		t.Fatalf("term calls=%d want=1", fake.termCalls)
	}
	if fake.ackCalls != 0 {
		t.Fatalf("ack calls=%d want=0", fake.ackCalls)
	}
}

func quarantineFailureDecision(err error) ingestDecision {
	retryable, reasonCode := ClassifyQuarantinePublishError(err)
	quarantineErr := problem.WithDetail(problem.New(problem.Unavailable, "jetstream quarantine publish failed"), "reason_code", reasonCode)
	if retryable {
		quarantineErr = problem.WithRetryable(quarantineErr)
	}
	return applyQuarantinePublishResult(poisonDecision(ingestReasonDecodeFailed), quarantineErr)
}

type fakeAckMessage struct {
	ackCalls  int
	nakCalls  int
	termCalls int
}

func (f *fakeAckMessage) Ack(...nats.AckOpt) error {
	f.ackCalls++
	return nil
}

func (f *fakeAckMessage) Nak(...nats.AckOpt) error {
	f.nakCalls++
	return nil
}

func (f *fakeAckMessage) Term(...nats.AckOpt) error {
	f.termCalls++
	return nil
}
