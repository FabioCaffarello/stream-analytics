package deliveryruntime

import (
	"sort"
	"strings"
)

const (
	coherenceSampleTopN       = 5
	coherenceSampleMaxUnique  = 64
	coherenceSampleFlushEvery = 50

	coherenceReasonOutOfOrderInput = "out_of_order_input"
	coherenceReasonStaleEvent      = "stale_event"
	coherenceReasonOwnerChange     = "owner_change"
	coherenceReasonResyncOverlap   = "resync_overlap"
	coherenceReasonReplayDuplicate = "replay_duplicate"
	coherenceReasonUnknown         = "unknown"
)

type coherenceSampleKey struct {
	streamKey    string
	lastSeq      int64
	candidateSeq int64
	reason       string
	owner        string
	instance     string
	origin       string
}

type coherenceSampleEntry struct {
	key   coherenceSampleKey
	count int
}

func processorInstanceIDFromMeta(meta map[string]string) string {
	if len(meta) == 0 {
		return ""
	}
	return strings.TrimSpace(meta[routerProcessorInstanceIDMetaKey])
}

func processorInstanceIDForLog(meta map[string]string) string {
	id := processorInstanceIDFromMeta(meta)
	if id == "" {
		return "unknown"
	}
	return id
}

func instanceIDForLog(meta map[string]string) string {
	if len(meta) == 0 {
		return "unknown"
	}
	if instance := strings.TrimSpace(meta[routerServerInstanceIDMetaKey]); instance != "" {
		return instance
	}
	return processorInstanceIDForLog(meta)
}

func originFromMeta(meta map[string]string) string {
	if len(meta) == 0 {
		return "router"
	}
	switch strings.ToLower(strings.TrimSpace(meta[routerOriginMetaKey])) {
	case "router", "session", "gateway":
		return strings.ToLower(strings.TrimSpace(meta[routerOriginMetaKey]))
	default:
		return "router"
	}
}

func (r *RouterActor) recordCoherenceSample(sample coherenceSampleKey) {
	if r.coherenceSamples == nil {
		r.coherenceSamples = make(map[coherenceSampleKey]int)
	}
	if _, exists := r.coherenceSamples[sample]; exists {
		r.coherenceSamples[sample]++
	} else if len(r.coherenceSamples) < coherenceSampleMaxUnique {
		r.coherenceSamples[sample] = 1
	} else {
		r.coherenceSampleDrops++
	}
	r.coherenceSampleWindow++
	if r.coherenceSampleWindow >= coherenceSampleFlushEvery {
		r.flushCoherenceSamples()
	}
}

func (r *RouterActor) flushCoherenceSamples() {
	if len(r.coherenceSamples) == 0 {
		r.coherenceSampleWindow = 0
		return
	}
	entries := make([]coherenceSampleEntry, 0, len(r.coherenceSamples))
	for key, count := range r.coherenceSamples {
		entries = append(entries, coherenceSampleEntry{key: key, count: count})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].count != entries[j].count {
			return entries[i].count > entries[j].count
		}
		if entries[i].key.streamKey != entries[j].key.streamKey {
			return entries[i].key.streamKey < entries[j].key.streamKey
		}
		if entries[i].key.reason != entries[j].key.reason {
			return entries[i].key.reason < entries[j].key.reason
		}
		if entries[i].key.owner != entries[j].key.owner {
			return entries[i].key.owner < entries[j].key.owner
		}
		if entries[i].key.instance != entries[j].key.instance {
			return entries[i].key.instance < entries[j].key.instance
		}
		if entries[i].key.lastSeq != entries[j].key.lastSeq {
			return entries[i].key.lastSeq < entries[j].key.lastSeq
		}
		return entries[i].key.candidateSeq < entries[j].key.candidateSeq
	})
	limit := coherenceSampleTopN
	if limit > len(entries) {
		limit = len(entries)
	}
	for i := 0; i < limit; i++ {
		entry := entries[i]
		r.logger.Warn("delivery router: sampled seq coherence violation",
			"stream_key", entry.key.streamKey,
			"last_seq", entry.key.lastSeq,
			"cand_seq", entry.key.candidateSeq,
			"reason", entry.key.reason,
			"owner", entry.key.owner,
			"instance", entry.key.instance,
			"origin", entry.key.origin,
			"count", entry.count,
			"sampled_unique", len(entries),
			"sample_dropped", r.coherenceSampleDrops,
		)
	}
	clear(r.coherenceSamples)
	r.coherenceSampleDrops = 0
	r.coherenceSampleWindow = 0
}
