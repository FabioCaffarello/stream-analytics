package ds_test

import (
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/market-raccoon/internal/shared/clock"
	"github.com/market-raccoon/internal/shared/ds"
)

func TestBoundedMap_EvictBySizeLRU(t *testing.T) {
	clk := clock.NewFakeClock(time.Unix(0, 0))
	m := ds.NewBoundedMap[string, int](2, time.Hour, clk)

	m.Put("a", 1)
	m.Put("b", 2)
	if _, ok := m.Get("a"); !ok {
		t.Fatal("expected key a present")
	}
	m.Put("c", 3)

	if _, ok := m.Get("b"); ok {
		t.Fatal("expected key b evicted by LRU size policy")
	}
	if got := m.Len(); got != 2 {
		t.Fatalf("len=%d want=2", got)
	}
}

func TestBoundedMap_EvictByTTL(t *testing.T) {
	clk := clock.NewFakeClock(time.Unix(0, 0))
	m := ds.NewBoundedMap[string, int](10, time.Second, clk)
	m.Put("a", 1)

	clk.Advance(2 * time.Second)
	if _, ok := m.Get("a"); ok {
		t.Fatal("expected key a expired by TTL")
	}
	if got := m.Len(); got != 0 {
		t.Fatalf("len=%d want=0", got)
	}
}

func TestBoundedMap_TTL_GetTreatsExpiredAsMiss_WithoutSweep(t *testing.T) {
	clk := clock.NewFakeClock(time.Unix(0, 0))
	m := ds.NewBoundedMap[string, int](10, 10*time.Second, clk)

	m.Put("a", 1)
	clk.Advance(11 * time.Second)

	if _, ok := m.Get("a"); ok {
		t.Fatal("expected expired key to be treated as miss without explicit Sweep")
	}
	if got := m.Len(); got != 0 {
		t.Fatalf("len=%d want=0 after lazy expiration on Get", got)
	}
}

func TestBoundedMap_TouchPromotesLRU(t *testing.T) {
	clk := clock.NewFakeClock(time.Unix(0, 0))
	m := ds.NewBoundedMap[string, int](2, 0, clk)
	m.Put("a", 1)
	m.Put("b", 2)

	if _, ok := m.Get("a"); !ok {
		t.Fatal("expected key a")
	}
	m.Put("c", 3)

	if _, ok := m.Get("a"); !ok {
		t.Fatal("expected a to stay after touch")
	}
	if _, ok := m.Get("b"); ok {
		t.Fatal("expected b evicted after a touch")
	}
}

func TestBoundedMap_EvictReasonCallback(t *testing.T) {
	clk := clock.NewFakeClock(time.Unix(0, 0))
	m := ds.NewBoundedMap[string, int](1, time.Second, clk)
	var reasons []string
	m.SetOnEvict(func(_ string, _ int, reason string) {
		reasons = append(reasons, reason)
	})

	m.Put("a", 1)
	m.Put("b", 2)
	clk.Advance(2 * time.Second)
	m.Sweep()

	if len(reasons) < 2 {
		t.Fatalf("expected at least 2 evictions, got %d", len(reasons))
	}
	if reasons[0] != ds.EvictReasonSize {
		t.Fatalf("first reason=%s want=%s", reasons[0], ds.EvictReasonSize)
	}
	if reasons[1] != ds.EvictReasonTTL {
		t.Fatalf("second reason=%s want=%s", reasons[1], ds.EvictReasonTTL)
	}
}

func TestBoundedMap_ConcurrentAccess(t *testing.T) {
	clk := clock.NewFakeClock(time.Unix(0, 0))
	m := ds.NewBoundedMap[string, int](100, 0, clk)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				k := fmt.Sprintf("%d-%d", worker, j)
				m.Put(k, j)
				_, _ = m.Get(k)
			}
		}(i)
	}
	wg.Wait()
}

func TestBoundedMap_OnEvictRunsOutsideLock(t *testing.T) {
	clk := clock.NewFakeClock(time.Unix(0, 0))
	m := ds.NewBoundedMap[string, int](1, 0, clk)

	done := make(chan struct{})
	m.SetOnEvict(func(_ string, _ int, _ string) {
		_ = m.Len()
		close(done)
	})

	m.Put("a", 1)
	m.Put("b", 2)

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("onEvict appears blocked by map lock")
	}
}

func TestBoundedMap_SweepThrottling_ByOps(t *testing.T) {
	clk := clock.NewFakeClock(time.Unix(0, 0))
	m := ds.NewBoundedMap[string, int](16, time.Second, clk)
	m.SetSweepEveryOps(3)

	m.Put("a", 1)
	m.Put("b", 2)
	if got := m.Len(); got != 2 {
		t.Fatalf("len=%d want=2 before expiration", got)
	}

	clk.Advance(2 * time.Second)
	_, _ = m.Get("missing")

	if got := m.Len(); got != 0 {
		t.Fatalf("len=%d want=0 after ops-throttled sweep", got)
	}
}

func TestBoundedMap_SweepThrottling_ByInterval(t *testing.T) {
	clk := clock.NewFakeClock(time.Unix(0, 0))
	m := ds.NewBoundedMap[string, int](16, time.Second, clk)
	m.SetSweepMinInterval(5 * time.Second)

	m.Put("a", 1)
	m.Put("b", 2)
	clk.Advance(4 * time.Second)
	_, _ = m.Get("missing-before-interval")
	if got := m.Len(); got != 2 {
		t.Fatalf("len=%d want=2 before interval elapses", got)
	}

	clk.Advance(time.Second)
	_, _ = m.Get("missing-after-interval")
	if got := m.Len(); got != 0 {
		t.Fatalf("len=%d want=0 after interval-throttled sweep", got)
	}
}

func BenchmarkBoundedMapPutGet(b *testing.B) {
	clk := clock.NewFakeClock(time.Unix(0, 0))
	m := ds.NewBoundedMap[string, int](4096, 0, clk)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		key := strconv.Itoa(i & 1023)
		m.Put(key, i)
		_, _ = m.Get(key)
	}
}

func BenchmarkBoundedMapConcurrentPutGet(b *testing.B) {
	clk := clock.NewFakeClock(time.Unix(0, 0))
	m := ds.NewBoundedMap[string, int](8192, 0, clk)

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := strconv.Itoa(i & 2047)
			m.Put(key, i)
			_, _ = m.Get(key)
			i++
		}
	})
}

func BenchmarkBoundedMapPutGet_WithSweepEveryOp(b *testing.B) {
	benchBoundedMapPutGetSweepCadence(b, 1)
}

func BenchmarkBoundedMapPutGet_ThrottledSweep(b *testing.B) {
	benchBoundedMapPutGetSweepCadence(b, 1024)
}

func benchBoundedMapPutGetSweepCadence(b *testing.B, everyOps uint64) {
	clk := clock.NewFakeClock(time.Unix(0, 0))
	m := ds.NewBoundedMap[string, int](4096, time.Nanosecond, clk)
	m.SetSweepEveryOps(everyOps)

	for i := 0; i < 4096; i++ {
		m.Put(strconv.Itoa(i), i)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		clk.Advance(2 * time.Nanosecond)
		key := strconv.Itoa(4096 + i)
		m.Put(key, i)
		_, _ = m.Get(key)
	}
}
