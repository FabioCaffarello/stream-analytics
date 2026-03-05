package contracts_test

import (
	"testing"

	marketmodel "github.com/market-raccoon/internal/core/marketmodel"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/contracts"
)

func TestRegisterSignalEnginePayloadV1(t *testing.T) {
	reg := codec.NewRegistry()
	if p := contracts.RegisterSignalEnginePayloadV1(reg); p != nil {
		t.Fatalf("RegisterSignalEnginePayloadV1 failed: %s", p.Message)
	}

	jsonKey := codec.SchemaKey{Type: "signal.event", Version: 1, Format: codec.FormatJSON}
	if _, ok := reg.Encoder(jsonKey); !ok {
		t.Fatal("missing signal.event JSON encoder")
	}
	if _, ok := reg.Decoder(jsonKey); !ok {
		t.Fatal("missing signal.event JSON decoder")
	}

	protoKey := codec.SchemaKey{Type: "signal.event", Version: 1, Format: codec.FormatProto}
	if _, ok := reg.Encoder(protoKey); !ok {
		t.Fatal("missing signal.event proto encoder")
	}
	if _, ok := reg.Decoder(protoKey); !ok {
		t.Fatal("missing signal.event proto decoder")
	}

	enc, _ := reg.Encoder(jsonKey)
	bytes, p := enc.Encode(marketmodel.SignalEvent{
		Type:           "liquidity_collapse",
		TsServer:       1,
		Scope:          marketmodel.SignalScopeStream,
		Venue:          "binance",
		Symbol:         "BTC-USDT",
		Severity:       "high",
		Confidence:     0.9,
		Features:       []marketmodel.SignalFeature{{Key: "x", Value: 1}},
		Explanation:    "fixture",
		Explain:        []string{"fixture"},
		SignalID:       "sig-1",
		RuleID:         "liquidity_collapse_rule",
		RuleVersion:    "v0",
		InputWatermark: []marketmodel.SignalInputSeqRange{{Venue: "binance", Symbol: "BTC-USDT", SeqStart: 1, SeqEnd: 1}},
		CorrelationID:  "cid",
		CorrelationIDs: []string{"cid", "evidence:1"},
	})
	if p != nil {
		t.Fatalf("encode failed: %s", p.Message)
	}
	dec, _ := reg.Decoder(jsonKey)
	decoded, p := dec.Decode(bytes)
	if p != nil {
		t.Fatalf("decode failed: %s", p.Message)
	}
	ev, ok := decoded.(marketmodel.SignalEvent)
	if !ok {
		t.Fatalf("decoded type=%T want marketmodel.SignalEvent", decoded)
	}
	if ev.SignalID != "sig-1" {
		t.Fatalf("signal_id=%q want=%q", ev.SignalID, "sig-1")
	}
	if ev.RuleID != "liquidity_collapse_rule" {
		t.Fatalf("rule_id=%q want=%q", ev.RuleID, "liquidity_collapse_rule")
	}
	if len(ev.Explain) != 1 || ev.Explain[0] != "fixture" {
		t.Fatalf("explain=%v want=[fixture]", ev.Explain)
	}
	if len(ev.CorrelationIDs) != 2 {
		t.Fatalf("correlation_ids len=%d want=2", len(ev.CorrelationIDs))
	}
}
