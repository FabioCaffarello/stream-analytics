package replay_test

import (
	"encoding/json"
	"testing"

	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/replay"
)

func makeTestEnvelope(typ, venue, instrument string, seq int64, payload map[string]any) envelope.Envelope {
	payloadBytes, _ := json.Marshal(payload)
	return envelope.Envelope{
		Type:           typ,
		Version:        1,
		Venue:          venue,
		Instrument:     instrument,
		TsExchange:     1710000000000,
		TsIngest:       1710000001000,
		Seq:            seq,
		IdempotencyKey: "test-key",
		ContentType:    "application/json",
		Payload:        payloadBytes,
	}
}

func TestDetectDrift_Identical(t *testing.T) {
	env := makeTestEnvelope("trade", "BINANCE", "BTCUSDT", 1, map[string]any{
		"price": 42000.0,
		"size":  1.5,
	})
	results, p := replay.DetectDrift([]envelope.Envelope{env}, []envelope.Envelope{env})
	if p != nil {
		t.Fatalf("DetectDrift: %v", p)
	}
	if len(results) != 0 {
		t.Errorf("expected no drift, got %d results", len(results))
	}
}

func TestDetectDrift_FieldValueMismatch(t *testing.T) {
	actual := makeTestEnvelope("trade", "BINANCE", "BTCUSDT", 1, map[string]any{
		"price": 42100.0,
		"size":  1.5,
	})
	golden := makeTestEnvelope("trade", "BINANCE", "BTCUSDT", 1, map[string]any{
		"price": 42000.0,
		"size":  1.5,
	})
	results, p := replay.DetectDrift([]envelope.Envelope{actual}, []envelope.Envelope{golden})
	if p != nil {
		t.Fatalf("DetectDrift: %v", p)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 drift result, got %d", len(results))
	}
	dr := results[0]
	if dr.Compatible {
		t.Error("value mismatch should not be compatible")
	}
	if len(dr.FieldMismatches) == 0 {
		t.Error("expected field mismatches")
	}
	found := false
	for _, fm := range dr.FieldMismatches {
		if fm.Path == "payload.price" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected mismatch at payload.price")
	}
}

func TestDetectDrift_NewField_Compatible(t *testing.T) {
	actual := makeTestEnvelope("trade", "BINANCE", "BTCUSDT", 1, map[string]any{
		"price":     42000.0,
		"size":      1.5,
		"new_field": "added",
	})
	golden := makeTestEnvelope("trade", "BINANCE", "BTCUSDT", 1, map[string]any{
		"price": 42000.0,
		"size":  1.5,
	})
	results, p := replay.DetectDrift([]envelope.Envelope{actual}, []envelope.Envelope{golden})
	if p != nil {
		t.Fatalf("DetectDrift: %v", p)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 drift result, got %d", len(results))
	}
	dr := results[0]
	if !dr.Compatible {
		t.Error("additive-only change should be compatible")
	}
	if len(dr.NewFields) == 0 {
		t.Error("expected new fields")
	}
}

func TestDetectDrift_DroppedField_Incompatible(t *testing.T) {
	actual := makeTestEnvelope("trade", "BINANCE", "BTCUSDT", 1, map[string]any{
		"price": 42000.0,
	})
	golden := makeTestEnvelope("trade", "BINANCE", "BTCUSDT", 1, map[string]any{
		"price": 42000.0,
		"size":  1.5,
	})
	results, p := replay.DetectDrift([]envelope.Envelope{actual}, []envelope.Envelope{golden})
	if p != nil {
		t.Fatalf("DetectDrift: %v", p)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 drift result, got %d", len(results))
	}
	dr := results[0]
	if dr.Compatible {
		t.Error("dropped field should not be compatible")
	}
	if len(dr.DroppedFields) == 0 {
		t.Error("expected dropped fields")
	}
}

func TestDetectDrift_CountMismatch(t *testing.T) {
	env := makeTestEnvelope("trade", "BINANCE", "BTCUSDT", 1, map[string]any{"price": 42000.0})
	_, p := replay.DetectDrift(
		[]envelope.Envelope{env, env},
		[]envelope.Envelope{env},
	)
	if p == nil {
		t.Error("expected error on count mismatch")
	}
}

