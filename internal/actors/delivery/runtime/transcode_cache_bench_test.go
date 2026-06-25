package deliveryruntime

import (
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/market-raccoon/internal/contracts"
	"github.com/market-raccoon/internal/core/marketdata/domain"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/envelope"
)

func BenchmarkTranscodeCache_Serial(b *testing.B) {
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		b.Fatalf("bootstrap codec: %v", p)
	}

	cache := NewTranscodeCache(1024)
	payload, p := codec.EncodePayload("marketdata.trade", 1, envelope.ContentTypeJSON, domain.TradeTickV1{
		Price: 42000.5, Size: 1.5, Side: "buy", TradeID: "t1", Timestamp: 1700000000000,
	})
	if p != nil {
		b.Fatalf("encode: %v", p)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = cache.TranscodeProtoToJSON("marketdata.trade", 1, envelope.ContentTypeJSON, payload)
	}
}

func BenchmarkTranscodeCache_Parallel(b *testing.B) {
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		b.Fatalf("bootstrap codec: %v", p)
	}

	const payloadCount = 32
	cache := NewTranscodeCache(1024)
	payloads := make([][]byte, payloadCount)
	for i := 0; i < payloadCount; i++ {
		pay, perr := codec.EncodePayload("marketdata.trade", 1, envelope.ContentTypeJSON, domain.TradeTickV1{
			Price: float64(i)*10 + 0.1, Size: 1.0, Side: "buy", TradeID: fmt.Sprintf("t%d", i), Timestamp: int64(1700000000000 + i),
		})
		if perr != nil {
			b.Fatalf("encode %d: %v", i, perr)
		}
		payloads[i] = pay
	}

	var counter uint64
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			idx := atomic.AddUint64(&counter, 1)
			p := payloads[idx%payloadCount]
			_, _ = cache.TranscodeProtoToJSON("marketdata.trade", 1, envelope.ContentTypeJSON, p)
		}
	})
}
