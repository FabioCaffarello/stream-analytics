package domain_test

import (
	"math"
	"testing"
	"time"

	"github.com/FabioCaffarello/stream-analytics/internal/core/evidence/domain"
)

func TestApplyConfidenceDecay_DeterministicAt30sAnd60s(t *testing.T) {
	base := 0.8
	halfLife := 60 * time.Second

	got30 := domain.ApplyConfidenceDecay(base, 30*time.Second, halfLife)
	got60 := domain.ApplyConfidenceDecay(base, 60*time.Second, halfLife)

	want30 := 0.8 * math.Pow(0.5, 0.5)
	want60 := 0.4

	if math.Abs(got30-want30) > 1e-12 {
		t.Fatalf("decay@30s=%0.12f want=%0.12f", got30, want30)
	}
	if math.Abs(got60-want60) > 1e-12 {
		t.Fatalf("decay@60s=%0.12f want=%0.12f", got60, want60)
	}
}

func TestApplyConfidenceDecay_ClampAndNoHalfLife(t *testing.T) {
	if got := domain.ApplyConfidenceDecay(1.2, 30*time.Second, 0); got != 1.0 {
		t.Fatalf("decay without half-life = %f want 1.0", got)
	}
	if got := domain.ApplyConfidenceDecay(-0.1, 30*time.Second, time.Minute); got != 0 {
		t.Fatalf("negative base decay = %f want 0", got)
	}
}
