package portfolioruntime_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/anthdm/hollywood/actor"
	portfolioruntime "github.com/market-raccoon/internal/actors/portfolio/runtime"
	"github.com/market-raccoon/internal/contracts"
	executiondomain "github.com/market-raccoon/internal/core/execution/domain"
	portfoliodomain "github.com/market-raccoon/internal/core/portfolio/domain"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/problem"
)

func init() {
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		panic(fmt.Sprintf("BootstrapPayloadCodecRegistry: %v", p))
	}
}

type capturePortfolioPublisher struct {
	ch chan envelope.Envelope
}

func (p *capturePortfolioPublisher) Publish(_ context.Context, env envelope.Envelope) *problem.Problem {
	p.ch <- env
	return nil
}

func TestPortfolioSubsystem_ConsumesExecutionAndPublishesState(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	input := make(chan envelope.Envelope, 2)
	pub := &capturePortfolioPublisher{ch: make(chan envelope.Envelope, 2)}
	pid := e.Spawn(portfolioruntime.NewSubsystemActor(portfolioruntime.SubsystemConfig{
		EnvelopeCh:   input,
		Publisher:    pub,
		ReplicaID:    0,
		ReplicaCount: 1,
	}), "portfolio-subsystem")
	defer func() {
		close(input)
		<-e.Poison(pid).Done()
	}()

	execPayload, p := codec.EncodePayload(executiondomain.EventType, executiondomain.EventVersion, envelope.ContentTypeProto, executiondomain.ExecutionEventV1{
		EventID:       "evt-1",
		Status:        executiondomain.ExecutionStatusFilled,
		Correlation:   executiondomain.ExecutionCorrelation{IntentID: "intent-1", OrderID: "order-1", Venue: "binance", Symbol: "BTCUSDT", AccountID: "paper"},
		TsEventMs:     1_700_000_001_500,
		ExecutionSeq:  1,
		Attempt:       1,
		RequestedQty:  2,
		LastFillQty:   2,
		LeavesQty:     0,
		AvgFillPrice:  100,
		LastFillPrice: 100,
		Provenance: executiondomain.ExecutionProvenance{
			CorrelationID: "corr-1",
			Source:        "executor.bootstrap.v1",
		},
	})
	if p != nil {
		t.Fatalf("encode execution payload: %v", p)
	}

	input <- envelope.Envelope{
		Type:        executiondomain.EventType,
		Version:     executiondomain.EventVersion,
		Venue:       "binance",
		Instrument:  "BTCUSDT",
		TsIngest:    1_700_000_001_500,
		Seq:         1,
		ContentType: envelope.ContentTypeProto,
		Payload:     execPayload,
	}

	select {
	case out := <-pub.ch:
		if out.Type != portfoliodomain.StateEventType {
			t.Fatalf("type=%q want=%q", out.Type, portfoliodomain.StateEventType)
		}
		decoded, p := codec.DecodePayload(out.Type, out.Version, out.ContentType, out.Payload)
		if p != nil {
			t.Fatalf("decode portfolio payload: %v", p)
		}
		state, ok := decoded.(portfoliodomain.PortfolioStateV1)
		if !ok {
			t.Fatalf("decoded type=%T want PortfolioStateV1", decoded)
		}
		if len(state.Positions) != 1 || state.Positions[0].Quantity != 2 {
			t.Fatalf("positions=%v want quantity 2", state.Positions)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting portfolio.state")
	}
}
