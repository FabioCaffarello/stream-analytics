package contracts_test

import (
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/contracts"
	marketdomain "github.com/FabioCaffarello/stream-analytics/internal/core/marketdata/domain"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/codec"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/envelope"
)

var (
	hotPathDecodeSink  any
	hotPathSubjectSink string
)

func BenchmarkHotPathDecodePayloadAndSubjectFromEnvelope(b *testing.B) {
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		b.Fatalf("BootstrapPayloadCodecRegistry: %v", p)
	}

	env := envelope.Envelope{
		Type:       "marketdata.trade",
		Version:    1,
		Venue:      "BINANCE",
		Instrument: "BTCUSDT",
	}
	data, p := codec.EncodePayload(
		env.Type,
		env.Version,
		envelope.ContentTypeJSON,
		marketdomain.TradeTickV1{
			Price:     65321.25,
			Size:      1.5,
			Side:      "sell",
			TradeID:   "trade-789",
			Timestamp: 1700001111222,
		},
	)
	if p != nil {
		b.Fatalf("EncodePayload: %v", p)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decoded, decodeProblem := codec.DecodePayload(env.Type, env.Version, envelope.ContentTypeJSON, data)
		if decodeProblem != nil {
			b.Fatalf("DecodePayload: %v", decodeProblem)
		}
		hotPathDecodeSink = decoded
		hotPathSubjectSink = envelope.SubjectFromEnvelope(env)
	}
}
