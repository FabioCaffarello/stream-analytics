//go:build soak
// +build soak

package main

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"testing"
	"time"

	adapterstorage "github.com/market-raccoon/internal/adapters/storage"
	"github.com/market-raccoon/internal/adapters/storage/clickhouse"
	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/contracts"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/problem"
)

type soakBatch struct {
	mu        sync.Mutex
	rows      [][]any
	flushes   int
	latencies []time.Duration
}

func (b *soakBatch) AppendRow(_ context.Context, values ...any) *problem.Problem {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rows = append(b.rows, append([]any(nil), values...))
	return nil
}

func (b *soakBatch) Flush(_ context.Context) (int64, *problem.Problem) {
	started := time.Now()
	b.mu.Lock()
	defer b.mu.Unlock()
	b.flushes++
	b.latencies = append(b.latencies, time.Since(started))
	return int64(len(b.rows)), nil
}

func (b *soakBatch) Close() *problem.Problem { return nil }

type soakBatchPreparer struct {
	batch *soakBatch
}

func (p *soakBatchPreparer) PrepareInsert(context.Context, string) (adapterstorage.BatchInserter, *problem.Problem) {
	return p.batch, nil
}

func TestStoreSoak_CandleColdWrite_10k(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping soak test in short mode")
	}
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		t.Fatalf("payload codec registry bootstrap: %v", p)
	}

	batch := &soakBatch{}
	candleWriter := clickhouse.NewChCandleWriterWithPreparer(&soakBatchPreparer{batch: batch})
	writers := &storeWriters{
		batcher: testBatcher(clickhouse.NewWriter()),
		candle:  candleWriter,
		stats:   clickhouse.NewChStatsWriter(nil),
		heatmap: clickhouse.NewChHeatmapWriter(nil),
	}

	const total = 10_000
	latencies := make([]time.Duration, 0, total)
	logger := slog.New(slog.NewTextHandler(testingWriter{t: t}, &slog.HandlerOptions{Level: slog.LevelError}))

	for i := 0; i < total; i++ {
		instrument := fmt.Sprintf("SOAK%02dUSDT", (i%10)+1)
		windowStart := int64(i) * 60_000
		windowEnd := windowStart + 60_000
		// Ensure buy + sell equals total volume to satisfy domain invariants.
		volume := 10 + float64(i%5)
		buy := 6 + float64(i%3)
		if buy > volume {
			buy = volume
		}
		sell := volume - buy
		dto := contracts.AggregationCandleClosedV1{
			Candle: contracts.AggregationCandleV1{
				Venue:         "BINANCE",
				Instrument:    instrument,
				Timeframe:     "1m",
				WindowStartTs: windowStart,
				WindowEndTs:   windowEnd,
				Open:          100 + float64(i%20),
				High:          101 + float64(i%20),
				Low:           99 + float64(i%20),
				ClosePrice:    100.5 + float64(i%20),
				Volume:        volume,
				BuyVolume:     buy,
				SellVolume:    sell,
				TradeCount:    int64((i % 9) + 1),
				SeqFirst:      int64(i*10 + 1),
				SeqLast:       int64(i*10 + 9),
				IsClosed:      true,
			},
		}
		payload, p := codec.EncodePayload("aggregation.candle", 1, envelope.ContentTypeJSON, dto)
		if p != nil {
			t.Fatalf("encode candle payload i=%d: %v", i, p)
		}

		env := envelope.Envelope{
			Type:           "aggregation.candle",
			Version:        1,
			Venue:          "BINANCE",
			Instrument:     instrument,
			Seq:            int64(i + 1),
			IdempotencyKey: fmt.Sprintf("candle-%d", i+1),
			ContentType:    envelope.ContentTypeJSON,
			Payload:        payload,
		}

		started := time.Now()
		if p := handleStoreEnvelope(context.Background(), env, writers, logger); p != nil {
			t.Fatalf("handleStoreEnvelope candle failed i=%d: %v", i, p)
		}
		latencies = append(latencies, time.Since(started))
	}

	if got := len(batch.rows); got != total {
		t.Fatalf("committed candles=%d want=%d", got, total)
	}

	_, p95, _ := durationQuantiles(latencies)
	if p95 > time.Millisecond {
		t.Fatalf("candle commit p95=%s exceeds 1ms", p95)
	}
}

