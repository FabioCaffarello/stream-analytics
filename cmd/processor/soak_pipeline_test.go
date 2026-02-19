package main

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"testing"

	aggapp "github.com/market-raccoon/internal/core/aggregation/app"
	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	"github.com/market-raccoon/internal/shared/problem"
)

const soakEnableEnv = "MR_ENABLE_SOAK"

type soakArtifactPublisher struct {
	snapshots int
	candles   int
	stats     int
}

func (p *soakArtifactPublisher) PublishSnapshot(context.Context, aggdomain.SnapshotProduced) *problem.Problem {
	p.snapshots++
	return nil
}

func (p *soakArtifactPublisher) PublishInconsistent(context.Context, aggdomain.OrderBookInconsistentDetected) *problem.Problem {
	return nil
}

func (p *soakArtifactPublisher) PublishCandleClosed(context.Context, aggdomain.CandleClosed) *problem.Problem {
	p.candles++
	return nil
}

func (p *soakArtifactPublisher) PublishStatsClosed(context.Context, aggdomain.StatsWindowClosed) *problem.Problem {
	p.stats++
	return nil
}

type soakHotStore struct{}

func (soakHotStore) Save(context.Context, aggdomain.SnapshotProduced) *problem.Problem { return nil }

type soakCandleStore struct{}

func (soakCandleStore) SaveCandle(context.Context, aggdomain.CandleClosed) *problem.Problem {
	return nil
}

type soakStatsStore struct{}

func (soakStatsStore) SaveStats(context.Context, aggdomain.StatsWindowClosed) *problem.Problem {
	return nil
}

//nolint:gocyclo // soak scenario intentionally exercises the full pipeline in one flow.
func TestSoak_FullPipeline_1M_Messages(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping soak test in short mode")
	}
	if os.Getenv(soakEnableEnv) != "1" {
		t.Skipf("set %s=1 to run soak tests", soakEnableEnv)
	}

	const (
		totalMessages = 1_000_000
		instrumentsN  = 100
		maxBooks      = 4_096
		maxCandles    = 50_000
		maxWindows    = 50_000
	)

	pub := &soakArtifactPublisher{}
	svc := aggapp.NewAggregationService(aggapp.AggregationServiceConfig{
		Update: aggapp.UpdateConfig{
			MaxBooks: maxBooks,
		},
		Candle: aggapp.BuildCandleConfig{
			MaxCandles: maxCandles,
		},
		Stats: aggapp.BuildStatsConfig{
			MaxWindows: maxWindows,
		},
		Publisher:   pub,
		Store:       soakHotStore{},
		CandleStore: soakCandleStore{},
		StatsStore:  soakStatsStore{},
	})

	seqByInstrument := make(map[string]int64, instrumentsN)

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	beforeG := runtime.NumGoroutine()

	ctx := context.Background()
	var expectedCandleClosed int
	const baseTs = int64(1_710_000_000_000)

	for i := 0; i < totalMessages; i++ {
		inst := fmt.Sprintf("SYM%03dUSDT", i%instrumentsN)
		seqByInstrument[inst]++
		seq := seqByInstrument[inst]
		ts := baseTs + int64(i)*500

		if res := svc.UpdateBook.Execute(ctx, aggapp.UpdateRequest{
			Venue:      "binance",
			Instrument: inst,
			Seq:        seq,
			Bids: []aggdomain.Level{{
				Price:    aggdomain.Price(100 + float64(i%10)),
				Quantity: aggdomain.Quantity(1),
			}},
			Asks: []aggdomain.Level{{
				Price:    aggdomain.Price(101 + float64(i%10)),
				Quantity: aggdomain.Quantity(1),
			}},
		}); res.IsFail() {
			t.Fatalf("update book failed at i=%d: %v", i, res.Problem())
		}

		candleResp, p := svc.Candle.Execute(ctx, aggapp.BuildCandleRequest{
			Venue:      "binance",
			Instrument: inst,
			Price:      100 + float64(i%10),
			Quantity:   1 + float64(i%3),
			IsBuy:      i%2 == 0,
			Seq:        seq,
			TsIngest:   ts,
		})
		if p != nil {
			t.Fatalf("build candle failed at i=%d: %v", i, p)
		}
		expectedCandleClosed += len(candleResp.Closed)

		statsReq := aggapp.BuildStatsRequest{
			Venue:      "binance",
			Instrument: inst,
			Seq:        seq,
			TsIngest:   ts,
		}
		switch i % 3 {
		case 0:
			statsReq.Kind = aggapp.StatsInputLiquidation
			statsReq.LiquidationSide = "buy"
			statsReq.LiquidationQty = 1
		case 1:
			statsReq.Kind = aggapp.StatsInputMarkPrice
			statsReq.MarkPrice = 100 + float64(i%10)
		default:
			statsReq.Kind = aggapp.StatsInputFundingRate
			statsReq.FundingRate = 0.0001
		}
		if _, p := svc.Stats.Execute(ctx, statsReq); p != nil {
			t.Fatalf("build stats failed at i=%d: %v", i, p)
		}
	}

	if got := svc.UpdateBook.ActiveBooks(); got > maxBooks {
		t.Fatalf("active books=%d exceeded max=%d", got, maxBooks)
	}
	if got := svc.Candle.ActiveCandles(); got > maxCandles {
		t.Fatalf("active candles=%d exceeded max=%d", got, maxCandles)
	}
	if got := svc.Stats.ActiveWindows(); got > maxWindows {
		t.Fatalf("active stats windows=%d exceeded max=%d", got, maxWindows)
	}
	if pub.candles != expectedCandleClosed {
		t.Fatalf("published candle closed=%d expected=%d", pub.candles, expectedCandleClosed)
	}

	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	afterG := runtime.NumGoroutine()
	if afterG-beforeG > 32 {
		t.Fatalf("goroutine drift too high: before=%d after=%d", beforeG, afterG)
	}
	var delta uint64
	if after.HeapAlloc > before.HeapAlloc {
		delta = after.HeapAlloc - before.HeapAlloc
	}
	if delta > 512*1024*1024 {
		t.Fatalf("heap growth too high delta=%d bytes", delta)
	}
}
