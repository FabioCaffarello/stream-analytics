package domain_test

import (
	"testing"

	"github.com/market-raccoon/internal/core/delivery/domain"
)

func TestShouldDropOnBackpressure_dropNewest(t *testing.T) {
	if got := domain.ShouldDropOnBackpressure(domain.BackpressureDropNewest, 0, 1); got {
		t.Fatal("queue below cap should not drop")
	}
	if got := domain.ShouldDropOnBackpressure(domain.BackpressureDropNewest, 1, 1); !got {
		t.Fatal("queue at cap should drop")
	}
}

func TestNormalizeBackpressurePolicy_unknownDefaultsToDropNewest(t *testing.T) {
	p := domain.NormalizeBackpressurePolicy(domain.BackpressurePolicy("UNKNOWN_POLICY"))
	if p != domain.BackpressureDropNewest {
		t.Fatalf("policy=%q want=%q", p, domain.BackpressureDropNewest)
	}
}
