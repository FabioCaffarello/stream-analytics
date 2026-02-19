package shardregistry

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeEntry struct {
	value []byte
	rev   uint64
}

func (e fakeEntry) Value() []byte    { return append([]byte(nil), e.value...) }
func (e fakeEntry) Revision() uint64 { return e.rev }

type fakeKV struct {
	mu      sync.Mutex
	entries map[string]fakeEntry
	nextRev uint64
}

func newFakeKV() *fakeKV {
	return &fakeKV{
		entries: make(map[string]fakeEntry),
		nextRev: 1,
	}
}

func (k *fakeKV) Get(key string) (kvEntry, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	entry, ok := k.entries[key]
	if !ok {
		return nil, errKeyNotFound
	}
	return entry, nil
}

func (k *fakeKV) Create(key string, value []byte) (uint64, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if _, ok := k.entries[key]; ok {
		return 0, errKeyExists
	}
	rev := k.nextRev
	k.nextRev++
	k.entries[key] = fakeEntry{value: append([]byte(nil), value...), rev: rev}
	return rev, nil
}

func (k *fakeKV) Update(key string, value []byte, revision uint64) (uint64, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	entry, ok := k.entries[key]
	if !ok {
		return 0, errKeyNotFound
	}
	if entry.rev != revision {
		return 0, errWrongVersion
	}
	rev := k.nextRev
	k.nextRev++
	k.entries[key] = fakeEntry{value: append([]byte(nil), value...), rev: rev}
	return rev, nil
}

func (k *fakeKV) Delete(key string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	delete(k.entries, key)
	return nil
}

func (k *fakeKV) Keys() ([]string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	out := make([]string, 0, len(k.entries))
	for key := range k.entries {
		out = append(out, key)
	}
	return out, nil
}

func (k *fakeKV) forceSet(key string, rec ownerRecord) {
	k.mu.Lock()
	defer k.mu.Unlock()
	raw, _ := json.Marshal(rec)
	rev := k.nextRev
	k.nextRev++
	k.entries[key] = fakeEntry{value: raw, rev: rev}
}

func TestDualOwnerRejected(t *testing.T) {
	kv := newFakeKV()
	reg := NewWithKV(kv, Config{LeaseTTL: 30 * time.Second, HeartbeatInterval: 10 * time.Second})
	reg.now = func() time.Time { return time.Unix(1_700_000_100, 0).UTC() }

	kv.forceSet("shard/0", ownerRecord{
		InstanceID:        "owner-a",
		Hostname:          "processor-a",
		PID:               101,
		StartedAt:         "2026-02-19T00:00:00Z",
		LastHeartbeatUnix: 1_700_000_090,
	})

	_, err := reg.Acquire(context.Background(), 0, 2, "owner-b")
	if err == nil {
		t.Fatal("expected dual-owner error")
	}
	var dualErr *DualOwnerError
	if !errors.As(err, &dualErr) {
		t.Fatalf("expected DualOwnerError, got %v", err)
	}
}

func TestOrphanDetected(t *testing.T) {
	kv := newFakeKV()
	reg := NewWithKV(kv, Config{LeaseTTL: 30 * time.Second, HeartbeatInterval: 10 * time.Second})
	reg.now = func() time.Time { return time.Unix(1_700_000_200, 0).UTC() }

	kv.forceSet("shard/0", ownerRecord{
		InstanceID:        "owner-0",
		Hostname:          "processor-0",
		PID:               201,
		StartedAt:         "2026-02-19T00:01:00Z",
		LastHeartbeatUnix: 1_700_000_195,
	})
	kv.forceSet("shard/1", ownerRecord{
		InstanceID:        "owner-1",
		Hostname:          "processor-1",
		PID:               202,
		StartedAt:         "2026-02-19T00:01:00Z",
		LastHeartbeatUnix: 1_700_000_195,
	})

	complete, err := reg.TopologyComplete(context.Background(), 3)
	if err != nil {
		t.Fatalf("topology check failed: %v", err)
	}
	if complete {
		t.Fatal("expected topology incomplete because shard/2 is missing")
	}
}

func TestGraceThenFailStrict(t *testing.T) {
	kv := newFakeKV()
	reg := NewWithKV(kv, Config{LeaseTTL: 30 * time.Second, HeartbeatInterval: 10 * time.Second})
	reg.now = time.Now

	kv.forceSet("shard/0", ownerRecord{
		InstanceID:        "owner-0",
		Hostname:          "processor-0",
		PID:               301,
		StartedAt:         "2026-02-19T00:02:00Z",
		LastHeartbeatUnix: time.Now().Unix(),
	})

	complete, err := reg.WaitForTopology(context.Background(), 2, 250*time.Millisecond)
	if err != nil {
		t.Fatalf("wait for topology failed: %v", err)
	}
	if complete {
		t.Fatal("expected incomplete topology after grace window")
	}
}

func TestLeaseLostTriggersShutdown(t *testing.T) {
	kv := newFakeKV()
	reg := NewWithKV(kv, Config{LeaseTTL: 30 * time.Second, HeartbeatInterval: 15 * time.Millisecond})
	reg.now = time.Now

	gotLease, err := reg.Acquire(context.Background(), 0, 1, "owner-a")
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}
	l := gotLease.(*lease)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	lostCh := make(chan error, 1)
	l.StartHeartbeat(ctx, func(err error) {
		lostCh <- err
	})

	time.Sleep(25 * time.Millisecond)
	kv.forceSet("shard/0", ownerRecord{
		InstanceID:        "owner-b",
		Hostname:          "processor-b",
		PID:               401,
		StartedAt:         "2026-02-19T00:03:00Z",
		LastHeartbeatUnix: time.Now().Unix(),
	})

	select {
	case err := <-lostCh:
		if err == nil {
			t.Fatal("expected non-nil lease-lost error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for lease-lost callback")
	}
}

func TestParseShardIndexFromKey(t *testing.T) {
	idx, ok := parseShardIndexFromKey("shard/2")
	if !ok || idx != 2 {
		t.Fatalf("parse shard key failed: idx=%d ok=%v", idx, ok)
	}
}

func TestDecodeOwnerRejectsEmptyInstance(t *testing.T) {
	raw := []byte(`{"instance_id":"","hostname":"h","pid":1,"started_at":"t","last_heartbeat_unix":1}`)
	_, err := decodeOwner(raw)
	if err == nil {
		t.Fatal("expected decodeOwner to fail for empty instance_id")
	}
}

func TestShardKey(t *testing.T) {
	if got := shardKey(3); got != "shard/3" {
		t.Fatalf("unexpected shard key: %s", got)
	}
}

func TestDualOwnerErrorMessage(t *testing.T) {
	err := (&DualOwnerError{
		ShardIndex: 2,
		Current: ownerRecord{
			InstanceID:        "owner",
			Hostname:          "host",
			PID:               99,
			LastHeartbeatUnix: 123,
		},
	}).Error()
	if !strings.Contains(err, "shard 2 already owned") {
		t.Fatalf("unexpected message: %s", err)
	}
}
