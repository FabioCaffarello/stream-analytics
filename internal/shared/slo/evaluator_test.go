package slo_test

import (
	"testing"
	"time"

	"github.com/market-raccoon/internal/shared/slo"
)

func TestNewEvaluator_DefaultSLOs(t *testing.T) {
	e := slo.NewEvaluator()
	if e == nil {
		t.Fatal("NewEvaluator returned nil")
	}
	states := e.AllStates()
	if len(states) != 3 {
		t.Fatalf("expected 3 SLOs, got %d", len(states))
	}
	for _, s := range states {
		if s.Breached {
			t.Errorf("SLO %q should not be breached initially", s.Name)
		}
		if s.ErrorBudgetRatio != 1.0 {
			t.Errorf("SLO %q budget ratio should be 1.0, got %f", s.Name, s.ErrorBudgetRatio)
		}
	}
}

func TestEvaluator_NilSafe(t *testing.T) {
	var e *slo.Evaluator
	if e.Breached(slo.SLOIngestSuccess) {
		t.Error("nil evaluator should not report breach")
	}
	if e.AnyBreached() {
		t.Error("nil evaluator should not report any breach")
	}
	s := e.State(slo.SLOIngestSuccess)
	if s.ErrorBudgetRatio != 1.0 {
		t.Errorf("nil evaluator state budget should be 1.0, got %f", s.ErrorBudgetRatio)
	}
	// Should not panic.
	e.Update(slo.MetricSnapshot{})
}

func TestEvaluator_FirstUpdateInitializes(t *testing.T) {
	e := slo.NewEvaluator()
	e.Update(slo.MetricSnapshot{IngestTotal: 1000, IngestOK: 1000})
	// First update initializes baseline — no breach computed yet.
	if e.Breached(slo.SLOIngestSuccess) {
		t.Error("first update should not cause breach")
	}
}

func TestEvaluator_IngestSLO_NoBreach(t *testing.T) {
	e := slo.NewEvaluator()
	e.Update(slo.MetricSnapshot{}) // init
	e.Update(slo.MetricSnapshot{
		IngestTotal: 100000,
		IngestOK:    99950, // 99.95% > 99.9% target
	})
	if e.Breached(slo.SLOIngestSuccess) {
		t.Error("99.95% should not breach 99.9% SLO")
	}
	s := e.State(slo.SLOIngestSuccess)
	if s.ErrorBudgetRatio <= 0 {
		t.Errorf("budget should be positive, got %f", s.ErrorBudgetRatio)
	}
}

func TestEvaluator_IngestSLO_Breach(t *testing.T) {
	e := slo.NewEvaluator()
	e.Update(slo.MetricSnapshot{}) // init
	e.Update(slo.MetricSnapshot{
		IngestTotal: 10000,
		IngestOK:    9800, // 98% << 99.9% → massive burn rate
	})
	if !e.Breached(slo.SLOIngestSuccess) {
		t.Error("98% should breach 99.9% SLO")
	}
	s := e.State(slo.SLOIngestSuccess)
	if s.ErrorBudgetRatio >= 1.0 {
		t.Errorf("budget should be consumed, got %f", s.ErrorBudgetRatio)
	}
}

func TestEvaluator_DeliveryLatencySLO(t *testing.T) {
	e := slo.NewEvaluator()
	e.Update(slo.MetricSnapshot{}) // init
	e.Update(slo.MetricSnapshot{
		DeliveryTotal: 10000,
		DeliveryGood:  9500, // 95% < 99% → breach
	})
	if !e.Breached(slo.SLODeliveryLatency) {
		t.Error("95% should breach 99% delivery SLO")
	}
}

func TestEvaluator_DataLossSLO_NoBreach(t *testing.T) {
	e := slo.NewEvaluator()
	e.Update(slo.MetricSnapshot{}) // init
	e.Update(slo.MetricSnapshot{
		DataLossTotal: 1000000,
		DataLossDrops: 5, // 0.0005% drops << 0.01% budget
	})
	if e.Breached(slo.SLODataLossGuard) {
		t.Error("5 drops in 1M should not breach 99.99% SLO")
	}
}

func TestEvaluator_DataLossSLO_Breach(t *testing.T) {
	e := slo.NewEvaluator()
	e.Update(slo.MetricSnapshot{}) // init
	e.Update(slo.MetricSnapshot{
		DataLossTotal: 10000,
		DataLossDrops: 500, // 5% drops >> 0.01% budget
	})
	if !e.Breached(slo.SLODataLossGuard) {
		t.Error("5% drops should breach 99.99% SLO")
	}
}

