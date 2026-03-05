package deliveryruntime

import (
	"strconv"
	"strings"

	"github.com/market-raccoon/internal/shared/ownership"
)

const (
	defaultSeqPolicyStaleGapWindow = int64(2048)
	metaKeyHandoffWatermarkSeq     = "handoff_watermark_seq"
	metaKeySnapshotWatermarkSeq    = "watermark_seq"
)

type seqPolicyAction string

const (
	seqPolicyActionAccept          seqPolicyAction = "accept"
	seqPolicyActionDrop            seqPolicyAction = "drop"
	seqPolicyActionConvertToResync seqPolicyAction = "convert_to_resync"
)

type seqPolicyInput struct {
	streamKey              string
	eventType              string
	candidateSeq           int64
	candidateTsIngest      int64
	lastSeq                int64
	lastTsIngest           int64
	candidateProcessorID   string
	lastProcessorID        string
	handoffWatermarkSeq    int64
	pendingResyncWatermark int64
}

type seqPolicyDecision struct {
	action          seqPolicyAction
	rejectReason    string
	violationType   string
	coherenceReason string
	resyncWatermark int64
	duplicate       bool
	outOfOrder      bool
}

type SeqPolicy interface {
	Decide(input seqPolicyInput) seqPolicyDecision
}

type defaultSeqPolicy struct {
	staleGapWindow int64
}

func newDefaultSeqPolicy() SeqPolicy {
	return defaultSeqPolicy{staleGapWindow: defaultSeqPolicyStaleGapWindow}
}

func (p defaultSeqPolicy) Decide(in seqPolicyInput) seqPolicyDecision {
	decision := ownership.DecideMonotonic(ownership.MonotonicInput{
		StreamKey:              in.streamKey,
		IsSnapshot:             isSnapshotEventType(in.eventType),
		CandidateSeq:           in.candidateSeq,
		CandidateWatermark:     in.candidateTsIngest,
		LastSeq:                in.lastSeq,
		LastWatermark:          in.lastTsIngest,
		CandidateOwner:         in.candidateProcessorID,
		LastOwner:              in.lastProcessorID,
		HandoffWatermarkSeq:    in.handoffWatermarkSeq,
		PendingResyncWatermark: in.pendingResyncWatermark,
		StaleGapWindow:         p.staleGapWindow,
	})

	switch decision.Action {
	case ownership.ActionAccept:
		return seqPolicyDecision{action: seqPolicyActionAccept}
	case ownership.ActionConvertToResync:
		return seqPolicyDecision{
			action:          seqPolicyActionConvertToResync,
			rejectReason:    decision.RejectReason,
			coherenceReason: decision.CoherenceReason,
			resyncWatermark: decision.ResyncWatermark,
			duplicate:       decision.Duplicate,
			outOfOrder:      decision.OutOfOrder,
		}
	default:
		return seqPolicyDecision{
			action:          seqPolicyActionDrop,
			rejectReason:    decision.RejectReason,
			violationType:   decision.ViolationType,
			coherenceReason: decision.CoherenceReason,
			duplicate:       decision.Duplicate,
			outOfOrder:      decision.OutOfOrder,
		}
	}
}

func isSnapshotEventType(eventType string) bool {
	et := strings.ToLower(strings.TrimSpace(eventType))
	return strings.Contains(et, "snapshot")
}

func parseInt64Meta(meta map[string]string, key string) int64 {
	if len(meta) == 0 {
		return 0
	}
	raw := strings.TrimSpace(meta[key])
	if raw == "" {
		return 0
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0
	}
	return v
}

func handoffWatermarkFromMeta(meta map[string]string) int64 {
	if w := parseInt64Meta(meta, metaKeyHandoffWatermarkSeq); w > 0 {
		return w
	}
	return parseInt64Meta(meta, metaKeySnapshotWatermarkSeq)
}
