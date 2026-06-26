package mdruntime

import (
	"bytes"
	"sync"

	"github.com/FabioCaffarello/stream-analytics/internal/actors/marketdata/ws"
)

type BackpressurePolicy string

const (
	BackpressureDropOldest       BackpressurePolicy = "drop_oldest"
	BackpressureDropDepthKeepOps BackpressurePolicy = "drop_depth_keep_trades"
)

func normalizeBackpressurePolicy(raw string) BackpressurePolicy {
	switch BackpressurePolicy(raw) {
	case BackpressureDropOldest, BackpressureDropDepthKeepOps:
		return BackpressurePolicy(raw)
	default:
		return BackpressureDropDepthKeepOps
	}
}

// wsQueue is a bounded, ring-buffer–backed queue with configurable
// backpressure eviction policy.  Enqueue, Pop, and drop_oldest are O(1).
type wsQueue struct {
	mu       sync.Mutex
	notEmpty *sync.Cond

	capacity int
	policy   BackpressurePolicy
	buf      []*ws.WsMessage // pre-allocated ring
	head     int             // index of first element
	count    int             // number of live elements
	closed   bool
}

func newWSQueue(capacity int, policy BackpressurePolicy) *wsQueue {
	if capacity <= 0 {
		capacity = 1024
	}
	q := &wsQueue{
		capacity: capacity,
		policy:   policy,
		buf:      make([]*ws.WsMessage, capacity),
	}
	q.notEmpty = sync.NewCond(&q.mu)
	return q
}

func (q *wsQueue) Close() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.closed = true
	q.notEmpty.Broadcast()
}

func (q *wsQueue) Enqueue(msg *ws.WsMessage) (dropped int, enteredBackpressure bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return 1, false
	}
	if q.count < q.capacity {
		q.buf[(q.head+q.count)%q.capacity] = msg
		q.count++
		q.notEmpty.Signal()
		return 0, false
	}

	enteredBackpressure = true
	switch q.policy {
	case BackpressureDropOldest:
		q.dropHead()
		q.pushTail(msg)
		q.notEmpty.Signal()
		return 1, true
	case BackpressureDropDepthKeepOps:
		if isDepthWSMessage(msg) {
			return 1, true
		}
		dropIdx := q.findFirstDepth()
		// Preserve markprice under pressure by evicting liquidation first
		// when no depth message is available.
		if dropIdx < 0 && isMarkPriceWSMessage(msg) {
			dropIdx = q.findFirstLiquidation()
		}
		if dropIdx < 0 {
			dropIdx = 0
		}
		q.removeAt(dropIdx)
		q.pushTail(msg)
		q.notEmpty.Signal()
		return 1, true
	default:
		q.dropHead()
		q.pushTail(msg)
		q.notEmpty.Signal()
		return 1, true
	}
}

func (q *wsQueue) Pop() (*ws.WsMessage, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for q.count == 0 && !q.closed {
		q.notEmpty.Wait()
	}
	if q.count == 0 && q.closed {
		return nil, false
	}
	msg := q.buf[q.head]
	q.buf[q.head] = nil // allow GC
	q.head = (q.head + 1) % q.capacity
	q.count--
	return msg, true
}

func (q *wsQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.count
}

// ── ring helpers (must be called under q.mu) ────────────────────────────────

func (q *wsQueue) dropHead() {
	q.buf[q.head] = nil
	q.head = (q.head + 1) % q.capacity
	q.count--
}

func (q *wsQueue) pushTail(msg *ws.WsMessage) {
	q.buf[(q.head+q.count)%q.capacity] = msg
	q.count++
}

// findFirstDepth scans from head looking for the first depth message.
// Returns the logical index (0-based from head), or -1 if none found.
func (q *wsQueue) findFirstDepth() int {
	for i := 0; i < q.count; i++ {
		if isDepthWSMessage(q.buf[(q.head+i)%q.capacity]) {
			return i
		}
	}
	return -1
}

func (q *wsQueue) findFirstLiquidation() int {
	for i := 0; i < q.count; i++ {
		if isLiquidationWSMessage(q.buf[(q.head+i)%q.capacity]) {
			return i
		}
	}
	return -1
}

// removeAt removes the element at logical index idx (0-based from head)
// and shifts subsequent elements backward to fill the gap.
func (q *wsQueue) removeAt(idx int) {
	if idx == 0 {
		q.dropHead()
		return
	}
	// Shift elements after idx toward head to fill the gap.
	for i := idx; i < q.count-1; i++ {
		src := (q.head + i + 1) % q.capacity
		dst := (q.head + i) % q.capacity
		q.buf[dst] = q.buf[src]
	}
	// Clear the old tail slot.
	tail := (q.head + q.count - 1) % q.capacity
	q.buf[tail] = nil
	q.count--
}

func isDepthWSMessage(msg *ws.WsMessage) bool {
	if msg == nil || len(msg.Data) == 0 {
		return false
	}
	return bytes.Contains(msg.Data, []byte(`"depthUpdate"`))
}

func isLiquidationWSMessage(msg *ws.WsMessage) bool {
	if msg == nil || len(msg.Data) == 0 {
		return false
	}
	return bytes.Contains(msg.Data, []byte(`"forceOrder"`))
}

func isMarkPriceWSMessage(msg *ws.WsMessage) bool {
	if msg == nil || len(msg.Data) == 0 {
		return false
	}
	return bytes.Contains(msg.Data, []byte(`"markPriceUpdate"`))
}
