package app

import (
	"testing"

	signalsdomain "github.com/market-raccoon/internal/core/signals/domain"
)

func TestSignalRateLimiter_DedupWithinWindow(t *testing.T) {
	limiter := NewSignalRateLimiter(DefaultRateLimitPolicy())
	signal := testSignal(1700000000000, 0.70)

	if decision := limiter.Allow(signal); !decision.Allowed {
		t.Fatalf("first decision=%+v want Allowed", decision)
	}
	if decision := limiter.Allow(signal); !decision.Deduplicated {
		t.Fatalf("second decision=%+v want Deduplicated", decision)
	}

	boosted := signal
	boosted.Confidence = 0.90 // > 1.2x 0.70
	if decision := limiter.Allow(boosted); !decision.Allowed {
		t.Fatalf("boosted decision=%+v want Allowed", decision)
	}
}

func TestSignalRateLimiter_RateLimitPerMinute(t *testing.T) {
	policy := DefaultRateLimitPolicy()
	policy.GlobalRateLimitMin = 1000
	limiter := NewSignalRateLimiter(policy)

	for i := 0; i < 10; i++ {
		signal := testSignal(1700000000000+int64(i), 0.80)
		signal.Kind = "absorption_" + string(rune('a'+i))
		if decision := limiter.Allow(signal); !decision.Allowed {
			t.Fatalf("signal %d decision=%+v want Allowed", i, decision)
		}
	}
	blocked := testSignal(1700000000500, 0.81)
	blocked.Kind = "absorption_blocked"
	if decision := limiter.Allow(blocked); !decision.RateLimited {
		t.Fatalf("11th decision=%+v want RateLimited", decision)
	}
}

func testSignal(ts int64, confidence float64) signalsdomain.CompositeSignalV1 {
	return signalsdomain.CompositeSignalV1{
		Kind:       "absorption",
		Venue:      "binance",
		Instrument: "BTCUSDT",
		Timeframe:  "1m",
		TsServer:   ts,
		Severity:   "high",
		Confidence: confidence,
		Evidence: []signalsdomain.SignalFeature{
			{Label: "volume_ratio", Value: "2.100000"},
		},
		Reason:      "test signal",
		Seq:         1,
		SourceKinds: []string{"absorption"},
	}
}
