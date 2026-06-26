package domain_test

import (
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/core/insights/domain"
)

func TestSessionPresets_AllValid(t *testing.T) {
	for label, anchor := range domain.SessionPresets {
		if p := anchor.Validate(); p != nil {
			t.Errorf("preset %q failed validation: %v", label, p)
		}
	}
}

func TestResolveSessionBounds_UTCDaily(t *testing.T) {
	anchor := domain.SessionPresets["UTC_DAILY"]
	// 2026-03-07 12:00:00 UTC = 1772920800000
	refMs := int64(1772920800000)
	start, end, p := domain.ResolveSessionBounds(anchor, refMs)
	if p != nil {
		t.Fatalf("unexpected error: %v", p)
	}
	dayMs := int64(24 * 3600 * 1000)
	expectedStart := (refMs / dayMs) * dayMs
	if start != expectedStart {
		t.Errorf("start: got %d, want %d", start, expectedStart)
	}
	if end != expectedStart+dayMs {
		t.Errorf("end: got %d, want %d", end, expectedStart+dayMs)
	}
}

func TestResolveSessionBounds_Custom4H(t *testing.T) {
	anchor := domain.SessionPresets["CRYPTO_4H"]
	refMs := int64(1772920800000) // arbitrary
	start, end, p := domain.ResolveSessionBounds(anchor, refMs)
	if p != nil {
		t.Fatalf("unexpected error: %v", p)
	}
	fourH := int64(4 * 3600 * 1000)
	expectedStart := (refMs / fourH) * fourH
	if start != expectedStart {
		t.Errorf("start: got %d, want %d", start, expectedStart)
	}
	if end-start != fourH {
		t.Errorf("duration: got %d, want %d", end-start, fourH)
	}
}

func TestResolveSessionBounds_Deterministic(t *testing.T) {
	for label, anchor := range domain.SessionPresets {
		refMs := int64(1772920800000)
		s1, e1, _ := domain.ResolveSessionBounds(anchor, refMs)
		s2, e2, _ := domain.ResolveSessionBounds(anchor, refMs)
		if s1 != s2 || e1 != e2 {
			t.Errorf("preset %q: non-deterministic (%d,%d) vs (%d,%d)", label, s1, e1, s2, e2)
		}
	}
}

func TestSessionAnchor_InvalidKind(t *testing.T) {
	anchor := domain.SessionAnchor{Kind: "bogus", Label: "test", DurationMs: 1000}
	if p := anchor.Validate(); p == nil {
		t.Fatal("expected validation error for unknown kind")
	}
}

func TestSessionAnchor_EmptyLabel(t *testing.T) {
	anchor := domain.SessionAnchor{Kind: domain.SessionAnchorUTC, DurationMs: 1000}
	if p := anchor.Validate(); p == nil {
		t.Fatal("expected validation error for empty label")
	}
}

func TestResolveSessionBounds_NegativeRef(t *testing.T) {
	anchor := domain.SessionPresets["UTC_DAILY"]
	_, _, p := domain.ResolveSessionBounds(anchor, -1)
	if p == nil {
		t.Fatal("expected error for negative ref")
	}
}