func TestDetectDrift_NestedMismatch(t *testing.T) {
	actual := makeTestEnvelope("trade", "BINANCE", "BTCUSDT", 1, map[string]any{
		"book": map[string]any{
			"best_bid": 42000.0,
			"best_ask": 42001.0,
		},
	})
	golden := makeTestEnvelope("trade", "BINANCE", "BTCUSDT", 1, map[string]any{
		"book": map[string]any{
			"best_bid": 41999.0,
			"best_ask": 42001.0,
		},
	})
	results, p := replay.DetectDrift([]envelope.Envelope{actual}, []envelope.Envelope{golden})
	if p != nil {
		t.Fatalf("DetectDrift: %v", p)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	found := false
	for _, fm := range results[0].FieldMismatches {
		if fm.Path == "payload.book.best_bid" {
			found = true
		}
	}
	if !found {
		t.Error("expected mismatch at payload.book.best_bid")
	}
}

func TestDetectDrift_ArrayMismatch(t *testing.T) {
	actual := makeTestEnvelope("snap", "BINANCE", "BTCUSDT", 1, map[string]any{
		"bids": []any{42000.0, 42001.0, 42002.0},
	})
	golden := makeTestEnvelope("snap", "BINANCE", "BTCUSDT", 1, map[string]any{
		"bids": []any{42000.0, 41999.0},
	})
	results, p := replay.DetectDrift([]envelope.Envelope{actual}, []envelope.Envelope{golden})
	if p != nil {
		t.Fatalf("DetectDrift: %v", p)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	dr := results[0]
	if dr.Compatible {
		t.Error("array element value mismatch should not be compatible")
	}
}

func TestDetectDrift_EnvelopeHeaderMismatch(t *testing.T) {
	actual := makeTestEnvelope("trade", "BINANCE", "BTCUSDT", 1, map[string]any{"price": 42000.0})
	golden := makeTestEnvelope("trade", "BINANCE", "ETHUSDT", 1, map[string]any{"price": 42000.0})
	results, p := replay.DetectDrift([]envelope.Envelope{actual}, []envelope.Envelope{golden})
	if p != nil {
		t.Fatalf("DetectDrift: %v", p)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Compatible {
		t.Error("instrument mismatch should not be compatible")
	}
}

func TestDetectDrift_MultipleEnvelopes(t *testing.T) {
	env1 := makeTestEnvelope("trade", "BINANCE", "BTCUSDT", 1, map[string]any{"price": 42000.0})
	env2 := makeTestEnvelope("trade", "BINANCE", "BTCUSDT", 2, map[string]any{"price": 42100.0})
	env2mod := makeTestEnvelope("trade", "BINANCE", "BTCUSDT", 2, map[string]any{"price": 42200.0})
	results, p := replay.DetectDrift(
		[]envelope.Envelope{env1, env2mod},
		[]envelope.Envelope{env1, env2},
	)
	if p != nil {
		t.Fatalf("DetectDrift: %v", p)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 drifted envelope, got %d", len(results))
	}
	if results[0].EnvelopeIndex != 1 {
		t.Errorf("expected drift at index 1, got %d", results[0].EnvelopeIndex)
	}
}

func TestDetectPayloadDrift_Identical(t *testing.T) {
	payload := json.RawMessage(`{"price":42000,"size":1.5}`)
	results, p := replay.DetectPayloadDrift(
		[]json.RawMessage{payload},
		[]json.RawMessage{payload},
	)
	if p != nil {
		t.Fatalf("DetectPayloadDrift: %v", p)
	}
	if len(results) != 0 {
		t.Errorf("expected no drift, got %d", len(results))
	}
}

func TestDetectPayloadDrift_Mismatch(t *testing.T) {
	actual := json.RawMessage(`{"price":42100,"size":1.5}`)
	golden := json.RawMessage(`{"price":42000,"size":1.5}`)
	results, p := replay.DetectPayloadDrift(
		[]json.RawMessage{actual},
		[]json.RawMessage{golden},
	)
	if p != nil {
		t.Fatalf("DetectPayloadDrift: %v", p)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 drift, got %d", len(results))
	}
}
