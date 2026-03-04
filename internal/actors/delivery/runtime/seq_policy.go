package deliveryruntime

import (
	"strconv"
	"strings"
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
	if strings.TrimSpace(in.streamKey) == "" || in.candidateSeq <= 0 {
		return seqPolicyDecision{
			action:          seqPolicyActionDrop,
			rejectReason:    "seq_invalid",
			violationType:   "seq_invalid",
			coherenceReason: coherenceReasonUnknown,
		}
	}
	if in.lastSeq <= 0 || in.candidateSeq > in.lastSeq {
		if in.pendingResyncWatermark > 0 && in.candidateSeq <= in.pendingResyncWatermark {
			return seqPolicyDecision{
				action:          seqPolicyActionDrop,
				rejectReason:    coherenceReasonResyncOverlap,
				coherenceReason: coherenceReasonResyncOverlap,
			}
		}
		return seqPolicyDecision{action: seqPolicyActionAccept}
	}
	if in.pendingResyncWatermark > 0 &&
		!isSnapshotEventType(in.eventType) &&
		in.candidateSeq <= in.pendingResyncWatermark {
		return seqPolicyDecision{
			action:          seqPolicyActionDrop,
			rejectReason:    coherenceReasonResyncOverlap,
			coherenceReason: coherenceReasonResyncOverlap,
		}
	}
	if in.candidateSeq == in.lastSeq {
		return seqPolicyDecision{
			action:          seqPolicyActionDrop,
			rejectReason:    coherenceReasonReplayDuplicate,
			coherenceReason: coherenceReasonReplayDuplicate,
		}
	}

	// Owner change requires explicit monotonic handoff watermark before emit.
	if in.candidateProcessorID != "" &&
		in.lastProcessorID != "" &&
		in.candidateProcessorID != in.lastProcessorID {
		watermark := in.handoffWatermarkSeq
		if watermark < in.lastSeq {
			watermark = in.lastSeq
		}
		if in.candidateSeq <= watermark {
			return seqPolicyDecision{
				action:          seqPolicyActionConvertToResync,
				rejectReason:    coherenceReasonOwnerChange,
				coherenceReason: coherenceReasonOwnerChange,
				resyncWatermark: watermark,
			}
		}
	}

	// Snapshot overlap can happen during replay/resync windows.
	if isSnapshotEventType(in.eventType) && in.handoffWatermarkSeq > 0 && in.candidateSeq <= in.handoffWatermarkSeq {
		return seqPolicyDecision{
			action:          seqPolicyActionConvertToResync,
			rejectReason:    coherenceReasonResyncOverlap,
			coherenceReason: coherenceReasonResyncOverlap,
			resyncWatermark: in.handoffWatermarkSeq,
		}
	}

	// Dominant production path: minor backwards gaps are stale arrivals to drop early.
	gap := in.lastSeq - in.candidateSeq
	if gap > 0 && gap <= p.staleGapWindow {
		return seqPolicyDecision{
			action:          seqPolicyActionDrop,
			rejectReason:    coherenceReasonStaleEvent,
			coherenceReason: coherenceReasonStaleEvent,
		}
	}
	if in.lastTsIngest > 0 && in.candidateTsIngest > 0 && in.candidateTsIngest < in.lastTsIngest {
		return seqPolicyDecision{
			action:          seqPolicyActionDrop,
			rejectReason:    coherenceReasonStaleEvent,
			coherenceReason: coherenceReasonStaleEvent,
		}
	}

	return seqPolicyDecision{
		action:          seqPolicyActionDrop,
		rejectReason:    "seq_non_monotonic",
		violationType:   "seq_non_monotonic",
		coherenceReason: coherenceReasonOutOfOrderInput,
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
