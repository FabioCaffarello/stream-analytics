package main

import (
	"context"
	"os"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/market-raccoon/internal/adapters/bus"
	"github.com/market-raccoon/internal/adapters/exchange/binance"
	aggapp "github.com/market-raccoon/internal/core/aggregation/app"
	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	mddomain "github.com/market-raccoon/internal/core/marketdata/domain"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/observability"
	"github.com/market-raccoon/internal/shared/problem"
)

type soakBusObserver struct {
	published atomic.Int64
	dropped   atomic.Int64
}

func (o *soakBusObserver) IncPublished(string, string)                 { o.published.Add(1) }
func (o *soakBusObserver) IncDropped(int)                              { o.dropped.Add(1) }
func (o *soakBusObserver) IncPublishError(string)                      {}
func (o *soakBusObserver) ObservePublishLatency(string, time.Duration) {}
func (o *soakBusObserver) IncConsumed(string, string)                  {}
func (o *soakBusObserver) IncRedelivered(string)                       {}
func (o *soakBusObserver) ObserveAckLatency(string, time.Duration)     {}
func (o *soakBusObserver) SetConsumerLag(string, int64)                {}

var _ observability.BusObserver = (*soakBusObserver)(nil)

type soakBusArtifactPublisher struct {
	inner   *soakArtifactPublisher
	bus     *bus.InMemoryBus
	seq     atomic.Int64
	candles atomic.Int64
}

func (p *soakBusArtifactPublisher) PublishSnapshot(ctx context.Context, evt aggdomain.SnapshotProduced) *problem.Problem {
	return p.inner.PublishSnapshot(ctx, evt)
}

func (p *soakBusArtifactPublisher) PublishInconsistent(ctx context.Context, evt aggdomain.OrderBookInconsistentDetected) *problem.Problem {
	return p.inner.PublishInconsistent(ctx, evt)
}

func (p *soakBusArtifactPublisher) PublishCandleClosed(ctx context.Context, evt aggdomain.CandleClosed) *problem.Problem {
	if prob := p.inner.PublishCandleClosed(ctx, evt); prob != nil {
		return prob
	}
	p.candles.Add(1)
	seq := p.seq.Add(1)
	return p.bus.Publish(ctx, envelope.Envelope{
		Type:        "aggregation.candle",
		Version:     1,
		Venue:       evt.Candle.Venue,
		Instrument:  evt.Candle.Instrument,
		Seq:         seq,
		TsIngest:    time.Now().UnixMilli(),
		ContentType: envelope.ContentTypeJSON,
		Payload:     []byte(`{}`),
	})
}

func (p *soakBusArtifactPublisher) PublishStatsClosed(ctx context.Context, evt aggdomain.StatsWindowClosed) *problem.Problem {
	return p.inner.PublishStatsClosed(ctx, evt)
}

