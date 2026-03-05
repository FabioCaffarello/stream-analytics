package signalsruntime

import (
	"strconv"
	"strings"

	"github.com/market-raccoon/internal/shared/ownership"
)

// StrategistOwnerRejectReason is the canonical owner-only reject reason.
const StrategistOwnerRejectReason = strategistDropReasonOwnerReject

// StrategistPolicyContractInput is a stable, testable view of strategist policy inputs.
type StrategistPolicyContractInput struct {
	StreamKey          ownership.StreamKey
	CandidateSeq       int64
	CandidateWatermark int64
	LastSeq            int64
	LastWatermark      int64
	ReplicaID          int
	ReplicaCount       int
}

// StrategistPolicyContractDecision is a stable, testable view of strategist policy outputs.
type StrategistPolicyContractDecision struct {
	Action       ownership.MonotonicAction
	Accept       bool
	RejectReason string
	Duplicate    bool
	OutOfOrder   bool
	OwnerReplica int
}

// DecideStrategistPolicyContract evaluates owner-only + monotonic policy without actor/runtime wiring.
func DecideStrategistPolicyContract(in StrategistPolicyContractInput) StrategistPolicyContractDecision {
	ownerReplica := ownership.OwnerReplica(ownership.SubsystemStrategist, in.StreamKey, in.ReplicaCount)
	if ownerReplica != in.ReplicaID {
		return StrategistPolicyContractDecision{
			Action:       ownership.ActionDrop,
			RejectReason: StrategistOwnerRejectReason,
			OwnerReplica: ownerReplica,
		}
	}
	if in.CandidateSeq <= 0 {
		return StrategistPolicyContractDecision{
			Action:       ownership.ActionAccept,
			Accept:       true,
			OwnerReplica: ownerReplica,
		}
	}

	streamKey := strings.ToLower(strings.TrimSpace(in.StreamKey.Venue)) + "|" +
		strings.ToUpper(strings.TrimSpace(in.StreamKey.Instrument)) + "|" +
		strings.ToLower(strings.TrimSpace(in.StreamKey.Channel))
	decision := ownership.DecideMonotonic(ownership.MonotonicInput{
		StreamKey:          streamKey,
		CandidateSeq:       in.CandidateSeq,
		CandidateWatermark: in.CandidateWatermark,
		LastSeq:            in.LastSeq,
		LastWatermark:      in.LastWatermark,
		StaleGapWindow:     strategistStaleGapWindow,
		CandidateOwner:     strconv.Itoa(ownerReplica),
	})
	if decision.Action == ownership.ActionAccept {
		return StrategistPolicyContractDecision{
			Action:       ownership.ActionAccept,
			Accept:       true,
			OwnerReplica: ownerReplica,
		}
	}
	return StrategistPolicyContractDecision{
		Action:       decision.Action,
		RejectReason: decision.RejectReason,
		Duplicate:    decision.Duplicate,
		OutOfOrder:   decision.OutOfOrder,
		OwnerReplica: ownerReplica,
	}
}
