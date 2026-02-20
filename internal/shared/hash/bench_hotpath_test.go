package hash_test

import (
	"testing"

	"github.com/market-raccoon/internal/shared/hash"
)

var benchHashSink string

// BenchmarkHotPathHashFields5 measures the typical idempotency-key computation
// path used in ingest and processor envelope construction (5 fields).
func BenchmarkHotPathHashFields5(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchHashSink = hash.HashFields(
			"marketdata.trade",
			"1",
			"BTCUSDT",
			"SPOT",
			"bench-key-00001",
		)
	}
}

// BenchmarkHotPathHashFieldsFast5 measures the FNV-1a fast-path replacement.
func BenchmarkHotPathHashFieldsFast5(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchHashSink = hash.HashFieldsFast(
			"marketdata.trade",
			"1",
			"BTCUSDT",
			"SPOT",
			"bench-key-00001",
		)
	}
}
