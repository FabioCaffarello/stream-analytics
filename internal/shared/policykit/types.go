package policykit

// Level captures overload severity.
type Level int

const (
	L0 Level = iota
	L1
	L2
	L3
)

// Signals carries deterministic runtime pressure inputs.
type Signals struct {
	QueueDepth    int
	QueueCapacity int

	Backlog    int
	BacklogCap int

	Occupancy int
	Limit     int

	ProcessingLatencyMs float64
}

// Decision is the action plan for the current event.
type Decision struct {
	Level   Level
	Actions []Action
}

// ActionType identifies an overload action.
type ActionType int

const (
	ActionDropDelta ActionType = iota
	ActionDegradeStride
	ActionCompressSnapshot
)

// Action is one overload action, optionally parameterized.
type Action struct {
	Type   ActionType
	Stride int
}

func (d Decision) HasAction(kind ActionType) bool {
	for _, action := range d.Actions {
		if action.Type == kind {
			return true
		}
	}
	return false
}

func (d Decision) DegradeStride() int {
	stride := 1
	for _, action := range d.Actions {
		if action.Type != ActionDegradeStride {
			continue
		}
		if action.Stride > stride {
			stride = action.Stride
		}
	}
	return stride
}

// Engine computes a decision from previous level and current signals.
type Engine interface {
	Decide(prev Level, signals Signals) Decision
}
