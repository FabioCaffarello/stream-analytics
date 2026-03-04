package domain

import (
	"strings"

	"github.com/market-raccoon/internal/shared/problem"
	"github.com/market-raccoon/internal/shared/validation"
)

// WatermarkWindowConfig defines boundedness and late-arrival tolerance.
type WatermarkWindowConfig struct {
	MaxOpenWindows  int
	LateToleranceMs int64
}

type watermarkState struct {
	windowStart int64
}

// WatermarkWindowManager manages deterministic window lifecycle by event-time watermark.
type WatermarkWindowManager struct {
	maxOpenWindows  int
	lateToleranceMs int64
	states          map[WindowKey]watermarkState
	order           []WindowKey
}

// NewWatermarkWindowManager constructs a bounded watermark manager.
func NewWatermarkWindowManager(cfg WatermarkWindowConfig) (*WatermarkWindowManager, *problem.Problem) {
	if cfg.MaxOpenWindows <= 0 {
		return nil, problem.New(problem.ValidationFailed, "max_open_windows must be > 0")
	}
	if cfg.LateToleranceMs < 0 {
		return nil, problem.New(problem.ValidationFailed, "late_tolerance_ms must be >= 0")
	}
	return &WatermarkWindowManager{
		maxOpenWindows:  cfg.MaxOpenWindows,
		lateToleranceMs: cfg.LateToleranceMs,
		states:          make(map[WindowKey]watermarkState, cfg.MaxOpenWindows),
		order:           make([]WindowKey, 0, cfg.MaxOpenWindows),
	}, nil
}

// ActiveWindows returns the current number of tracked open windows.
func (m *WatermarkWindowManager) ActiveWindows() int {
	if m == nil {
		return 0
	}
	return len(m.states)
}

// Observe computes a deterministic window lifecycle decision for an incoming event.
func (m *WatermarkWindowManager) Observe(
	key WindowKey,
	eventTsMs int64,
	windowDurationMs int64,
) (WindowDecision, *problem.Problem) {
	if m == nil {
		return WindowDecision{}, problem.New(problem.ValidationFailed, "window manager is nil")
	}
	if p := validation.Collect(
		validation.NonEmptyString("venue", key.Venue),
		validation.NonEmptyString("instrument", key.Instrument),
		validation.NonEmptyString("timeframe", key.Timeframe),
		validation.PositiveInt("window_duration_ms", windowDurationMs),
	); p != nil {
		return WindowDecision{}, p
	}
	if eventTsMs < 0 {
		return WindowDecision{}, problem.Newf(problem.ValidationFailed, "event_ts_ms must be >= 0, got %d", eventTsMs)
	}

	normalizedKey := WindowKey{
		Venue:      strings.ToLower(strings.TrimSpace(key.Venue)),
		Instrument: strings.ToUpper(strings.TrimSpace(key.Instrument)),
		Timeframe:  strings.ToLower(strings.TrimSpace(key.Timeframe)),
	}
	windowStart := bucketWindowStart(eventTsMs, windowDurationMs)
	if state, ok := m.states[normalizedKey]; ok {
		return m.observeExisting(normalizedKey, state, windowStart), nil
	}
	return m.observeNew(normalizedKey, windowStart), nil
}

func (m *WatermarkWindowManager) observeExisting(key WindowKey, state watermarkState, windowStart int64) WindowDecision {
	if windowStart < state.windowStart {
		lag := state.windowStart - windowStart
		if lag > m.lateToleranceMs {
			return WindowDecision{
				WindowStart: windowStart,
				IsLate:      true,
			}
		}
		// Out-of-order events are treated as late for deterministic assignment
		// because this manager tracks only one open window per key.
		return WindowDecision{
			WindowStart: windowStart,
			IsLate:      true,
		}
	}

	if windowStart == state.windowStart {
		return WindowDecision{WindowStart: windowStart}
	}

	m.states[key] = watermarkState{windowStart: windowStart}
	return WindowDecision{
		WindowStart:         windowStart,
		ShouldCloseCurrent:  true,
		PreviousWindowStart: state.windowStart,
	}
}

func (m *WatermarkWindowManager) observeNew(key WindowKey, windowStart int64) WindowDecision {
	decision := WindowDecision{WindowStart: windowStart}
	if len(m.states) >= m.maxOpenWindows {
		forcedKey, forcedState := m.oldestWindow()
		delete(m.states, forcedKey)
		m.removeFromOrder(forcedKey)
		decision.ForcedClose = &ForcedWindowClose{
			Key:         forcedKey,
			WindowStart: forcedState.windowStart,
		}
	}
	m.states[key] = watermarkState{windowStart: windowStart}
	m.order = append(m.order, key)
	return decision
}

func (m *WatermarkWindowManager) oldestWindow() (WindowKey, watermarkState) {
	oldestKey := m.order[0]
	oldestState := m.states[oldestKey]
	for _, key := range m.order[1:] {
		state := m.states[key]
		if state.windowStart < oldestState.windowStart {
			oldestKey = key
			oldestState = state
			continue
		}
		if state.windowStart == oldestState.windowStart && compareWindowKey(key, oldestKey) < 0 {
			oldestKey = key
			oldestState = state
		}
	}
	return oldestKey, oldestState
}

func (m *WatermarkWindowManager) removeFromOrder(target WindowKey) {
	for i, key := range m.order {
		if key == target {
			copy(m.order[i:], m.order[i+1:])
			m.order = m.order[:len(m.order)-1]
			return
		}
	}
}

func compareWindowKey(a, b WindowKey) int {
	if a.Venue != b.Venue {
		return strings.Compare(a.Venue, b.Venue)
	}
	if a.Instrument != b.Instrument {
		return strings.Compare(a.Instrument, b.Instrument)
	}
	return strings.Compare(a.Timeframe, b.Timeframe)
}

func bucketWindowStart(tsMs, windowMs int64) int64 {
	return (tsMs / windowMs) * windowMs
}
