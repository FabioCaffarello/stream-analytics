package ownership_test

import (
	"math"
	"strconv"
	"testing"

	deliveryruntime "github.com/market-raccoon/internal/actors/delivery/runtime"
	signalruntime "github.com/market-raccoon/internal/actors/signal/runtime"
	signalsruntime "github.com/market-raccoon/internal/actors/signals/runtime"
	"github.com/market-raccoon/internal/shared/ownership"
)

const deterministicFixtureSeed uint64 = 0x17_0000_0000_0002

type deterministicLCG struct {
	state uint64
}

func newDeterministicLCG(seed uint64) *deterministicLCG {
	if seed == 0 {
		seed = 1
	}
	return &deterministicLCG{state: seed}
}

func (g *deterministicLCG) next() uint64 {
	g.state = g.state*6364136223846793005 + 1442695040888963407
	return g.state
}

func safeUint64ToInt64(v uint64) int64 {
	if v > uint64(math.MaxInt64) {
		panic("deterministic fixture overflow")
	}
	return int64(v)
}

type policyEvent struct {
	Name               string
	Seq                int64
	PrevSeq            int64
	TsServer           int64
	Watermark          int64
	EventType          string
	CandidateProcessor string
}

type fixtureSet struct {
	Seed                     uint64
	Base                     []policyEvent
	Duplicate                policyEvent
	OutOfOrder               policyEvent
	WatermarkRegressive      policyEvent
	OwnerFlipReplicaSequence []int
}

func buildFixtureSet(seed uint64) fixtureSet {
	g := newDeterministicLCG(seed)
	baseSeq := int64(1_000) + safeUint64ToInt64(g.next()%128)
	baseTS := int64(1_700_000_000_000) + safeUint64ToInt64(g.next()%10_000)

	base := make([]policyEvent, 0, 2)
	for i := 0; i < 2; i++ {
		step := safeUint64ToInt64((g.next() % 5) + 1)
		prev := baseSeq
		baseSeq += step
		baseTS += 100 + (step * 7)
		base = append(base, policyEvent{
			Name:               "base_" + strconv.Itoa(i),
			Seq:                baseSeq,
			PrevSeq:            prev,
			TsServer:           baseTS,
			Watermark:          baseTS,
			EventType:          "marketdata.trade",
			CandidateProcessor: "processor-a",
		})
	}

	last := base[len(base)-1]
	duplicate := last
	duplicate.Name = "duplicate"
	outOfOrder := policyEvent{
		Name:               "out_of_order",
		Seq:                last.Seq - 1,
		PrevSeq:            last.Seq,
		TsServer:           last.TsServer - 9,
		Watermark:          last.Watermark - 9,
		EventType:          "marketdata.trade",
		CandidateProcessor: "processor-a",
	}
	watermarkRegressive := policyEvent{
		Name:               "watermark_regressive_seq_advancing",
		Seq:                last.Seq + 1,
		PrevSeq:            last.Seq,
		TsServer:           last.TsServer + 33,
		Watermark:          last.Watermark - 31,
		EventType:          "marketdata.trade",
		CandidateProcessor: "processor-a",
	}

	flipA := 0
	if g.next()%2 == 1 {
		flipA = 1
	}
	return fixtureSet{
		Seed:                seed,
		Base:                base,
		Duplicate:           duplicate,
		OutOfOrder:          outOfOrder,
		WatermarkRegressive: watermarkRegressive,
		OwnerFlipReplicaSequence: []int{
			flipA,
			1 - flipA,
		},
	}
}

type deliveryState struct {
	lastSeq             int64
	lastTs              int64
	lastProcessorID     string
	pendingResyncWaterm int64
}

type streamState struct {
	lastSeq       int64
	lastWatermark int64
	lastOwner     string
}

