package signal

// fixedRing is a deterministic bounded ring buffer.
type fixedRing[T any] struct {
	buf  []T
	head int
	size int
}

func newFixedRing[T any](capacity int) fixedRing[T] {
	if capacity <= 0 {
		capacity = 1
	}
	return fixedRing[T]{buf: make([]T, capacity)}
}

func (r *fixedRing[T]) Capacity() int {
	return len(r.buf)
}

func (r *fixedRing[T]) Len() int {
	return r.size
}

func (r *fixedRing[T]) Push(v T) {
	if len(r.buf) == 0 {
		return
	}
	if r.size < len(r.buf) {
		idx := (r.head + r.size) % len(r.buf)
		r.buf[idx] = v
		r.size++
		return
	}
	r.buf[r.head] = v
	r.head = (r.head + 1) % len(r.buf)
}

func (r *fixedRing[T]) Values() []T {
	if r.size == 0 {
		return nil
	}
	out := make([]T, r.size)
	for i := 0; i < r.size; i++ {
		idx := (r.head + i) % len(r.buf)
		out[i] = r.buf[idx]
	}
	return out
}

func (r *fixedRing[T]) Oldest() (T, bool) {
	var zero T
	if r.size == 0 {
		return zero, false
	}
	return r.buf[r.head], true
}

func (r *fixedRing[T]) Newest() (T, bool) {
	var zero T
	if r.size == 0 {
		return zero, false
	}
	idx := (r.head + r.size - 1) % len(r.buf)
	return r.buf[idx], true
}
