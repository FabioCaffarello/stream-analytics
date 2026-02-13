package replay

import (
	"testing"

	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/problem"
)

func TestReplaySequencerMonotonicPerStreamDeterministic(t *testing.T) {
	seq := NewReplaySequencer()
	envs := []envelope.Envelope{
		{Venue: "binance", Instrument: "BTC-USDT", Seq: 1, Meta: map[string]string{"instrument_market_type": "SPOT"}},
		{Venue: "bybit", Instrument: "BTC-USDT", Seq: 1, Meta: map[string]string{"instrument_market_type": "SPOT"}},
		{Venue: "binance", Instrument: "BTC-USDT", Seq: 2, Meta: map[string]string{"instrument_market_type": "SPOT"}},
	}
	for i := range envs {
		if p := seq.Enqueue(envs[i]); p != nil {
			t.Fatalf("Enqueue[%d]: %v", i, p)
		}
	}

	got1, p := seq.Next("BINANCE", "BTCUSDT:SPOT")
	if p != nil {
		t.Fatalf("Next BINANCE#1: %v", p)
	}
	got2, p := seq.Next("BYBIT", "BTCUSDT:SPOT")
	if p != nil {
		t.Fatalf("Next BYBIT#1: %v", p)
	}
	got3, p := seq.Next("binance", "btcusdt:spot")
	if p != nil {
		t.Fatalf("Next BINANCE#2: %v", p)
	}

	if got1 != 1 || got2 != 1 || got3 != 2 {
		t.Fatalf("unexpected sequence values got=(%d,%d,%d) want=(1,1,2)", got1, got2, got3)
	}
}

func TestReplaySequencerRejectsNonMonotonicFixtureStream(t *testing.T) {
	seq := NewReplaySequencer()
	first := envelope.Envelope{
		Venue:      "binance",
		Instrument: "BTC-USDT",
		Seq:        2,
	}
	second := envelope.Envelope{
		Venue:      "binance",
		Instrument: "BTC-USDT",
		Seq:        2,
	}

	if p := seq.Enqueue(first); p != nil {
		t.Fatalf("Enqueue first: %v", p)
	}
	p := seq.Enqueue(second)
	if p == nil {
		t.Fatal("expected non-monotonic enqueue failure")
	}
	if p.Code != problem.ValidationFailed {
		t.Fatalf("code=%s want=%s", p.Code, problem.ValidationFailed)
	}
}

func TestReplaySequencerNextWithoutQueueFailsDeterministically(t *testing.T) {
	seq := NewReplaySequencer()
	_, p := seq.Next("BINANCE", "BTCUSDT:SPOT")
	if p == nil {
		t.Fatal("expected missing queue failure")
	}
	if p.Code != problem.ValidationFailed {
		t.Fatalf("code=%s want=%s", p.Code, problem.ValidationFailed)
	}
}
