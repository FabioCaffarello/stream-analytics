package observability

import "sync/atomic"

type WSStateSnapshot struct {
	SessionsActiveKnown       bool
	SessionsActive            int64
	PreferProtoSessionsKnown  bool
	PreferProtoSessions       int64
	DeliveriesProtoTotalKnown bool
	DeliveriesProtoTotal      uint64
	DeliveriesJSONTotalKnown  bool
	DeliveriesJSONTotal       uint64
	ReconnectsTotalKnown      bool
	ReconnectsTotal           uint64
}

type wsStateStore struct {
	sessionsActive       int64
	preferProtoSessions  int64
	deliveriesProtoTotal uint64
	deliveriesJSONTotal  uint64
	reconnectsTotal      uint64

	sessionsActiveKnown       uint32
	preferProtoSessionsKnown  uint32
	deliveriesProtoTotalKnown uint32
	deliveriesJSONTotalKnown  uint32
	reconnectsTotalKnown      uint32
}

var globalWSStateStore wsStateStore

func IncSessionsActive() {
	atomic.StoreUint32(&globalWSStateStore.sessionsActiveKnown, 1)
	atomic.AddInt64(&globalWSStateStore.sessionsActive, 1)
}

func DecSessionsActive() {
	atomic.StoreUint32(&globalWSStateStore.sessionsActiveKnown, 1)
	for {
		cur := atomic.LoadInt64(&globalWSStateStore.sessionsActive)
		if cur <= 0 {
			return
		}
		if atomic.CompareAndSwapInt64(&globalWSStateStore.sessionsActive, cur, cur-1) {
			return
		}
	}
}

func IncPreferProtoSessions() {
	atomic.StoreUint32(&globalWSStateStore.preferProtoSessionsKnown, 1)
	atomic.AddInt64(&globalWSStateStore.preferProtoSessions, 1)
}

func DecPreferProtoSessions() {
	atomic.StoreUint32(&globalWSStateStore.preferProtoSessionsKnown, 1)
	for {
		cur := atomic.LoadInt64(&globalWSStateStore.preferProtoSessions)
		if cur <= 0 {
			return
		}
		if atomic.CompareAndSwapInt64(&globalWSStateStore.preferProtoSessions, cur, cur-1) {
			return
		}
	}
}

func IncDeliveryProto() {
	atomic.StoreUint32(&globalWSStateStore.deliveriesProtoTotalKnown, 1)
	atomic.AddUint64(&globalWSStateStore.deliveriesProtoTotal, 1)
}

func IncDeliveryJSON() {
	atomic.StoreUint32(&globalWSStateStore.deliveriesJSONTotalKnown, 1)
	atomic.AddUint64(&globalWSStateStore.deliveriesJSONTotal, 1)
}

func IncReconnects() {
	atomic.StoreUint32(&globalWSStateStore.reconnectsTotalKnown, 1)
	atomic.AddUint64(&globalWSStateStore.reconnectsTotal, 1)
}

func SnapshotWSState() WSStateSnapshot {
	return WSStateSnapshot{
		SessionsActiveKnown:       atomic.LoadUint32(&globalWSStateStore.sessionsActiveKnown) == 1,
		SessionsActive:            atomic.LoadInt64(&globalWSStateStore.sessionsActive),
		PreferProtoSessionsKnown:  atomic.LoadUint32(&globalWSStateStore.preferProtoSessionsKnown) == 1,
		PreferProtoSessions:       atomic.LoadInt64(&globalWSStateStore.preferProtoSessions),
		DeliveriesProtoTotalKnown: atomic.LoadUint32(&globalWSStateStore.deliveriesProtoTotalKnown) == 1,
		DeliveriesProtoTotal:      atomic.LoadUint64(&globalWSStateStore.deliveriesProtoTotal),
		DeliveriesJSONTotalKnown:  atomic.LoadUint32(&globalWSStateStore.deliveriesJSONTotalKnown) == 1,
		DeliveriesJSONTotal:       atomic.LoadUint64(&globalWSStateStore.deliveriesJSONTotal),
		ReconnectsTotalKnown:      atomic.LoadUint32(&globalWSStateStore.reconnectsTotalKnown) == 1,
		ReconnectsTotal:           atomic.LoadUint64(&globalWSStateStore.reconnectsTotal),
	}
}
