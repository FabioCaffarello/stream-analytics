package deliveryruntime

// deliveryRing is a bounded, pre-allocated ring buffer for DeliveryEvent.
// It is NOT goroutine-safe; the SessionActor processes messages sequentially
// via the Hollywood actor model, so no mutex is needed.
//
// This replaces the previous []DeliveryEvent slice which leaked memory via
// reslicing (s.outbound[1:] keeps the backing array alive).
type deliveryRing struct {
	buf   []DeliveryEvent
	head  int // index of first element
	count int // number of live elements
	cap   int
}

func newDeliveryRing(capacity int) *deliveryRing {
	if capacity <= 0 {
		capacity = 256
	}
	return &deliveryRing{
		buf: make([]DeliveryEvent, capacity),
		cap: capacity,
	}
}

func (r *deliveryRing) Len() int     { return r.count }
func (r *deliveryRing) Cap() int     { return r.cap }
func (r *deliveryRing) IsFull() bool { return r.count >= r.cap }

// PushBack appends an event to the tail. Caller must check IsFull() first.
func (r *deliveryRing) PushBack(evt DeliveryEvent) {
	idx := (r.head + r.count) % r.cap
	r.buf[idx] = evt
	r.count++
}

// PopFront removes and returns the head element.
// Returns zero value and false if empty.
func (r *deliveryRing) PopFront() (DeliveryEvent, bool) {
	if r.count == 0 {
		var zero DeliveryEvent
		return zero, false
	}
	evt := r.buf[r.head]
	r.buf[r.head] = DeliveryEvent{} // allow GC of envelope payload
	r.head = (r.head + 1) % r.cap
	r.count--
	return evt, true
}

// DropFront discards the head element without returning it.
func (r *deliveryRing) DropFront() {
	if r.count == 0 {
		return
	}
	r.buf[r.head] = DeliveryEvent{}
	r.head = (r.head + 1) % r.cap
	r.count--
}

// At returns the element at logical index i (0 = head).
func (r *deliveryRing) At(i int) DeliveryEvent {
	return r.buf[(r.head+i)%r.cap]
}

// RemoveAt removes the element at logical index i and shifts subsequent
// elements backward to fill the gap. O(n) but only used in priority-drop
// path which scans the queue anyway.
func (r *deliveryRing) RemoveAt(i int) {
	if i == 0 {
		r.DropFront()
		return
	}
	// Shift elements after i toward head to fill the gap.
	for j := i; j < r.count-1; j++ {
		src := (r.head + j + 1) % r.cap
		dst := (r.head + j) % r.cap
		r.buf[dst] = r.buf[src]
	}
	// Clear old tail slot to allow GC.
	tail := (r.head + r.count - 1) % r.cap
	r.buf[tail] = DeliveryEvent{}
	r.count--
}
