package aggruntime

import (
	"encoding/json"
	"testing"

	aggapp "github.com/market-raccoon/internal/core/aggregation/app"
	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	"github.com/market-raccoon/internal/shared/envelope"
)

func TestDetectMicrostructureEvidence_DeterministicReplay(t *testing.T) {
	req := aggapp.UpdateRequest{
		Venue:      "binance",
		Instrument: "BTCUSDT",
		Seq:        101,
		Bids: []aggdomain.Level{
			{Price: 100.0, Quantity: 25},
			{Price: 99.9, Quantity: 20},
			{Price: 99.8, Quantity: 18},
		},
		Asks: []aggdomain.Level{
			{Price: 100.6, Quantity: 2},
			{Price: 100.7, Quantity: 2},
			{Price: 100.8, Quantity: 1},
		},
	}
	resp := aggapp.UpdateResponse{Seq: 101, Spread: 0.6}

	first := detectMicrostructureEvidence(req, resp)
	second := detectMicrostructureEvidence(req, resp)
	a, _ := json.Marshal(first)
	b, _ := json.Marshal(second)
	if string(a) != string(b) {
		t.Fatalf("nondeterministic evidence\nfirst=%s\nsecond=%s", string(a), string(b))
	}
	if len(first) == 0 {
		t.Fatal("expected at least one evidence")
	}
}

func TestBuildMicrostructureEvidenceEnvelope_Valid(t *testing.T) {
	src := envelope.Envelope{
		Type:           "marketdata.bookdelta",
		Version:        1,
		Venue:          "binance",
		Instrument:     "BTCUSDT",
		TsIngest:       1700000000123,
		Seq:            77,
		IdempotencyKey: "src-77",
		ContentType:    envelope.ContentTypeJSON,
		Payload:        []byte(`{"ok":true}`),
	}
	ev := microstructureEvidenceV1{
		Kind:       string(evidenceSpreadExplosion),
		Confidence: 0.8,
		Features:   []string{"spread_bps"},
		Reason:     "spread expanded",
		TsIngest:   src.TsIngest,
		Seq:        src.Seq,
	}
	out, p := buildMicrostructureEvidenceEnvelope(src, ev)
	if p != nil {
		t.Fatalf("build envelope failed: %v", p)
	}
	if got, want := out.Type, microstructureEvidenceType; got != want {
		t.Fatalf("type=%q want=%q", got, want)
	}
	if out.IdempotencyKey == src.IdempotencyKey {
		t.Fatal("idempotency key should be derived for evidence")
	}
}
