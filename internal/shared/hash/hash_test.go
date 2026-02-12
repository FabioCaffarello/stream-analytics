package hash_test

import (
	"testing"

	"github.com/market-raccoon/internal/shared/hash"
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
