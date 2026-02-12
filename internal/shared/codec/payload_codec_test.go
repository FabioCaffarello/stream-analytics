package codec_test

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	marketdomain "github.com/market-raccoon/internal/core/marketdata/domain"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/contracts"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/problem"
)

func TestEncodeDecodePayload_Trade_JSONAndProtoSemanticEquivalence(t *testing.T) {
	bootstrapPayloadRegistry(t)

	in := marketdomain.TradeTickV1{
		Price:     65321.25,
		Size:      1.5,
		Side:      "sell",
		TradeID:   "trade-789",
		Timestamp: 1700001111222,
	}

	jsonBytes, p := codec.EncodePayload("marketdata.trade", 1, envelope.ContentTypeJSON, in)
	if p != nil {
		t.Fatalf("EncodePayload(JSON): %v", p)
	}
	protoBytes, p := codec.EncodePayload("marketdata.trade", 1, envelope.ContentTypeProto, in)
	if p != nil {
		t.Fatalf("EncodePayload(PROTO): %v", p)
	}

	jsonAny, p := codec.DecodePayload("marketdata.trade", 1, envelope.ContentTypeJSON, jsonBytes)
	if p != nil {
		t.Fatalf("DecodePayload(JSON): %v", p)
	}
	protoAny, p := codec.DecodePayload("marketdata.trade", 1, envelope.ContentTypeProto, protoBytes)
	if p != nil {
		t.Fatalf("DecodePayload(PROTO): %v", p)
	}

	jsonOut, ok := jsonAny.(marketdomain.TradeTickV1)
	if !ok {
		t.Fatalf("json decoded type = %T, want %T", jsonAny, marketdomain.TradeTickV1{})
	}
	protoOut, ok := protoAny.(marketdomain.TradeTickV1)
	if !ok {
		t.Fatalf("proto decoded type = %T, want %T", protoAny, marketdomain.TradeTickV1{})
	}

	if jsonOut != in {
		t.Fatalf("json roundtrip mismatch: got %+v want %+v", jsonOut, in)
	}
	if protoOut != in {
		t.Fatalf("proto semantic mismatch: got %+v want %+v", protoOut, in)
	}
}

func TestEncodeDecodePayload_BookDelta_JSONAndProtoSemanticEquivalence(t *testing.T) {
	bootstrapPayloadRegistry(t)

	in := marketdomain.BookDeltaV1{
		Bids: []marketdomain.PriceLevel{
			{Price: 100.5, Size: 2.0},
			{Price: 100.0, Size: 3.5},
		},
		Asks: []marketdomain.PriceLevel{
			{Price: 101.0, Size: 1.25},
			{Price: 101.5, Size: 0.75},
		},
		FirstID:   1200,
		FinalID:   1210,
		PrevFinal: 1199,
		Timestamp: 1700002222333,
	}

	jsonBytes, p := codec.EncodePayload("marketdata.bookdelta", 1, envelope.ContentTypeJSON, in)
	if p != nil {
		t.Fatalf("EncodePayload(JSON): %v", p)
	}
	protoBytes, p := codec.EncodePayload("marketdata.bookdelta", 1, envelope.ContentTypeProto, in)
	if p != nil {
		t.Fatalf("EncodePayload(PROTO): %v", p)
	}

	jsonAny, p := codec.DecodePayload("marketdata.bookdelta", 1, envelope.ContentTypeJSON, jsonBytes)
	if p != nil {
		t.Fatalf("DecodePayload(JSON): %v", p)
	}
	protoAny, p := codec.DecodePayload("marketdata.bookdelta", 1, envelope.ContentTypeProto, protoBytes)
	if p != nil {
		t.Fatalf("DecodePayload(PROTO): %v", p)
	}

	jsonOut, ok := jsonAny.(marketdomain.BookDeltaV1)
	if !ok {
		t.Fatalf("json decoded type = %T, want %T", jsonAny, marketdomain.BookDeltaV1{})
	}
	protoOut, ok := protoAny.(marketdomain.BookDeltaV1)
	if !ok {
		t.Fatalf("proto decoded type = %T, want %T", protoAny, marketdomain.BookDeltaV1{})
	}

	if !reflect.DeepEqual(jsonOut, in) {
		t.Fatalf("json roundtrip mismatch: got %+v want %+v", jsonOut, in)
	}
	if !reflect.DeepEqual(protoOut, in) {
		t.Fatalf("proto semantic mismatch: got %+v want %+v", protoOut, in)
	}
}

