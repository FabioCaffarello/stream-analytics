package ring

import (
	"marketmonkey/types"
)

// Deque is a double‐ended ring buffer.
type Deque[T types.Unixer] struct {
	items []T
	head  int
	tail  int
	count int
	size  int
}

// NewDeque creates a new deque with the given capacity.
func NewDeque[T types.Unixer](capacity int) *Deque[T] {
	if capacity <= 0 {
		panic("capacity must be positive")
	}
	return &Deque[T]{
		items: make([]T, capacity),
		size:  capacity,
	}
}

// Len returns the current number of elements.
func (d *Deque[T]) Len() int {
	return d.count
}

func (d *Deque[T]) isFull() bool {
	return d.count == d.size
}

// PushBack appends an item at the tail.
func (d *Deque[T]) PushBack(item T) {
	if d.isFull() {
		// Remove from head to make space.
		d.head = (d.head + 1) % d.size
		d.count--
	}
	d.items[d.tail] = item
	d.tail = (d.tail + 1) % d.size
	d.count++
}

// PushFront prepends an item at the head.
func (d *Deque[T]) PushFront(item T) {
	if d.isFull() {
		// Remove from tail to make space.
		d.tail = (d.tail - 1 + d.size) % d.size
		d.count--
	}
	d.head = (d.head - 1 + d.size) % d.size
	d.items[d.head] = item
	d.count++
}

// PopFront removes and returns the front element.
func (d *Deque[T]) PopFront() (T, bool) {
	if d.count == 0 {
		var zero T
		return zero, false
	}
	item := d.items[d.head]
	var zero T
	d.items[d.head] = zero
	d.head = (d.head + 1) % d.size
	d.count--
	return item, true
}

// PopBack removes and returns the last element.
func (d *Deque[T]) PopBack() (T, bool) {
	if d.count == 0 {
		var zero T
		return zero, false
	}
	d.tail = (d.tail - 1 + d.size) % d.size
	item := d.items[d.tail]
	var zero T
	d.items[d.tail] = zero
	d.count--
	return item, true
}

// Front returns the first element.
func (d *Deque[T]) Front() T {
	if d.count == 0 {
		var zero T
		return zero
	}
	return d.items[d.head]
}

// Back returns the last element.
func (d *Deque[T]) Back() T {
	if d.count == 0 {
		var zero T
		return zero
	}
	index := (d.tail - 1 + d.size) % d.size
	return d.items[index]
}

// Get returns the element at index i.
func (d *Deque[T]) Get(i int) T {
	if i < 0 || i >= d.count {
		var zero T
		return zero
	}
	index := (d.head + i) % d.size
	return d.items[index]
}

// GetRange returns a slice of items between start (inclusive) and end (exclusive).
func (d *Deque[T]) GetRange(start, end int) []T {
	if start < 0 || end > d.count || start > end {
		return nil
	}
	length := end - start
	result := make([]T, length)
	for i := 0; i < length; i++ {
		result[i] = d.Get(start + i)
	}
	return result
}

// Inserts the given range at the correct place in the buffer.
// Returns -1 if older 1 if newer than the current.
func (d *Deque[T]) PushRange(items []T) int {
	if len(items) == 0 {
		return 0
	}
	first := items[0]
	if first.GetUnix() < d.Front().GetUnix() {
		for i := len(items) - 1; i >= 0; i-- {
			item := items[i]
			if item.GetUnix() < d.Front().GetUnix() {
				d.PushFront(item)
			}
		}
		return -1
	}
	if first.GetUnix() > d.Back().GetUnix() {
		for _, item := range items {
			if item.GetUnix() > d.Back().GetUnix() {
				d.PushBack(item)
			}
		}
		return 1
	}
	return 0
}

// SetBack updates the last element.
func (d *Deque[T]) SetBack(item T) {
	if d.count == 0 {
		return
	}
	index := (d.tail - 1 + d.size) % d.size
	d.items[index] = item
}

// Reset clears the buffer and resets all internal indices.
func (d *Deque[T]) Reset() {
	var zero T
	for i := range d.items {
		d.items[i] = zero
	}
	d.head = 0
	d.tail = 0
	d.count = 0
}
