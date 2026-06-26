package deliveryruntime

import "github.com/FabioCaffarello/stream-analytics/internal/shared/ownership"

// SeqPolicyContractInput is a stable, testable view of delivery seq-policy inputs.
type SeqPolicyContractInput struct {
	StreamKey              string
	EventType              string
	CandidateSeq           int64
	CandidateTsIngest      int64
	LastSeq                int64
	LastTsIngest           int64
	CandidateProcessorID   string
	LastProcessorID        string
	HandoffWatermarkSeq    int64
	PendingResyncWatermark int64
}

// SeqPolicyContractDecision is a stable, testable view of delivery seq-policy outputs.
type SeqPolicyContractDecision struct {
	Action          ownership.MonotonicAction
	RejectReason    string
	ViolationType   string
	CoherenceReason string
	ResyncWatermark int64
	Duplicate       bool
	OutOfOrder      bool
}

// DecideSeqPolicyContract evaluates delivery seq policy without requiring actor/runtime wiring.
func DecideSeqPolicyContract(in SeqPolicyContractInput) SeqPolicyContractDecision {
	decision := newDefaultSeqPolicy().Decide(seqPolicyInput{
		streamKey:              in.StreamKey,
		eventType:              in.EventType,
		candidateSeq:           in.CandidateSeq,
		candidateTsIngest:      in.CandidateTsIngest,
		lastSeq:                in.LastSeq,
		lastTsIngest:           in.LastTsIngest,
		candidateProcessorID:   in.CandidateProcessorID,
		lastProcessorID:        in.LastProcessorID,
		handoffWatermarkSeq:    in.HandoffWatermarkSeq,
		pendingResyncWatermark: in.PendingResyncWatermark,
	})

	out := SeqPolicyContractDecision{
		RejectReason:    decision.rejectReason,
		ViolationType:   decision.violationType,
		CoherenceReason: decision.coherenceReason,
		ResyncWatermark: decision.resyncWatermark,
		Duplicate:       decision.duplicate,
		OutOfOrder:      decision.outOfOrder,
	}

	switch decision.action {
	case seqPolicyActionAccept:
		out.Action = ownership.ActionAccept
	case seqPolicyActionConvertToResync:
		out.Action = ownership.ActionConvertToResync
	default:
		out.Action = ownership.ActionDrop
	}
	return out
}
