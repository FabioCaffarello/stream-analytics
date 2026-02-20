package envelope

import (
	"testing"
)

func BenchmarkTopicKey(b *testing.B) {
	env := Envelope{
		Type:       "marketdata.trade",
		Venue:      "binance",
		Instrument: "BTC-PERP",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = env.TopicKey()
	}
}
