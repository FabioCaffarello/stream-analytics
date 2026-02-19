package app_test

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/market-raccoon/internal/adapters/bus"
	aggapp "github.com/market-raccoon/internal/core/aggregation/app"
	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	mdapp "github.com/market-raccoon/internal/core/marketdata/app"
	mddomain "github.com/market-raccoon/internal/core/marketdata/domain"
	"github.com/market-raccoon/internal/shared/clock"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/envelope"
	sharedhash "github.com/market-raccoon/internal/shared/hash"
	"github.com/market-raccoon/internal/shared/problem"
)

var (
	e2eHashSink string
	e2eSeqSink  int64
)

type e2eSequencer struct {
	n int64
}

func (s *e2eSequencer) Next(_, _ string) (int64, *problem.Problem) {
	s.n++
	return s.n, nil
}

type benchArtifactPublisher struct{}

func (benchArtifactPublisher) PublishSnapshot(context.Context, aggdomain.SnapshotProduced) *problem.Problem {
	return nil
}
func (benchArtifactPublisher) PublishInconsistent(context.Context, aggdomain.OrderBookInconsistentDetected) *problem.Problem {
	return nil
}
func (benchArtifactPublisher) PublishCandleClosed(context.Context, aggdomain.CandleClosed) *problem.Problem {
	return nil
}
func (benchArtifactPublisher) PublishStatsClosed(context.Context, aggdomain.StatsWindowClosed) *problem.Problem {
	return nil
}

type benchHotStore struct {
	last aggdomain.SnapshotProduced
}

func (s *benchHotStore) Save(_ context.Context, snap aggdomain.SnapshotProduced) *problem.Problem {
	s.last = snap
	return nil
}

type benchCandleStore struct{}

func (benchCandleStore) SaveCandle(context.Context, aggdomain.CandleClosed) *problem.Problem {
	return nil
}

func BenchmarkE2E_IngestToOrderbookSnapshot(b *testing.B) {
	clk := clock.NewFakeClock(time.Unix(1_710_000_000, 0))
	memBus := bus.NewInMemoryBus(4096)
	stream := memBus.Subscribe()

	ingest := mdapp.NewIngestMarketDataWithConfig(clk, &e2eSequencer{}, memBus, mdapp.IngestConfig{
		MaxStreams:         256,
		PublishContentType: envelope.ContentTypeJSON,
	})
	hotStore := &benchHotStore{}
	update := aggapp.NewUpdateOrderBookFromEvents(benchArtifactPublisher{}, hotStore)

	reqs := make([]mdapp.IngestRequest, 1000)
	for i := 0; i < len(reqs); i++ {
		reqs[i] = mdapp.IngestRequest{
			Venue:          "binance",
			Instrument:     "BTC-USDT",
			MarketType:     "SPOT",
			EventType:      "marketdata.bookdelta",
			Version:        1,
			TsExchange:     1_710_000_000_000 + int64(i),
			IdempotencyKey: "e2e-bookdelta-" + strconv.Itoa(i),
			Payload: mddomain.BookDeltaV1{
				Bids:      []mddomain.PriceLevel{{Price: 50_000, Size: 1.0}},
				Asks:      []mddomain.PriceLevel{{Price: 50_100, Size: 1.0}},
				FirstID:   int64(i + 1),
				FinalID:   int64(i + 1),
				PrevFinal: int64(i),
				Timestamp: 1_710_000_000_000 + int64(i),
			},
		}
	}

	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := reqs[i%len(reqs)]
		req.IdempotencyKey = "e2e-bookdelta-bench-" + strconv.Itoa(i)
		clk.Advance(time.Millisecond)
		if res := ingest.Execute(ctx, req); res.IsFail() {
			b.Fatalf("ingest.Execute: %v", res.Problem())
		}

		env := <-stream
		decoded, p := codec.DecodePayload(env.Type, env.Version, env.ContentType, env.Payload)
		if p != nil {
			b.Fatalf("DecodePayload: %v", p)
		}
		delta, ok := decoded.(mddomain.BookDeltaV1)
		if !ok {
			b.Fatalf("decoded type=%T want=%T", decoded, mddomain.BookDeltaV1{})
		}

		bids := make([]aggdomain.Level, 0, len(delta.Bids))
		for _, bid := range delta.Bids {
			bids = append(bids, aggdomain.Level{Price: aggdomain.Price(bid.Price), Quantity: aggdomain.Quantity(bid.Size)})
		}
		asks := make([]aggdomain.Level, 0, len(delta.Asks))
		for _, ask := range delta.Asks {
			asks = append(asks, aggdomain.Level{Price: aggdomain.Price(ask.Price), Quantity: aggdomain.Quantity(ask.Size)})
		}

		if out := update.Execute(ctx, aggapp.UpdateRequest{
			Venue:      env.Venue,
			Instrument: env.Instrument,
			Seq:        env.Seq,
			Bids:       bids,
			Asks:       asks,
		}); out.IsFail() {
			b.Fatalf("update.Execute: %v", out.Problem())
		}

		snap := hotStore.last
		e2eHashSink = sharedhash.HashFields(
			snap.BookID.Venue,
			snap.BookID.Instrument,
			strconv.FormatInt(snap.Seq, 10),
			fmt.Sprintf("%d:%d", len(snap.Bids), len(snap.Asks)),
		)
		e2eSeqSink = snap.Seq
	}
}

