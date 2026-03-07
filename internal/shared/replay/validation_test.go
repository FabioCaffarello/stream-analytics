package replay_test

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/replay"
)

func TestExpectOutputCount_Pass(t *testing.T) {
	hook := replay.ExpectOutputCount(2)
	envs := []envelope.Envelope{{}, {}}
	if p := hook(replay.ReplaySummary{}, envs); p != nil {
		t.Fatalf("expected pass, got: %v", p)
	}
}

func TestExpectOutputCount_Fail(t *testing.T) {
	hook := replay.ExpectOutputCount(3)
	envs := []envelope.Envelope{{}, {}}
	if p := hook(replay.ReplaySummary{}, envs); p == nil {
		t.Error("expected failure on count mismatch")
	}
}

func TestExpectOutputSHA_Pass(t *testing.T) {
	payload := []byte(`{"price":42000}`)
	envs := []envelope.Envelope{{Payload: payload}}

	h := sha256.New()
	for i := range envs {
		_, _ = h.Write(envs[i].Payload)
		_, _ = h.Write([]byte{'\n'})
	}
	expected := hex.EncodeToString(h.Sum(nil))

	hook := replay.ExpectOutputSHA(expected)
	if p := hook(replay.ReplaySummary{}, envs); p != nil {
		t.Fatalf("expected pass, got: %v", p)
	}
}

func TestExpectOutputSHA_Fail(t *testing.T) {
	envs := []envelope.Envelope{{Payload: []byte(`{"price":42000}`)}}
	hook := replay.ExpectOutputSHA("0000000000000000000000000000000000000000000000000000000000000000")
	if p := hook(replay.ReplaySummary{}, envs); p == nil {
		t.Error("expected failure on SHA mismatch")
	}
}

func TestExpectInputSHA_Pass(t *testing.T) {
	hook := replay.ExpectInputSHA("abc123")
	if p := hook(replay.ReplaySummary{InputSHA: "abc123"}, nil); p != nil {
		t.Fatalf("expected pass, got: %v", p)
	}
}

func TestExpectInputSHA_CaseInsensitive(t *testing.T) {
	hook := replay.ExpectInputSHA("ABC123")
	if p := hook(replay.ReplaySummary{InputSHA: "abc123"}, nil); p != nil {
		t.Fatalf("expected case-insensitive pass, got: %v", p)
	}
}

func TestExpectInputSHA_Fail(t *testing.T) {
	hook := replay.ExpectInputSHA("abc123")
	if p := hook(replay.ReplaySummary{InputSHA: "def456"}, nil); p == nil {
		t.Error("expected failure on SHA mismatch")
	}
}

func TestExpectNoDrift_Pass(t *testing.T) {
	env := makeTestEnvelope("trade", "BINANCE", "BTCUSDT", 1, map[string]any{"price": 42000.0})
	hook := replay.ExpectNoDrift([]envelope.Envelope{env})
	if p := hook(replay.ReplaySummary{}, []envelope.Envelope{env}); p != nil {
		t.Fatalf("expected pass, got: %v", p)
	}
}

func TestExpectNoDrift_AdditiveCompatible(t *testing.T) {
	golden := makeTestEnvelope("trade", "BINANCE", "BTCUSDT", 1, map[string]any{"price": 42000.0})
	actual := makeTestEnvelope("trade", "BINANCE", "BTCUSDT", 1, map[string]any{
		"price":     42000.0,
		"new_field": "added",
	})
	hook := replay.ExpectNoDrift([]envelope.Envelope{golden})
	if p := hook(replay.ReplaySummary{}, []envelope.Envelope{actual}); p != nil {
		t.Fatalf("additive change should pass NoDrift, got: %v", p)
	}
}

func TestExpectNoDrift_BreakingFail(t *testing.T) {
	golden := makeTestEnvelope("trade", "BINANCE", "BTCUSDT", 1, map[string]any{
		"price": 42000.0,
		"size":  1.5,
	})
	actual := makeTestEnvelope("trade", "BINANCE", "BTCUSDT", 1, map[string]any{
		"price": 42000.0,
	})
	hook := replay.ExpectNoDrift([]envelope.Envelope{golden})
	if p := hook(replay.ReplaySummary{}, []envelope.Envelope{actual}); p == nil {
		t.Error("dropped field should fail NoDrift")
	}
}

func TestExpectNoFieldMismatch_Strict(t *testing.T) {
	golden := makeTestEnvelope("trade", "BINANCE", "BTCUSDT", 1, map[string]any{"price": 42000.0})
	actual := makeTestEnvelope("trade", "BINANCE", "BTCUSDT", 1, map[string]any{
		"price":     42000.0,
		"new_field": "added",
	})
	hook := replay.ExpectNoFieldMismatch([]envelope.Envelope{golden})
	if p := hook(replay.ReplaySummary{}, []envelope.Envelope{actual}); p == nil {
		t.Error("strict mode should fail on additive change")
	}
}

func TestRunValidations_AllPass(t *testing.T) {
	envs := []envelope.Envelope{{}, {}}
	hooks := []replay.ValidationHook{
		replay.ExpectOutputCount(2),
	}
	if p := replay.RunValidations(replay.ReplaySummary{}, envs, hooks); p != nil {
		t.Fatalf("expected all pass, got: %v", p)
	}
}

func TestRunValidations_StopsOnFirstFailure(t *testing.T) {
	envs := []envelope.Envelope{{}}
	hooks := []replay.ValidationHook{
		replay.ExpectOutputCount(5), // fails
		replay.ExpectOutputCount(1), // would pass but not reached
	}
	if p := replay.RunValidations(replay.ReplaySummary{}, envs, hooks); p == nil {
		t.Error("expected failure on first hook")
	}
}
