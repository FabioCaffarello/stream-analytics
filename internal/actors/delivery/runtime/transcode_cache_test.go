package deliveryruntime

import (
	"sync"
	"testing"

	"github.com/market-raccoon/internal/contracts"
	"github.com/market-raccoon/internal/core/marketdata/domain"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/envelope"
)

func TestTranscodeCache_HitAfterMiss(t *testing.T) {
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		t.Fatalf("bootstrap codec: %v", p)
	}

	cache := NewTranscodeCache(64)

	trade := domain.TradeTickV1{
		Price: 42000.5, Size: 1.5, Side: "buy",
		TradeID: "t1", Timestamp: 1700000000000,
	}
	jsonPayload, p := codec.EncodePayload("marketdata.trade", 1, envelope.ContentTypeJSON, trade)
	if p != nil {
		t.Fatalf("encode: %v", p)
	}

	// First call: miss.
	result1, p := cache.TranscodeProtoToJSON("marketdata.trade", 1, envelope.ContentTypeJSON, jsonPayload)
	if p != nil {
		t.Fatalf("transcode 1: %v", p)
	}
	if result1 == nil {
		t.Fatal("expected non-nil result")
	}

	hits1, misses1 := cache.Stats()
	if hits1 != 0 || misses1 != 1 {
		t.Fatalf("want hits=0 misses=1, got hits=%d misses=%d", hits1, misses1)
	}

	// Second call with same payload: hit.
	result2, p := cache.TranscodeProtoToJSON("marketdata.trade", 1, envelope.ContentTypeJSON, jsonPayload)
	if p != nil {
		t.Fatalf("transcode 2: %v", p)
	}

	hits2, misses2 := cache.Stats()
	if hits2 != 1 || misses2 != 1 {
		t.Fatalf("want hits=1 misses=1, got hits=%d misses=%d", hits2, misses2)
	}

	if string(result1) != string(result2) {
		t.Fatalf("cached result differs: %s vs %s", result1, result2)
	}
}

func TestTranscodeCache_EvictionOnOverflow(t *testing.T) {
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		t.Fatalf("bootstrap codec: %v", p)
	}

	cache := NewTranscodeCache(4)

	for i := 0; i < 8; i++ {
		jsonPayload, p := codec.EncodePayload("marketdata.trade", 1, envelope.ContentTypeJSON, domain.TradeTickV1{
			Price: float64(i)*100 + 0.1, Size: 1.0, Side: "buy",
			TradeID: "t", Timestamp: int64(1700000000000 + i),
		})
		if p != nil {
			t.Fatalf("encode %d: %v", i, p)
		}
		_, p = cache.TranscodeProtoToJSON("marketdata.trade", 1, envelope.ContentTypeJSON, jsonPayload)
		if p != nil {
			t.Fatalf("transcode %d: %v", i, p)
		}
	}

	if cache.Len() > 4 {
		t.Fatalf("cache len=%d exceeds max=4", cache.Len())
	}
}

func TestTranscodeCache_DifferentKeysNoCollision(t *testing.T) {
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		t.Fatalf("bootstrap codec: %v", p)
	}

	cache := NewTranscodeCache(64)

	payloadA, p := codec.EncodePayload("marketdata.trade", 1, envelope.ContentTypeJSON, domain.TradeTickV1{
		Price: 100.0, Size: 1.0, Side: "buy", TradeID: "a", Timestamp: 1700000000000,
	})
	if p != nil {
		t.Fatalf("encode A: %v", p)
	}

	payloadB, p := codec.EncodePayload("marketdata.trade", 1, envelope.ContentTypeJSON, domain.TradeTickV1{
		Price: 200.0, Size: 2.0, Side: "sell", TradeID: "b", Timestamp: 1700000000001,
	})
	if p != nil {
		t.Fatalf("encode B: %v", p)
	}

	resultA, _ := cache.TranscodeProtoToJSON("marketdata.trade", 1, envelope.ContentTypeJSON, payloadA)
	resultB, _ := cache.TranscodeProtoToJSON("marketdata.trade", 1, envelope.ContentTypeJSON, payloadB)

	if string(resultA) == string(resultB) {
		t.Fatal("different payloads should produce different cache results")
	}
	if cache.Len() != 2 {
		t.Fatalf("cache len=%d want=2", cache.Len())
	}
}

func TestTranscodeCache_ConcurrentAccess(t *testing.T) {
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		t.Fatalf("bootstrap codec: %v", p)
	}

	cache := NewTranscodeCache(64)
	payload, p := codec.EncodePayload("marketdata.trade", 1, envelope.ContentTypeJSON, domain.TradeTickV1{
		Price: 42000.5, Size: 1.0, Side: "buy", TradeID: "t1", Timestamp: 1700000000000,
	})
	if p != nil {
		t.Fatalf("encode: %v", p)
	}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_, p := cache.TranscodeProtoToJSON("marketdata.trade", 1, envelope.ContentTypeJSON, payload)
				if p != nil {
					t.Errorf("transcode error: %v", p)
					return
				}
			}
		}()
	}
	wg.Wait()

	hits, misses := cache.Stats()
	total := hits + misses
	if total != 5000 {
		t.Fatalf("total=%d want=5000", total)
	}
	if misses > 50 {
		t.Fatalf("misses=%d > 50 (too many cache misses)", misses)
	}
}
