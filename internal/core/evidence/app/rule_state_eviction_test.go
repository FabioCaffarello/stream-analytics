package app

import "testing"

func TestEvictOldestDeterministicByKey(t *testing.T) {
	a, b, c := 1, 2, 3
	streams := map[string]*int{
		"venue:eth-usdt": &b,
		"venue:btc-usdt": &a,
		"venue:sol-usdt": &c,
	}

	evictOldest(streams)

	if _, ok := streams["venue:btc-usdt"]; ok {
		t.Fatal("expected lexicographically smallest key to be evicted")
	}
	if len(streams) != 2 {
		t.Fatalf("len(streams) = %d, want 2", len(streams))
	}
}
