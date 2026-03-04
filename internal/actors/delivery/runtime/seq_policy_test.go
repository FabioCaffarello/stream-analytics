package deliveryruntime

import "testing"

func TestSeqPolicy_DominantOutOfOrderBecomesStaleDrop(t *testing.T) {
	policy := newDefaultSeqPolicy()
	decision := policy.Decide(seqPolicyInput{
		streamKey:    "marketdata.trade/binance/BTCUSDT/raw",
		eventType:    "marketdata.trade",
		candidateSeq: 990,
		lastSeq:      1000,
		lastTsIngest: 5_000,
	})

	if got, want := decision.action, seqPolicyActionDrop; got != want {
		t.Fatalf("action=%q want=%q", got, want)
	}
	if got, want := decision.rejectReason, coherenceReasonStaleEvent; got != want {
		t.Fatalf("reject_reason=%q want=%q", got, want)
	}
	if got := decision.violationType; got != "" {
		t.Fatalf("violation_type=%q want empty", got)
	}
}

func TestSeqPolicy_ReplayDuplicateDropsWithoutCoherenceViolation(t *testing.T) {
	policy := newDefaultSeqPolicy()
	decision := policy.Decide(seqPolicyInput{
		streamKey:    "marketdata.trade/binance/BTCUSDT/raw",
		eventType:    "marketdata.trade",
		candidateSeq: 1000,
		lastSeq:      1000,
		lastTsIngest: 5_000,
	})

	if got, want := decision.action, seqPolicyActionDrop; got != want {
		t.Fatalf("action=%q want=%q", got, want)
	}
	if got, want := decision.rejectReason, coherenceReasonReplayDuplicate; got != want {
		t.Fatalf("reject_reason=%q want=%q", got, want)
	}
	if got := decision.violationType; got != "" {
		t.Fatalf("violation_type=%q want empty", got)
	}
}

func TestSeqPolicy_ReplicasOwnerChangeCannotReemitLowerSeq(t *testing.T) {
	policy := newDefaultSeqPolicy()
	lowerSeqFromNewOwner := policy.Decide(seqPolicyInput{
		streamKey:            "marketdata.trade/binance/BTCUSDT/raw",
		eventType:            "marketdata.trade",
		candidateSeq:         199,
		lastSeq:              200,
		lastProcessorID:      "processor-1",
		candidateProcessorID: "processor-2",
		handoffWatermarkSeq:  200,
	})

	if got, want := lowerSeqFromNewOwner.action, seqPolicyActionConvertToResync; got != want {
		t.Fatalf("lower action=%q want=%q", got, want)
	}
	if got, want := lowerSeqFromNewOwner.rejectReason, coherenceReasonOwnerChange; got != want {
		t.Fatalf("lower reject_reason=%q want=%q", got, want)
	}
	if got, want := lowerSeqFromNewOwner.resyncWatermark, int64(200); got != want {
		t.Fatalf("lower resync_watermark=%d want=%d", got, want)
	}

	higherSeqFromNewOwner := policy.Decide(seqPolicyInput{
		streamKey:              "marketdata.trade/binance/BTCUSDT/raw",
		eventType:              "marketdata.trade",
		candidateSeq:           201,
		lastSeq:                200,
		lastProcessorID:        "processor-1",
		candidateProcessorID:   "processor-2",
		handoffWatermarkSeq:    200,
		pendingResyncWatermark: 200,
	})
	if got, want := higherSeqFromNewOwner.action, seqPolicyActionAccept; got != want {
		t.Fatalf("higher action=%q want=%q", got, want)
	}
}
