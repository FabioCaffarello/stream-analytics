package mdruntime

import (
	"testing"
	"time"
)

func TestParserTelemetry_RecordSkipByReason(t *testing.T) {
	tel := newParserTelemetry()

	tel.recordSkip("binance", "aggTrade", "unsupported_event", "", "BTC-USDT", "btcusdt@aggTrade")
	tel.recordSkip("binance", "aggTrade", "parse_error", "VALIDATION_FAILED", "BTC-USDT", "btcusdt@aggTrade")

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
	if got, want := tel.byWSStream["btcusdt@aggTrade"], uint64(2); got != want {
		t.Fatalf("byWSStream count = %d, want %d", got, want)
	}
	if got, want := tel.byTicker["BTC-USDT"], uint64(2); got != want {
		t.Fatalf("byTicker count = %d, want %d", got, want)
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

func TestParserTelemetry_TopTickerSharePercent(t *testing.T) {
	tel := newParserTelemetry()
	tel.recordIngest("marketdata.trade", "BTC-USDT", "btcusdt@aggTrade")
	tel.recordIngest("marketdata.trade", "BTC-USDT", "btcusdt@aggTrade")
	tel.recordIngest("marketdata.trade", "ETH-USDT", "ethusdt@aggTrade")

	top := tel.topTickerSharePercent(2)
	if top["BTC-USDT"] < 66.0 || top["BTC-USDT"] > 67.0 {
		t.Fatalf("BTC-USDT share = %f, want approx 66.67", top["BTC-USDT"])
	}
	if top["ETH-USDT"] < 33.0 || top["ETH-USDT"] > 34.0 {
		t.Fatalf("ETH-USDT share = %f, want approx 33.33", top["ETH-USDT"])
	}
}

func TestParserTelemetry_RecordDepthSequenceGap(t *testing.T) {
	tel := newParserTelemetry()

	gap, _ := tel.recordDepthSequence("BTCUSDT", 101, 105)
	if gap {
		t.Fatal("first depth sample must not report gap")
	}

	gap, last := tel.recordDepthSequence("BTCUSDT", 108, 110)
	if !gap {
		t.Fatal("expected depth gap")
	}
	if last != 105 {
		t.Fatalf("last final = %d, want 105", last)
	}
	if tel.depthGapsTotal != 1 {
		t.Fatalf("depthGapsTotal = %d, want 1", tel.depthGapsTotal)
	}
	if tel.depthGapsBySymbol["BTCUSDT"] != 1 {
		t.Fatalf("depthGapsBySymbol[BTCUSDT] = %d, want 1", tel.depthGapsBySymbol["BTCUSDT"])
	}
}
