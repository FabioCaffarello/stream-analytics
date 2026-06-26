package app

import "math"

const ringCap = 64

// RingFloat64 is a fixed-size ring buffer for rolling statistics.
// Zero value is ready to use. No heap allocations.
type RingFloat64 struct {
	buf   [ringCap]float64
	head  int
	count int
}

// RingInt64 is a fixed-size ring buffer for sequence windows.
// Zero value is ready to use.
type RingInt64 struct {
	buf   [ringCap]int64
	head  int
	count int
}

// Push appends a value, overwriting the oldest if full.
func (r *RingFloat64) Push(v float64) {
	r.buf[r.head] = v
	r.head = (r.head + 1) % ringCap
	if r.count < ringCap {
		r.count++
	}
}

// Len returns the number of values currently in the buffer.
func (r *RingFloat64) Len() int {
	return r.count
}

// Latest returns the most recently pushed value.
// Returns 0 if empty.
func (r *RingFloat64) Latest() float64 {
	if r.count == 0 {
		return 0
	}
	idx := (r.head - 1 + ringCap) % ringCap
	return r.buf[idx]
}

// Mean returns the arithmetic mean of all values in the buffer.
// Returns 0 if empty.
func (r *RingFloat64) Mean() float64 {
	if r.count == 0 {
		return 0
	}
	sum := 0.0
	start := (r.head - r.count + ringCap) % ringCap
	for i := range r.count {
		sum += r.buf[(start+i)%ringCap]
	}
	return sum / float64(r.count)
}

// StdDev returns the population standard deviation of values in the buffer.
// Returns 0 if fewer than 2 values.
func (r *RingFloat64) StdDev() float64 {
	if r.count < 2 {
		return 0
	}
	mean := r.Mean()
	sumSq := 0.0
	start := (r.head - r.count + ringCap) % ringCap
	for i := range r.count {
		d := r.buf[(start+i)%ringCap] - mean
		sumSq += d * d
	}
	return math.Sqrt(sumSq / float64(r.count))
}

// Reset clears the buffer.
func (r *RingFloat64) Reset() {
	r.head = 0
	r.count = 0
}

// Push appends one sequence, overwriting oldest when full.
func (r *RingInt64) Push(v int64) {
	r.buf[r.head] = v
	r.head = (r.head + 1) % ringCap
	if r.count < ringCap {
		r.count++
	}
}

// Len returns the number of values currently in the buffer.
func (r *RingInt64) Len() int {
	return r.count
}

// Oldest returns the oldest sequence in the ring.
func (r *RingInt64) Oldest() int64 {
	if r.count == 0 {
		return 0
	}
	start := (r.head - r.count + ringCap) % ringCap
	return r.buf[start]
}
