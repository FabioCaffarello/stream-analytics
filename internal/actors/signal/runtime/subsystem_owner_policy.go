package signalruntime

import (
	"sort"
	"strconv"
	"strings"

	marketmodel "github.com/market-raccoon/internal/core/marketmodel"
	signalcore "github.com/market-raccoon/internal/core/signal"
	sharedhash "github.com/market-raccoon/internal/shared/hash"
	"github.com/market-raccoon/internal/shared/metrics"
)

const (
	signalStateMaxStreams    = 4096
	dropSampleTopN           = 5
	dropSampleMaxUnique      = 64
	dropSampleFlushEvery     = 50
	dropReasonOwnerReject    = "owner_reject"
	dropReasonDuplicate      = "duplicate"
	dropReasonRateLimited    = "rate_limited"
	dropReasonDecodeFailed   = "decode_failed"
	dropReasonValidationFail = "validation_failed"
)

type dropSampleKey struct {
	streamKey string
	lastSeq   int64
	candSeq   int64
	reason    string
	owner     string
	instance  string
}

type dropSampleEntry struct {
	key   dropSampleKey
	count int
}

func deterministicIntentID(emission signalcore.Emission) string {
	return "intent_" + sharedhash.HashFieldsFast(
		strings.TrimSpace(emission.Tenant),
		string(emission.StreamKey.Venue),
		string(emission.StreamKey.Symbol),
		string(emission.StreamKey.Channel),
		strings.TrimSpace(emission.Event.Type),
		strings.TrimSpace(emission.Event.SignalID),
		strings.TrimSpace(emission.Event.CorrelationID),
		strconv.FormatInt(emission.Seq, 10),
	)
}

func signalShardKey(key marketmodel.StreamKey) uint64 {
	return sharedhash.SumFieldsFast64(string(key.Venue), string(key.Symbol), string(key.Channel))
}

func signalStreamKeyLabel(key marketmodel.StreamKey) string {
	return strings.ToLower(strings.TrimSpace(string(key.Venue))) +
		"|" +
		strings.ToUpper(strings.TrimSpace(string(key.Symbol))) +
		"|" +
		strings.ToLower(strings.TrimSpace(string(key.Channel)))
}

func (s *SubsystemActor) ownerReplicaID(key marketmodel.StreamKey) int {
	if s.replicaCount <= 1 {
		return s.replicaID
	}
	owner := signalShardKey(key) % uint64(s.replicaCount)
	ownerID, err := strconv.Atoi(strconv.FormatUint(owner, 10))
	if err != nil {
		return 0
	}
	if ownerID >= 0 && ownerID < s.replicaCount {
		return ownerID
	}
	return 0
}

func (s *SubsystemActor) acceptOwner(key marketmodel.StreamKey, candidateSeq int64) bool {
	owner := s.ownerReplicaID(key)
	if owner == s.replicaID {
		return true
	}
	streamKey := signalStreamKeyLabel(key)
	metrics.IncSignalDrop(dropReasonOwnerReject)
	s.recordDropSample(dropSampleKey{
		streamKey: streamKey,
		lastSeq:   s.lastStreamSeq(streamKey),
		candSeq:   candidateSeq,
		reason:    dropReasonOwnerReject,
		owner:     strconv.Itoa(owner),
		instance:  strconv.Itoa(s.replicaID),
	})
	return false
}

func (s *SubsystemActor) noteStreamSeq(streamKey string, seq int64) {
	streamKey = strings.TrimSpace(streamKey)
	if streamKey == "" || seq <= 0 {
		return
	}
	if last, ok := s.streamLastSeq[streamKey]; ok {
		if seq > last {
			s.streamLastSeq[streamKey] = seq
		}
		return
	}
	if len(s.streamLastSeq) >= signalStateMaxStreams {
		if len(s.streamOrder) == 0 {
			return
		}
		if s.streamOrderIdx < 0 || s.streamOrderIdx >= len(s.streamOrder) {
			s.streamOrderIdx = 0
		}
		evictKey := s.streamOrder[s.streamOrderIdx]
		delete(s.streamLastSeq, evictKey)
		s.streamOrder[s.streamOrderIdx] = streamKey
		s.streamOrderIdx = (s.streamOrderIdx + 1) % signalStateMaxStreams
		s.streamLastSeq[streamKey] = seq
		return
	}
	s.streamLastSeq[streamKey] = seq
	s.streamOrder = append(s.streamOrder, streamKey)
	if len(s.streamOrder) == signalStateMaxStreams {
		s.streamOrderIdx = 0
	}
}

func (s *SubsystemActor) lastStreamSeq(streamKey string) int64 {
	if s.streamLastSeq == nil {
		return 0
	}
	return s.streamLastSeq[streamKey]
}

func (s *SubsystemActor) recordDropSample(sample dropSampleKey) {
	if s.dropSamples == nil {
		s.dropSamples = make(map[dropSampleKey]int)
	}
	if _, ok := s.dropSamples[sample]; ok {
		s.dropSamples[sample]++
	} else if len(s.dropSamples) < dropSampleMaxUnique {
		s.dropSamples[sample] = 1
	} else {
		s.dropSampleDrops++
	}
	s.dropSampleWindow++
	if s.dropSampleWindow >= dropSampleFlushEvery {
		s.flushDropSamples()
	}
}

func (s *SubsystemActor) flushDropSamples() {
	if len(s.dropSamples) == 0 {
		s.dropSampleWindow = 0
		return
	}
	entries := make([]dropSampleEntry, 0, len(s.dropSamples))
	for key, count := range s.dropSamples {
		entries = append(entries, dropSampleEntry{key: key, count: count})
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
		return entries[i].key.candSeq < entries[j].key.candSeq
	})
	limit := dropSampleTopN
	if limit > len(entries) {
		limit = len(entries)
	}
	for i := 0; i < limit; i++ {
		entry := entries[i]
		s.logger.Warn("signal subsystem: sampled drop",
			"stream_key", entry.key.streamKey,
			"last_seq", entry.key.lastSeq,
			"cand_seq", entry.key.candSeq,
			"reason", entry.key.reason,
			"owner", entry.key.owner,
			"instance", entry.key.instance,
			"count", entry.count,
			"sampled_unique", len(entries),
			"sample_dropped", s.dropSampleDrops,
		)
	}
	clear(s.dropSamples)
	s.dropSampleDrops = 0
	s.dropSampleWindow = 0
}
