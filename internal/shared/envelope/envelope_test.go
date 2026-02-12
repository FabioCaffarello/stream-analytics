package envelope_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/problem"
)

type mapCodec struct{}

func (c mapCodec) Encode(v any) ([]byte, *problem.Problem) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, problem.New(problem.ValidationFailed, "mapCodec expects map[string]any")
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, problem.Wrap(err, problem.Internal, "mapCodec marshal failed")
	}
	return b, nil
}

func (c mapCodec) Decode(b []byte) (any, *problem.Problem) {
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, problem.Wrap(err, problem.Internal, "mapCodec unmarshal failed")
	}
	return m, nil
}

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
	if e.ContentType != envelope.ContentTypeJSON {
		t.Errorf("content_type = %q; want %q", e.ContentType, envelope.ContentTypeJSON)
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
		{"invalid content_type", func(e *envelope.Envelope) { e.ContentType = "application/xml" }, "content_type"},
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

func TestNormalizeContentType(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		want        string
		wantProblem bool
	}{
		{name: "empty defaults to json", input: "", want: envelope.ContentTypeJSON},
		{name: "space defaults to json", input: "   ", want: envelope.ContentTypeJSON},
		{name: "json passthrough", input: envelope.ContentTypeJSON, want: envelope.ContentTypeJSON},
		{name: "proto passthrough", input: envelope.ContentTypeProto, want: envelope.ContentTypeProto},
		{name: "uppercase canonicalized", input: "APPLICATION/JSON", want: envelope.ContentTypeJSON},
		{name: "invalid rejected", input: "application/xml", wantProblem: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, p := envelope.NormalizeContentType(tc.input)
			if tc.wantProblem {
				if p == nil {
					t.Fatal("expected problem, got nil")
				}
				return
			}
			if p != nil {
				t.Fatalf("unexpected problem: %v", p)
			}
			if got != tc.want {
				t.Fatalf("NormalizeContentType(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestMarshalPayload_DefaultContentTypeJSON(t *testing.T) {
	reg := codec.NewRegistry()
	key := codec.SchemaKey{Type: "marketdata.trade", Version: 1, Format: codec.FormatJSON}
	jsonCodec := mapCodec{}
	if p := reg.Register(key, jsonCodec, jsonCodec); p != nil {
		t.Fatalf("register codec: %v", p)
	}

	envIn := envelope.Envelope{Type: "marketdata.trade", Version: 1}
	data, p := envelope.MarshalPayload(reg, envIn, map[string]any{"price": 1.23})
	if p != nil {
		t.Fatalf("MarshalPayload: %v", p)
	}
	if len(data) == 0 {
		t.Fatal("expected encoded payload bytes")
	}
}

func TestMarshalPayload_MissingCodec(t *testing.T) {
	reg := codec.NewRegistry()
	envIn := envelope.Envelope{Type: "marketdata.trade", Version: 1}

	_, p := envelope.MarshalPayload(reg, envIn, map[string]any{"price": 1.23})
	if p == nil {
		t.Fatal("expected problem when encoder is not registered")
	}
	if p.Code != problem.ValidationFailed {
		t.Fatalf("problem code = %s; want %s", p.Code, problem.ValidationFailed)
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

func TestSubjectFromEnvelope(t *testing.T) {
	env := validEnvelope()
	env.Instrument = "BTC-USDT"
	got := envelope.SubjectFromEnvelope(env)
	want := "marketdata.trade.v1.binance.BTCUSDT"
	if got != want {
		t.Fatalf("SubjectFromEnvelope = %q, want %q", got, want)
	}
}

func TestSubjectFromEnvelope_Deterministic(t *testing.T) {
	env := validEnvelope()
	env.Venue = "BINANCE"
	env.Instrument = "btc_usdt"
	s1 := envelope.SubjectFromEnvelope(env)
	s2 := envelope.SubjectFromEnvelope(env)
	if s1 != s2 {
		t.Fatalf("subject must be deterministic: %q vs %q", s1, s2)
	}
}

func TestMarshalBinary_RoundTrip(t *testing.T) {
	envIn := validEnvelope()
	envIn.ContentType = envelope.ContentTypeJSON

	data, p := envelope.MarshalBinary(envIn)
	if p != nil {
		t.Fatalf("MarshalBinary: %v", p)
	}
	if len(data) == 0 {
		t.Fatal("MarshalBinary returned empty payload")
	}

	envOut, p := envelope.UnmarshalBinary(data)
	if p != nil {
		t.Fatalf("UnmarshalBinary: %v", p)
	}
	if envOut.Type != envIn.Type || envOut.Instrument != envIn.Instrument || envOut.IdempotencyKey != envIn.IdempotencyKey {
		t.Fatalf("roundtrip mismatch: got=%+v want=%+v", envOut, envIn)
	}
}

func TestUnmarshalBinary_InvalidPayload(t *testing.T) {
	_, p := envelope.UnmarshalBinary([]byte(`{bad`))
	if p == nil {
		t.Fatal("expected error for invalid envelope payload")
	}
	if p.Code != problem.ValidationFailed {
		t.Fatalf("problem code=%s want=%s", p.Code, problem.ValidationFailed)
	}
}

func TestMarshalBinary_ValidationError(t *testing.T) {
	_, p := envelope.MarshalBinary(envelope.Envelope{})
	if p == nil {
		t.Fatal("expected validation problem")
	}
	if !strings.HasPrefix(string(p.Code), "VAL_") {
		t.Fatalf("expected validation code, got %s", p.Code)
	}
}