func TestCrossSubsystem_MonotonicConsistency(t *testing.T) {
	fx := buildFixtureSet(deterministicFixtureSeed)

	signalKey := ownership.StreamKey{
		Venue:      "binance",
		Instrument: "BTC-USDT",
		Channel:    "evidence",
	}
	strategistKey := ownership.StreamKey{
		Venue:      "binance",
		Instrument: "BTC-USDT",
		Channel:    "signal",
		Timeframe:  "raw",
	}
	replicaCount := 2
	signalOwner := ownership.OwnerReplica(ownership.SubsystemSignals, signalKey, replicaCount)
	strategistOwner := ownership.OwnerReplica(ownership.SubsystemStrategist, strategistKey, replicaCount)

	tests := []struct {
		name       string
		target     policyEvent
		wantAction ownership.MonotonicAction
		wantReason string
	}{
		{name: "duplicate_input", target: fx.Duplicate, wantAction: ownership.ActionDrop, wantReason: ownership.ReasonReplayDuplicate},
		{name: "out_of_order_seq", target: fx.OutOfOrder, wantAction: ownership.ActionDrop, wantReason: ownership.ReasonStaleEvent},
		{name: "watermark_regression_seq_advancing", target: fx.WatermarkRegressive, wantAction: ownership.ActionAccept, wantReason: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dState := deliveryState{}
			sState := streamState{}
			stState := streamState{}
			events := []policyEvent{fx.Base[0], fx.Base[1], tc.target}

			for _, ev := range events {
				deliveryDecision := deliveryruntime.DecideSeqPolicyContract(deliveryruntime.SeqPolicyContractInput{
					StreamKey:              "marketdata.trade/binance/BTC-USDT/raw",
					EventType:              ev.EventType,
					CandidateSeq:           ev.Seq,
					CandidateTsIngest:      ev.Watermark,
					LastSeq:                dState.lastSeq,
					LastTsIngest:           dState.lastTs,
					CandidateProcessorID:   ev.CandidateProcessor,
					LastProcessorID:        dState.lastProcessorID,
					PendingResyncWatermark: dState.pendingResyncWaterm,
				})
				signalDecision := signalruntime.DecidePolicyContract(signalruntime.PolicyContractInput{
					StreamKey:          signalKey,
					CandidateSeq:       ev.Seq,
					CandidateWatermark: ev.Watermark,
					LastSeq:            sState.lastSeq,
					LastWatermark:      sState.lastWatermark,
					LastOwner:          sState.lastOwner,
					ReplicaID:          signalOwner,
					ReplicaCount:       replicaCount,
				})
				strategistDecision := signalsruntime.DecideStrategistPolicyContract(signalsruntime.StrategistPolicyContractInput{
					StreamKey:          strategistKey,
					CandidateSeq:       ev.Seq,
					CandidateWatermark: ev.Watermark,
					LastSeq:            stState.lastSeq,
					LastWatermark:      stState.lastWatermark,
					ReplicaID:          strategistOwner,
					ReplicaCount:       replicaCount,
				})

				if deliveryDecision.Action != signalDecision.Action || deliveryDecision.Action != strategistDecision.Action {
					t.Fatalf("event=%s action mismatch delivery=%q signal=%q strategist=%q", ev.Name, deliveryDecision.Action, signalDecision.Action, strategistDecision.Action)
				}

				if deliveryDecision.Action == ownership.ActionAccept {
					dState.lastSeq = ev.Seq
					if ev.Watermark > dState.lastTs {
						dState.lastTs = ev.Watermark
					}
					dState.lastProcessorID = ev.CandidateProcessor
					sState.lastSeq = ev.Seq
					if ev.Watermark > sState.lastWatermark {
						sState.lastWatermark = ev.Watermark
					}
					sState.lastOwner = strconv.Itoa(signalDecision.OwnerReplica)
					stState.lastSeq = ev.Seq
					if ev.Watermark > stState.lastWatermark {
						stState.lastWatermark = ev.Watermark
					}
					continue
				}

				if deliveryDecision.RejectReason != signalDecision.RejectReason || deliveryDecision.RejectReason != strategistDecision.RejectReason {
					t.Fatalf("event=%s reject_reason mismatch delivery=%q signal=%q strategist=%q", ev.Name, deliveryDecision.RejectReason, signalDecision.RejectReason, strategistDecision.RejectReason)
				}
			}

			finalDelivery := deliveryruntime.DecideSeqPolicyContract(deliveryruntime.SeqPolicyContractInput{
				StreamKey:              "marketdata.trade/binance/BTC-USDT/raw",
				EventType:              tc.target.EventType,
				CandidateSeq:           tc.target.Seq,
				CandidateTsIngest:      tc.target.Watermark,
				LastSeq:                fx.Base[1].Seq,
				LastTsIngest:           fx.Base[1].Watermark,
				CandidateProcessorID:   tc.target.CandidateProcessor,
				LastProcessorID:        "processor-a",
				PendingResyncWatermark: 0,
			})
			if finalDelivery.Action != tc.wantAction {
				t.Fatalf("final action=%q want=%q", finalDelivery.Action, tc.wantAction)
			}
			if finalDelivery.RejectReason != tc.wantReason {
				t.Fatalf("final reject_reason=%q want=%q", finalDelivery.RejectReason, tc.wantReason)
			}
		})
	}
}

