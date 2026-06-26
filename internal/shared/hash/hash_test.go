package hash_test

import (
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/shared/hash"
)

func TestHashBytes_stable(t *testing.T) {
	data := []byte("hello world")
	h1 := hash.HashBytes(data)
	h2 := hash.HashBytes(data)
	if h1 != h2 {
		t.Error("HashBytes must be stable for same input")
	}
	if len(h1) != 64 {
		t.Errorf("expected 64 hex chars, got %d", len(h1))
	}
}

func TestHashBytes_knownVector(t *testing.T) {
	// SHA-256 of empty string is well-known.
	want := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if got := hash.HashBytes([]byte{}); got != want {
		t.Errorf("HashBytes([]) = %q; want %q", got, want)
	}
}

func TestHashFields_stable(t *testing.T) {
	h1 := hash.HashFields("binance", "BTC-PERP", "123456")
	h2 := hash.HashFields("binance", "BTC-PERP", "123456")
	if h1 != h2 {
		t.Error("HashFields must be stable")
	}
}

func TestHashFields_orderMatters(t *testing.T) {
	h1 := hash.HashFields("a", "b")
	h2 := hash.HashFields("b", "a")
	if h1 == h2 {
		t.Error("HashFields(a,b) must differ from HashFields(b,a)")
	}
}

func TestHashFields_separatorPreventsCollision(t *testing.T) {
	// Without separator "ab","c" and "a","bc" would collide.
	h1 := hash.HashFields("ab", "c")
	h2 := hash.HashFields("a", "bc")
	if h1 == h2 {
		t.Error("HashFields must avoid concatenation collisions")
	}
}

func TestHashFieldsFast_stable(t *testing.T) {
	h1 := hash.HashFieldsFast("binance", "BTC-PERP", "123456")
	h2 := hash.HashFieldsFast("binance", "BTC-PERP", "123456")
	if h1 != h2 {
		t.Error("HashFieldsFast must be stable")
	}
}

func TestHashFieldsFast_orderMatters(t *testing.T) {
	h1 := hash.HashFieldsFast("a", "b")
	h2 := hash.HashFieldsFast("b", "a")
	if h1 == h2 {
		t.Error("HashFieldsFast(a,b) must differ from HashFieldsFast(b,a)")
	}
}

func TestHashFieldsFast_separatorPreventsCollision(t *testing.T) {
	h1 := hash.HashFieldsFast("ab", "c")
	h2 := hash.HashFieldsFast("a", "bc")
	if h1 == h2 {
		t.Error("HashFieldsFast must avoid concatenation collisions")
	}
}

func TestHashFieldsFast_isHex(t *testing.T) {
	got := hash.HashFieldsFast("test")
	for _, c := range got {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			t.Errorf("HashFieldsFast output contains non-hex char %q in %q", string(c), got)
			break
		}
	}
}

func TestHashFloat64Sequence_stable(t *testing.T) {
	vals := []float64{1.0, 2.5, 3.14159}
	h1 := hash.HashFloat64Sequence(vals)
	h2 := hash.HashFloat64Sequence(vals)
	if h1 != h2 {
		t.Error("HashFloat64Sequence must be stable")
	}
}

func TestHashFloat64Sequence_orderMatters(t *testing.T) {
	h1 := hash.HashFloat64Sequence([]float64{1.0, 2.0})
	h2 := hash.HashFloat64Sequence([]float64{2.0, 1.0})
	if h1 == h2 {
		t.Error("order must matter for HashFloat64Sequence")
	}
}

func TestHashFloat64Sequence_emptySlice(t *testing.T) {
	h1 := hash.HashFloat64Sequence(nil)
	h2 := hash.HashFloat64Sequence([]float64{})
	if h1 != h2 {
		t.Error("nil and empty slice must produce the same hash")
	}
}

func TestHashFloat64Sequence_isHex(t *testing.T) {
	got := hash.HashFloat64Sequence([]float64{3.14159, 2.71828})
	for _, c := range got {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			t.Errorf("output contains non-hex char %q in %q", string(c), got)
			break
		}
	}
}

func TestHashFloat64Sequence_lengthSensitive(t *testing.T) {
	h1 := hash.HashFloat64Sequence([]float64{1.0})
	h2 := hash.HashFloat64Sequence([]float64{1.0, 0.0})
	if h1 == h2 {
		t.Error("different-length slices must not collide")
	}
}

func BenchmarkHashFloat64Sequence(b *testing.B) {
	vals := make([]float64, 20)
	for i := range vals {
		vals[i] = float64(i) * 1.23456
	}
	b.ReportAllocs()
	for b.Loop() {
		hash.HashFloat64Sequence(vals)
	}
}