func BenchmarkE2E_TradeToCandle(b *testing.B) {
	clk := clock.NewFakeClock(time.Unix(1_710_000_000, 0))
	memBus := bus.NewInMemoryBus(4096)
	stream := memBus.Subscribe()

	ingest := mdapp.NewIngestMarketDataWithConfig(clk, &e2eSequencer{}, memBus, mdapp.IngestConfig{
		MaxStreams:         256,
		PublishContentType: envelope.ContentTypeJSON,
	})
	candles := aggapp.NewBuildCandleFromEvents(benchArtifactPublisher{}, benchCandleStore{}, aggapp.BuildCandleConfig{
		MaxCandles: 1_000,
		CandleTTL:  time.Hour,
		Clock:      clk,
	})

	reqs := make([]mdapp.IngestRequest, 1000)
	for i := 0; i < len(reqs); i++ {
		reqs[i] = mdapp.IngestRequest{
			Venue:          "binance",
			Instrument:     "BTC-USDT",
			MarketType:     "SPOT",
			EventType:      "marketdata.trade",
			Version:        1,
			TsExchange:     1_710_000_000_000 + int64(i)*60_000,
			IdempotencyKey: "e2e-trade-" + strconv.Itoa(i),
			Payload: mddomain.TradeTickV1{
				Price:     50_000 + float64(i%20),
				Size:      0.5 + float64(i%3)*0.1,
				Side:      "buy",
				TradeID:   "trade-" + strconv.Itoa(i),
				Timestamp: 1_710_000_000_000 + int64(i)*60_000,
			},
		}
	}

	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := reqs[i%len(reqs)]
		req.IdempotencyKey = "e2e-trade-bench-" + strconv.Itoa(i)
		if trade, ok := req.Payload.(mddomain.TradeTickV1); ok {
			trade.TradeID = "trade-bench-" + strconv.Itoa(i)
			req.Payload = trade
		}
		clk.Advance(time.Minute)
		if res := ingest.Execute(ctx, req); res.IsFail() {
			b.Fatalf("ingest.Execute: %v", res.Problem())
		}

		env := <-stream
		decoded, p := codec.DecodePayload(env.Type, env.Version, env.ContentType, env.Payload)
		if p != nil {
			b.Fatalf("DecodePayload: %v", p)
		}
		trade, ok := decoded.(mddomain.TradeTickV1)
		if !ok {
			b.Fatalf("decoded type=%T want=%T", decoded, mddomain.TradeTickV1{})
		}

		resp, p := candles.Execute(ctx, aggapp.BuildCandleRequest{
			Venue:      env.Venue,
			Instrument: env.Instrument,
			Price:      trade.Price,
			Quantity:   trade.Size,
			IsBuy:      trade.Side == "buy",
			Seq:        env.Seq,
			TsIngest:   env.TsIngest,
		})
		if p != nil {
			b.Fatalf("candles.Execute: %v", p)
		}

		if len(resp.Closed) > 0 {
			closed := resp.Closed[0]
			e2eHashSink = sharedhash.HashFields(
				closed.Candle.Venue,
				closed.Candle.Instrument,
				closed.Candle.Timeframe,
				strconv.FormatInt(closed.Candle.WindowStartTs, 10),
			)
			e2eSeqSink = closed.Candle.SeqLast
		}
	}
}