func TestStoreSoak_StatsColdWrite_10k(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping soak test in short mode")
	}
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		t.Fatalf("payload codec registry bootstrap: %v", p)
	}

	batch := &soakBatch{}
	statsWriter := clickhouse.NewChStatsWriterWithPreparer(&soakBatchPreparer{batch: batch})
	writers := &storeWriters{
		batcher: testBatcher(clickhouse.NewWriter()),
		candle:  clickhouse.NewChCandleWriter(nil),
		stats:   statsWriter,
		heatmap: clickhouse.NewChHeatmapWriter(nil),
	}

	const total = 10_000
	latencies := make([]time.Duration, 0, total)
	logger := slog.New(slog.NewTextHandler(testingWriter{t: t}, &slog.HandlerOptions{Level: slog.LevelError}))

	for i := 0; i < total; i++ {
		instrument := fmt.Sprintf("SOAK%02dUSDT", (i%10)+1)
		windowStart := int64(i) * 60_000
		windowEnd := windowStart + 60_000
		mark := 0.0
		funding := 0.0
		if i%3 != 0 {
			mark = 200 + float64(i%50)
		}
		if i%4 == 0 {
			funding = 0.0001
		}
		dto := contracts.AggregationStatsWindowClosedV1{
			Stats: contracts.AggregationStatsWindowV1{
				Venue:           "BYBIT",
				Instrument:      instrument,
				Timeframe:       "1m",
				WindowStartTs:   windowStart,
				WindowEndTs:     windowEnd,
				LiqBuyVolume:    3 + float64(i%3),
				LiqSellVolume:   2 + float64(i%2),
				LiqTotalVolume:  5 + float64(i%5),
				LiqCount:        int64((i % 7) + 1),
				MarkPriceOpen:   mark,
				MarkPriceHigh:   mark,
				MarkPriceLow:    mark,
				MarkPriceClose:  mark,
				FundingRateAvg:  funding,
				FundingRateLast: funding,
				SeqFirst:        int64(i*10 + 1),
				SeqLast:         int64(i*10 + 9),
				IsClosed:        true,
			},
		}
		payload, p := codec.EncodePayload("aggregation.stats", 1, envelope.ContentTypeJSON, dto)
		if p != nil {
			t.Fatalf("encode stats payload i=%d: %v", i, p)
		}

		env := envelope.Envelope{
			Type:           "aggregation.stats",
			Version:        1,
			Venue:          "BYBIT",
			Instrument:     instrument,
			Seq:            int64(i + 1),
			IdempotencyKey: fmt.Sprintf("stats-%d", i+1),
			ContentType:    envelope.ContentTypeJSON,
			Payload:        payload,
		}

		started := time.Now()
		if p := handleStoreEnvelope(context.Background(), env, writers, logger); p != nil {
			t.Fatalf("handleStoreEnvelope stats failed i=%d: %v", i, p)
		}
		latencies = append(latencies, time.Since(started))
	}

	if got := len(batch.rows); got != total {
		t.Fatalf("committed stats=%d want=%d", got, total)
	}

	_, p95, _ := durationQuantiles(latencies)
	if p95 > time.Millisecond {
		t.Fatalf("stats commit p95=%s exceeds 1ms", p95)
	}
}

type testingWriter struct{ t *testing.T }

func (w testingWriter) Write(p []byte) (int, error) {
	w.t.Logf("%s", p)
	return len(p), nil
}

func durationQuantiles(values []time.Duration) (time.Duration, time.Duration, time.Duration) {
	if len(values) == 0 {
		return 0, 0, 0
	}
	sorted := append([]time.Duration(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	p50 := sorted[len(sorted)/2]
	p95 := sorted[(len(sorted)-1)*95/100]
	p99 := sorted[(len(sorted)-1)*99/100]
	return p50, p95, p99
}

var _ aggdomain.SnapshotProduced
