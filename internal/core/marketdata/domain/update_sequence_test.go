package domain_test

import (
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/core/marketdata/domain"
)

func TestNewUpdateSequence(t *testing.T) {
	seq, p := domain.NewUpdateSequence(101, 105)
	if p != nil {
		t.Fatalf("NewUpdateSequence failed: %v", p)
	}
	if seq.First != 101 || seq.Final != 105 {
		t.Fatalf("unexpected sequence: %#v", seq)
	}
}

func TestNewUpdateSequence_Invalid(t *testing.T) {
	if _, p := domain.NewUpdateSequence(0, 10); p == nil {
		t.Fatal("expected validation problem for first")
	}
	if _, p := domain.NewUpdateSequence(10, 9); p == nil {
		t.Fatal("expected validation problem for ordering")
	}
}
