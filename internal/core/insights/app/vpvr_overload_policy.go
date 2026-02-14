package app

import (
	"strings"
	"sync"

	"github.com/market-raccoon/internal/core/insights/domain"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/naming"
)

type VPVROverloadLevel int

const (
	VPVROverloadL0 VPVROverloadLevel = iota
	VPVROverloadL1
	VPVROverloadL2
	VPVROverloadL3
)

type VPVROverloadSignals struct {
	QueueDepth          int
	QueueCapacity       int
	BoundedMapOccupancy int
	BoundedMapLimit     int
	ProcessingLatencyMs float64
}

type VPVROverloadState struct {
	Level      VPVROverloadLevel
	EventCount uint64
}

type VPVROverloadInput struct {
	Venue          string
	Instrument     string
	Timeframe      string
	Seq            int64
	WindowClose    bool
	Signals        VPVROverloadSignals
	Snapshot       domain.VolumeProfileSnapshotV1
	Delta          domain.VolumeProfileSnapshotV1
	HasDelta       bool
	ProcessingMs   float64
	PartitionState VPVROverloadState
}

type VPVROverloadOutput struct {
	NextState    VPVROverloadState
	Level        VPVROverloadLevel
	Snapshot     domain.VolumeProfileSnapshotV1
	EmitSnapshot bool
	Delta        domain.VolumeProfileSnapshotV1
	EmitDelta    bool
}

type VPVREmitPolicy struct {
	mu     sync.Mutex
	states map[string]VPVROverloadState
}

func NewVPVREmitPolicy() *VPVREmitPolicy {
	return &VPVREmitPolicy{
		states: make(map[string]VPVROverloadState),
	}
}

func NextVPVROverloadLevel(prev VPVROverloadLevel, signals VPVROverloadSignals) VPVROverloadLevel {
	queueRatio := ratio(signals.QueueDepth, signals.QueueCapacity)
	mapRatio := ratio(signals.BoundedMapOccupancy, signals.BoundedMapLimit)
	latencyMs := signals.ProcessingLatencyMs

	severity := classifyOverloadSeverity(queueRatio, mapRatio, latencyMs)
	switch prev {
	case VPVROverloadL0:
		return severity
	case VPVROverloadL1:
		if severity >= VPVROverloadL2 {
			return severity
		}
		if shouldRecoverToL0(queueRatio, mapRatio, latencyMs) {
			return VPVROverloadL0
		}
		return VPVROverloadL1
	case VPVROverloadL2:
		if severity == VPVROverloadL3 {
			return VPVROverloadL3
		}
		if shouldRecoverToL1(queueRatio, mapRatio, latencyMs) {
			return VPVROverloadL1
		}
		return VPVROverloadL2
	default:
		if shouldRecoverToL2(queueRatio, mapRatio, latencyMs) {
			return VPVROverloadL2
		}
		return VPVROverloadL3
	}
}

func EvaluateVPVROverload(input VPVROverloadInput) VPVROverloadOutput {
	next := input.PartitionState
	next.Level = NextVPVROverloadLevel(input.PartitionState.Level, input.Signals)
	next.EventCount++

	return VPVROverloadOutput{
		NextState:    next,
		Level:        next.Level,
		Snapshot:     input.Snapshot,
		EmitSnapshot: true,
		Delta:        input.Delta,
		EmitDelta:    input.HasDelta,
	}
}

func (p *VPVREmitPolicy) Apply(input VPVROverloadInput) VPVROverloadOutput {
	if p == nil {
		return EvaluateVPVROverload(input)
	}
	key := overloadPartitionKey(input.Venue, input.Instrument, input.Timeframe)
	p.mu.Lock()
	state := p.states[key]
	input.PartitionState = state
	out := EvaluateVPVROverload(input)
	p.states[key] = out.NextState
	p.mu.Unlock()

	metrics.SetVPVROverloadLevel(input.Venue, input.Instrument, input.Timeframe, int(out.Level))
	metrics.ObserveVPVRProcessingLatencyMilliseconds(input.ProcessingMs)
	return out
}

func overloadPartitionKey(venue, instrument, timeframe string) string {
	return naming.CanonicalVenue(venue) + "|" + naming.CanonicalInstrument(instrument) + "|" + strings.ToLower(strings.TrimSpace(timeframe))
}

func ratio(current, capacity int) float64 {
	if capacity <= 0 {
		return 0
	}
	if current <= 0 {
		return 0
	}
	return float64(current) / float64(capacity)
}

func classifyOverloadSeverity(queueRatio, mapRatio, latencyMs float64) VPVROverloadLevel {
	if queueRatio >= 0.92 || mapRatio >= 0.95 || latencyMs >= 80 {
		return VPVROverloadL3
	}
	if queueRatio >= 0.80 || mapRatio >= 0.85 || latencyMs >= 40 {
		return VPVROverloadL2
	}
	if queueRatio >= 0.60 || mapRatio >= 0.70 || latencyMs >= 20 {
		return VPVROverloadL1
	}
	return VPVROverloadL0
}

func shouldRecoverToL0(queueRatio, mapRatio, latencyMs float64) bool {
	return queueRatio < 0.50 && mapRatio < 0.60 && latencyMs < 15
}

func shouldRecoverToL1(queueRatio, mapRatio, latencyMs float64) bool {
	return queueRatio < 0.70 && mapRatio < 0.80 && latencyMs < 30
}

func shouldRecoverToL2(queueRatio, mapRatio, latencyMs float64) bool {
	return queueRatio < 0.85 && mapRatio < 0.90 && latencyMs < 60
}
