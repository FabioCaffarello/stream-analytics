package hello

import "testing"

func TestMessageDefault(t *testing.T) {
	if got := Message(""); got != "hello world" {
		t.Fatalf("expected default greeting, got %q", got)
	}
}

func TestMessageWithName(t *testing.T) {
	if got := Message("workspace"); got != "hello workspace" {
		t.Fatalf("expected named greeting, got %q", got)
	}
}
