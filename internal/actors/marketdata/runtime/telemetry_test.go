package mdruntime

import (
	"testing"
	"time"
)

func TestParserTelemetry_RecordSkipByReason(t *testing.T) {
	tel := newParserTelemetry()

	tel.recordSkip("binance", "aggTrade", "unsupported_event", "")
	tel.recordSkip("binance", "aggTrade", "parse_error", "VALIDATION_FAILED")

	if got, want := tel.total, uint64(2); got != want {
		t.Fatalf("total = %d, want %d", got, want)
	}
	if got, want := tel.skipped, uint64(2); got != want {
		t.Fatalf("skipped = %d, want %d", got, want)
	}
	if got, want := tel.bySkipReason["unsupported_event"], uint64(1); got != want {
		t.Fatalf("unsupported_event count = %d, want %d", got, want)
	}
	if got, want := tel.bySkipReason["parse_error"], uint64(1); got != want {
		t.Fatalf("parse_error count = %d, want %d", got, want)
	}
	if got, want := tel.parseErrorsByProblemCode["VALIDATION_FAILED"], uint64(1); got != want {
		t.Fatalf("parse error code count = %d, want %d", got, want)
	}
	if got, want := tel.byExchangeEventAndSkip["binance|aggTrade|unsupported_event"], uint64(1); got != want {
		t.Fatalf("byExchangeEventAndSkip count = %d, want %d", got, want)
	}
}

func TestParserTelemetry_ShouldSampleRateLimited(t *testing.T) {
	tel := newParserTelemetry()
	now := time.Now()

	if !tel.shouldSample(now, "parse_error") {
		t.Fatal("first sample should pass")
	}
	if tel.shouldSample(now.Add(5*time.Second), "parse_error") {
		t.Fatal("sample should be rate limited before sampleWindow")
	}
	if !tel.shouldSample(now.Add(31*time.Second), "parse_error") {
		t.Fatal("sample should pass after sampleWindow")
	}
}
