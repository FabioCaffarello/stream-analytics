package slo

import (
	"sync"
	"time"
)

// SLOName identifies a named service level objective.
type SLOName string

const (
	SLOIngestSuccess   SLOName = "ingest_success"
	SLODeliveryLatency SLOName = "delivery_latency"
	SLODataLossGuard   SLOName = "data_loss_guard"
)

// SLODefinition declares a single service level objective with budget parameters.
type SLODefinition struct {
	Name           SLOName
	ObjectivePct   float64       // target percentage (e.g. 99.9)
	WindowDuration time.Duration // error budget window (e.g. 30 days)
	BurnRateFast   float64       // 5m/1h fast burn threshold
	BurnRateSlow   float64       // 30m/6h slow burn threshold
}

// SLOState captures the current breach status of a single SLO.
type SLOState struct {
	Name             SLOName
	Breached         bool
	BreachFast       bool
	BreachSlow       bool
	BurnRateFast     float64
	BurnRateSlow     float64
	ErrorBudgetRatio float64 // remaining budget as 0.0–1.0
}

// MetricSnapshot provides the counter values needed to evaluate SLOs.
type MetricSnapshot struct {
	// Ingest SLO: success ratio
	IngestTotal float64
	IngestOK    float64

	// Delivery SLO: latency good ratio
	DeliveryTotal float64
	DeliveryGood  float64

	// Data loss SLO: drop counters summed
	DataLossDrops float64
	DataLossTotal float64
}

// Evaluator tracks SLO breach state from periodic metric snapshots.
// Thread-safe: Updated on metrics tick, queried by actors.
type Evaluator struct {
	mu          sync.RWMutex
	definitions map[SLOName]SLODefinition
	states      map[SLOName]SLOState
	prevSnap    MetricSnapshot
	initialized bool
}

// NewEvaluator creates an evaluator with the default MR SLO definitions.
func NewEvaluator() *Evaluator {
	defs := map[SLOName]SLODefinition{
		SLOIngestSuccess: {
			Name:           SLOIngestSuccess,
			ObjectivePct:   99.9,
			WindowDuration: 30 * 24 * time.Hour,
			BurnRateFast:   14.4,
			BurnRateSlow:   6.0,
		},
		SLODeliveryLatency: {
			Name:           SLODeliveryLatency,
			ObjectivePct:   99.0,
			WindowDuration: 30 * 24 * time.Hour,
			BurnRateFast:   14.4,
			BurnRateSlow:   6.0,
		},
		SLODataLossGuard: {
			Name:           SLODataLossGuard,
			ObjectivePct:   99.99,
			WindowDuration: 30 * 24 * time.Hour,
			BurnRateFast:   14.4,
			BurnRateSlow:   6.0,
		},
	}
	states := make(map[SLOName]SLOState, len(defs))
	for name := range defs {
		states[name] = SLOState{Name: name, ErrorBudgetRatio: 1.0}
	}
	return &Evaluator{
		definitions: defs,
		states:      states,
	}
}

// NewEvaluatorWithDefs creates an evaluator with custom SLO definitions.
func NewEvaluatorWithDefs(defs []SLODefinition) *Evaluator {
	dm := make(map[SLOName]SLODefinition, len(defs))
	states := make(map[SLOName]SLOState, len(defs))
	for i := range defs {
		dm[defs[i].Name] = defs[i]
		states[defs[i].Name] = SLOState{Name: defs[i].Name, ErrorBudgetRatio: 1.0}
	}
	return &Evaluator{
		definitions: dm,
		states:      states,
	}
}

// Update ingests a metric snapshot and recomputes breach state for all SLOs.
func (e *Evaluator) Update(snap MetricSnapshot) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.initialized {
		e.prevSnap = snap
		e.initialized = true
		return
	}

	if def, ok := e.definitions[SLOIngestSuccess]; ok {
		e.states[SLOIngestSuccess] = evaluateSLO(def, snap.IngestTotal, snap.IngestOK)
	}
	if def, ok := e.definitions[SLODeliveryLatency]; ok {
		e.states[SLODeliveryLatency] = evaluateSLO(def, snap.DeliveryTotal, snap.DeliveryGood)
	}
	if def, ok := e.definitions[SLODataLossGuard]; ok {
		e.states[SLODataLossGuard] = evaluateSLO(def, snap.DataLossTotal, snap.DataLossTotal-snap.DataLossDrops)
	}

	e.prevSnap = snap
}

// UpdateSLO evaluates a single named SLO against total/good counters.
func (e *Evaluator) UpdateSLO(name SLOName, total, good float64) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	def, ok := e.definitions[name]
	if !ok {
		return
	}
	if !e.initialized {
		e.initialized = true
	}
	e.states[name] = evaluateSLO(def, total, good)
}

// Breached returns true if the named SLO is currently in breach.
func (e *Evaluator) Breached(name SLOName) bool {
	if e == nil {
		return false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	s, ok := e.states[name]
	if !ok {
		return false
	}
	return s.Breached
}

// State returns the current state of a named SLO.
func (e *Evaluator) State(name SLOName) SLOState {
	if e == nil {
		return SLOState{Name: name, ErrorBudgetRatio: 1.0}
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	s, ok := e.states[name]
	if !ok {
		return SLOState{Name: name, ErrorBudgetRatio: 1.0}
	}
	return s
}

// AllStates returns a snapshot of all SLO states.
func (e *Evaluator) AllStates() []SLOState {
	if e == nil {
		return nil
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]SLOState, 0, len(e.states))
	for _, s := range e.states {
		out = append(out, s)
	}
	return out
}

// AnyBreached returns true if any SLO is currently in breach.
func (e *Evaluator) AnyBreached() bool {
	if e == nil {
		return false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	for _, s := range e.states {
		if s.Breached {
			return true
		}
	}
	return false
}

// evaluateSLO computes breach state for a single SLO given total/good counters.
func evaluateSLO(def SLODefinition, total, good float64) SLOState {
	state := SLOState{Name: def.Name, ErrorBudgetRatio: 1.0}

	if total <= 0 {
		return state
	}

	objectiveRatio := def.ObjectivePct / 100.0
	errorBudgetTotal := (1.0 - objectiveRatio) * total
	errors := total - good
	if errors < 0 {
		errors = 0
	}

	if errorBudgetTotal > 0 {
		consumed := errors / errorBudgetTotal
		state.ErrorBudgetRatio = 1.0 - consumed
		if state.ErrorBudgetRatio < 0 {
			state.ErrorBudgetRatio = 0
		}
	} else {
		if errors > 0 {
			state.ErrorBudgetRatio = 0
		}
	}

	// Burn rate = (error rate) / (error budget rate).
	// error rate = errors / total
	// budget rate = 1 - objective
	budgetRate := 1.0 - objectiveRatio
	if budgetRate > 0 {
		errorRate := errors / total
		burnRate := errorRate / budgetRate
		state.BurnRateFast = burnRate
		state.BurnRateSlow = burnRate

		state.BreachFast = burnRate > def.BurnRateFast
		state.BreachSlow = burnRate > def.BurnRateSlow
	}

	state.Breached = state.BreachFast || state.BreachSlow || state.ErrorBudgetRatio <= 0
	return state
}
