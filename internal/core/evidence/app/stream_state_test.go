package app

import (
	"math"
	"testing"
)

func TestRingFloat64PushAndLen(t *testing.T) {
	var r RingFloat64
	if r.Len() != 0 {
		t.Fatalf("empty ring Len = %d, want 0", r.Len())
	}
	r.Push(1.0)
	if r.Len() != 1 {
		t.Fatalf("after 1 push Len = %d, want 1", r.Len())
	}
	for i := range 63 {
		r.Push(float64(i + 2))
	}
	if r.Len() != 64 {
		t.Fatalf("after 64 pushes Len = %d, want 64", r.Len())
	}
	// Wrap around
	r.Push(999)
	if r.Len() != 64 {
		t.Fatalf("after wrap Len = %d, want 64", r.Len())
	}
}

func TestRingFloat64Latest(t *testing.T) {
	var r RingFloat64
	if r.Latest() != 0 {
		t.Fatal("empty ring Latest should be 0")
	}
	r.Push(42.5)
	if r.Latest() != 42.5 {
		t.Fatalf("Latest = %f, want 42.5", r.Latest())
	}
	r.Push(99.9)
	if r.Latest() != 99.9 {
		t.Fatalf("Latest = %f, want 99.9", r.Latest())
	}
}

func TestRingFloat64MeanPartialFill(t *testing.T) {
	var r RingFloat64
	r.Push(10)
	r.Push(20)
	r.Push(30)
	got := r.Mean()
	want := 20.0
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("Mean = %f, want %f", got, want)
	}
}

func TestRingFloat64MeanFullWrap(t *testing.T) {
	var r RingFloat64
	for i := range 128 {
		r.Push(float64(i))
	}
	// After 128 pushes, buffer contains values 64..127
	want := (64.0 + 127.0) / 2 // = 95.5
	got := r.Mean()
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("Mean after wrap = %f, want %f", got, want)
	}
}

func TestRingFloat64MeanEmpty(t *testing.T) {
	var r RingFloat64
	if r.Mean() != 0 {
		t.Fatal("empty ring Mean should be 0")
	}
}

func TestRingFloat64StdDev(t *testing.T) {
	var r RingFloat64
	// stddev of {2, 4, 4, 4, 5, 5, 7, 9} = 2.0
	vals := []float64{2, 4, 4, 4, 5, 5, 7, 9}
	for _, v := range vals {
		r.Push(v)
	}
	got := r.StdDev()
	want := 2.0
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("StdDev = %f, want %f", got, want)
	}
}

func TestRingFloat64StdDevSingleValue(t *testing.T) {
	var r RingFloat64
	r.Push(5.0)
	if r.StdDev() != 0 {
		t.Fatal("single-value StdDev should be 0")
	}
}

func TestRingFloat64StdDevEmpty(t *testing.T) {
	var r RingFloat64
	if r.StdDev() != 0 {
		t.Fatal("empty StdDev should be 0")
	}
}

func TestRingFloat64Reset(t *testing.T) {
	var r RingFloat64
	r.Push(1)
	r.Push(2)
	r.Reset()
	if r.Len() != 0 {
		t.Fatalf("after Reset, Len = %d, want 0", r.Len())
	}
	if r.Latest() != 0 {
		t.Fatal("after Reset, Latest should be 0")
	}
}
