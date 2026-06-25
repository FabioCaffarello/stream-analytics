package contracts_test

import (
	"math"
	"strconv"
	"testing"

	marketdomain "github.com/market-raccoon/internal/core/marketdata/domain"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/problem"
)

func TestPayloadCodecG115_MaxInt32Version_NoOverflow(t *testing.T) {
	reg := codec.NewRegistry()
	key := codec.SchemaKey{Type: "marketdata.trade", Version: math.MaxInt32, Format: codec.FormatJSON}
	jsonCodec := codec.JSONCodec[marketdomain.TradeTickV1]{}
	if p := reg.Register(key, jsonCodec, jsonCodec); p != nil {
		t.Fatalf("register codec: %v", p)
	}

	env := envelope.Envelope{
		Type:    "marketdata.trade",
		Version: int(math.MaxInt32),
	}
	in := marketdomain.TradeTickV1{
		Price:     65000.25,
		Size:      0.42,
		Side:      "buy",
		TradeID:   "t-123",
		Timestamp: 1700000000123,
	}

	assertNotPanic(t, func() {
		data, p := envelope.MarshalPayload(reg, env, in)
		if p != nil {
			t.Fatalf("MarshalPayload: %v", p)
		}
		outAny, p := envelope.UnmarshalPayload(reg, env, data)
		if p != nil {
			t.Fatalf("UnmarshalPayload: %v", p)
		}
		out, ok := outAny.(marketdomain.TradeTickV1)
		if !ok {
			t.Fatalf("decoded type = %T; want %T", outAny, marketdomain.TradeTickV1{})
		}
		if out != in {
			t.Fatalf("roundtrip mismatch: got %+v want %+v", out, in)
		}
	})
}

func TestPayloadCodecG115_VersionOverflowRejected_NoPanic(t *testing.T) {
	if strconv.IntSize <= 32 {
		t.Skip("int overflow path requires 64-bit int")
	}

	reg := codec.NewRegistry()
	env := envelope.Envelope{
		Type:    "marketdata.trade",
		Version: int(math.MaxInt32) + 1,
	}

	assertNotPanic(t, func() {
		_, p := envelope.MarshalPayload(reg, env, marketdomain.TradeTickV1{})
		if p == nil {
			t.Fatal("expected validation problem for version overflow")
		}
		if p.Code != problem.ValidationFailed {
			t.Fatalf("problem code = %s; want %s", p.Code, problem.ValidationFailed)
		}
		if got := p.Details["field"]; got != "version" {
			t.Fatalf("problem field = %v; want version", got)
		}
	})
}

func assertNotPanic(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("unexpected panic: %v", r)
		}
	}()
	fn()
}
