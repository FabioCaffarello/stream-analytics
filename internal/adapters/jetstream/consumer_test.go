package jetstream

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/observability"
	"github.com/market-raccoon/internal/shared/problem"
	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus/testutil"
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

func TestAckWithDisposition_AckUsesAckSyncWhenAvailable(t *testing.T) {
	c := &Consumer{observer: observability.NopBusObserver()}
	msg := &fakeAckSyncMessage{}

	if p := c.ackWithDisposition(context.Background(), msg, DispositionAck, "ok", ingestReasonOK, time.Now()); p != nil {
		t.Fatalf("ackWithDisposition failed: %v", p)
	}
	if msg.ackSyncCalls != 1 {
		t.Fatalf("ackSync calls=%d want=1", msg.ackSyncCalls)
	}
	if msg.ackCalls != 0 {
		t.Fatalf("ack calls=%d want=0 when AckSync is available", msg.ackCalls)
	}
}

type fakeAckSyncMessage struct {
	ackCalls     int
	ackSyncCalls int
	nakCalls     int
	termCalls    int
}

func (f *fakeAckSyncMessage) Ack(...nats.AckOpt) error {
	f.ackCalls++
	return nil
}

func (f *fakeAckSyncMessage) AckSync(...nats.AckOpt) error {
	f.ackSyncCalls++
	return nil
}

func (f *fakeAckSyncMessage) Nak(...nats.AckOpt) error {
	f.nakCalls++
	return nil
}

func (f *fakeAckSyncMessage) Term(...nats.AckOpt) error {
	f.termCalls++
	return nil
}

func TestConsumerConfigDefaultsAndValidation(t *testing.T) {
	cfg := withConsumerDefaults(ConsumerConfig{
		URL:         "nats://127.0.0.1:4222",
		StreamName:  "MARKETDATA",
		DedupWindow: 5 * time.Minute,
		MaxAge:      24 * time.Hour,
		MaxBytes:    1_000_000,
	})
	if cfg.ConsumerDurable == "" || cfg.AckWait <= 0 || cfg.MaxAckPending <= 0 || cfg.MaxDeliver <= 0 || cfg.FetchBatchSize <= 0 {
		t.Fatalf("defaults not applied: %+v", cfg)
	}
	if p := validateConsumerConfig(cfg); p != nil {
		t.Fatalf("validateConsumerConfig failed: %v", p)
	}
}

func TestConsumer_FetchBatchSizeClamp(t *testing.T) {
	tests := []struct {
		name string
		cfg  ConsumerConfig
		want int
	}{
		{
			name: "default batch size",
			cfg:  ConsumerConfig{},
			want: defaultFetchBatchSize,
		},
		{
			name: "respects configured size",
			cfg: ConsumerConfig{
				FetchBatchSize: 64,
				MaxAckPending:  1024,
			},
			want: 64,
		},
		{
			name: "clamped by max ack pending",
			cfg: ConsumerConfig{
				FetchBatchSize: 64,
				MaxAckPending:  16,
			},
			want: 16,
		},
		{
			name: "clamped by hard max",
			cfg: ConsumerConfig{
				FetchBatchSize: 4096,
				MaxAckPending:  4096,
			},
			want: maxFetchBatchSize,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := &Consumer{cfg: tc.cfg}
			if got := c.fetchBatchSize(); got != tc.want {
				t.Fatalf("fetchBatchSize()=%d want=%d", got, tc.want)
			}
		})
	}
}

