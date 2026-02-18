package codec_test

import (
	"testing"

	marketdomain "github.com/market-raccoon/internal/core/marketdata/domain"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/contracts"
	"github.com/market-raccoon/internal/shared/envelope"
)

var protoJSONDecodeSink any

func BenchmarkProtoVsJSON_EncodeDecode(b *testing.B) {
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		b.Fatalf("BootstrapPayloadCodecRegistry: %v", p)
	}

	trade := marketdomain.TradeTickV1{
		Price:     65_321.25,
		Size:      1.5,
		Side:      "sell",
		TradeID:   "trade-bench-001",
		Timestamp: 1_700_001_111_222,
	}

	b.Run("JSON", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			data, p := codec.EncodePayload("marketdata.trade", 1, envelope.ContentTypeJSON, trade)
			if p != nil {
				b.Fatalf("EncodePayload(JSON): %v", p)
			}
			out, p := codec.DecodePayload("marketdata.trade", 1, envelope.ContentTypeJSON, data)
			if p != nil {
				b.Fatalf("DecodePayload(JSON): %v", p)
			}
			protoJSONDecodeSink = out
		}
	})

	b.Run("Proto", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			data, p := codec.EncodePayload("marketdata.trade", 1, envelope.ContentTypeProto, trade)
			if p != nil {
				b.Fatalf("EncodePayload(PROTO): %v", p)
			}
			out, p := codec.DecodePayload("marketdata.trade", 1, envelope.ContentTypeProto, data)
			if p != nil {
				b.Fatalf("DecodePayload(PROTO): %v", p)
			}
			protoJSONDecodeSink = out
		}
	})
}
