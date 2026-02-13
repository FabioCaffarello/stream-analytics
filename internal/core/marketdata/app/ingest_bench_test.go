package app_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/market-raccoon/internal/core/marketdata/app"
	"github.com/market-raccoon/internal/core/marketdata/domain"
	"github.com/market-raccoon/internal/shared/clock"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/problem"
)

type benchSequencer struct {
	n int64
}

func (s *benchSequencer) Next(_, _ string) (int64, *problem.Problem) {
	s.n++
	return s.n, nil
}

type benchPublisher struct{}

func (benchPublisher) Publish(_ context.Context, _ envelope.Envelope) *problem.Problem {
	return nil
}

func BenchmarkIngest_1000Envelopes(b *testing.B) {
	b.ReportAllocs()

	clk := clock.NewFakeClock(time.Unix(1_710_000_000, 0))
	uc := app.NewIngestMarketDataWithConfig(clk, &benchSequencer{}, benchPublisher{}, app.IngestConfig{
		DedupWindowSize:    1,
		MaxStreams:         64,
		StreamTTL:          time.Hour,
		PublishContentType: envelope.ContentTypeJSON,
	})

	const envelopesPerIteration = 1000
	reqs := make([]app.IngestRequest, envelopesPerIteration)
	for i := 0; i < envelopesPerIteration; i++ {
		reqs[i] = app.IngestRequest{
			Venue:          "binance",
			Instrument:     "SYM" + strconv.Itoa(i%20) + "/USDT",
			MarketType:     "SPOT",
			EventType:      "marketdata.trade",
			Version:        1,
			TsExchange:     1_710_000_000_000 + int64(i),
			IdempotencyKey: "bench-" + strconv.Itoa(i),
			Payload: domain.TradeTickV1{
				Price:   50_000 + float64(i%50),
				Size:    1.0 + float64(i%5)*0.1,
				Side:    "buy",
				TradeID: "t" + strconv.Itoa(i),
			},
		}
	}

	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < envelopesPerIteration; j++ {
			if result := uc.Execute(ctx, reqs[j]); result.IsFail() {
				b.Fatalf("execute failed at iteration=%d envelope=%d: %v", i, j, result.Problem())
			}
		}
	}
}
