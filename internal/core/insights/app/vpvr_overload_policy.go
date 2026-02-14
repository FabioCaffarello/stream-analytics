package app

import (
	"math"
	"slices"
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
	NextState      VPVROverloadState
	Level          VPVROverloadLevel
	Snapshot       domain.VolumeProfileSnapshotV1
	EmitSnapshot   bool
	Delta          domain.VolumeProfileSnapshotV1
	EmitDelta      bool
	Compressed     bool
	CompressRatio  float64
	CadenceDropped bool
	DeltaDropped   bool
	DropReason     string
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
	snapshot := input.Snapshot
	compressed := false
	compressRatio := 1.0
	if !input.WindowClose {
		snapshot, compressed, compressRatio = compressSnapshotByLevel(snapshot, next.Level)
	}
	emitSnapshot := shouldEmitSnapshotAtCadence(next.EventCount, next.Level, input.WindowClose)
	emitDelta, dropReason := shouldEmitDelta(next.EventCount, next.Level, input.WindowClose, input.HasDelta)

	return VPVROverloadOutput{
		NextState:      next,
		Level:          next.Level,
		Snapshot:       snapshot,
		EmitSnapshot:   emitSnapshot,
		Delta:          input.Delta,
		EmitDelta:      emitDelta,
		Compressed:     compressed,
		CompressRatio:  compressRatio,
		CadenceDropped: !emitSnapshot,
		DeltaDropped:   input.HasDelta && !emitDelta,
		DropReason:     dropReason,
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
	if out.Compressed {
		metrics.IncVPVRDegrade("compress")
	}
	if out.CadenceDropped {
		metrics.IncVPVRDegrade("cadence_skip")
	}
	if out.DeltaDropped {
		metrics.IncVPVRDrop(out.DropReason)
	}
	metrics.ObserveVPVRCompressRatio(out.CompressRatio)
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

func compressSnapshotByLevel(snapshot domain.VolumeProfileSnapshotV1, level VPVROverloadLevel) (domain.VolumeProfileSnapshotV1, bool, float64) {
	switch level {
	case VPVROverloadL1:
		return compressSnapshotByRatio(snapshot, 0.75)
	case VPVROverloadL2:
		return compressSnapshotByRatio(snapshot, 0.50)
	case VPVROverloadL3:
		return compressSnapshotByRatio(snapshot, 0.25)
	default:
		return snapshot, false, 1.0
	}
}

func compressSnapshotByRatio(snapshot domain.VolumeProfileSnapshotV1, ratio float64) (domain.VolumeProfileSnapshotV1, bool, float64) {
	if ratio >= 1 || len(snapshot.Buckets) <= 1 {
		return snapshot, false, 1.0
	}
	buckets := append([]domain.VolumeProfileBucketV1(nil), snapshot.Buckets...)
	target := int(math.Ceil(float64(len(buckets)) * ratio))
	if target < 1 {
		target = 1
	}
	if target >= len(buckets) {
		return snapshot, false, 1.0
	}

	slices.SortFunc(buckets, func(a, b domain.VolumeProfileBucketV1) int {
		if a.TotalVolume != b.TotalVolume {
			if a.TotalVolume > b.TotalVolume {
				return -1
			}
			return 1
		}
		return compareCompressedBucketPrice(a, b)
	})
	buckets = buckets[:target]
	slices.SortFunc(buckets, compareCompressedBucketPrice)

	out := snapshot
	out.Buckets = buckets
	poc := buckets[0].PriceLow
	maxTotal := buckets[0].TotalVolume
	for _, b := range buckets {
		if b.TotalVolume > maxTotal {
			maxTotal = b.TotalVolume
			poc = b.PriceLow
		}
	}
	out.POCPrice = poc
	out.ValueAreaLow = buckets[0].PriceLow
	out.ValueAreaHigh = buckets[len(buckets)-1].PriceHigh

	compressRatio := float64(len(buckets)) / float64(len(snapshot.Buckets))
	return out, true, compressRatio
}

func compareCompressedBucketPrice(a, b domain.VolumeProfileBucketV1) int {
	if a.PriceLow != b.PriceLow {
		if a.PriceLow < b.PriceLow {
			return -1
		}
		return 1
	}
	if a.PriceHigh < b.PriceHigh {
		return -1
	}
	if a.PriceHigh > b.PriceHigh {
		return 1
	}
	return 0
}

func shouldEmitSnapshotAtCadence(eventCount uint64, level VPVROverloadLevel, windowClose bool) bool {
	if windowClose {
		return true
	}
	stride := cadenceStrideForLevel(level)
	if stride <= 1 {
		return true
	}
	return eventCount%uint64(stride) == 0
}

func cadenceStrideForLevel(level VPVROverloadLevel) int {
	switch level {
	case VPVROverloadL2:
		return 2
	case VPVROverloadL3:
		return 4
	default:
		return 1
	}
}

func shouldEmitDelta(eventCount uint64, level VPVROverloadLevel, windowClose bool, hasDelta bool) (bool, string) {
	if !hasDelta {
		return false, ""
	}
	if windowClose {
		return true, ""
	}
	switch level {
	case VPVROverloadL3:
		return false, "delta_l3"
	case VPVROverloadL2:
		if eventCount%2 == 1 {
			return false, "delta_l2"
		}
		return true, ""
	default:
		return true, ""
	}
}
