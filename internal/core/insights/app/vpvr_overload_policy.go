package app

import (
	"math"
	"slices"
	"sync"

	"github.com/FabioCaffarello/stream-analytics/internal/core/insights/domain"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/metrics"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/naming"
)

// VPVROverloadLevel represents overload severity (L0=normal through L3=critical).
type VPVROverloadLevel = int

const (
	VPVROverloadL0 VPVROverloadLevel = 0
	VPVROverloadL1 VPVROverloadLevel = 1
	VPVROverloadL2 VPVROverloadLevel = 2
	VPVROverloadL3 VPVROverloadLevel = 3
)

// OverloadDecideFunc resolves overload level and actions from previous level
// and runtime signals.  The caller provides the policykit binding; core only
// depends on this function signature.
type OverloadDecideFunc func(prev VPVROverloadLevel, signals VPVROverloadSignals) (
	nextLevel VPVROverloadLevel,
	compressSnapshot bool,
	degradeStride int,
	dropDelta bool,
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
	decide OverloadDecideFunc
}

func NewVPVREmitPolicy(decide OverloadDecideFunc) *VPVREmitPolicy {
	return &VPVREmitPolicy{
		states: make(map[string]VPVROverloadState),
		decide: decide,
	}
}

// EvaluateVPVROverload is a pure function that applies pre-computed overload
// decisions to the VPVR pipeline.  It does not call policykit; the caller
// must resolve nextLevel/compressSnapshot/degradeStride/dropDelta beforehand.
func EvaluateVPVROverload(input VPVROverloadInput, nextLevel VPVROverloadLevel, compressSnapshot bool, degradeStride int, dropDelta bool) VPVROverloadOutput {
	next := input.PartitionState
	next.Level = nextLevel
	next.EventCount++
	snapshot := input.Snapshot
	compressed := false
	compressRatio := 1.0
	if !input.WindowClose && compressSnapshot {
		snapshot, compressed, compressRatio = compressSnapshotByLevel(snapshot, next.Level)
	}
	emitSnapshot := shouldEmitSnapshotAtCadence(next.EventCount, degradeStride, input.WindowClose)
	emitDelta, dropReason := shouldEmitDelta(next.EventCount, dropDelta, degradeStride, input.WindowClose, input.HasDelta)

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
	key := overloadPartitionKey(input.Venue, input.Instrument, input.Timeframe)
	p.mu.Lock()
	state := p.states[key]
	input.PartitionState = state

	nextLevel, compress, stride, drop := p.decide(state.Level, input.Signals)
	out := EvaluateVPVROverload(input, nextLevel, compress, stride, drop)

	p.states[key] = out.NextState
	p.mu.Unlock()

	metrics.SetVPVROverloadLevel(input.Venue, input.Instrument, input.Timeframe, int(out.Level))
	metrics.SetPolicyKitOverloadLevel("insights.volume_profile", input.Venue, input.Instrument, int(out.Level))
	metrics.ObserveVPVRProcessingLatencySeconds(input.ProcessingMs / 1000)
	metrics.ObservePolicyKitLatencySeconds("insights.volume_profile", input.ProcessingMs/1000)
	if out.Compressed {
		metrics.IncVPVRDegrade("compress")
		metrics.IncPolicyKitCompress("insights.volume_profile")
		metrics.IncPolicyKitDegrade("insights.volume_profile", "compress")
	}
	if out.CadenceDropped {
		metrics.IncVPVRDegrade("cadence_skip")
		metrics.IncPolicyKitDegrade("insights.volume_profile", "cadence_skip")
	}
	if out.DeltaDropped {
		metrics.IncVPVRDrop(out.DropReason)
		metrics.IncPolicyKitDrop("insights.volume_profile", out.DropReason)
	}
	metrics.ObserveVPVRCompressRatio(out.CompressRatio)
	return out
}

func overloadPartitionKey(venue, instrument, timeframe string) string {
	return naming.CanonicalVenue(venue) + "|" + naming.CanonicalInstrument(instrument) + "|" + naming.NormalizeTimeframe(timeframe)
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

func shouldEmitSnapshotAtCadence(eventCount uint64, stride int, windowClose bool) bool {
	if windowClose {
		return true
	}
	if stride <= 1 {
		return true
	}
	return eventCount%uint64(stride) == 0
}

func shouldEmitDelta(eventCount uint64, dropDelta bool, degradeStride int, windowClose bool, hasDelta bool) (bool, string) {
	if !hasDelta {
		return false, ""
	}
	if windowClose {
		return true, ""
	}
	if dropDelta {
		return false, "delta_l3"
	}
	if degradeStride == 2 {
		if eventCount%2 == 1 {
			return false, "delta_l2"
		}
		return true, ""
	}
	return true, ""
}
