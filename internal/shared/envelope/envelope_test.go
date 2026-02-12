package envelope_test

import (
	"testing"

	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/problem"
)

func validEnvelope() envelope.Envelope {
	return envelope.Envelope{
		Type:           "marketdata.trade",
		Version:        1,
		Venue:          "binance",
		Instrument:     "BTC-PERP",
		TsExchange:     1710000000000,
		TsIngest:       1710000005000,
		Seq:            1,
		IdempotencyKey: "binance-BTC-PERP-123456",
		Payload:        []byte(`{"price":50000}`),
	}
}

func TestValidate_valid(t *testing.T) {
	e := validEnvelope()
	if p := e.Validate(); p != nil {
		t.Errorf("unexpected problem: %s", p)
	}
}

func TestValidate_missingFields(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*envelope.Envelope)
		field  string
	}{
		{"empty type", func(e *envelope.Envelope) { e.Type = "" }, "type"},
		{"whitespace type", func(e *envelope.Envelope) { e.Type = "   " }, "type"},
		{"version zero", func(e *envelope.Envelope) { e.Version = 0 }, "version"},
		{"version negative", func(e *envelope.Envelope) { e.Version = -1 }, "version"},
		{"empty venue", func(e *envelope.Envelope) { e.Venue = "" }, "venue"},
		{"empty instrument", func(e *envelope.Envelope) { e.Instrument = "" }, "instrument"},
		{"zero ts_ingest", func(e *envelope.Envelope) { e.TsIngest = 0 }, "ts_ingest"},
		{"negative seq", func(e *envelope.Envelope) { e.Seq = -1 }, "seq"},
		{"empty idempotency_key", func(e *envelope.Envelope) { e.IdempotencyKey = "" }, "idempotency_key"},
		{"empty payload", func(e *envelope.Envelope) { e.Payload = nil }, "payload"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := validEnvelope()
			tc.mutate(&e)
			p := e.Validate()
			if p == nil {
				t.Fatalf("expected problem, got nil")
			}
			if p.Code != problem.ValidationFailed {
				t.Errorf("expected VALIDATION_FAILED, got %s", p.Code)
			}
			if p.Details["field"] != tc.field {
				t.Errorf("expected field=%q, got %q", tc.field, p.Details["field"])
			}
		})
	}
}

func TestTopicKey_deterministic(t *testing.T) {
	e := validEnvelope()
	k1 := e.TopicKey()
	k2 := e.TopicKey()
	if k1 != k2 {
		t.Error("TopicKey must be deterministic")
	}

	want := "marketdata.trade.binance.btc-perp"
	if k1 != want {
		t.Errorf("TopicKey = %q; want %q", k1, want)
	}
}

func TestTopicKey_lowercase(t *testing.T) {
	e := validEnvelope()
	e.Venue = "BINANCE"
	e.Instrument = "BTC-PERP"
	e.Type = "MARKETDATA.TRADE"

	want := "marketdata.trade.binance.btc-perp"
	if got := e.TopicKey(); got != want {
		t.Errorf("TopicKey = %q; want %q", got, want)
	}
}

func TestWithMeta_immutable(t *testing.T) {
	e := validEnvelope()
	e2 := e.WithMeta("source", "ws")

	if e2.Meta["source"] != "ws" {
		t.Error("meta not set on copy")
	}
	if e.Meta != nil && e.Meta["source"] == "ws" {
		t.Error("original must not be mutated")
	}
}

func TestWithMeta_chaining(t *testing.T) {
	e := validEnvelope()
	e2 := e.WithMeta("k1", "v1").WithMeta("k2", "v2")

	if e2.Meta["k1"] != "v1" || e2.Meta["k2"] != "v2" {
		t.Error("meta chain broken")
	}
}
