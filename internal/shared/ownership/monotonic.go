package ownership

import "strings"

type MonotonicAction string

const (
	ActionAccept          MonotonicAction = "accept"
	ActionDrop            MonotonicAction = "drop"
	ActionConvertToResync MonotonicAction = "convert_to_resync"
)

const (
	ReasonUnknown         = "unknown"
	ReasonSeqInvalid      = "seq_invalid"
	ReasonReplayDuplicate = "replay_duplicate"
	ReasonOwnerChange     = "owner_change"
	ReasonResyncOverlap   = "resync_overlap"
	ReasonStaleEvent      = "stale_event"
	ReasonOutOfOrderInput = "out_of_order_input"
	ReasonSeqNonMonotonic = "seq_non_monotonic"
)

// MonotonicInput defines sequence/watermark constraints for a stream observation.
type MonotonicInput struct {
	StreamKey              string
	IsSnapshot             bool
	CandidateSeq           int64
	CandidateWatermark     int64
	LastSeq                int64
	LastWatermark          int64
	CandidateOwner         string
	LastOwner              string
	HandoffWatermarkSeq    int64
	PendingResyncWatermark int64
	StaleGapWindow         int64
}

// MonotonicDecision is the policy result.
type MonotonicDecision struct {
	Action          MonotonicAction
	RejectReason    string
	ViolationType   string
	CoherenceReason string
	ResyncWatermark int64
	Duplicate       bool
	OutOfOrder      bool
}

func dropDecision(reason string, duplicate, outOfOrder bool) MonotonicDecision {
	return MonotonicDecision{
		Action:          ActionDrop,
		RejectReason:    reason,
		CoherenceReason: reason,
		Duplicate:       duplicate,
		OutOfOrder:      outOfOrder,
	}
}

func invalidDecision() MonotonicDecision {
	return MonotonicDecision{
		Action:          ActionDrop,
		RejectReason:    ReasonSeqInvalid,
		ViolationType:   ReasonSeqInvalid,
		CoherenceReason: ReasonUnknown,
	}
}

func isInvalid(in MonotonicInput) bool {
	return strings.TrimSpace(in.StreamKey) == "" || in.CandidateSeq <= 0
}

func pendingResyncOverlap(in MonotonicInput) bool {
	return in.PendingResyncWatermark > 0 && !in.IsSnapshot && in.CandidateSeq <= in.PendingResyncWatermark
}

func replayDuplicate(in MonotonicInput) bool {
	return in.LastSeq > 0 && in.CandidateSeq == in.LastSeq
}

func ownerChangeDecision(in MonotonicInput) (MonotonicDecision, bool) {
	if in.CandidateOwner == "" || in.LastOwner == "" || in.CandidateOwner == in.LastOwner {
		return MonotonicDecision{}, false
	}
	watermark := in.HandoffWatermarkSeq
	if watermark < in.LastSeq {
		watermark = in.LastSeq
	}
	if in.CandidateSeq > watermark {
		return MonotonicDecision{}, false
	}
	return MonotonicDecision{
		Action:          ActionConvertToResync,
		RejectReason:    ReasonOwnerChange,
		CoherenceReason: ReasonOwnerChange,
		ResyncWatermark: watermark,
		OutOfOrder:      true,
	}, true
}

func snapshotOverlapDecision(in MonotonicInput) (MonotonicDecision, bool) {
	if !in.IsSnapshot || in.HandoffWatermarkSeq <= 0 || in.CandidateSeq > in.HandoffWatermarkSeq {
		return MonotonicDecision{}, false
	}
	return MonotonicDecision{
		Action:          ActionConvertToResync,
		RejectReason:    ReasonResyncOverlap,
		CoherenceReason: ReasonResyncOverlap,
		ResyncWatermark: in.HandoffWatermarkSeq,
		OutOfOrder:      true,
	}, true
}

func watermarkRegressed(in MonotonicInput) bool {
	return in.LastWatermark > 0 && in.CandidateWatermark > 0 && in.CandidateWatermark < in.LastWatermark
}

func staleGap(in MonotonicInput) bool {
	gap := in.LastSeq - in.CandidateSeq
	return gap > 0 && gap <= in.StaleGapWindow
}

// DecideMonotonic centralizes seq/watermark monotonic policy.
func DecideMonotonic(in MonotonicInput) MonotonicDecision {
	if isInvalid(in) {
		return invalidDecision()
	}
	if pendingResyncOverlap(in) {
		return dropDecision(ReasonResyncOverlap, false, true)
	}
	if replayDuplicate(in) {
		return dropDecision(ReasonReplayDuplicate, true, false)
	}
	if decision, ok := ownerChangeDecision(in); ok {
		return decision
	}
	if decision, ok := snapshotOverlapDecision(in); ok {
		return decision
	}
	if in.LastSeq <= 0 || in.CandidateSeq > in.LastSeq {
		return MonotonicDecision{Action: ActionAccept}
	}
	if watermarkRegressed(in) {
		return dropDecision(ReasonStaleEvent, false, true)
	}
	if staleGap(in) {
		return dropDecision(ReasonStaleEvent, false, true)
	}
	return MonotonicDecision{
		Action:          ActionDrop,
		RejectReason:    ReasonSeqNonMonotonic,
		ViolationType:   ReasonSeqNonMonotonic,
		CoherenceReason: ReasonOutOfOrderInput,
		OutOfOrder:      true,
	}
}