func TestConsumerConfigValidation_InvalidFilterSubjectFailsFast(t *testing.T) {
	cfg := withConsumerDefaults(ConsumerConfig{
		URL:            "nats://127.0.0.1:4222",
		StreamName:     "MARKETDATA",
		DedupWindow:    5 * time.Minute,
		MaxAge:         24 * time.Hour,
		MaxBytes:       1_000_000,
		FilterSubjects: []string{"freeprefix.>"},
	})
	if p := validateConsumerConfig(cfg); p == nil {
		t.Fatal("expected validation failure for invalid filter_subjects")
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

func TestPullSubscribeSubject_Mapping(t *testing.T) {
	if got := pullSubscribeSubject([]string{"marketdata.>"}); got != "marketdata.>" {
		t.Fatalf("single filter subject=%q want=%q", got, "marketdata.>")
	}
	if got := pullSubscribeSubject([]string{"aggregation.snapshot.v1.>", "aggregation.candle.v1.>"}); got != "" {
		t.Fatalf("multi filter subject=%q want empty", got)
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

func TestConsumer_HandlerPanic_HeartbeatCleansUp(t *testing.T) {
	before := runtime.NumGoroutine()

	data, p := envelope.MarshalBinary(envelope.Envelope{
		Type:           "marketdata.bookdelta",
		Version:        1,
		Venue:          "binance",
		Instrument:     "BTCUSDT",
		TsExchange:     time.Now().UnixMilli(),
		TsIngest:       time.Now().UnixMilli(),
		Seq:            1,
		IdempotencyKey: "panic-heartbeat-cleanup",
		Payload:        []byte(`{"ok":true}`),
	})
	if p != nil {
		t.Fatalf("marshal envelope: %v", p)
	}

	c := &Consumer{
		cfg: ConsumerConfig{
			AckWait: 300 * time.Millisecond,
		},
		observer: observability.NopBusObserver(),
	}

	const iterations = 16
	for i := 0; i < iterations; i++ {
		msg := nats.NewMsg("marketdata.bookdelta.v1.binance.BTCUSDT")
		msg.Data = data

		func() {
			defer func() {
				if r := recover(); r == nil {
					t.Fatal("expected panic from handler")
				}
			}()

			_ = c.consumeOne(context.Background(), msg, func(context.Context, envelope.Envelope) *problem.Problem {
				panic("handler panic")
			})
		}()
	}

	waitUntil(t, 2*time.Second, func() bool {
		return runtime.NumGoroutine() <= before+4
	})
	after := runtime.NumGoroutine()
	if delta := after - before; delta > 4 {
		t.Fatalf("goroutine delta too high after panic cleanup: before=%d after=%d delta=%d", before, after, delta)
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

func TestClassifyQuarantinePublishError_PermissionDenied_Terms(t *testing.T) {
	retryable, reasonCode := ClassifyQuarantinePublishError(errors.New("nats: Authorization Violation"))
	if retryable {
		t.Fatal("expected permission denied to be non-retryable")
	}
	if reasonCode != ingestReasonQuarantinePublishError {
		t.Fatalf("reason=%q want=%q", reasonCode, ingestReasonQuarantinePublishError)
	}

	decision := applyQuarantinePublishResult(
		poisonDecision(ingestReasonDecodeFailed),
		problem.New(problem.Unavailable, "jetstream quarantine publish failed"),
	)
	if decision.Disposition != DispositionTerm {
		t.Fatalf("disposition=%v want=%v", decision.Disposition, DispositionTerm)
	}

	before := testutil.ToFloat64(metrics.IngestTermTotal.WithLabelValues("quarantine_publish_failed"))
	recordIngestDecisionMetrics(decision.Disposition, decision.ReasonCode)
	after := testutil.ToFloat64(metrics.IngestTermTotal.WithLabelValues("quarantine_publish_failed"))
	if after != before+1 {
		t.Fatalf("term metric delta=%f want=1", after-before)
	}
}

func TestClassifyQuarantinePublishError_Timeout_Naks(t *testing.T) {
	retryable, reasonCode := ClassifyQuarantinePublishError(context.DeadlineExceeded)
	if !retryable {
		t.Fatal("expected timeout to be retryable")
	}
	if reasonCode != ingestReasonQuarantinePublishError {
		t.Fatalf("reason=%q want=%q", reasonCode, ingestReasonQuarantinePublishError)
	}

	decision := applyQuarantinePublishResult(
		poisonDecision(ingestReasonDecodeFailed),
		problem.WithRetryable(problem.New(problem.Unavailable, "jetstream quarantine publish failed")),
	)
	if decision.Disposition != DispositionNak {
		t.Fatalf("disposition=%v want=%v", decision.Disposition, DispositionNak)
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

func TestQuarantineSubjectTaxonomy_IsStrict(t *testing.T) {
	msg := nats.NewMsg("marketdata.trade.v1.binance.BTCUSDT")
	msg.Data = []byte(`{"p":1}`)
	msg.Header.Set(nats.MsgIdHdr, "q-taxonomy-1")

	out, p := buildQuarantineEnvelope(msg, envelope.Envelope{
		Venue:          "BINANCE",
		Instrument:     "BTCUSDT",
		TsIngest:       1,
		IdempotencyKey: "src-1",
	}, ingestReasonDecodeFailed, problem.New(problem.ValidationFailed, "decode failed"))
	if p != nil {
		t.Fatalf("buildQuarantineEnvelope failed: %v", p)
	}

	subject := envelope.SubjectFromEnvelope(out)
	if subject != "quarantine.v1.binance.BTCUSDT" {
		t.Fatalf("subject=%q want=%q", subject, "quarantine.v1.binance.BTCUSDT")
	}
	parts := strings.Split(subject, ".")
	if len(parts) != 4 {
		t.Fatalf("subject parts=%d want=4 (%q)", len(parts), subject)
	}
	if parts[0] != "quarantine" || parts[1] != "v1" {
		t.Fatalf("unexpected event/version in subject: %q", subject)
	}
	if strings.TrimSpace(parts[2]) == "" || strings.TrimSpace(parts[3]) == "" {
		t.Fatalf("venue/instrument must be non-empty in subject: %q", subject)
	}
}

func TestBuildQuarantineEnvelope_DeterministicErrorTextIgnoresCause(t *testing.T) {
	msg := nats.NewMsg("marketdata.bookdelta.v1.binance.BTCUSDT")
	msg.Data = []byte(`{"a":"b"}`)
	msg.Header.Set(nats.MsgIdHdr, "q-deterministic-cause")

	probA := problem.Wrap(errors.New("dial tcp 127.0.0.1:4222: i/o timeout"), problem.ValidationFailed, "decode failed")
	probB := problem.Wrap(errors.New("dial tcp 127.0.0.1:4333: i/o timeout"), problem.ValidationFailed, "decode failed")
	envA, p := buildQuarantineEnvelope(msg, envelope.Envelope{}, ingestReasonDecodeFailed, probA)
	if p != nil {
		t.Fatalf("buildQuarantineEnvelope(A) failed: %v", p)
	}
	envB, p := buildQuarantineEnvelope(msg, envelope.Envelope{}, ingestReasonDecodeFailed, probB)
	if p != nil {
		t.Fatalf("buildQuarantineEnvelope(B) failed: %v", p)
	}
	if string(envA.Payload) != string(envB.Payload) {
		t.Fatal("quarantine payload error text must stay deterministic across variable causes")
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