func TestEncodePayload_DeterministicJSONBytes_Trade_100Runs(t *testing.T) {
	bootstrapPayloadRegistry(t)
	setFallbackPolicyForTest(t, codec.FallbackPolicyAllowUnknownJSON)

	in := marketdomain.TradeTickV1{
		Price:     65321.25,
		Size:      1.5,
		Side:      "sell",
		TradeID:   "trade-789",
		Timestamp: 1700001111222,
	}

	first, p := codec.EncodePayload("marketdata.trade", 1, envelope.ContentTypeJSON, in)
	if p != nil {
		t.Fatalf("first encode: %v", p)
	}
	for i := 0; i < 100; i++ {
		next, nextProblem := codec.EncodePayload("marketdata.trade", 1, envelope.ContentTypeJSON, in)
		if nextProblem != nil {
			t.Fatalf("encode run %d: %v", i, nextProblem)
		}
		if !bytes.Equal(first, next) {
			t.Fatalf("json bytes changed at run %d\nfirst=%s\nnext=%s", i, string(first), string(next))
		}
	}
}

func TestEncodePayload_DeterministicProtoBytes_Trade_100Runs(t *testing.T) {
	bootstrapPayloadRegistry(t)
	setFallbackPolicyForTest(t, codec.FallbackPolicyAllowUnknownJSON)

	in := marketdomain.TradeTickV1{
		Price:     65321.25,
		Size:      1.5,
		Side:      "sell",
		TradeID:   "trade-789",
		Timestamp: 1700001111222,
	}

	first, p := codec.EncodePayload("marketdata.trade", 1, envelope.ContentTypeProto, in)
	if p != nil {
		t.Fatalf("first encode: %v", p)
	}
	for i := 0; i < 100; i++ {
		next, nextProblem := codec.EncodePayload("marketdata.trade", 1, envelope.ContentTypeProto, in)
		if nextProblem != nil {
			t.Fatalf("encode run %d: %v", i, nextProblem)
		}
		if !bytes.Equal(first, next) {
			t.Fatalf("protobuf bytes changed at run %d", i)
		}
	}
}

func TestDecodePayload_UnknownEvent_EmptyContentType_FallsBackToJSON(t *testing.T) {
	bootstrapPayloadRegistry(t)
	setFallbackPolicyForTest(t, codec.FallbackPolicyAllowUnknownJSON)

	out, p := codec.DecodePayload("marketdata.unknown", 1, "", []byte(`{"TradeID":"abc-1","Price":12.34}`))
	if p != nil {
		t.Fatalf("DecodePayload: %v", p)
	}
	payloadMap, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("decoded fallback type = %T, want map[string]any", out)
	}
	if payloadMap["TradeID"] != "abc-1" {
		t.Fatalf("TradeID = %v, want abc-1", payloadMap["TradeID"])
	}
}

func TestDecodePayload_UnknownEvent_JSONContentType_FallsBackToJSON(t *testing.T) {
	bootstrapPayloadRegistry(t)
	setFallbackPolicyForTest(t, codec.FallbackPolicyAllowUnknownJSON)

	out, p := codec.DecodePayload("marketdata.unknown", 1, envelope.ContentTypeJSON, []byte(`{"TradeID":"abc-2","Price":45.67}`))
	if p != nil {
		t.Fatalf("DecodePayload: %v", p)
	}
	payloadMap, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("decoded fallback type = %T, want map[string]any", out)
	}
	if payloadMap["TradeID"] != "abc-2" {
		t.Fatalf("TradeID = %v, want abc-2", payloadMap["TradeID"])
	}
}

func TestDecodePayload_UnknownEvent_ProtoContentType_Rejected(t *testing.T) {
	bootstrapPayloadRegistry(t)
	setFallbackPolicyForTest(t, codec.FallbackPolicyAllowUnknownJSON)

	_, p := codec.DecodePayload("marketdata.unknown", 1, envelope.ContentTypeProto, []byte{0x01, 0x02})
	if p == nil {
		t.Fatal("expected validation error for unknown protobuf event type")
	}
	if p.Code != problem.ValidationFailed {
		t.Fatalf("problem code = %s, want %s", p.Code, problem.ValidationFailed)
	}
	if p.Details["reason"] != "validation_failed_unknown_event_type_proto" {
		t.Fatalf("reason = %v, want validation_failed_unknown_event_type_proto", p.Details["reason"])
	}
}

func TestDecodePayload_UnknownEvent_InvalidContentType_Rejected(t *testing.T) {
	bootstrapPayloadRegistry(t)
	setFallbackPolicyForTest(t, codec.FallbackPolicyAllowUnknownJSON)

	_, p := codec.DecodePayload("marketdata.unknown", 1, "application/xml", []byte(`{}`))
	if p == nil {
		t.Fatal("expected validation error for unknown content_type")
	}
	if p.Code != problem.ValidationFailed {
		t.Fatalf("problem code = %s, want %s", p.Code, problem.ValidationFailed)
	}
	if p.Details["reason"] != "validation_failed_unknown_content_type" {
		t.Fatalf("reason = %v, want validation_failed_unknown_content_type", p.Details["reason"])
	}
}

