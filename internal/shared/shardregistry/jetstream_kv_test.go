package shardregistry

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

// ---------------------------------------------------------------------------
// normalizeKVError — pure function tests
// ---------------------------------------------------------------------------

func TestNormalizeKVError(t *testing.T) {
	tests := []struct {
		name    string
		input   error
		want    error
		wantNil bool
	}{
		{
			name:    "nil error returns nil",
			input:   nil,
			wantNil: true,
		},
		{
			name:  "ErrKeyNotFound maps to errKeyNotFound",
			input: nats.ErrKeyNotFound,
			want:  errKeyNotFound,
		},
		{
			name:  "wrapped ErrKeyNotFound maps to errKeyNotFound",
			input: fmt.Errorf("get failed: %w", nats.ErrKeyNotFound),
			want:  errKeyNotFound,
		},
		{
			name:  "ErrKeyExists maps to errKeyExists",
			input: nats.ErrKeyExists,
			want:  errKeyExists,
		},
		{
			name:  "wrapped ErrKeyExists maps to errKeyExists",
			input: fmt.Errorf("create failed: %w", nats.ErrKeyExists),
			want:  errKeyExists,
		},
		{
			name:  "wrong last sequence substring maps to errWrongVersion",
			input: errors.New("nats: wrong last sequence: 5"),
			want:  errWrongVersion,
		},
		{
			name:  "Wrong Last Sequence mixed case maps to errWrongVersion",
			input: errors.New("WRONG LAST SEQUENCE"),
			want:  errWrongVersion,
		},
		{
			name:  "wrong last sequence embedded in longer message",
			input: errors.New("update key shard/0: wrong last sequence"),
			want:  errWrongVersion,
		},
		{
			name:  "context.Canceled passes through",
			input: context.Canceled,
			want:  context.Canceled,
		},
		{
			name:  "context.DeadlineExceeded passes through",
			input: context.DeadlineExceeded,
			want:  context.DeadlineExceeded,
		},
		{
			name:  "unknown error passes through unchanged",
			input: errors.New("some random nats error"),
			want:  errors.New("some random nats error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeKVError(tt.input)
			if tt.wantNil {
				if got != nil {
					t.Fatalf("expected nil, got %v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected non-nil error, got nil")
			}
			// For sentinel errors, check identity.
			if errors.Is(tt.want, errKeyNotFound) || errors.Is(tt.want, errKeyExists) ||
				errors.Is(tt.want, errWrongVersion) || errors.Is(tt.want, context.Canceled) ||
				errors.Is(tt.want, context.DeadlineExceeded) {
				if !errors.Is(got, tt.want) {
					t.Fatalf("expected errors.Is(%v, %v) to be true", got, tt.want)
				}
				return
			}
			// For unknown/passthrough errors, compare message.
			if got.Error() != tt.want.Error() {
				t.Fatalf("expected error message %q, got %q", tt.want.Error(), got.Error())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// fakeNatsKVEntry — implements nats.KeyValueEntry for test use
// ---------------------------------------------------------------------------

type fakeNatsKVEntry struct {
	key   string
	value []byte
	rev   uint64
}

func (e *fakeNatsKVEntry) Bucket() string             { return "test-bucket" }
func (e *fakeNatsKVEntry) Key() string                { return e.key }
func (e *fakeNatsKVEntry) Value() []byte              { return append([]byte(nil), e.value...) }
func (e *fakeNatsKVEntry) Revision() uint64           { return e.rev }
func (e *fakeNatsKVEntry) Created() time.Time         { return time.Time{} }
func (e *fakeNatsKVEntry) Delta() uint64              { return 0 }
func (e *fakeNatsKVEntry) Operation() nats.KeyValueOp { return nats.KeyValuePut }

// ---------------------------------------------------------------------------
// fakeNatsKV — implements nats.KeyValue for testing jsKV delegation
// ---------------------------------------------------------------------------

type fakeNatsKV struct {
	getFunc    func(key string) (nats.KeyValueEntry, error)
	createFunc func(key string, value []byte) (uint64, error)
	updateFunc func(key string, value []byte, last uint64) (uint64, error)
	deleteFunc func(key string, opts ...nats.DeleteOpt) error
	keysFunc   func(opts ...nats.WatchOpt) ([]string, error)
}

func (f *fakeNatsKV) Get(key string) (nats.KeyValueEntry, error) {
	if f.getFunc != nil {
		return f.getFunc(key)
	}
	return nil, nats.ErrKeyNotFound
}

func (f *fakeNatsKV) GetRevision(key string, revision uint64) (nats.KeyValueEntry, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeNatsKV) Put(key string, value []byte) (uint64, error) {
	return 0, errors.New("not implemented")
}

func (f *fakeNatsKV) PutString(key string, value string) (uint64, error) {
	return 0, errors.New("not implemented")
}

func (f *fakeNatsKV) Create(key string, value []byte) (uint64, error) {
	if f.createFunc != nil {
		return f.createFunc(key, value)
	}
	return 0, nats.ErrKeyExists
}

func (f *fakeNatsKV) Update(key string, value []byte, last uint64) (uint64, error) {
	if f.updateFunc != nil {
		return f.updateFunc(key, value, last)
	}
	return 0, errors.New("wrong last sequence")
}

func (f *fakeNatsKV) Delete(key string, opts ...nats.DeleteOpt) error {
	if f.deleteFunc != nil {
		return f.deleteFunc(key, opts...)
	}
	return nil
}

func (f *fakeNatsKV) Purge(key string, opts ...nats.DeleteOpt) error {
	return errors.New("not implemented")
}

func (f *fakeNatsKV) Watch(keys string, opts ...nats.WatchOpt) (nats.KeyWatcher, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeNatsKV) WatchAll(opts ...nats.WatchOpt) (nats.KeyWatcher, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeNatsKV) WatchFiltered(keys []string, opts ...nats.WatchOpt) (nats.KeyWatcher, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeNatsKV) Keys(opts ...nats.WatchOpt) ([]string, error) {
	if f.keysFunc != nil {
		return f.keysFunc(opts...)
	}
	return nil, nil
}

func (f *fakeNatsKV) ListKeys(opts ...nats.WatchOpt) (nats.KeyLister, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeNatsKV) History(key string, opts ...nats.WatchOpt) ([]nats.KeyValueEntry, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeNatsKV) Bucket() string { return "test-bucket" }

func (f *fakeNatsKV) PurgeDeletes(opts ...nats.PurgeOpt) error {
	return errors.New("not implemented")
}

func (f *fakeNatsKV) Status() (nats.KeyValueStatus, error) {
	return nil, errors.New("not implemented")
}

// ---------------------------------------------------------------------------
// jsKV.Get — delegation and error normalization
// ---------------------------------------------------------------------------

func TestJsKV_Get_Success(t *testing.T) {
	fake := &fakeNatsKV{
		getFunc: func(key string) (nats.KeyValueEntry, error) {
			if key != "shard/0" {
				t.Fatalf("unexpected key: %s", key)
			}
			return &fakeNatsKVEntry{key: key, value: []byte("data"), rev: 42}, nil
		},
	}
	j := &jsKV{kv: fake}
	entry, err := j.Get("shard/0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.Revision() != 42 {
		t.Fatalf("expected revision 42, got %d", entry.Revision())
	}
	if string(entry.Value()) != "data" {
		t.Fatalf("expected value 'data', got %q", string(entry.Value()))
	}
}

func TestJsKV_Get_NotFound(t *testing.T) {
	fake := &fakeNatsKV{
		getFunc: func(key string) (nats.KeyValueEntry, error) {
			return nil, nats.ErrKeyNotFound
		},
	}
	j := &jsKV{kv: fake}
	_, err := j.Get("missing")
	if !errors.Is(err, errKeyNotFound) {
		t.Fatalf("expected errKeyNotFound, got %v", err)
	}
}

func TestJsKV_Get_UnknownError(t *testing.T) {
	sentinel := errors.New("connection timeout")
	fake := &fakeNatsKV{
		getFunc: func(key string) (nats.KeyValueEntry, error) {
			return nil, sentinel
		},
	}
	j := &jsKV{kv: fake}
	_, err := j.Get("key")
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected passthrough of unknown error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// jsKV.Create — delegation and error normalization
// ---------------------------------------------------------------------------

func TestJsKV_Create_Success(t *testing.T) {
	fake := &fakeNatsKV{
		createFunc: func(key string, value []byte) (uint64, error) {
			if key != "shard/1" {
				t.Fatalf("unexpected key: %s", key)
			}
			if string(value) != "payload" {
				t.Fatalf("unexpected value: %q", string(value))
			}
			return 7, nil
		},
	}
	j := &jsKV{kv: fake}
	rev, err := j.Create("shard/1", []byte("payload"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rev != 7 {
		t.Fatalf("expected revision 7, got %d", rev)
	}
}

func TestJsKV_Create_KeyExists(t *testing.T) {
	fake := &fakeNatsKV{
		createFunc: func(key string, value []byte) (uint64, error) {
			return 0, nats.ErrKeyExists
		},
	}
	j := &jsKV{kv: fake}
	rev, err := j.Create("shard/0", []byte("x"))
	if !errors.Is(err, errKeyExists) {
		t.Fatalf("expected errKeyExists, got %v", err)
	}
	if rev != 0 {
		t.Fatalf("expected revision 0 on error, got %d", rev)
	}
}

func TestJsKV_Create_WrappedKeyExists(t *testing.T) {
	fake := &fakeNatsKV{
		createFunc: func(key string, value []byte) (uint64, error) {
			return 0, fmt.Errorf("nats: %w", nats.ErrKeyExists)
		},
	}
	j := &jsKV{kv: fake}
	_, err := j.Create("shard/0", []byte("x"))
	if !errors.Is(err, errKeyExists) {
		t.Fatalf("expected errKeyExists for wrapped nats error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// jsKV.Update — delegation and error normalization
// ---------------------------------------------------------------------------

func TestJsKV_Update_Success(t *testing.T) {
	fake := &fakeNatsKV{
		updateFunc: func(key string, value []byte, last uint64) (uint64, error) {
			if key != "shard/2" {
				t.Fatalf("unexpected key: %s", key)
			}
			if last != 10 {
				t.Fatalf("expected last revision 10, got %d", last)
			}
			return 11, nil
		},
	}
	j := &jsKV{kv: fake}
	rev, err := j.Update("shard/2", []byte("updated"), 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rev != 11 {
		t.Fatalf("expected revision 11, got %d", rev)
	}
}

func TestJsKV_Update_WrongLastSequence(t *testing.T) {
	fake := &fakeNatsKV{
		updateFunc: func(key string, value []byte, last uint64) (uint64, error) {
			return 0, errors.New("nats: wrong last sequence: 5")
		},
	}
	j := &jsKV{kv: fake}
	rev, err := j.Update("shard/0", []byte("x"), 3)
	if !errors.Is(err, errWrongVersion) {
		t.Fatalf("expected errWrongVersion, got %v", err)
	}
	if rev != 0 {
		t.Fatalf("expected revision 0 on error, got %d", rev)
	}
}

func TestJsKV_Update_KeyNotFound(t *testing.T) {
	fake := &fakeNatsKV{
		updateFunc: func(key string, value []byte, last uint64) (uint64, error) {
			return 0, nats.ErrKeyNotFound
		},
	}
	j := &jsKV{kv: fake}
	_, err := j.Update("shard/99", []byte("x"), 1)
	if !errors.Is(err, errKeyNotFound) {
		t.Fatalf("expected errKeyNotFound, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// jsKV.Delete — delegation and error normalization
// ---------------------------------------------------------------------------

func TestJsKV_Delete_Success(t *testing.T) {
	called := false
	fake := &fakeNatsKV{
		deleteFunc: func(key string, opts ...nats.DeleteOpt) error {
			called = true
			if key != "shard/5" {
				t.Fatalf("unexpected key: %s", key)
			}
			return nil
		},
	}
	j := &jsKV{kv: fake}
	err := j.Delete("shard/5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("expected underlying Delete to be called")
	}
}

func TestJsKV_Delete_Error(t *testing.T) {
	sentinel := errors.New("nats: connection lost")
	fake := &fakeNatsKV{
		deleteFunc: func(key string, opts ...nats.DeleteOpt) error {
			return sentinel
		},
	}
	j := &jsKV{kv: fake}
	err := j.Delete("shard/0")
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected passthrough error, got %v", err)
	}
}

func TestJsKV_Delete_KeyNotFound(t *testing.T) {
	fake := &fakeNatsKV{
		deleteFunc: func(key string, opts ...nats.DeleteOpt) error {
			return nats.ErrKeyNotFound
		},
	}
	j := &jsKV{kv: fake}
	err := j.Delete("shard/0")
	if !errors.Is(err, errKeyNotFound) {
		t.Fatalf("expected errKeyNotFound, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// jsKV.Keys — delegation and error normalization
// ---------------------------------------------------------------------------

func TestJsKV_Keys_Success(t *testing.T) {
	fake := &fakeNatsKV{
		keysFunc: func(opts ...nats.WatchOpt) ([]string, error) {
			return []string{"shard/0", "shard/1", "shard/2"}, nil
		},
	}
	j := &jsKV{kv: fake}
	keys, err := j.Keys()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(keys) != 3 {
		t.Fatalf("expected 3 keys, got %d", len(keys))
	}
}

func TestJsKV_Keys_Empty(t *testing.T) {
	fake := &fakeNatsKV{
		keysFunc: func(opts ...nats.WatchOpt) ([]string, error) {
			return nil, nil
		},
	}
	j := &jsKV{kv: fake}
	keys, err := j.Keys()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("expected 0 keys, got %d", len(keys))
	}
}

func TestJsKV_Keys_Error(t *testing.T) {
	sentinel := errors.New("nats: no responders")
	fake := &fakeNatsKV{
		keysFunc: func(opts ...nats.WatchOpt) ([]string, error) {
			return nil, sentinel
		},
	}
	j := &jsKV{kv: fake}
	_, err := j.Keys()
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected passthrough error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// NewJetStreamKV — input validation (no NATS required)
// ---------------------------------------------------------------------------

func TestNewJetStreamKV_EmptyURL(t *testing.T) {
	_, _, err := NewJetStreamKV(context.Background(), "", "test-bucket", Config{})
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
	if !strings.Contains(err.Error(), "nats url must not be empty") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestNewJetStreamKV_WhitespaceOnlyURL(t *testing.T) {
	_, _, err := NewJetStreamKV(context.Background(), "   ", "test-bucket", Config{})
	if err == nil {
		t.Fatal("expected error for whitespace-only URL")
	}
	if !strings.Contains(err.Error(), "nats url must not be empty") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestNewJetStreamKV_InvalidURL(t *testing.T) {
	// A non-empty but unreachable URL will fail at nats.Connect, not at
	// our validation. This confirms we get past input validation and into
	// the NATS connect path, which returns a connection error.
	_, _, err := NewJetStreamKV(context.Background(), "nats://127.0.0.1:0", "bucket", Config{})
	if err == nil {
		t.Fatal("expected connection error for unreachable URL")
	}
	// Should NOT be our "must not be empty" error.
	if strings.Contains(err.Error(), "nats url must not be empty") {
		t.Fatalf("got input validation error instead of connection error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// normalizeKVError — edge cases and idempotency
// ---------------------------------------------------------------------------

func TestNormalizeKVError_Idempotent(t *testing.T) {
	// Normalizing an already-normalized error should return the same sentinel.
	tests := []struct {
		name  string
		input error
		want  error
	}{
		{"errKeyNotFound is stable", errKeyNotFound, errKeyNotFound},
		{"errKeyExists is stable", errKeyExists, errKeyExists},
		// errWrongVersion message does NOT contain "wrong last sequence"
		// substring, so it passes through as-is (which is the correct
		// sentinel anyway since it is not a match).
		{"errWrongVersion passes through", errWrongVersion, errWrongVersion},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// errKeyNotFound and errKeyExists are not nats sentinels,
			// so they will pass through normalizeKVError unchanged.
			got := normalizeKVError(tt.input)
			if got != tt.want {
				t.Fatalf("expected %v, got %v", tt.want, got)
			}
		})
	}
}

func TestNormalizeKVError_WrongLastSequenceVariants(t *testing.T) {
	variants := []string{
		"wrong last sequence",
		"Wrong Last Sequence",
		"WRONG LAST SEQUENCE",
		"nats: wrong last sequence: 12",
		"update shard/0: wrong last sequence: expected 5 got 6",
	}
	for _, msg := range variants {
		t.Run(msg, func(t *testing.T) {
			got := normalizeKVError(errors.New(msg))
			if !errors.Is(got, errWrongVersion) {
				t.Fatalf("expected errWrongVersion for %q, got %v", msg, got)
			}
		})
	}
}

func TestNormalizeKVError_DoesNotFalsePositiveOnSubstring(t *testing.T) {
	// Make sure errors that do not contain the "wrong last sequence"
	// substring are not incorrectly mapped to errWrongVersion.
	harmless := []string{
		"wrong sequence",
		"last sequence wrong",
		"sequence mismatch",
		"wrong_last_sequence",
	}
	for _, msg := range harmless {
		t.Run(msg, func(t *testing.T) {
			got := normalizeKVError(errors.New(msg))
			if errors.Is(got, errWrongVersion) {
				t.Fatalf("should not map %q to errWrongVersion", msg)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// jsKV integration: full round-trip through the wrapper
// ---------------------------------------------------------------------------

func newFakeMapKV() (*fakeNatsKV, *jsKV) {
	store := make(map[string]struct {
		value []byte
		rev   uint64
	})
	var nextRev uint64 = 1

	fake := &fakeNatsKV{
		createFunc: func(key string, value []byte) (uint64, error) {
			if _, exists := store[key]; exists {
				return 0, nats.ErrKeyExists
			}
			rev := nextRev
			nextRev++
			store[key] = struct {
				value []byte
				rev   uint64
			}{value: append([]byte(nil), value...), rev: rev}
			return rev, nil
		},
		getFunc: func(key string) (nats.KeyValueEntry, error) {
			s, ok := store[key]
			if !ok {
				return nil, nats.ErrKeyNotFound
			}
			return &fakeNatsKVEntry{key: key, value: s.value, rev: s.rev}, nil
		},
		updateFunc: func(key string, value []byte, last uint64) (uint64, error) {
			s, ok := store[key]
			if !ok {
				return 0, nats.ErrKeyNotFound
			}
			if s.rev != last {
				return 0, errors.New("wrong last sequence")
			}
			rev := nextRev
			nextRev++
			store[key] = struct {
				value []byte
				rev   uint64
			}{value: append([]byte(nil), value...), rev: rev}
			return rev, nil
		},
		deleteFunc: func(key string, opts ...nats.DeleteOpt) error {
			delete(store, key)
			return nil
		},
		keysFunc: func(opts ...nats.WatchOpt) ([]string, error) {
			out := make([]string, 0, len(store))
			for k := range store {
				out = append(out, k)
			}
			return out, nil
		},
	}

	return fake, &jsKV{kv: fake}
}

func TestJsKV_RoundTrip_CreateAndGet(t *testing.T) {
	_, j := newFakeMapKV()

	rev, err := j.Create("shard/0", []byte("v1"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if rev != 1 {
		t.Fatalf("create: expected rev 1, got %d", rev)
	}

	_, err = j.Create("shard/0", []byte("v1-dup"))
	if !errors.Is(err, errKeyExists) {
		t.Fatalf("duplicate create: expected errKeyExists, got %v", err)
	}

	entry, err := j.Get("shard/0")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(entry.Value()) != "v1" {
		t.Fatalf("get: expected value 'v1', got %q", string(entry.Value()))
	}
}

func TestJsKV_RoundTrip_UpdateAndKeys(t *testing.T) {
	_, j := newFakeMapKV()

	_, _ = j.Create("shard/0", []byte("v1"))

	rev2, err := j.Update("shard/0", []byte("v2"), 1)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if rev2 != 2 {
		t.Fatalf("update: expected rev 2, got %d", rev2)
	}

	_, err = j.Update("shard/0", []byte("v3"), 1)
	if !errors.Is(err, errWrongVersion) {
		t.Fatalf("stale update: expected errWrongVersion, got %v", err)
	}

	keys, err := j.Keys()
	if err != nil {
		t.Fatalf("keys: %v", err)
	}
	if len(keys) != 1 || keys[0] != "shard/0" {
		t.Fatalf("keys: expected [shard/0], got %v", keys)
	}
}

func TestJsKV_RoundTrip_DeleteThenGet(t *testing.T) {
	_, j := newFakeMapKV()

	_, _ = j.Create("shard/0", []byte("v1"))

	if err := j.Delete("shard/0"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	_, err := j.Get("shard/0")
	if !errors.Is(err, errKeyNotFound) {
		t.Fatalf("get after delete: expected errKeyNotFound, got %v", err)
	}
}
