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

func TestMustParseDuration_PanicsOnInvalid(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for invalid duration")
		}
	}()
	c := ConsumerConfig{MaxWebsocketLifetime: "invalid"}
	_ = c.MaxWebsocketLifetimeDuration()
}