func TestSetFallbackPolicy_RejectUnknown_JSONUnknownEventRejected(t *testing.T) {
	bootstrapPayloadRegistry(t)
	setFallbackPolicyForTest(t, codec.FallbackPolicyRejectUnknown)

	_, p := codec.DecodePayload("marketdata.unknown", 1, envelope.ContentTypeJSON, []byte(`{"TradeID":"abc-3"}`))
	if p == nil {
		t.Fatal("expected validation error when reject_unknown policy is enabled")
	}
	if p.Code != problem.ValidationFailed {
		t.Fatalf("problem code = %s, want %s", p.Code, problem.ValidationFailed)
	}
	if p.Details["reason"] != "validation_failed_unknown_event_type_rejected" {
		t.Fatalf("reason = %v, want validation_failed_unknown_event_type_rejected", p.Details["reason"])
	}
}

func TestSetFallbackPolicy_InvalidPolicyRejected(t *testing.T) {
	setFallbackPolicyForTest(t, codec.FallbackPolicyAllowUnknownJSON)

	p := codec.SetFallbackPolicy(codec.FallbackPolicy("invalid_policy"))
	if p == nil {
		t.Fatal("expected validation problem for invalid fallback policy")
	}
	if p.Code != problem.ValidationFailed {
		t.Fatalf("problem code = %s, want %s", p.Code, problem.ValidationFailed)
	}
	if p.Details["reason"] != "validation_failed_invalid_fallback_policy" {
		t.Fatalf("reason = %v, want validation_failed_invalid_fallback_policy", p.Details["reason"])
	}
}

func TestEncodePayload_UnknownContentTypeRejected(t *testing.T) {
	bootstrapPayloadRegistry(t)

	_, p := codec.EncodePayload("marketdata.trade", 1, "application/xml", marketdomain.TradeTickV1{})
	if p == nil {
		t.Fatal("expected validation error for unknown content type")
	}
	if p.Code != problem.ValidationFailed {
		t.Fatalf("problem code = %s, want %s", p.Code, problem.ValidationFailed)
	}
	if p.Details["reason"] != "validation_failed_unknown_content_type" {
		t.Fatalf("reason = %v, want validation_failed_unknown_content_type", p.Details["reason"])
	}
}

func bootstrapPayloadRegistry(t *testing.T) {
	t.Helper()
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		t.Fatalf("BootstrapPayloadCodecRegistry: %v", p)
	}
}

func setFallbackPolicyForTest(t *testing.T, policy codec.FallbackPolicy) {
	t.Helper()
	original := codec.FallbackPolicyValue()
	t.Cleanup(func() {
		if p := codec.SetFallbackPolicy(original); p != nil {
			t.Fatalf("restore fallback policy: %v", p)
		}
	})
	if p := codec.SetFallbackPolicy(policy); p != nil {
		t.Fatalf("SetFallbackPolicy(%q): %v", policy, p)
	}
}

func TestDecodePayload_UnknownEvent_EmptyContentType_FallbackDecodesArrays(t *testing.T) {
	bootstrapPayloadRegistry(t)
	setFallbackPolicyForTest(t, codec.FallbackPolicyAllowUnknownJSON)

	out, p := codec.DecodePayload("marketdata.unknown", 1, "", []byte(`[1,2,3]`))
	if p != nil {
		t.Fatalf("DecodePayload: %v", p)
	}

	arr, ok := out.([]any)
	if !ok {
		t.Fatalf("decoded fallback type = %T, want []any", out)
	}
	if len(arr) != 3 {
		t.Fatalf("decoded array length = %d, want 3", len(arr))
	}
}

func TestDecodePayload_UnknownEvent_JSONFallbackRejectsInvalidJSON(t *testing.T) {
	bootstrapPayloadRegistry(t)
	setFallbackPolicyForTest(t, codec.FallbackPolicyAllowUnknownJSON)

	_, p := codec.DecodePayload("marketdata.unknown", 1, envelope.ContentTypeJSON, []byte(`{"a":`))
	if p == nil {
		t.Fatal("expected fallback decode validation error")
	}
	if p.Code != problem.ValidationFailed {
		t.Fatalf("problem code = %s, want %s", p.Code, problem.ValidationFailed)
	}
	if p.Details["reason"] != "validation_failed_unknown_event_type_json_fallback_decode" {
		t.Fatalf("reason = %v, want validation_failed_unknown_event_type_json_fallback_decode", p.Details["reason"])
	}
}

func TestDecodePayload_UnknownEvent_JSONFallbackProducesValidJSONValue(t *testing.T) {
	bootstrapPayloadRegistry(t)
	setFallbackPolicyForTest(t, codec.FallbackPolicyAllowUnknownJSON)

	out, p := codec.DecodePayload("marketdata.unknown", 1, envelope.ContentTypeJSON, []byte(`{"k":"v"}`))
	if p != nil {
		t.Fatalf("DecodePayload: %v", p)
	}
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal fallback output: %v", err)
	}
	if !bytes.Equal(raw, []byte(`{"k":"v"}`)) {
		t.Fatalf("fallback output = %s, want %s", string(raw), `{"k":"v"}`)
	}
}
