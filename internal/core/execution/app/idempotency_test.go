package app

import "testing"

func TestIdempotencyCache_NotSeenOnFirstCall(t *testing.T) {
	c := newIdempotencyCache(8)
	if c.seen("intent-1", 1000) {
		t.Fatal("first call should not be seen")
	}
}

func TestIdempotencyCache_SeenOnSecondCall(t *testing.T) {
	c := newIdempotencyCache(8)
	c.seen("intent-1", 1000)
	if !c.seen("intent-1", 2000) {
		t.Fatal("second call with same ID should be seen")
	}
}

func TestIdempotencyCache_DifferentIDsNotSeen(t *testing.T) {
	c := newIdempotencyCache(8)
	c.seen("intent-1", 1000)
	if c.seen("intent-2", 2000) {
		t.Fatal("different intentID should not be seen")
	}
}

func TestIdempotencyCache_EmptyIDNeverSeen(t *testing.T) {
	c := newIdempotencyCache(8)
	if c.seen("", 1000) {
		t.Fatal("empty intentID should never be seen")
	}
	// Call again — still not seen.
	if c.seen("", 2000) {
		t.Fatal("empty intentID should never be seen on repeat")
	}
	if c.size() != 0 {
		t.Fatalf("empty IDs should not occupy cache, size=%d", c.size())
	}
}

func TestIdempotencyCache_EvictsOldestAtCapacity(t *testing.T) {
	c := newIdempotencyCache(3)
	c.seen("a", 1)
	c.seen("b", 2)
	c.seen("c", 3)
	if c.size() != 3 {
		t.Fatalf("size=%d want=3", c.size())
	}
	// Adding a 4th should evict "a".
	if c.seen("d", 4) {
		t.Fatal("d should not be seen")
	}
	if c.size() != 3 {
		t.Fatalf("size=%d want=3 after eviction", c.size())
	}
	// "a" should no longer be seen.
	if c.seen("a", 5) {
		t.Fatal("evicted entry 'a' should not be seen")
	}
	// Now "b" was evicted by "a" re-insertion.
	if c.seen("b", 6) {
		t.Fatal("evicted entry 'b' should not be seen")
	}
}

func TestIdempotencyCache_SizeTracksCorrectly(t *testing.T) {
	c := newIdempotencyCache(16)
	if c.size() != 0 {
		t.Fatalf("initial size=%d want=0", c.size())
	}
	c.seen("x", 1)
	c.seen("y", 2)
	if c.size() != 2 {
		t.Fatalf("size=%d want=2", c.size())
	}
	// Duplicate should not increase size.
	c.seen("x", 3)
	if c.size() != 2 {
		t.Fatalf("size=%d want=2 after duplicate", c.size())
	}
}

func TestIdempotencyCache_DefaultMaxSizeOnZero(t *testing.T) {
	c := newIdempotencyCache(0)
	if c.maxSize != 1024 {
		t.Fatalf("maxSize=%d want=1024", c.maxSize)
	}
}

func TestIdempotencyCache_DefaultMaxSizeOnNegative(t *testing.T) {
	c := newIdempotencyCache(-5)
	if c.maxSize != 1024 {
		t.Fatalf("maxSize=%d want=1024", c.maxSize)
	}
}