func BenchmarkE2E_MarkPriceToStats(b *testing.B) {
	clk := clock.NewFakeClock(time.Unix(1_710_000_000, 0))
	memBus := bus.NewInMemoryBus(4096)
	stream := memBus.Subscribe()

	ingest := mdapp.NewIngestMarketDataWithConfig(clk, &e2eSequencer{}, memBus, mdapp.IngestConfig{
		MaxStreams:         256,
		PublishContentType: envelope.ContentTypeJSON,
	})
	stats := aggapp.NewBuildStatsFromEvents(benchArtifactPublisher{}, nil, aggapp.BuildStatsConfig{
		MaxWindows: 1_000,
		WindowTTL:  time.Hour,
		Clock:      clk,
	})

	reqs := make([]mdapp.IngestRequest, 1000)
	for i := 0; i < len(reqs); i++ {
		reqs[i] = mdapp.IngestRequest{
			Venue:          "binance",
			Instrument:     "BTC-USDT",
			MarketType:     "USD_M_FUTURES",
			EventType:      "marketdata.markprice",
			Version:        1,
			TsExchange:     1_710_000_000_000 + int64(i)*60_000,
			IdempotencyKey: "e2e-markprice-" + strconv.Itoa(i),
			Payload: mddomain.MarkPriceTickV1{
				MarkPrice:   50_000 + float64(i%20),
				IndexPrice:  49_999 + float64(i%10),
				FundingRate: 0.0001 + float64(i%5)*0.00001,
				Timestamp:   1_710_000_000_000 + int64(i)*60_000,
			},
		}
	}

	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := reqs[i%len(reqs)]
		req.IdempotencyKey = "e2e-markprice-bench-" + strconv.Itoa(i)
		clk.Advance(time.Minute)
		if res := ingest.Execute(ctx, req); res.IsFail() {
			b.Fatalf("ingest.Execute: %v", res.Problem())
		}

		env := <-stream
		decoded, p := codec.DecodePayload(env.Type, env.Version, env.ContentType, env.Payload)
		if p != nil {
			b.Fatalf("DecodePayload: %v", p)
		}
		mp, ok := decoded.(mddomain.MarkPriceTickV1)
		if !ok {
			b.Fatalf("decoded type=%T want=%T", decoded, mddomain.MarkPriceTickV1{})
		}

		resp, p := stats.Execute(ctx, aggapp.BuildStatsRequest{
			Venue:      env.Venue,
			Instrument: env.Instrument,
			Kind:       aggapp.StatsInputMarkPrice,
			Seq:        env.Seq,
			TsIngest:   env.TsIngest,
			MarkPrice:  mp.MarkPrice,
		})
		if p != nil {
			b.Fatalf("stats.Execute(markprice): %v", p)
		}
		if mp.FundingRate != 0 {
			resp, p = stats.Execute(ctx, aggapp.BuildStatsRequest{
				Venue:       env.Venue,
				Instrument:  env.Instrument,
				Kind:        aggapp.StatsInputFundingRate,
				Seq:         env.Seq,
				TsIngest:    env.TsIngest,
				FundingRate: mp.FundingRate,
			})
			if p != nil {
				b.Fatalf("stats.Execute(funding): %v", p)
			}
		}

		if len(resp.Closed) > 0 {
			closed := resp.Closed[0]
			e2eHashSink = sharedhash.HashFields(
				closed.Stats.Venue,
				closed.Stats.Instrument,
				closed.Stats.Timeframe,
				strconv.FormatInt(closed.Stats.WindowStartTs, 10),
			)
			e2eSeqSink = closed.Stats.SeqLast
		}
	}
}
