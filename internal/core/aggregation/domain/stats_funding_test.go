package domain_test

import (
	"math"
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

func TestApplyFundingRate_SingleRate_SetsAvgAndLast(t *testing.T) {
	w := newStatsWindow(t)
	if p := w.ApplyFundingRate(0.0001, 1); p != nil {
		t.Fatalf("ApplyFundingRate: %v", p)
	}
	if w.FundingRateAvg != w.FundingRateLast {
		t.Fatalf("single rate: avg=%v should equal last=%v", w.FundingRateAvg, w.FundingRateLast)
	}
	if w.FundingRateLast == 0 {
		t.Fatal("single rate: last should not be zero")
	}
}

func TestApplyFundingRate_MultipleRates_AverageIsCorrect(t *testing.T) {
	w := newStatsWindow(t)
	rates := []float64{0.0003, -0.0001, 0.0, 0.0005, -0.0002}
	for i, r := range rates {
		if p := w.ApplyFundingRate(r, int64(i+1)); p != nil {
			t.Fatalf("ApplyFundingRate[%d]: %v", i, p)
		}
	}
	// Expected average: (0.0003 + -0.0001 + 0.0 + 0.0005 + -0.0002) / 5 = 0.0001
	// Due to fixed-point math there may be rounding, but it should be very close.
	expectedAvg := 0.0001
	tolerance := 1e-9
	if math.Abs(w.FundingRateAvg-expectedAvg) > tolerance {
		t.Fatalf("avg=%v want≈%v (tolerance=%v)", w.FundingRateAvg, expectedAvg, tolerance)
	}
	if w.FundingRateLast != rates[len(rates)-1] {
		t.Fatalf("last=%v want=%v", w.FundingRateLast, rates[len(rates)-1])
	}
}

func TestApplyFundingRate_NaN_Rejected(t *testing.T) {
	w := newStatsWindow(t)
	p := w.ApplyFundingRate(math.NaN(), 1)
	if p == nil || p.Code != problem.ValidationFailed {
		t.Fatalf("expected ValidationFailed for NaN, got=%v", p)
	}
}

func TestApplyFundingRate_Inf_Rejected(t *testing.T) {
	w := newStatsWindow(t)
	p := w.ApplyFundingRate(math.Inf(1), 1)
	if p == nil || p.Code != problem.ValidationFailed {
		t.Fatalf("expected ValidationFailed for +Inf, got=%v", p)
	}
	w2 := newStatsWindow(t)
	p = w2.ApplyFundingRate(math.Inf(-1), 1)
	if p == nil || p.Code != problem.ValidationFailed {
		t.Fatalf("expected ValidationFailed for -Inf, got=%v", p)
	}
}

func TestApplyFundingRate_ClosedWindow_Rejected(t *testing.T) {
	w := newStatsWindow(t)
	if p := w.ApplyMarkPrice(100, 1); p != nil {
		t.Fatalf("ApplyMarkPrice: %v", p)
	}
	if p := w.Close(120_000); p != nil {
		t.Fatalf("Close: %v", p)
	}
	p := w.ApplyFundingRate(0.0001, 2)
	if p == nil || p.Code != problem.Conflict {
		t.Fatalf("expected Conflict for closed window, got=%v", p)
	}
}

func TestApplyFundingRate_NonMonotonicSeq_Rejected(t *testing.T) {
	w := newStatsWindow(t)
	if p := w.ApplyFundingRate(0.0001, 10); p != nil {
		t.Fatalf("ApplyFundingRate seq=10: %v", p)
	}
	p := w.ApplyFundingRate(0.0002, 9)
	if p == nil || p.Code != problem.OutOfOrder {
		t.Fatalf("expected OutOfOrder for seq=9 after seq=10, got=%v", p)
	}
}

func TestApplyFundingRate_NegativeRate_Accepted(t *testing.T) {
	w := newStatsWindow(t)
	if p := w.ApplyFundingRate(-0.0005, 1); p != nil {
		t.Fatalf("ApplyFundingRate negative: %v", p)
	}
	if w.FundingRateLast >= 0 {
		t.Fatalf("expected negative last rate, got=%v", w.FundingRateLast)
	}
}

func TestApplyFundingRate_CloseWindow_PreservesValues(t *testing.T) {
	w := newStatsWindow(t)
	if p := w.ApplyFundingRate(0.0003, 1); p != nil {
		t.Fatalf("ApplyFundingRate #1: %v", p)
	}
	if p := w.ApplyFundingRate(-0.0001, 2); p != nil {
		t.Fatalf("ApplyFundingRate #2: %v", p)
	}
	avgBefore := w.FundingRateAvg
	lastBefore := w.FundingRateLast
	if p := w.Close(120_000); p != nil {
		t.Fatalf("Close: %v", p)
	}
	if w.FundingRateAvg != avgBefore {
		t.Fatalf("close changed avg: before=%v after=%v", avgBefore, w.FundingRateAvg)
	}
	if w.FundingRateLast != lastBefore {
		t.Fatalf("close changed last: before=%v after=%v", lastBefore, w.FundingRateLast)
	}
	if !w.IsClosed {
		t.Fatal("expected IsClosed=true")
	}
}
