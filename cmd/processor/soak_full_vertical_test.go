//go:build soak
// +build soak

package main

import (
	"context"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anthdm/hollywood/actor"
	aggruntime "github.com/market-raccoon/internal/actors/aggregation/runtime"
	"github.com/market-raccoon/internal/adapters/bus"
	aggapp "github.com/market-raccoon/internal/core/aggregation/app"
	insightsapp "github.com/market-raccoon/internal/core/insights/app"
	insightsdomain "github.com/market-raccoon/internal/core/insights/domain"
	mddomain "github.com/market-raccoon/internal/core/marketdata/domain"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/contracts"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/problem"
)

const (
	verticalMaxBooks = 16
	verticalTicks    = 240
)

type soakBusPublisher struct {
	bus *bus.InMemoryBus
}

func (p *soakBusPublisher) Publish(ctx context.Context, env envelope.Envelope) *problem.Problem {
	if p == nil || p.bus == nil {
		return nil
	}
	return p.bus.Publish(ctx, env)
}

type soakTickerActor struct {
	tickCh <-chan aggruntime.SnapshotTickKind
	done   chan struct{}
}

func (a *soakTickerActor) Receive(c *actor.Context) {
	switch c.Message().(type) {
	case actor.Started:
		parent := c.Parent()
		engine := c.Engine()
		a.done = make(chan struct{})
		go func() {
			for {
				select {
				case <-a.done:
					return
				case kind, ok := <-a.tickCh:
					if !ok {
						return
					}
					if parent != nil {
						engine.Send(parent, aggruntime.SnapshotTick{Kind: kind})
					}
				}
			}
		}()
	case actor.Stopped:
		if a.done != nil {
			close(a.done)
			a.done = nil
		}
	}
}

type snapshotCounters struct {
	orderbook atomic.Int64
	heatmap   atomic.Int64
	volume    atomic.Int64
}

func (c *snapshotCounters) add(env envelope.Envelope) {
	switch env.Type {
	case "aggregation.snapshot":
		c.orderbook.Add(1)
	case insightsdomain.HeatmapSnapshotType:
		c.heatmap.Add(1)
	case insightsdomain.VolumeProfileSnapshotType:
		c.volume.Add(1)
	}
}

func (c *snapshotCounters) ready(minPerType int64) bool {
	return c.orderbook.Load() >= minPerType && c.heatmap.Load() >= minPerType && c.volume.Load() >= minPerType
}

func startSnapshotConsumers(memBus *bus.InMemoryBus, counters *snapshotCounters, consumers int) *sync.WaitGroup {
	var wg sync.WaitGroup
	for i := 0; i < consumers; i++ {
		ch := memBus.Subscribe()
		wg.Add(1)
		go func(events <-chan envelope.Envelope) {
			defer wg.Done()
			for env := range events {
				counters.add(env)
			}
		}(ch)
	}
	return &wg
}

func publishVerticalInputs(t *testing.T, in chan<- envelope.Envelope) {
	t.Helper()
	bookPayload, p := codec.EncodePayload("marketdata.bookdelta", 1, envelope.ContentTypeJSON, mddomain.BookDeltaV1{
		Bids:      []mddomain.PriceLevel{{Price: 100.1, Size: 1.2}},
		Asks:      []mddomain.PriceLevel{{Price: 100.2, Size: 1.1}},
		Timestamp: 1_735_689_600_000,
	})
	if p != nil {
		t.Fatalf("encode bookdelta payload: %v", p)
	}
	tradePayload, p := codec.EncodePayload("marketdata.trade", 1, envelope.ContentTypeJSON, mddomain.TradeTickV1{
		Price:     100.15,
		Size:      0.8,
		Side:      "buy",
		TradeID:   "vertical-1",
		Timestamp: 1_735_689_600_100,
	})
	if p != nil {
		t.Fatalf("encode trade payload: %v", p)
	}

	in <- envelope.Envelope{
		Type:        "marketdata.bookdelta",
		Version:     1,
		Venue:       "binance",
		Instrument:  "BTC-USDT",
		Seq:         1,
		TsIngest:    1_735_689_600_000,
		ContentType: envelope.ContentTypeJSON,
		Payload:     bookPayload,
	}
	for i := 0; i < 24; i++ {
		in <- envelope.Envelope{
			Type:        "marketdata.trade",
			Version:     1,
			Venue:       "binance",
			Instrument:  "BTC-USDT",
			Seq:         int64(2 + i),
			TsIngest:    1_735_689_600_100 + int64(i),
			ContentType: envelope.ContentTypeJSON,
			Payload:     tradePayload,
		}
	}
}

