package aggruntime

import (
	"fmt"
	"sort"
	"time"

	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/observability"
	"github.com/market-raccoon/internal/shared/policykit"
)

func (p *ProcessorSubsystemActor) applyPolicyKit(env envelope.Envelope) (envelope.Envelope, bool) {
	if p.cfg.PolicyKitEngine == nil || p.policyApplier == nil {
		return env, false
	}
	started := time.Now()

	// Intern the partition key to avoid per-envelope allocations.
	// The number of unique triples is small (bounded by type×venue×instrument),
	// so the cache stays small while eliminating ~1.8M allocs/min.
	partitionKey := env.Type + "|" + env.Venue + "|" + env.Instrument
	partition := p.internPolicyPartitionWithCap(partitionKey, policyPartitionCacheCap)
	prev := p.policyLevels[partition]
	decision := p.cfg.PolicyKitEngine.Decide(prev, policykit.Signals{
		Backlog:    len(p.cfg.EnvelopeCh),
		BacklogCap: p.cfg.PolicyKitBacklogCapacity,
	})
	p.policyLevels[partition] = decision.Level
	metrics.SetPolicyKitOverloadLevel(env.Type, env.Venue, env.Instrument, int(decision.Level))
	stride := decision.DegradeStride()
	enter, recover := activeThresholdsForLevel(decision.Level)
	observability.UpdatePolicyKitOverload(observability.PolicyKitOverloadEntry{
		Stream:        env.Type,
		Venue:         env.Venue,
		OverloadLevel: int(decision.Level),
		Stride:        stride,
		Thresholds: observability.PolicyKitThresholdPair{
			Enter: observability.PolicyKitThreshold{
				QueueRatio:   enter.QueueRatio,
				BacklogRatio: enter.BacklogRatio,
				MapRatio:     enter.MapRatio,
				LatencyMs:    enter.LatencyMs,
			},
			Recover: observability.PolicyKitThreshold{
				QueueRatio:   recover.QueueRatio,
				BacklogRatio: recover.BacklogRatio,
				MapRatio:     recover.MapRatio,
				LatencyMs:    recover.LatencyMs,
			},
		},
	})
	if stride > 1 {
		metrics.IncPolicyKitDegrade(env.Type, fmt.Sprintf("stride_%d", stride))
	}

	applied, keep := p.policyApplier.ApplySingle(decision, env, policykit.ApplyHooks{})
	metrics.ObservePolicyKitLatencySeconds(env.Type, time.Since(started).Seconds())
	if !keep {
		metrics.IncPolicyKitDrop(env.Type, "policy_drop")
		return env, true
	}
	return applied, false
}

func (p *ProcessorSubsystemActor) internPolicyPartitionWithCap(partitionKey string, maxEntries int) string {
	if maxEntries <= 0 {
		maxEntries = 1
	}
	if p.policyPartitions == nil {
		p.policyPartitions = make(map[string]string, maxEntries)
	}
	if p.policyLevels == nil {
		p.policyLevels = make(map[string]policykit.Level, maxEntries)
	}
	if p.policyPartitionQ == nil {
		p.policyPartitionQ = make([]string, 0, maxEntries)
	}
	if partition, ok := p.policyPartitions[partitionKey]; ok {
		return partition
	}

	if len(p.policyPartitions) >= maxEntries {
		if len(p.policyPartitionQ) == 0 {
			keys := make([]string, 0, len(p.policyPartitions))
			for key := range p.policyPartitions {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			if len(keys) > 0 {
				evictKey := keys[0]
				if evictPartition, ok := p.policyPartitions[evictKey]; ok {
					delete(p.policyPartitions, evictKey)
					delete(p.policyLevels, evictPartition)
				}
			}
		} else {
			if p.policyPartitionI < 0 || p.policyPartitionI >= len(p.policyPartitionQ) {
				p.policyPartitionI = 0
			}
			evictKey := p.policyPartitionQ[p.policyPartitionI]
			if evictPartition, ok := p.policyPartitions[evictKey]; ok {
				delete(p.policyPartitions, evictKey)
				delete(p.policyLevels, evictPartition)
			}
			p.policyPartitionQ[p.policyPartitionI] = partitionKey
			p.policyPartitionI = (p.policyPartitionI + 1) % maxEntries
		}
	} else {
		p.policyPartitionQ = append(p.policyPartitionQ, partitionKey)
		if len(p.policyPartitionQ) == maxEntries {
			p.policyPartitionI = 0
		}
	}

	p.policyPartitions[partitionKey] = partitionKey
	return partitionKey
}

func activeThresholdsForLevel(level policykit.Level) (policykit.Threshold, policykit.Threshold) {
	cfg := policykit.DefaultThresholdConfig()
	switch level {
	case policykit.L3:
		return cfg.EnterL3, cfg.RecoverL3
	case policykit.L2:
		return cfg.EnterL2, cfg.RecoverL2
	default:
		return cfg.EnterL1, cfg.RecoverL1
	}
}
