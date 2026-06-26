package main

import (
	"context"
	"log/slog"
	"testing"

	adapterstorage "github.com/FabioCaffarello/stream-analytics/internal/adapters/storage"
	"github.com/FabioCaffarello/stream-analytics/internal/adapters/storage/clickhouse"
	"github.com/FabioCaffarello/stream-analytics/internal/contracts"
	insightsdomain "github.com/FabioCaffarello/stream-analytics/internal/core/insights/domain"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/codec"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/envelope"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

type heatmapBatch struct {
	rows    [][]any
	flushes int
}

func (b *heatmapBatch) AppendRow(_ context.Context, values ...any) *problem.Problem {
	b.rows = append(b.rows, append([]any(nil), values...))
	return nil
}

func (b *heatmapBatch) Flush(context.Context) (int64, *problem.Problem) {
	b.flushes++
	return int64(len(b.rows)), nil
}

func (b *heatmapBatch) Close() *problem.Problem { return nil }

type heatmapBatchPreparer struct {
	batch *heatmapBatch
}

func (p *heatmapBatchPreparer) PrepareInsert(context.Context, string) (adapterstorage.BatchInserter, *problem.Problem) {
	return p.batch, nil
}

func TestHandleInsightsHeatmapSnapshot_CommitsToColdWriter(t *testing.T) {
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		t.Fatalf("payload codec registry bootstrap: %v", p)
	}

	artifact := insightsdomain.HeatmapArtifactV1{
		Venue:         "BINANCE",
		Instrument:    "BTCUSDT",
		Timeframe:     "1m",
		WindowStartTs: 1_700_000_000_000,
		WindowEndTs:   1_700_000_060_000,
		Cells: []insightsdomain.HeatmapCellV1{
			{
				PriceBucketLow:  100,
				PriceBucketHigh: 100.5,
				SizeBucket:      "M",
				BidLiquidity:    1,
				AskLiquidity:    2,
				TradeVolume:     3,
				SeqMin:          1,
				SeqMax:          10,
				Samples:         4,
			},
		},
	}
	payload, p := codec.EncodePayload(
		insightsdomain.HeatmapSnapshotType,
		insightsdomain.HeatmapSnapshotVersion,
		envelope.ContentTypeJSON,
		artifact,
	)
	if p != nil {
		t.Fatalf("encode payload: %v", p)
	}
	env := envelope.Envelope{
		Type:           insightsdomain.HeatmapSnapshotType,
		Version:        insightsdomain.HeatmapSnapshotVersion,
		Venue:          artifact.Venue,
		Instrument:     artifact.Instrument,
		Seq:            10,
		ContentType:    envelope.ContentTypeJSON,
		IdempotencyKey: "hm-1",
		Payload:        payload,
	}

	batch := &heatmapBatch{}
	writer := clickhouse.NewChHeatmapWriterWithPreparer(&heatmapBatchPreparer{batch: batch})
	if p := handleInsightsHeatmapSnapshot(context.Background(), env, writer, slog.Default()); p != nil {
		t.Fatalf("handle heatmap snapshot: %v", p)
	}
	if got, want := len(batch.rows), 1; got != want {
		t.Fatalf("rows=%d want=%d", got, want)
	}
	if got, want := batch.flushes, 1; got != want {
		t.Fatalf("flushes=%d want=%d", got, want)
	}
}

func TestHandleStoreEnvelope_RoutesHeatmapSnapshot(t *testing.T) {
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		t.Fatalf("payload codec registry bootstrap: %v", p)
	}

	artifact := insightsdomain.HeatmapArtifactV1{
		Venue:         "BYBIT",
		Instrument:    "ETHUSDT",
		Timeframe:     "1m",
		WindowStartTs: 1_700_000_060_000,
		WindowEndTs:   1_700_000_120_000,
		Cells: []insightsdomain.HeatmapCellV1{
			{
				PriceBucketLow:  2000,
				PriceBucketHigh: 2001,
				SizeBucket:      "S",
				BidLiquidity:    0.5,
				AskLiquidity:    0.7,
				TradeVolume:     1.2,
				SeqMin:          11,
				SeqMax:          15,
				Samples:         3,
			},
		},
	}
	payload, p := codec.EncodePayload(
		insightsdomain.HeatmapSnapshotType,
		insightsdomain.HeatmapSnapshotVersion,
		envelope.ContentTypeJSON,
		artifact,
	)
	if p != nil {
		t.Fatalf("encode payload: %v", p)
	}
	env := envelope.Envelope{
		Type:           insightsdomain.HeatmapSnapshotType,
		Version:        insightsdomain.HeatmapSnapshotVersion,
		Venue:          artifact.Venue,
		Instrument:     artifact.Instrument,
		Seq:            15,
		ContentType:    envelope.ContentTypeJSON,
		IdempotencyKey: "hm-2",
		Payload:        payload,
	}

	batch := &heatmapBatch{}
	writers := &storeWriters{
		batcher: testBatcher(clickhouse.NewWriter()),
		candle:  clickhouse.NewChCandleWriter(nil),
		stats:   clickhouse.NewChStatsWriter(nil),
		heatmap: clickhouse.NewChHeatmapWriterWithPreparer(&heatmapBatchPreparer{batch: batch}),
	}

	if p := handleStoreEnvelope(context.Background(), env, writers, slog.Default()); p != nil {
		t.Fatalf("handle store envelope: %v", p)
	}
	if got, want := len(batch.rows), 1; got != want {
		t.Fatalf("rows=%d want=%d", got, want)
	}
}
