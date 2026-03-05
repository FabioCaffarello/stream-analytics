package ownership

import "testing"

func TestOwnerReplicaDeterministic(t *testing.T) {
	key := StreamKey{Venue: "BINANCE", Instrument: "btcusdt", Channel: "evidence", Timeframe: "raw"}
	first := OwnerReplica(SubsystemSignals, key, 2)
	second := OwnerReplica(SubsystemSignals, key, 2)
	if first != second {
		t.Fatalf("owner mismatch first=%d second=%d", first, second)
	}
	if first < 0 || first > 1 {
		t.Fatalf("owner=%d out of range", first)
	}
}

func TestShardKeyDiffersBySubsystem(t *testing.T) {
	key := StreamKey{Venue: "binance", Instrument: "BTCUSDT", Channel: "trade", Timeframe: "raw"}
	a := ShardKey(SubsystemSignals, key)
	b := ShardKey(SubsystemStrategist, key)
	if a == b {
		t.Fatal("expected distinct shard keys across subsystems")
	}
}

func TestDecideMonotonicWatermarkRegressionAcceptsWhenSeqAdvances(t *testing.T) {
	decision := DecideMonotonic(MonotonicInput{
		StreamKey:          "marketdata.trade/binance/BTCUSDT/raw",
		CandidateSeq:       101,
		CandidateWatermark: 4999,
		LastSeq:            100,
		LastWatermark:      5000,
		StaleGapWindow:     2048,
	})
	if decision.Action != ActionAccept {
		t.Fatalf("action=%q want=%q", decision.Action, ActionAccept)
	}
}

func TestDecideMonotonicWatermarkRegressionDropsWhenSeqDoesNotAdvance(t *testing.T) {
	decision := DecideMonotonic(MonotonicInput{
		StreamKey:          "marketdata.trade/binance/BTCUSDT/raw",
		CandidateSeq:       100,
		CandidateWatermark: 4999,
		LastSeq:            101,
		LastWatermark:      5000,
		StaleGapWindow:     2048,
	})
	if decision.Action != ActionDrop {
		t.Fatalf("action=%q want=%q", decision.Action, ActionDrop)
	}
	if decision.RejectReason != ReasonStaleEvent {
		t.Fatalf("reason=%q want=%q", decision.RejectReason, ReasonStaleEvent)
	}
}
