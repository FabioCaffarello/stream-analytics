package ds_test

import (
	"fmt"
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
