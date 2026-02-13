package jetstream

import (
	"context"
	"encoding/json"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/problem"
	"github.com/nats-io/nats.go"
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

func TestHeartbeatIntervalClamp(t *testing.T) {
	tests := []struct {
		name    string
		ackWait time.Duration
		want    time.Duration
	}{
		{
			name:    "min clamp",
			ackWait: 300 * time.Millisecond,
			want:    minHeartbeatInterval,
		},
		{
			name:    "default division",
			ackWait: 3 * time.Second,
			want:    1 * time.Second,
		},
		{
			name:    "max clamp",
			ackWait: 60 * time.Second,
			want:    maxHeartbeatInterval,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := heartbeatInterval(tc.ackWait); got != tc.want {
				t.Fatalf("heartbeatInterval(%s)=%s want=%s", tc.ackWait, got, tc.want)
			}
		})
	}
}

func TestAckHeartbeatStopsPromptlyAfterDisposition(t *testing.T) {
	ctx := context.Background()
	calls := make(chan struct{}, 16)

	stop := startAckHeartbeat(
		ctx,
		900*time.Millisecond,
		func(...nats.AckOpt) error {
			select {
			case calls <- struct{}{}:
			default:
			}
			return nil
		},
		nil,
	)

	waitUntil(t, 2*time.Second, func() bool { return len(calls) > 0 })

	done := make(chan struct{})
	go func() {
		stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("heartbeat stop did not return promptly")
	}

	before := len(calls)
	time.Sleep(600 * time.Millisecond)
	after := len(calls)
	if after != before {
		t.Fatalf("heartbeat kept running after stop: before=%d after=%d", before, after)
	}
}

func TestAckHeartbeatGoroutineDeltaBounded(t *testing.T) {
	before := runtime.NumGoroutine()

	const n = 24
	for i := 0; i < n; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		stop := startAckHeartbeat(
			ctx,
			750*time.Millisecond,
			func(...nats.AckOpt) error {
				time.Sleep(20 * time.Millisecond)
				return nil
			},
			nil,
		)

		// Simulate slow message handling that lasts long enough for heartbeat ticks.
		time.Sleep(320 * time.Millisecond)
		stop()
		cancel()
	}

	waitUntil(t, 2*time.Second, func() bool {
		return runtime.NumGoroutine() <= before+6
	})
	after := runtime.NumGoroutine()
	if delta := after - before; delta > 6 {
		t.Fatalf("goroutine delta too high: before=%d after=%d delta=%d", before, after, delta)
	}
}

func TestClassifyIngestError_UnknownTypeTerminated(t *testing.T) {
	prob := problem.WithDetail(problem.New(problem.ValidationFailed, "unhandled envelope type"), "reason_code", ingestReasonUnknownEventType)
	decision := ClassifyIngestError(prob, envelope.Envelope{
		Type:    "unknown.type",
		Version: 1,
	})
	if decision.Disposition != DispositionTerm {
		t.Fatalf("disposition=%v want=%v", decision.Disposition, DispositionTerm)
	}
	if decision.ReasonCode != ingestReasonUnknownEventType {
		t.Fatalf("reason=%q want=%q", decision.ReasonCode, ingestReasonUnknownEventType)
	}
	if !decision.Quarantine {
		t.Fatal("unknown type must route to quarantine")
	}
}

func TestClassifyIngestError_UnknownVersionTerminated(t *testing.T) {
	decision := ClassifyIngestError(problem.New(problem.ValidationFailed, "bad version"), envelope.Envelope{
		Type:    "marketdata.trade",
		Version: 2,
	})
	if decision.Disposition != DispositionTerm {
		t.Fatalf("disposition=%v want=%v", decision.Disposition, DispositionTerm)
	}
	if decision.ReasonCode != ingestReasonUnknownEventVersion {
		t.Fatalf("reason=%q want=%q", decision.ReasonCode, ingestReasonUnknownEventVersion)
	}
}

func TestApplyQuarantinePublishResult_FailureTurnsIntoNak(t *testing.T) {
	decision := applyQuarantinePublishResult(
		poisonDecision(ingestReasonDecodeFailed),
		problem.WithRetryable(problem.New(problem.Unavailable, "nats unavailable")),
	)
	if decision.Disposition != DispositionNak {
		t.Fatalf("disposition=%v want=%v", decision.Disposition, DispositionNak)
	}
	if decision.Status != "nak" {
		t.Fatalf("status=%q want=%q", decision.Status, "nak")
	}
	if decision.ReasonCode != ingestReasonQuarantinePublishError {
		t.Fatalf("reason=%q want=%q", decision.ReasonCode, ingestReasonQuarantinePublishError)
	}
}

func TestBuildQuarantineEnvelope_TruncatesProblemTextDeterministically(t *testing.T) {
	msg := nats.NewMsg("marketdata.bookdelta.v1.binance.BTCUSDT")
	msg.Data = []byte(`{"a":"b"}`)
	msg.Header.Set(nats.MsgIdHdr, "quarantine-msg-1")

	longErr := strings.Repeat("x", quarantineErrorMaxLen*2)
	env, p := buildQuarantineEnvelope(msg, envelope.Envelope{}, ingestReasonDecodeFailed, problem.New(problem.ValidationFailed, longErr))
	if p != nil {
		t.Fatalf("buildQuarantineEnvelope failed: %v", p)
	}

	var q quarantinePayload
	if err := json.Unmarshal(env.Payload, &q); err != nil {
		t.Fatalf("unmarshal quarantine payload failed: %v", err)
	}
	if got := len(q.Error); got != quarantineErrorMaxLen {
		t.Fatalf("error length=%d want=%d", got, quarantineErrorMaxLen)
	}
	env2, p := buildQuarantineEnvelope(msg, envelope.Envelope{}, ingestReasonDecodeFailed, problem.New(problem.ValidationFailed, longErr))
	if p != nil {
		t.Fatalf("second buildQuarantineEnvelope failed: %v", p)
	}
	if string(env.Payload) != string(env2.Payload) {
		t.Fatal("quarantine payload must be deterministic for identical input")
	}
}

func waitUntil(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}