//nolint:gocyclo // soak scenario intentionally validates aggregation and delivery behavior jointly.
func TestSoak_PipelineWithDelivery_100k(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping soak test in short mode")
	}
	if os.Getenv(soakEnableEnv) != "1" {
		t.Skipf("set %s=1 to run soak tests", soakEnableEnv)
	}

	const (
		totalMessages          = 100_000
		maxBooks               = 4_096
		maxCandles             = 50_000
		maxWindows             = 50_000
		heapBudgetBytes uint64 = 256 * 1024 * 1024
	)

	observer := &soakBusObserver{}
	memBus := bus.NewInMemoryBus(64, observer)
	pub := &soakBusArtifactPublisher{inner: &soakArtifactPublisher{}, bus: memBus}

	svc := aggapp.NewAggregationService(aggapp.AggregationServiceConfig{
		Update: aggapp.UpdateConfig{MaxBooks: maxBooks},
		Candle: aggapp.BuildCandleConfig{MaxCandles: maxCandles},
		Stats:  aggapp.BuildStatsConfig{MaxWindows: maxWindows},

		Publisher:   pub,
		Store:       soakHotStore{},
		CandleStore: soakCandleStore{},
		StatsStore:  soakStatsStore{},
	})

	var fastCounts [10]atomic.Int64
	var slowCounts [5]atomic.Int64
	var wg sync.WaitGroup

	for i := range fastCounts {
		ch := memBus.Subscribe()
		wg.Add(1)
		go func(idx int, c <-chan envelope.Envelope) {
			defer wg.Done()
			for range c {
				fastCounts[idx].Add(1)
			}
		}(i, ch)
	}
	for i := range slowCounts {
		ch := memBus.Subscribe()
		wg.Add(1)
		go func(idx int, c <-chan envelope.Envelope) {
			defer wg.Done()
			for range c {
				time.Sleep(time.Millisecond)
				slowCounts[idx].Add(1)
			}
		}(i, ch)
	}

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	beforeG := runtime.NumGoroutine()

	ctx := context.Background()
	latencies := make([]time.Duration, 0, totalMessages)
	const baseTs = int64(1_735_689_600_000)

	for i := 0; i < totalMessages; i++ {
		started := time.Now()
		symbol := "BTCUSDT"
		seq := int64(i + 1)
		ts := baseTs + int64(i)*61_000

		raw := buildBinanceTradePayload(symbol, 100.0+float64(i%100), 0.05+float64(i%10)/100.0, ts, seq)
		req, skip, p := binance.ParseMessageForMarketType(raw, time.UnixMilli(ts), mddomain.MarketTypeSpot.String())
		if p != nil || skip {
			t.Fatalf("trade parse failed at i=%d skip=%v p=%v", i, skip, p)
		}

		payload, ok := req.Payload.(mddomain.TradeTickV1)
		if !ok {
			t.Fatalf("unexpected payload type=%T", req.Payload)
		}

		if _, p := svc.Candle.Execute(ctx, aggapp.BuildCandleRequest{
			Venue:      "binance",
			Instrument: req.Instrument,
			Price:      payload.Price,
			Quantity:   payload.Size,
			IsBuy:      payload.Side == "buy",
			Seq:        seq,
			TsIngest:   payload.Timestamp,
		}); p != nil {
			t.Fatalf("build candle failed at i=%d: %v", i, p)
		}

		latencies = append(latencies, time.Since(started))
	}

	time.Sleep(2 * time.Second)
	published := pub.candles.Load()
	if published == 0 {
		t.Fatal("expected at least one published candle")
	}

	memBus.Close()
	wg.Wait()

	if got := svc.UpdateBook.ActiveBooks(); got > maxBooks {
		t.Fatalf("active books=%d exceeded max=%d", got, maxBooks)
	}
	if got := svc.Candle.ActiveCandles(); got > maxCandles {
		t.Fatalf("active candles=%d exceeded max=%d", got, maxCandles)
	}
	if got := svc.Stats.ActiveWindows(); got > maxWindows {
		t.Fatalf("active stats windows=%d exceeded max=%d", got, maxWindows)
	}

	const fastConsumerMinRatio = 0.99
	sumFast := func() int64 {
		var total int64
		for i := range fastCounts {
			total += fastCounts[i].Load()
		}
		return total
	}
	for i := 0; i < 3; i++ {
		beforeDrain := sumFast()
		time.Sleep(200 * time.Millisecond)
		afterDrain := sumFast()
		if afterDrain == beforeDrain {
			break
		}
	}

	min := int64(float64(published) * fastConsumerMinRatio)
	for i := range fastCounts {
		got := fastCounts[i].Load()
		if got < min {
			t.Fatalf("fast consumer %d got=%d published=%d min=%d ratio=%.2f", i, got, published, min, fastConsumerMinRatio)
		}
	}

	slowMissed := false
	for i := range slowCounts {
		got := slowCounts[i].Load()
		if got < published {
			slowMissed = true
		}
	}
	if !slowMissed {
		t.Fatalf("expected at least one slow consumer to miss messages, published=%d", published)
	}

	drops := observer.dropped.Load()
	if drops <= 0 {
		t.Fatalf("expected total drops > 0, got=%d", drops)
	}

	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	afterG := runtime.NumGoroutine()

	if afterG-beforeG > 48 {
		t.Fatalf("goroutine drift too high: before=%d after=%d", beforeG, afterG)
	}
	var heapDelta uint64
	if after.HeapAlloc > before.HeapAlloc {
		heapDelta = after.HeapAlloc - before.HeapAlloc
	}
	if heapDelta > heapBudgetBytes {
		t.Fatalf("heap growth too high delta=%d bytes", heapDelta)
	}

	p50, p95, p99 := latencyQuantilesAll(latencies)
	if p95 > 25*time.Millisecond {
		t.Fatalf("p95 latency=%s exceeds budget=25ms", p95)
	}
	if p99 > 50*time.Millisecond {
		t.Fatalf("p99 latency=%s exceeds budget=50ms", p99)
	}

	t.Logf("pipeline+delivery soak: published=%d drops=%d p50=%s p95=%s p99=%s",
		published,
		drops,
		p50,
		p95,
		p99,
	)
}

func latencyQuantilesAll(samples []time.Duration) (time.Duration, time.Duration, time.Duration) {
	if len(samples) == 0 {
		return 0, 0, 0
	}
	sorted := append([]time.Duration(nil), samples...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	p50 := sorted[len(sorted)/2]
	p95 := sorted[(len(sorted)-1)*95/100]
	p99 := sorted[(len(sorted)-1)*99/100]
	return p50, p95, p99
}