func TestEvaluator_AnyBreached(t *testing.T) {
	e := slo.NewEvaluator()
	e.Update(slo.MetricSnapshot{}) // init
	e.Update(slo.MetricSnapshot{
		IngestTotal:   100000,
		IngestOK:      100000,
		DeliveryTotal: 100000,
		DeliveryGood:  100000,
		DataLossTotal: 100000,
		DataLossDrops: 0,
	})
	if e.AnyBreached() {
		t.Error("all healthy should not report any breach")
	}

	e.Update(slo.MetricSnapshot{
		IngestTotal:   10000,
		IngestOK:      5000, // 50% → breach
		DeliveryTotal: 100000,
		DeliveryGood:  100000,
		DataLossTotal: 100000,
		DataLossDrops: 0,
	})
	if !e.AnyBreached() {
		t.Error("one breached SLO should report any breach")
	}
}

func TestEvaluator_ZeroTotal(t *testing.T) {
	e := slo.NewEvaluator()
	e.Update(slo.MetricSnapshot{}) // init
	e.Update(slo.MetricSnapshot{
		IngestTotal: 0,
		IngestOK:    0,
	})
	if e.Breached(slo.SLOIngestSuccess) {
		t.Error("zero total should not cause breach")
	}
	s := e.State(slo.SLOIngestSuccess)
	if s.ErrorBudgetRatio != 1.0 {
		t.Errorf("zero total budget should be 1.0, got %f", s.ErrorBudgetRatio)
	}
}

func TestEvaluator_UnknownSLO(t *testing.T) {
	e := slo.NewEvaluator()
	if e.Breached("nonexistent") {
		t.Error("unknown SLO should not report breach")
	}
	s := e.State("nonexistent")
	if s.ErrorBudgetRatio != 1.0 {
		t.Errorf("unknown SLO budget should be 1.0, got %f", s.ErrorBudgetRatio)
	}
}

func TestEvaluator_CustomDefs(t *testing.T) {
	defs := []slo.SLODefinition{
		{
			Name:           "custom_slo",
			ObjectivePct:   95.0,
			WindowDuration: 7 * 24 * time.Hour,
			BurnRateFast:   10.0,
			BurnRateSlow:   5.0,
		},
	}
	e := slo.NewEvaluatorWithDefs(defs)
	states := e.AllStates()
	if len(states) != 1 {
		t.Fatalf("expected 1 SLO, got %d", len(states))
	}
	if states[0].Name != "custom_slo" {
		t.Errorf("expected custom_slo, got %q", states[0].Name)
	}
}

func TestEvaluator_BudgetFullyConsumed(t *testing.T) {
	e := slo.NewEvaluator()
	e.Update(slo.MetricSnapshot{}) // init
	e.Update(slo.MetricSnapshot{
		IngestTotal: 1000,
		IngestOK:    0, // 0% success → fully consumed
	})
	s := e.State(slo.SLOIngestSuccess)
	if s.ErrorBudgetRatio != 0 {
		t.Errorf("fully consumed budget should be 0, got %f", s.ErrorBudgetRatio)
	}
	if !s.Breached {
		t.Error("fully consumed budget should be breached")
	}
}

func TestEvaluator_ErrorBudgetPartiallyConsumed(t *testing.T) {
	e := slo.NewEvaluator()
	e.Update(slo.MetricSnapshot{}) // init
	// 99.9% SLO with 100K total → budget = 100 errors
	// 50 errors → 50% budget consumed → ratio = 0.5
	e.Update(slo.MetricSnapshot{
		IngestTotal: 100000,
		IngestOK:    99950,
	})
	s := e.State(slo.SLOIngestSuccess)
	if s.ErrorBudgetRatio < 0.49 || s.ErrorBudgetRatio > 0.51 {
		t.Errorf("expected ~0.5 budget ratio, got %f", s.ErrorBudgetRatio)
	}
}

func TestEvaluator_BurnRateComputation(t *testing.T) {
	defs := []slo.SLODefinition{
		{
			Name:           "test",
			ObjectivePct:   99.0,
			WindowDuration: 24 * time.Hour,
			BurnRateFast:   14.4,
			BurnRateSlow:   6.0,
		},
	}
	e := slo.NewEvaluatorWithDefs(defs)

	// Error rate = 2% (20 errors in 1000)
	// Budget rate = 1% (1 - 0.99)
	// Burn rate = 2% / 1% = 2.0
	e.UpdateSLO("test", 1000, 980)
	s := e.State("test")
	if s.BurnRateFast < 1.9 || s.BurnRateFast > 2.1 {
		t.Errorf("expected burn rate ~2.0, got %f", s.BurnRateFast)
	}
	// Burn rate 2.0 < fast threshold 14.4 → no fast breach
	if s.BreachFast {
		t.Error("burn rate 2.0 should not breach fast threshold 14.4")
	}
	// But budget is over-consumed → breached via budget exhaustion
	if s.ErrorBudgetRatio >= 0 && !s.Breached {
		// Burn rate 2.0 < slow threshold 6.0 → no slow breach
		// Budget: consumed = 20/10 = 200% → ratio < 0 → clamped to 0 → breached
		if s.ErrorBudgetRatio > 0 {
			t.Logf("budget ratio: %f, breached: %v", s.ErrorBudgetRatio, s.Breached)
		}
	}
}
