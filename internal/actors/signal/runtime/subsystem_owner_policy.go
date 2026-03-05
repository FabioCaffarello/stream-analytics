package signalruntime

import (
	"sort"
	"strconv"
	"strings"

	marketmodel "github.com/market-raccoon/internal/core/marketmodel"
	signalcore "github.com/market-raccoon/internal/core/signal"
	sharedhash "github.com/market-raccoon/internal/shared/hash"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/ownership"
)

const (
	signalStateMaxStreams    = 4096
	signalStaleGapWindow     = int64(2048)
	signalsBoundedMapName    = "signals_ownership_contract"
	dropSampleTopN           = 5
	dropSampleMaxUnique      = 64
	dropSampleFlushEvery     = 50
	dropReasonOwnerReject    = "owner_reject"
	dropReasonDuplicate      = "duplicate"
	dropReasonOutOfOrder     = "out_of_order"
	dropReasonRateLimited    = "rate_limited"
	dropReasonDecodeFailed   = "decode_failed"
	dropReasonValidationFail = "validation_failed"
)

type streamProgress struct {
	lastSeq       int64
	lastWatermark int64
	owner         string
}

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

func signalStreamKeyLabel(key marketmodel.StreamKey) string {
	return ownership.CanonicalLabel(ownership.StreamKey{
		Venue:      string(key.Venue),
		Instrument: string(key.Symbol),
		Channel:    string(key.Channel),
	})
}

func (s *SubsystemActor) ownerReplicaID(key marketmodel.StreamKey) int {
	return ownership.OwnerReplica(ownership.SubsystemSignals, ownership.StreamKey{
		Venue:      string(key.Venue),
		Instrument: string(key.Symbol),
		Channel:    string(key.Channel),
	}, s.replicaCount)
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
		lastSeq:   s.lastStreamProgress(streamKey).lastSeq,
		candSeq:   candidateSeq,
		reason:    dropReasonOwnerReject,
		owner:     strconv.Itoa(owner),
		instance:  strconv.Itoa(s.replicaID),
	})
	return false
}

func (s *SubsystemActor) noteStreamProgress(streamKey string, seq, watermark int64, owner string) {
	streamKey = strings.TrimSpace(streamKey)
	if streamKey == "" || seq <= 0 {
		return
	}
	if state, ok := s.streamState[streamKey]; ok {
		if seq >= state.lastSeq {
			state.lastSeq = seq
		}
		if watermark > state.lastWatermark {
			state.lastWatermark = watermark
		}
		if strings.TrimSpace(owner) != "" {
			state.owner = owner
		}
		s.streamState[streamKey] = state
		return
	}
	if len(s.streamState) >= signalStateMaxStreams {
		if len(s.streamOrder) == 0 {
			return
		}
		if s.streamOrderIdx < 0 || s.streamOrderIdx >= len(s.streamOrder) {
			s.streamOrderIdx = 0
		}
		evictKey := s.streamOrder[s.streamOrderIdx]
		delete(s.streamState, evictKey)
		metrics.IncBoundedMapEviction(signalsBoundedMapName, "size")
		metrics.IncSignalEvicted("capacity")
		metrics.IncOwnershipContractEvicted("signals", "size")
		s.streamOrder[s.streamOrderIdx] = streamKey
		s.streamOrderIdx = (s.streamOrderIdx + 1) % signalStateMaxStreams
		s.streamState[streamKey] = streamProgress{lastSeq: seq, lastWatermark: watermark, owner: owner}
		metrics.SetBoundedMapSize(signalsBoundedMapName, len(s.streamState))
		metrics.SetOwnershipContractEntries("signals", len(s.streamState))
		return
	}
	s.streamState[streamKey] = streamProgress{lastSeq: seq, lastWatermark: watermark, owner: owner}
	s.streamOrder = append(s.streamOrder, streamKey)
	if len(s.streamOrder) == signalStateMaxStreams {
		s.streamOrderIdx = 0
	}
	metrics.SetBoundedMapSize(signalsBoundedMapName, len(s.streamState))
	metrics.SetOwnershipContractEntries("signals", len(s.streamState))
}

func (s *SubsystemActor) lastStreamProgress(streamKey string) streamProgress {
	if s.streamState == nil {
		return streamProgress{}
	}
	return s.streamState[streamKey]
}

func (s *SubsystemActor) acceptMonotonicProgress(key marketmodel.StreamKey, streamKey string, seq, watermark int64) bool {
	if seq <= 0 {
		return true
	}
	last := s.lastStreamProgress(streamKey)
	owner := strconv.Itoa(s.ownerReplicaID(key))
	decision := ownership.DecideMonotonic(ownership.MonotonicInput{
		StreamKey:          streamKey,
		CandidateSeq:       seq,
		CandidateWatermark: watermark,
		LastSeq:            last.lastSeq,
		LastWatermark:      last.lastWatermark,
		CandidateOwner:     owner,
		LastOwner:          last.owner,
		StaleGapWindow:     signalStaleGapWindow,
	})
	if decision.Action == ownership.ActionAccept {
		return true
	}
	if decision.Duplicate {
		metrics.IncSignalDrop(dropReasonDuplicate)
		metrics.IncOwnershipContractDuplicate("signals")
	} else if decision.OutOfOrder {
		metrics.IncSignalDrop(dropReasonOutOfOrder)
		metrics.IncOwnershipContractOutOfOrder("signals")
	}
	s.recordDropSample(dropSampleKey{
		streamKey: streamKey,
		lastSeq:   last.lastSeq,
		candSeq:   seq,
		reason:    decision.RejectReason,
		owner:     owner,
		instance:  strconv.Itoa(s.replicaID),
	})
	return false
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
