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
		RuleVersion:    "v0",
		InputWatermark: []marketmodel.SignalInputSeqRange{{Venue: "binance", Symbol: "BTC-USDT", SeqStart: 1, SeqEnd: 1}},
		CorrelationID:  "cid",
	})
	if p != nil {
		t.Fatalf("encode failed: %s", p.Message)
	}
	dec, _ := reg.Decoder(jsonKey)
	decoded, p := dec.Decode(bytes)
	if p != nil {
		t.Fatalf("decode failed: %s", p.Message)
	}
	if _, ok := decoded.(marketmodel.SignalEvent); !ok {
		t.Fatalf("decoded type=%T want marketmodel.SignalEvent", decoded)
	}
}
