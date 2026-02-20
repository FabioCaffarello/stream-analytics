package hash_test

import (
	"math"
	"testing"

	"github.com/market-raccoon/internal/shared/hash"
)

func TestFieldHasher_stable(t *testing.T) {
	h1 := hash.NewFieldHasher().String("binance").String("BTC-PERP").Int64(1000).Hex()
	h2 := hash.NewFieldHasher().String("binance").String("BTC-PERP").Int64(1000).Hex()
	if h1 != h2 {
		t.Error("FieldHasher must be stable for same inputs")
	}
}

func TestFieldHasher_orderMatters(t *testing.T) {
	h1 := hash.NewFieldHasher().String("a").String("b").Hex()
	h2 := hash.NewFieldHasher().String("b").String("a").Hex()
	if h1 == h2 {
		t.Error("FieldHasher must be order-sensitive")
	}
}

func TestFieldHasher_separatorPreventsCollision(t *testing.T) {
	h1 := hash.NewFieldHasher().String("ab").String("c").Hex()
	h2 := hash.NewFieldHasher().String("a").String("bc").Hex()
	if h1 == h2 {
		t.Error("separator must prevent concatenation collisions")
	}
}

func TestFieldHasher_typeDifferentiation(t *testing.T) {
	// String "42" vs Int64(42) must produce different hashes.
	h1 := hash.NewFieldHasher().String("42").Hex()
	h2 := hash.NewFieldHasher().Int64(42).Hex()
	if h1 == h2 {
		t.Error("String(\"42\") must differ from Int64(42)")
	}
}

func TestFieldHasher_intEqualsInt64(t *testing.T) {
	h1 := hash.NewFieldHasher().Int(42).Sum64()
	h2 := hash.NewFieldHasher().Int64(42).Sum64()
	if h1 != h2 {
		t.Errorf("Int(42) = %x, Int64(42) = %x; must be equal", h1, h2)
	}
}

func TestFieldHasher_float64EdgeCases(t *testing.T) {
	cases := []struct {
		name string
		a, b float64
		same bool
	}{
		{"zero vs neg zero", 0.0, math.Copysign(0, -1), false},
		{"NaN vs NaN", math.NaN(), math.NaN(), true}, // same bit pattern
		{"Inf vs -Inf", math.Inf(1), math.Inf(-1), false},
		{"pi vs e", math.Pi, math.E, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h1 := hash.NewFieldHasher().Float64(tc.a).Sum64()
			h2 := hash.NewFieldHasher().Float64(tc.b).Sum64()
			if tc.same && h1 != h2 {
				t.Errorf("expected same hash, got %x vs %x", h1, h2)
			}
			if !tc.same && h1 == h2 {
				t.Errorf("expected different hash for %v vs %v", tc.a, tc.b)
			}
		})
	}
}

func TestFieldHasher_int64EdgeCases(t *testing.T) {
	cases := []struct {
		name string
		a, b int64
	}{
		{"zero vs one", 0, 1},
		{"min vs max", math.MinInt64, math.MaxInt64},
		{"zero vs min", 0, math.MinInt64},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h1 := hash.NewFieldHasher().Int64(tc.a).Sum64()
			h2 := hash.NewFieldHasher().Int64(tc.b).Sum64()
			if h1 == h2 {
				t.Errorf("Int64(%d) and Int64(%d) must produce different hashes", tc.a, tc.b)
			}
		})
	}
}

func TestFieldHasher_mixedTypes(t *testing.T) {
	// Realistic mark-price dedup key.
	h := hash.NewFieldHasher().
		String("mark_price").
		Int(1).
		String("binance").
		String("BTC-PERP").
		Float64(42000.5).
		Float64(41999.8).
		Float64(0.0001).
		Int64(1700000000000).
		Int64(1700000000001).
		Int64(1700000000002).
		Hex()
	if h == "" {
		t.Error("mixed-type hash must not be empty")
	}
	// Verify stability.
	h2 := hash.NewFieldHasher().
		String("mark_price").
		Int(1).
		String("binance").
		String("BTC-PERP").
		Float64(42000.5).
		Float64(41999.8).
		Float64(0.0001).
		Int64(1700000000000).
		Int64(1700000000001).
		Int64(1700000000002).
		Hex()
	if h != h2 {
		t.Error("mixed-type hash must be stable")
	}
}

func TestFieldHasher_hexIsLowercaseHex(t *testing.T) {
	got := hash.NewFieldHasher().String("test").Float64(3.14).Hex()
	for _, c := range got {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			t.Errorf("Hex() output contains non-hex char %q in %q", string(c), got)
			break
		}
	}
}

func TestFieldHasher_valueSemanticsForkedChain(t *testing.T) {
	base := hash.NewFieldHasher().String("prefix")
	fork1 := base.String("a").Hex()
	fork2 := base.String("b").Hex()
	if fork1 == fork2 {
		t.Error("forked chains must produce different hashes (value semantics)")
	}
	// base must be unmodified.
	baseHex := base.Hex()
	base2 := hash.NewFieldHasher().String("prefix").Hex()
	if baseHex != base2 {
		t.Error("base must not be mutated by forked chains")
	}
}

func TestFieldHasher_emptyStringBetweenFields(t *testing.T) {
	// An empty string between two fields inserts an extra separator,
	// so String("a").String("").String("b") differs from String("a").String("b").
	h1 := hash.NewFieldHasher().String("a").String("").String("b").Hex()
	h2 := hash.NewFieldHasher().String("a").String("b").Hex()
	if h1 == h2 {
		t.Error("empty string between fields must produce different hash (extra separator)")
	}
}

func BenchmarkFieldHasher_MarkPriceDedup(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		hash.NewFieldHasher().
			String("mark_price").
			Int(1).
			String("binance").
			String("BTC-PERP").
			Float64(42000.50).
			Float64(41999.80).
			Float64(0.0001).
			Int64(1700000000000).
			Int64(1700000000001).
			Int64(1700000000002).
			Hex()
	}
}

func BenchmarkFieldHasher_Sum64(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		hash.NewFieldHasher().
			String("mark_price").
			Int(1).
			String("binance").
			String("BTC-PERP").
			Float64(42000.50).
			Float64(41999.80).
			Float64(0.0001).
			Int64(1700000000000).
			Int64(1700000000001).
			Int64(1700000000002).
			Sum64()
	}
}
