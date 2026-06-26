package contracts_test

import (
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/contracts"
)

func TestDecodeEnvelopeV1FromHTTP_ValidEnvelope(t *testing.T) {
	raw, p := contracts.MarshalEnvelopeV1FromPayload("runtime.snapshot", []byte(`{"ok":true}`), "application/json")
	if p != nil {
		t.Fatalf("MarshalEnvelopeV1FromPayload: %v", p)
	}

	out, p := contracts.DecodeEnvelopeV1FromHTTP(raw)
	if p != nil {
		t.Fatalf("DecodeEnvelopeV1FromHTTP: %v", p)
	}
	if got, want := out.Type, "runtime.snapshot"; got != want {
		t.Fatalf("type=%q want=%q", got, want)
	}
	if got, want := out.ContentType, "application/json"; got != want {
		t.Fatalf("content_type=%q want=%q", got, want)
	}
	if string(out.PayloadJSONBytes) != `{"ok":true}` {
		t.Fatalf("payload=%q want=%q", string(out.PayloadJSONBytes), `{"ok":true}`)
	}
}

func TestDecodeEnvelopeV1FromHTTP_RejectsInvalidJSONPayload(t *testing.T) {
	raw, p := contracts.MarshalEnvelopeV1FromPayload("runtime.snapshot", []byte(`not-json`), "application/json")
	if p != nil {
		t.Fatalf("MarshalEnvelopeV1FromPayload: %v", p)
	}

	if _, p := contracts.DecodeEnvelopeV1FromHTTP(raw); p == nil {
		t.Fatal("expected validation problem for invalid payload JSON")
	}
}
