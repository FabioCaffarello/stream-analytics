package mdruntime

import (
	"bytes"
	"sync"

	"github.com/market-raccoon/internal/actors/marketdata/ws"
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

type wsQueue struct {
	mu       sync.Mutex
	notEmpty *sync.Cond

	capacity int
	policy   BackpressurePolicy
	items    []*ws.WsMessage
	closed   bool
}

func newWSQueue(capacity int, policy BackpressurePolicy) *wsQueue {
	if capacity <= 0 {
		capacity = 1024
	}
	q := &wsQueue{
		capacity: capacity,
		policy:   policy,
		items:    make([]*ws.WsMessage, 0, capacity),
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
	if len(q.items) < q.capacity {
		q.items = append(q.items, msg)
		q.notEmpty.Signal()
		return 0, false
	}

	enteredBackpressure = true
	switch q.policy {
	case BackpressureDropOldest:
		q.items = q.items[1:]
		q.items = append(q.items, msg)
		q.notEmpty.Signal()
		return 1, true
	case BackpressureDropDepthKeepOps:
		if isDepthWSMessage(msg) {
			return 1, true
		}
		dropIdx := -1
		for i := 0; i < len(q.items); i++ {
			if isDepthWSMessage(q.items[i]) {
				dropIdx = i
				break
			}
		}
		if dropIdx < 0 {
			dropIdx = 0
		}
		q.items = append(q.items[:dropIdx], q.items[dropIdx+1:]...)
		q.items = append(q.items, msg)
		q.notEmpty.Signal()
		return 1, true
	default:
		q.items = q.items[1:]
		q.items = append(q.items, msg)
		q.notEmpty.Signal()
		return 1, true
	}
}

func (q *wsQueue) Pop() (*ws.WsMessage, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.items) == 0 && !q.closed {
		q.notEmpty.Wait()
	}
	if len(q.items) == 0 && q.closed {
		return nil, false
	}
	msg := q.items[0]
	q.items = q.items[1:]
	return msg, true
}

func (q *wsQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

func isDepthWSMessage(msg *ws.WsMessage) bool {
	if msg == nil || len(msg.Data) == 0 {
		return false
	}
	return bytes.Contains(msg.Data, []byte(`"depthUpdate"`))
}
