package policykit

// Threshold captures pressure bounds for transitions.
type Threshold struct {
	QueueRatio   float64
	BacklogRatio float64
	MapRatio     float64
	LatencyMs    float64
}

// ThresholdConfig configures enter/recover transitions and level actions.
type ThresholdConfig struct {
	EnterL1 Threshold
	EnterL2 Threshold
	EnterL3 Threshold

	RecoverL1 Threshold
	RecoverL2 Threshold
	RecoverL3 Threshold

	L2Stride int
	L3Stride int
}

func DefaultThresholdConfig() ThresholdConfig {
	return ThresholdConfig{
		EnterL1: Threshold{QueueRatio: 0.60, BacklogRatio: 0.60, MapRatio: 0.70, LatencyMs: 20},
		EnterL2: Threshold{QueueRatio: 0.80, BacklogRatio: 0.80, MapRatio: 0.85, LatencyMs: 40},
		EnterL3: Threshold{QueueRatio: 0.92, BacklogRatio: 0.92, MapRatio: 0.95, LatencyMs: 80},

		RecoverL1: Threshold{QueueRatio: 0.50, BacklogRatio: 0.50, MapRatio: 0.60, LatencyMs: 15},
		RecoverL2: Threshold{QueueRatio: 0.70, BacklogRatio: 0.70, MapRatio: 0.80, LatencyMs: 30},
		RecoverL3: Threshold{QueueRatio: 0.85, BacklogRatio: 0.85, MapRatio: 0.90, LatencyMs: 60},

		L2Stride: 2,
		L3Stride: 4,
	}
}

// ThresholdEngine applies deterministic threshold transitions with hysteresis.
type ThresholdEngine struct {
	cfg ThresholdConfig
}

func NewThresholdEngine(cfg ThresholdConfig) *ThresholdEngine {
	if cfg.L2Stride <= 1 {
		cfg.L2Stride = 2
	}
	if cfg.L3Stride <= 1 {
		cfg.L3Stride = 4
	}
	return &ThresholdEngine{cfg: cfg}
}

func (e *ThresholdEngine) Decide(prev Level, signals Signals) Decision {
	level := e.nextLevel(prev, signals)
	return Decision{
		Level:   level,
		Actions: e.actionsFor(level),
	}
}

func (e *ThresholdEngine) nextLevel(prev Level, signals Signals) Level {
	severity := e.classify(signals)
	switch prev {
	case L0:
		return severity
	case L1:
		if severity >= L2 {
			return severity
		}
		if meetsRecover(signals, e.cfg.RecoverL1) {
			return L0
		}
		return L1
	case L2:
		if severity == L3 {
			return L3
		}
		if meetsRecover(signals, e.cfg.RecoverL2) {
			return L1
		}
		return L2
	default:
		if meetsRecover(signals, e.cfg.RecoverL3) {
			return L2
		}
		return L3
	}
}

func (e *ThresholdEngine) classify(signals Signals) Level {
	if meetsEnter(signals, e.cfg.EnterL3) {
		return L3
	}
	if meetsEnter(signals, e.cfg.EnterL2) {
		return L2
	}
	if meetsEnter(signals, e.cfg.EnterL1) {
		return L1
	}
	return L0
}

func (e *ThresholdEngine) actionsFor(level Level) []Action {
	switch level {
	case L1:
		return []Action{{Type: ActionCompressSnapshot}}
	case L2:
		return []Action{
			{Type: ActionCompressSnapshot},
			{Type: ActionDegradeStride, Stride: e.cfg.L2Stride},
		}
	case L3:
		return []Action{
			{Type: ActionCompressSnapshot},
			{Type: ActionDegradeStride, Stride: e.cfg.L3Stride},
			{Type: ActionDropDelta},
		}
	default:
		return nil
	}
}

func meetsEnter(signals Signals, t Threshold) bool {
	return ratio(signals.QueueDepth, signals.QueueCapacity) >= t.QueueRatio ||
		ratio(signals.Backlog, signals.BacklogCap) >= t.BacklogRatio ||
		ratio(signals.Occupancy, signals.Limit) >= t.MapRatio ||
		signals.ProcessingLatencyMs >= t.LatencyMs
}

func meetsRecover(signals Signals, t Threshold) bool {
	return ratio(signals.QueueDepth, signals.QueueCapacity) < t.QueueRatio &&
		ratio(signals.Backlog, signals.BacklogCap) < t.BacklogRatio &&
		ratio(signals.Occupancy, signals.Limit) < t.MapRatio &&
		signals.ProcessingLatencyMs < t.LatencyMs
}

func ratio(current, capacity int) float64 {
	if capacity <= 0 || current <= 0 {
		return 0
	}
	return float64(current) / float64(capacity)
}
