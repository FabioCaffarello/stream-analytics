package config

import "testing"

func TestMustParseDuration_Valid(t *testing.T) {
	c := ConsumerConfig{MaxWebsocketLifetime: "1m", RespawnOverlap: "5s"}
	if got := c.MaxWebsocketLifetimeDuration().String(); got != "1m0s" {
		t.Fatalf("MaxWebsocketLifetimeDuration = %s, want 1m0s", got)
	}
	if got := c.RespawnOverlapDuration().String(); got != "5s" {
		t.Fatalf("RespawnOverlapDuration = %s, want 5s", got)
	}
}

func TestJetStreamHelpers_Valid(t *testing.T) {
	js := JetStreamConfig{
		DedupWindow: "5m",
		MaxAge:      "24h",
		MaxBytes:    "10GB",
		AckWait:     "30s",
	}

	if got := js.DedupWindowDuration().String(); got != "5m0s" {
		t.Fatalf("DedupWindowDuration = %s, want 5m0s", got)
	}
	if got := js.MaxAgeDuration().String(); got != "24h0m0s" {
		t.Fatalf("MaxAgeDuration = %s, want 24h0m0s", got)
	}
	if got := js.MaxBytesInt64(); got != 10_000_000_000 {
		t.Fatalf("MaxBytesInt64 = %d, want %d", got, int64(10_000_000_000))
	}
	if got := js.AckWaitDuration().String(); got != "30s" {
		t.Fatalf("AckWaitDuration = %s, want 30s", got)
	}
}

func TestMustParseDuration_PanicsOnInvalid(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for invalid duration")
		}
	}()
	c := ConsumerConfig{MaxWebsocketLifetime: "invalid"}
	_ = c.MaxWebsocketLifetimeDuration()
}
