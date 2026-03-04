package ticksize_test

import (
	"math"
	"testing"

	"github.com/market-raccoon/internal/shared/ticksize"
)

func TestAutoGroupSize_EdgeCases(t *testing.T) {
	if got := ticksize.AutoGroupSize(0); got != 1 {
		t.Fatalf("price=0 got=%f want=1", got)
	}
	if got := ticksize.AutoGroupSize(-10); got != 1 {
		t.Fatalf("price<0 got=%f want=1", got)
	}
	if got := ticksize.AutoGroupSize(0.0001); !almostEqual(got, 0.00000001) {
		t.Fatalf("price=0.0001 got=%f want=%f", got, 0.00000001)
	}
}

func TestRegistry_VenueLookup(t *testing.T) {
	r := ticksize.NewRegistry()
	if got := r.GroupSizeForPrice("binance", 90000); got != 10 {
		t.Fatalf("binance@90000 got=%f want=10", got)
	}
	if got := r.GroupSizeForPrice("coinbase", 3000); got != 0.01 {
		t.Fatalf("coinbase@3000 got=%f want=0.01", got)
	}
}

func TestRegistry_Fallback(t *testing.T) {
	r := ticksize.NewRegistry()
	price := 1234.56
	got := r.GroupSizeForPrice("unknown", price)
	want := ticksize.AutoGroupSize(price)
	if got != want {
		t.Fatalf("fallback got=%f want=%f", got, want)
	}
}

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-12
}
