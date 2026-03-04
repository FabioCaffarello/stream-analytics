package aggruntime_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/anthdm/hollywood/actor"
	aggruntime "github.com/market-raccoon/internal/actors/aggregation/runtime"
	mddomain "github.com/market-raccoon/internal/core/marketdata/domain"
	"github.com/market-raccoon/internal/shared/contracts"
	"github.com/market-raccoon/internal/shared/envelope"
)

func TestProcessor_Determinism_BookDelta_Replay(t *testing.T) {
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		t.Fatalf("BootstrapPayloadCodecRegistry: %v", p)
	}

	const snapshotsPerRun = 50
	baseTs := int64(1710000000000)
	fixedNow := time.UnixMilli(baseTs + 10_000)

	run := func() []envelope.Envelope {
		pub := &spyArtifactPublisher{}
		outPublisher := &spyEnvelopePublisher{}
		aggSvc := newAggService(pub)
		processedCh := make(chan aggruntime.EnvelopeProcessResult, snapshotsPerRun)

		ch := make(chan envelope.Envelope, 1024)
		cfg := aggruntime.ProcessorConfig{
			EnvelopeCh:      ch,
			Service:         aggSvc,
			PublishEnvelope: outPublisher,
			Now: func() time.Time {
				return fixedNow
			},
			OnEnvelopeProcessed: func(res aggruntime.EnvelopeProcessResult) {
				select {
				case processedCh <- res:
				default:
				}
			},
		}

		e := newEngine(t)
		pid := e.Spawn(aggruntime.NewProcessorSubsystemActor(cfg), "processor", actor.WithID("processor"))

		for i := 1; i <= snapshotsPerRun; i++ {
			tsIngest := baseTs + int64(i)
			ch <- makeBookDeltaEnvelopeAt(
				"BINANCE", "BTC-USDT", int64(i), tsIngest,
				[]mddomain.PriceLevel{{Price: 42000 + float64(i), Size: 1.0}},
				[]mddomain.PriceLevel{{Price: 42001 + float64(i), Size: 1.0}},
			)
			select {
			case <-processedCh:
			case <-time.After(2 * time.Second):
				t.Fatalf("timeout waiting for envelope processing at seq=%d", i)
			}
			e.Send(pid, aggruntime.SnapshotTick{Kind: aggruntime.SnapshotTickOrderBook})
		}

		waitFor(t, 2*time.Second, func() bool {
			count := 0
			for _, env := range outPublisher.all() {
				if env.Type == "aggregation.snapshot" {
					count++
				}
			}
			return count >= snapshotsPerRun
		})
		all := outPublisher.all()
		<-(e.Poison(pid)).Done()
		return all
	}

	first := run()
	second := run()

	filterSnapshots := func(in []envelope.Envelope) []envelope.Envelope {
		out := make([]envelope.Envelope, 0, len(in))
		for _, env := range in {
			if env.Type == "aggregation.snapshot" {
				out = append(out, env)
			}
		}
		return out
	}

	firstSnaps := filterSnapshots(first)
	secondSnaps := filterSnapshots(second)
	if len(firstSnaps) != len(secondSnaps) {
		t.Fatalf("snapshot count mismatch first=%d second=%d", len(firstSnaps), len(secondSnaps))
	}
	for i := range firstSnaps {
		if firstSnaps[i].Seq != secondSnaps[i].Seq {
			t.Fatalf("snapshot[%d] seq mismatch first=%d second=%d", i, firstSnaps[i].Seq, secondSnaps[i].Seq)
		}
		if !bytes.Equal(firstSnaps[i].Payload, secondSnaps[i].Payload) {
			t.Fatalf("snapshot[%d] payload mismatch", i)
		}
	}
}
