package signalruntime

import (
	"strconv"

	"github.com/market-raccoon/internal/shared/ownership"
)

// SignalOwnerRejectReason is the canonical owner-only reject reason.
const SignalOwnerRejectReason = dropReasonOwnerReject

// PolicyContractInput is a stable, testable view of signal subsystem policy inputs.
type PolicyContractInput struct {
	StreamKey          ownership.StreamKey
	CandidateSeq       int64
	CandidateWatermark int64
	LastSeq            int64
	LastWatermark      int64
	LastOwner          string
	ReplicaID          int
	ReplicaCount       int
}

// PolicyContractDecision is a stable, testable view of signal subsystem policy outputs.
type PolicyContractDecision struct {
	Action       ownership.MonotonicAction
	Accept       bool
	RejectReason string
	Duplicate    bool
	OutOfOrder   bool
	OwnerReplica int
}

// DecidePolicyContract evaluates owner-only + monotonic policy without actor/runtime wiring.
func DecidePolicyContract(in PolicyContractInput) PolicyContractDecision {
	ownerReplica := ownership.OwnerReplica(ownership.SubsystemSignals, in.StreamKey, in.ReplicaCount)
	if ownerReplica != in.ReplicaID {
		return PolicyContractDecision{
			Action:       ownership.ActionDrop,
			RejectReason: SignalOwnerRejectReason,
			OwnerReplica: ownerReplica,
		}
	}
	if in.CandidateSeq <= 0 {
		return PolicyContractDecision{
			Action:       ownership.ActionAccept,
			Accept:       true,
			OwnerReplica: ownerReplica,
		}
	}

	owner := strconv.Itoa(ownerReplica)
	decision := ownership.DecideMonotonic(ownership.MonotonicInput{
		StreamKey:          ownership.CanonicalLabel(in.StreamKey),
		CandidateSeq:       in.CandidateSeq,
		CandidateWatermark: in.CandidateWatermark,
		LastSeq:            in.LastSeq,
		LastWatermark:      in.LastWatermark,
		CandidateOwner:     owner,
		LastOwner:          in.LastOwner,
		StaleGapWindow:     signalStaleGapWindow,
	})

	if decision.Action == ownership.ActionAccept {
		return PolicyContractDecision{
			Action:       ownership.ActionAccept,
			Accept:       true,
			OwnerReplica: ownerReplica,
		}
	}
	return PolicyContractDecision{
		Action:       decision.Action,
		RejectReason: decision.RejectReason,
		Duplicate:    decision.Duplicate,
		OutOfOrder:   decision.OutOfOrder,
		OwnerReplica: ownerReplica,
	}
}