func TestCrossSubsystem_OwnerOnlyAndDedup_NoDoubleEmit(t *testing.T) {
	fx := buildFixtureSet(deterministicFixtureSeed)
	replicaCount := 2

	t.Run("signal_owner_only_with_duplicate", func(t *testing.T) {
		key := ownership.StreamKey{Venue: "binance", Instrument: "BTC-USDT", Channel: "evidence"}
		replicaState := []streamState{{}, {}}
		emitsBySeq := map[int64]int{}
		events := []policyEvent{fx.Base[1], fx.Duplicate}

		for _, ev := range events {
			for _, replicaID := range fx.OwnerFlipReplicaSequence {
				decision := signalruntime.DecidePolicyContract(signalruntime.PolicyContractInput{
					StreamKey:          key,
					CandidateSeq:       ev.Seq,
					CandidateWatermark: ev.Watermark,
					LastSeq:            replicaState[replicaID].lastSeq,
					LastWatermark:      replicaState[replicaID].lastWatermark,
					LastOwner:          replicaState[replicaID].lastOwner,
					ReplicaID:          replicaID,
					ReplicaCount:       replicaCount,
				})
				if decision.Accept {
					emitsBySeq[ev.Seq]++
					replicaState[replicaID].lastSeq = ev.Seq
					if ev.Watermark > replicaState[replicaID].lastWatermark {
						replicaState[replicaID].lastWatermark = ev.Watermark
					}
					replicaState[replicaID].lastOwner = strconv.Itoa(decision.OwnerReplica)
				}
			}
		}

		if emitsBySeq[fx.Base[1].Seq] != 1 {
			t.Fatalf("signal seq=%d emitted %d times, want=1", fx.Base[1].Seq, emitsBySeq[fx.Base[1].Seq])
		}

		nonOwner := 1 - ownership.OwnerReplica(ownership.SubsystemSignals, key, replicaCount)
		nonOwnerDecision := signalruntime.DecidePolicyContract(signalruntime.PolicyContractInput{
			StreamKey:          key,
			CandidateSeq:       fx.Base[1].Seq,
			CandidateWatermark: fx.Base[1].Watermark,
			LastSeq:            0,
			LastWatermark:      0,
			ReplicaID:          nonOwner,
			ReplicaCount:       replicaCount,
		})
		if nonOwnerDecision.RejectReason != signalruntime.SignalOwnerRejectReason {
			t.Fatalf("signal non-owner reject_reason=%q want=%q", nonOwnerDecision.RejectReason, signalruntime.SignalOwnerRejectReason)
		}
	})

	t.Run("strategist_owner_only_with_duplicate", func(t *testing.T) {
		key := ownership.StreamKey{Venue: "binance", Instrument: "BTC-USDT", Channel: "signal", Timeframe: "raw"}
		replicaState := []streamState{{}, {}}
		emitsBySeq := map[int64]int{}
		events := []policyEvent{fx.Base[1], fx.Duplicate}

		for _, ev := range events {
			for _, replicaID := range fx.OwnerFlipReplicaSequence {
				decision := signalsruntime.DecideStrategistPolicyContract(signalsruntime.StrategistPolicyContractInput{
					StreamKey:          key,
					CandidateSeq:       ev.Seq,
					CandidateWatermark: ev.Watermark,
					LastSeq:            replicaState[replicaID].lastSeq,
					LastWatermark:      replicaState[replicaID].lastWatermark,
					ReplicaID:          replicaID,
					ReplicaCount:       replicaCount,
				})
				if decision.Accept {
					emitsBySeq[ev.Seq]++
					replicaState[replicaID].lastSeq = ev.Seq
					if ev.Watermark > replicaState[replicaID].lastWatermark {
						replicaState[replicaID].lastWatermark = ev.Watermark
					}
				}
			}
		}

		if emitsBySeq[fx.Base[1].Seq] != 1 {
			t.Fatalf("strategist seq=%d emitted %d times, want=1", fx.Base[1].Seq, emitsBySeq[fx.Base[1].Seq])
		}

		nonOwner := 1 - ownership.OwnerReplica(ownership.SubsystemStrategist, key, replicaCount)
		nonOwnerDecision := signalsruntime.DecideStrategistPolicyContract(signalsruntime.StrategistPolicyContractInput{
			StreamKey:          key,
			CandidateSeq:       fx.Base[1].Seq,
			CandidateWatermark: fx.Base[1].Watermark,
			LastSeq:            0,
			LastWatermark:      0,
			ReplicaID:          nonOwner,
			ReplicaCount:       replicaCount,
		})
		if nonOwnerDecision.RejectReason != signalsruntime.StrategistOwnerRejectReason {
			t.Fatalf("strategist non-owner reject_reason=%q want=%q", nonOwnerDecision.RejectReason, signalsruntime.StrategistOwnerRejectReason)
		}
	})
}
