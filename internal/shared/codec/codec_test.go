package codec_test

import (
	"testing"

	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/problem"
)

type sample struct {
	Venue   string  `json:"venue"`
	Price   float64 `json:"price"`
	Version int     `json:"version"`
}

type sampleCodec struct{}

func (sampleCodec) Encode(v any) ([]byte, *problem.Problem) {
	return codec.Marshal(v)
}

func (sampleCodec) Decode(data []byte) (any, *problem.Problem) {
	var s sample
	if p := codec.Unmarshal(data, &s); p != nil {
		return nil, p
	}
	return s, nil
}

func TestRoundTrip(t *testing.T) {
	orig := sample{Venue: "binance", Price: 50_000.25, Version: 1}

	data, p := codec.Marshal(orig)
	if p != nil {
		t.Fatalf("Marshal failed: %s", p)
	}
	if len(data) == 0 {
		t.Fatal("marshaled data must not be empty")
	}

	var got sample
	if p := codec.Unmarshal(data, &got); p != nil {
		t.Fatalf("Unmarshal failed: %s", p)
	}
	if got != orig {
		t.Errorf("round-trip mismatch: got %+v; want %+v", got, orig)
	}
}

func TestUnmarshal_invalid(t *testing.T) {
	var out sample
	p := codec.Unmarshal([]byte("not json"), &out)
	if p == nil {
		t.Fatal("expected problem on invalid input")
	}
	if p.Code != problem.Internal {
		t.Errorf("code = %s; want INTERNAL", p.Code)
	}
	if p.Cause == nil {
		t.Error("cause must be set")
	}
}

func TestRegistry_RegisterAndLookup(t *testing.T) {
	reg := codec.NewRegistry()
	key := codec.SchemaKey{Type: "marketdata.trade", Version: 1, Format: codec.FormatJSON}

	c := sampleCodec{}
	if p := reg.Register(key, c, c); p != nil {
		t.Fatalf("register: %v", p)
	}

	if _, ok := reg.Encoder(key); !ok {
		t.Fatal("expected encoder to be registered")
	}
	if _, ok := reg.Decoder(key); !ok {
		t.Fatal("expected decoder to be registered")
	}
}

func TestMarshalPayload_success(t *testing.T) {
	orig := sample{Venue: "binance", Price: 1.0, Version: 1}
	data, p := codec.MarshalPayload("marketdata.trade", 1, orig)
	if p != nil {
		t.Fatalf("MarshalPayload: %s", p)
	}
	if len(data) == 0 {
		t.Error("data must not be empty")
	}
}

func TestUnmarshalPayload_errorHasContext(t *testing.T) {
	var out sample
	p := codec.UnmarshalPayload("marketdata.trade", 1, []byte("bad json"), &out)
	if p == nil {
		t.Fatal("expected problem")
	}
	if p.Code != problem.Internal {
		t.Errorf("code = %s; want SYS_INTERNAL", p.Code)
	}
	if p.Details["event_type"] != "marketdata.trade" {
		t.Errorf("missing event_type detail: %v", p.Details)
	}
	if p.Details["version"] != 1 {
		t.Errorf("missing version detail: %v", p.Details)
	}
	if _, ok := p.Details["payload_size"]; !ok {
		t.Error("missing payload_size detail")
	}
}
