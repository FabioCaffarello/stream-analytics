package jetstream

import (
	"container/heap"
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

// ---------------------------------------------------------------------------
// envelopeLess — exhaustive 6-level tie-break coverage
// ---------------------------------------------------------------------------

func TestEnvelopeLess_TsIngestPrimary(t *testing.T) {
	early := envelope.Envelope{TsIngest: 100}
	late := envelope.Envelope{TsIngest: 200}
	if !envelopeLess(early, late) {
		t.Fatal("lower TsIngest must sort first")
	}
	if envelopeLess(late, early) {
		t.Fatal("higher TsIngest must not sort first")
	}
}

func TestEnvelopeLess_VenueTieBreak(t *testing.T) {
	a := envelope.Envelope{TsIngest: 1, Venue: "binance"}
	b := envelope.Envelope{TsIngest: 1, Venue: "kraken"}
	if !envelopeLess(a, b) {
		t.Fatal("binance < kraken alphabetically")
	}
	if envelopeLess(b, a) {
		t.Fatal("kraken !< binance")
	}
}

func TestEnvelopeLess_InstrumentTieBreak(t *testing.T) {
	a := envelope.Envelope{TsIngest: 1, Venue: "binance", Instrument: "BTCUSDT"}
	b := envelope.Envelope{TsIngest: 1, Venue: "binance", Instrument: "ETHUSDT"}
	if !envelopeLess(a, b) {
		t.Fatal("BTCUSDT < ETHUSDT alphabetically")
	}
	if envelopeLess(b, a) {
		t.Fatal("ETHUSDT !< BTCUSDT")
	}
}

func TestEnvelopeLess_TypeTieBreak(t *testing.T) {
	a := envelope.Envelope{TsIngest: 1, Venue: "binance", Instrument: "BTCUSDT", Type: "marketdata.bookdelta"}
	b := envelope.Envelope{TsIngest: 1, Venue: "binance", Instrument: "BTCUSDT", Type: "marketdata.trade"}
	if !envelopeLess(a, b) {
		t.Fatal("bookdelta < trade alphabetically")
	}
	if envelopeLess(b, a) {
		t.Fatal("trade !< bookdelta")
	}
}

func TestEnvelopeLess_SeqTieBreak(t *testing.T) {
	a := envelope.Envelope{TsIngest: 1, Venue: "binance", Instrument: "BTCUSDT", Type: "marketdata.trade", Seq: 10}
	b := envelope.Envelope{TsIngest: 1, Venue: "binance", Instrument: "BTCUSDT", Type: "marketdata.trade", Seq: 20}
	if !envelopeLess(a, b) {
		t.Fatal("seq 10 < seq 20")
	}
	if envelopeLess(b, a) {
		t.Fatal("seq 20 !< seq 10")
	}
}

func TestEnvelopeLess_IdempotencyKeyTieBreak(t *testing.T) {
	a := envelope.Envelope{TsIngest: 1, Venue: "binance", Instrument: "BTCUSDT", Type: "marketdata.trade", Seq: 1, IdempotencyKey: "aaa"}
	b := envelope.Envelope{TsIngest: 1, Venue: "binance", Instrument: "BTCUSDT", Type: "marketdata.trade", Seq: 1, IdempotencyKey: "bbb"}
	if !envelopeLess(a, b) {
		t.Fatal("aaa < bbb lexicographically")
	}
	if envelopeLess(b, a) {
		t.Fatal("bbb !< aaa")
	}
}

func TestEnvelopeLess_EqualEnvelopesNotLess(t *testing.T) {
	e := envelope.Envelope{
		TsIngest: 1, Venue: "binance", Instrument: "BTCUSDT",
		Type: "marketdata.trade", Seq: 1, IdempotencyKey: "x",
	}
	if envelopeLess(e, e) {
		t.Fatal("equal envelopes must return false (strict less)")
	}
}

func TestEnvelopeLess_CaseInsensitiveVenue(t *testing.T) {
	a := envelope.Envelope{TsIngest: 1, Venue: "BINANCE"}
	b := envelope.Envelope{TsIngest: 1, Venue: "binance"}
	if envelopeLess(a, b) || envelopeLess(b, a) {
		t.Fatal("venue comparison must be case-insensitive")
	}
}

func TestEnvelopeLess_CaseInsensitiveInstrument(t *testing.T) {
	a := envelope.Envelope{TsIngest: 1, Venue: "binance", Instrument: "btcusdt"}
	b := envelope.Envelope{TsIngest: 1, Venue: "binance", Instrument: "BTCUSDT"}
	if envelopeLess(a, b) || envelopeLess(b, a) {
		t.Fatal("instrument comparison must be case-insensitive")
	}
}

// ---------------------------------------------------------------------------
// envelopeHeap — merge buffer ordering invariant
// ---------------------------------------------------------------------------

func TestEnvelopeHeap_PopOrderMatchesEnvelopeLess(t *testing.T) {
	envs := []envelope.Envelope{
		{TsIngest: 30, Venue: "kraken", Instrument: "ETHUSDT", Type: "marketdata.trade", Seq: 1, IdempotencyKey: "c"},
		{TsIngest: 10, Venue: "binance", Instrument: "BTCUSDT", Type: "marketdata.bookdelta", Seq: 1, IdempotencyKey: "a"},
		{TsIngest: 20, Venue: "binance", Instrument: "BTCUSDT", Type: "marketdata.trade", Seq: 2, IdempotencyKey: "b"},
		{TsIngest: 10, Venue: "binance", Instrument: "BTCUSDT", Type: "marketdata.trade", Seq: 1, IdempotencyKey: "a"},
		{TsIngest: 10, Venue: "binance", Instrument: "ETHUSDT", Type: "marketdata.trade", Seq: 1, IdempotencyKey: "d"},
	}

	order := newEnvelopeHeap(len(envs) + 10)
	h := &order.items
	heap.Init(h)
	for _, e := range envs {
		heap.Push(h, orderedMsg{env: e})
	}

	var prev envelope.Envelope
	hasPrev := false
	for h.Len() > 0 {
		item := heap.Pop(h).(orderedMsg)
		if hasPrev && envelopeLess(item.env, prev) {
			t.Fatalf("heap order violation: popped %+v after %+v", item.env, prev)
		}
		prev = item.env
		hasPrev = true
	}
}

func TestEnvelopeHeap_SingleElement(t *testing.T) {
	order := newEnvelopeHeap(4)
	h := &order.items
	heap.Init(h)
	heap.Push(h, orderedMsg{env: envelope.Envelope{TsIngest: 42}})
	if h.Len() != 1 {
		t.Fatalf("len=%d want=1", h.Len())
	}
	item := heap.Pop(h).(orderedMsg)
	if item.env.TsIngest != 42 {
		t.Fatalf("TsIngest=%d want=42", item.env.TsIngest)
	}
}

func TestNewEnvelopeHeap_ZeroDefaultsToDefault(t *testing.T) {
	order := newEnvelopeHeap(0)
	if order.maxSize != defaultReplayMergeBuffer {
		t.Fatalf("maxSize=%d want=%d", order.maxSize, defaultReplayMergeBuffer)
	}
}

func TestNewEnvelopeHeap_NegativeDefaultsToDefault(t *testing.T) {
	order := newEnvelopeHeap(-5)
	if order.maxSize != defaultReplayMergeBuffer {
		t.Fatalf("maxSize=%d want=%d", order.maxSize, defaultReplayMergeBuffer)
	}
}

// ---------------------------------------------------------------------------
// withReplaySourceDefaults — branch coverage
// ---------------------------------------------------------------------------

func TestReplaySourceDefaults_AllDeliverWhenNoWindow(t *testing.T) {
	cfg := withReplaySourceDefaults(ReplaySourceConfig{
		URL:             "nats://127.0.0.1:4222",
		StreamName:      "MARKETDATA",
		SubjectFilter:   "marketdata.>",
		ConsumerDurable: "test",
	})
	if cfg.DeliverPolicy != replayDeliverAll {
		t.Fatalf("deliver policy=%q want=%q when no window", cfg.DeliverPolicy, replayDeliverAll)
	}
}

func TestReplaySourceDefaults_ByStartTimeWhenWindowSet(t *testing.T) {
	cfg := withReplaySourceDefaults(ReplaySourceConfig{
		URL:             "nats://127.0.0.1:4222",
		StreamName:      "MARKETDATA",
		SubjectFilter:   "marketdata.>",
		ConsumerDurable: "test",
		Window:          10 * time.Minute,
	})
	if cfg.DeliverPolicy != replayDeliverByStartTime {
		t.Fatalf("deliver policy=%q want=%q when window set", cfg.DeliverPolicy, replayDeliverByStartTime)
	}
}

func TestReplaySourceDefaults_DecodeErrorModeDefault(t *testing.T) {
	cfg := withReplaySourceDefaults(ReplaySourceConfig{
		URL:           "nats://127.0.0.1:4222",
		StreamName:    "MARKETDATA",
		SubjectFilter: "marketdata.>",
	})
	if cfg.DecodeErrorMode != replayDecodeErrorFail {
		t.Fatalf("decode_error_mode=%q want=%q", cfg.DecodeErrorMode, replayDecodeErrorFail)
	}
}

func TestReplaySourceDefaults_FillsMissingBufferSizes(t *testing.T) {
	cfg := withReplaySourceDefaults(ReplaySourceConfig{
		URL:           "nats://127.0.0.1:4222",
		StreamName:    "MARKETDATA",
		SubjectFilter: "marketdata.>",
	})
	if cfg.MergeBufferSize != defaultReplayMergeBuffer {
		t.Fatalf("merge_buffer=%d want=%d", cfg.MergeBufferSize, defaultReplayMergeBuffer)
	}
	if cfg.OutputBufferSize != defaultReplayOutputBuffer {
		t.Fatalf("output_buffer=%d want=%d", cfg.OutputBufferSize, defaultReplayOutputBuffer)
	}
	if cfg.FetchTimeout != defaultReplayFetchTimeout {
		t.Fatalf("fetch_timeout=%v want=%v", cfg.FetchTimeout, defaultReplayFetchTimeout)
	}
	if cfg.IdleTimeoutLimit != defaultReplayIdleTimeouts {
		t.Fatalf("idle_timeouts=%d want=%d", cfg.IdleTimeoutLimit, defaultReplayIdleTimeouts)
	}
}

func TestReplaySourceDefaults_PreservesExplicitValues(t *testing.T) {
	cfg := withReplaySourceDefaults(ReplaySourceConfig{
		URL:              "nats://custom:4222",
		StreamName:       "CUSTOM",
		SubjectFilter:    "marketdata.>",
		ConsumerDurable:  "custom-durable",
		MergeBufferSize:  512,
		OutputBufferSize: 64,
		FetchTimeout:     2 * time.Second,
		IdleTimeoutLimit: 5,
		MaxMessages:      5000,
		DecodeErrorMode:  replayDecodeErrorSkip,
		DeliverPolicy:    replayDeliverAll,
		Window:           10 * time.Minute,
	})
	if cfg.MergeBufferSize != 512 {
		t.Fatalf("merge_buffer=%d want=512", cfg.MergeBufferSize)
	}
	if cfg.OutputBufferSize != 64 {
		t.Fatalf("output_buffer=%d want=64", cfg.OutputBufferSize)
	}
	if cfg.FetchTimeout != 2*time.Second {
		t.Fatalf("fetch_timeout=%v want=2s", cfg.FetchTimeout)
	}
	if cfg.IdleTimeoutLimit != 5 {
		t.Fatalf("idle_timeouts=%d want=5", cfg.IdleTimeoutLimit)
	}
	if cfg.MaxMessages != 5000 {
		t.Fatalf("max_messages=%d want=5000", cfg.MaxMessages)
	}
	if cfg.DecodeErrorMode != replayDecodeErrorSkip {
		t.Fatalf("decode_error_mode=%q want=skip", cfg.DecodeErrorMode)
	}
	if cfg.DeliverPolicy != replayDeliverAll {
		t.Fatalf("deliver_policy=%q want=all (explicit override)", cfg.DeliverPolicy)
	}
}

// ---------------------------------------------------------------------------
// validateReplaySourceConfig — exhaustive negative paths
// ---------------------------------------------------------------------------

func TestReplaySourceValidation_EmptyURL(t *testing.T) {
	cfg := validReplaySourceCfg()
	cfg.URL = ""
	expectValidationFailed(t, cfg, "empty URL")
}

func TestReplaySourceValidation_EmptyStreamName(t *testing.T) {
	cfg := validReplaySourceCfg()
	cfg.StreamName = ""
	expectValidationFailed(t, cfg, "empty stream name")
}

func TestReplaySourceValidation_EmptySubjectFilter(t *testing.T) {
	cfg := validReplaySourceCfg()
	cfg.SubjectFilter = ""
	expectValidationFailed(t, cfg, "empty subject filter")
}

func TestReplaySourceValidation_EmptyConsumerDurable(t *testing.T) {
	cfg := validReplaySourceCfg()
	cfg.ConsumerDurable = ""
	expectValidationFailed(t, cfg, "empty consumer durable")
}

func TestReplaySourceValidation_NonPositiveAckWait(t *testing.T) {
	cfg := validReplaySourceCfg()
	cfg.AckWait = 0
	expectValidationFailed(t, cfg, "zero ack_wait")
}

func TestReplaySourceValidation_NonPositiveMaxAckPending(t *testing.T) {
	cfg := validReplaySourceCfg()
	cfg.MaxAckPending = 0
	expectValidationFailed(t, cfg, "zero max_ack_pending")
}

func TestReplaySourceValidation_NonPositiveMaxDeliver(t *testing.T) {
	cfg := validReplaySourceCfg()
	cfg.MaxDeliver = 0
	expectValidationFailed(t, cfg, "zero max_deliver")
}

func TestReplaySourceValidation_NonPositiveFetchTimeout(t *testing.T) {
	cfg := validReplaySourceCfg()
	cfg.FetchTimeout = 0
	expectValidationFailed(t, cfg, "zero fetch_timeout")
}

func TestReplaySourceValidation_NonPositiveIdleTimeoutLimit(t *testing.T) {
	cfg := validReplaySourceCfg()
	cfg.IdleTimeoutLimit = 0
	expectValidationFailed(t, cfg, "zero idle_timeout_limit")
}

func TestReplaySourceValidation_NonPositiveMergeBufferSize(t *testing.T) {
	cfg := validReplaySourceCfg()
	cfg.MergeBufferSize = 0
	expectValidationFailed(t, cfg, "zero merge_buffer_size")
}

func TestReplaySourceValidation_NonPositiveOutputBufferSize(t *testing.T) {
	cfg := validReplaySourceCfg()
	cfg.OutputBufferSize = 0
	expectValidationFailed(t, cfg, "zero output_buffer_size")
}

func TestReplaySourceValidation_ZeroMaxMessages(t *testing.T) {
	cfg := validReplaySourceCfg()
	cfg.MaxMessages = 0
	expectValidationFailed(t, cfg, "zero max_messages")
}

func TestReplaySourceValidation_NonPositiveMaxBytes(t *testing.T) {
	cfg := validReplaySourceCfg()
	cfg.MaxBytes = 0
	expectValidationFailed(t, cfg, "zero max_bytes")
}

func TestReplaySourceValidation_NonPositiveMaxAge(t *testing.T) {
	cfg := validReplaySourceCfg()
	cfg.MaxAge = 0
	expectValidationFailed(t, cfg, "zero max_age")
}

func TestReplaySourceValidation_NonPositiveDedupWindow(t *testing.T) {
	cfg := validReplaySourceCfg()
	cfg.DedupWindow = 0
	expectValidationFailed(t, cfg, "zero dedup_window")
}

func TestReplaySourceValidation_InvalidDeliverPolicy(t *testing.T) {
	cfg := validReplaySourceCfg()
	cfg.DeliverPolicy = "last"
	expectValidationFailed(t, cfg, "invalid deliver_policy")
}

func TestReplaySourceValidation_InvalidDecodeErrorMode(t *testing.T) {
	cfg := validReplaySourceCfg()
	cfg.DecodeErrorMode = "panic"
	expectValidationFailed(t, cfg, "invalid decode_error_mode")
}

func TestReplaySourceValidation_ValidCfgPasses(t *testing.T) {
	cfg := validReplaySourceCfg()
	if p := validateReplaySourceConfig(cfg); p != nil {
		t.Fatalf("expected valid config to pass, got: %v", p)
	}
}

// ---------------------------------------------------------------------------
// replayDeliverPolicy — branch coverage
// ---------------------------------------------------------------------------

func TestReplayDeliverPolicy_All(t *testing.T) {
	_, p := replayDeliverPolicy("all")
	if p != nil {
		t.Fatalf("unexpected error: %v", p)
	}
}

func TestReplayDeliverPolicy_ByStartTime(t *testing.T) {
	_, p := replayDeliverPolicy("by_start_time")
	if p != nil {
		t.Fatalf("unexpected error: %v", p)
	}
}

func TestReplayDeliverPolicy_Invalid(t *testing.T) {
	_, p := replayDeliverPolicy("new")
	if p == nil {
		t.Fatal("expected error for unsupported policy")
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func validReplaySourceCfg() ReplaySourceConfig {
	return ReplaySourceConfig{
		URL:              "nats://127.0.0.1:4222",
		StreamName:       "MARKETDATA",
		SubjectFilter:    "marketdata.>",
		ConsumerDurable:  "test-replay",
		DedupWindow:      5 * time.Minute,
		MaxAge:           24 * time.Hour,
		MaxBytes:         10_000_000_000,
		AckWait:          30 * time.Second,
		MaxAckPending:    1024,
		MaxDeliver:       10,
		DeliverPolicy:    replayDeliverAll,
		MaxMessages:      100_000,
		FetchTimeout:     750 * time.Millisecond,
		IdleTimeoutLimit: 2,
		MergeBufferSize:  4096,
		OutputBufferSize: 256,
		DecodeErrorMode:  replayDecodeErrorFail,
	}
}

func expectValidationFailed(t *testing.T, cfg ReplaySourceConfig, label string) {
	t.Helper()
	p := validateReplaySourceConfig(cfg)
	if p == nil {
		t.Fatalf("[%s] expected validation failure, got nil", label)
	}
	if p.Code != problem.ValidationFailed {
		t.Fatalf("[%s] problem code=%s want=%s", label, p.Code, problem.ValidationFailed)
	}
}