func emitManualTicks(ch chan<- aggruntime.SnapshotTickKind, total int) {
	kinds := []aggruntime.SnapshotTickKind{
		aggruntime.SnapshotTickOrderBook,
		aggruntime.SnapshotTickHeatmap,
		aggruntime.SnapshotTickVolume,
	}
	for i := 0; i < total; i++ {
		ch <- kinds[i%len(kinds)]
	}
}

func waitForCounts(timeout time.Duration, fn func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

func newVerticalProcessor(
	t *testing.T,
	e *actor.Engine,
	in <-chan envelope.Envelope,
	out *bus.InMemoryBus,
	tickCh <-chan aggruntime.SnapshotTickKind,
	onProcessed func(aggruntime.EnvelopeProcessResult),
) (*actor.PID, *aggapp.AggregationService) {
	t.Helper()
	pub := &soakArtifactPublisher{}
	aggSvc := aggapp.NewAggregationService(aggapp.AggregationServiceConfig{
		Update:    aggapp.UpdateConfig{MaxBooks: verticalMaxBooks},
		Publisher: pub,
		Store:     soakHotStore{},
	})
	insightsSvc := insightsapp.NewInsightsService(insightsapp.InsightsServiceConfig{
		Heatmap:       insightsapp.BuildHeatmapConfig{},
		VolumeProfile: insightsapp.BuildVolumeProfileConfig{},
	})

	pid := e.Spawn(aggruntime.NewProcessorSubsystemActor(aggruntime.ProcessorConfig{
		EnvelopeCh:      in,
		Service:         aggSvc,
		Insights:        insightsSvc,
		PublishEnvelope: &soakBusPublisher{bus: out},
		RTPublish: aggruntime.ProcessorRTPublishConfig{
			OrderbookInterval: time.Millisecond,
			HeatmapInterval:   time.Millisecond,
			VolumeInterval:    time.Millisecond,
		},
		TickerProducer: func() actor.Receiver {
			return &soakTickerActor{tickCh: tickCh}
		},
		OnEnvelopeProcessed: onProcessed,
	}), "vertical-processor")
	return pid, aggSvc
}

func TestSoak_FullVertical_TimerSnapshots(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping soak test in short mode")
	}
	if os.Getenv(soakEnableEnv) != "1" {
		t.Skipf("set %s=1 to run soak tests", soakEnableEnv)
	}
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		t.Fatalf("BootstrapPayloadCodecRegistry: %v", p)
	}

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	beforeG := runtime.NumGoroutine()

	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	outBus := bus.NewInMemoryBus(4096)
	inCh := make(chan envelope.Envelope, 64)
	tickCh := make(chan aggruntime.SnapshotTickKind, verticalTicks+8)
	counters := &snapshotCounters{}
	consumersWG := startSnapshotConsumers(outBus, counters, 3)
	var processed atomic.Int64

	processorPID, aggSvc := newVerticalProcessor(t, e, inCh, outBus, tickCh, func(aggruntime.EnvelopeProcessResult) {
		processed.Add(1)
	})
	publishVerticalInputs(t, inCh)
	if !waitForCounts(5*time.Second, func() bool {
		return processed.Load() >= 25 && aggSvc.UpdateBook.ActiveBooks() >= 1
	}) {
		t.Fatalf("processor not ready before ticks: processed=%d books=%d", processed.Load(), aggSvc.UpdateBook.ActiveBooks())
	}
	emitManualTicks(tickCh, verticalTicks)

	if !waitForCounts(10*time.Second, func() bool { return counters.ready(10) }) {
		t.Fatalf(
			"snapshot counts too low: orderbook=%d heatmap=%d volume=%d",
			counters.orderbook.Load(),
			counters.heatmap.Load(),
			counters.volume.Load(),
		)
	}
	if got := aggSvc.UpdateBook.ActiveBooks(); got > verticalMaxBooks {
		t.Fatalf("active books=%d exceeded max=%d", got, verticalMaxBooks)
	}

	<-e.Poison(processorPID).Done()
	close(tickCh)
	outBus.Close()
	consumersWG.Wait()

	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	afterG := runtime.NumGoroutine()
	if afterG-beforeG > 64 {
		t.Fatalf("goroutine drift too high: before=%d after=%d", beforeG, afterG)
	}
	var heapDelta uint64
	if after.HeapAlloc > before.HeapAlloc {
		heapDelta = after.HeapAlloc - before.HeapAlloc
	}
	if heapDelta > 256*1024*1024 {
		t.Fatalf("heap growth too high delta=%d", heapDelta)
	}
}
