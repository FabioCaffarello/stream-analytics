package app_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/app"
	"github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/domain"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

type fakeGapCandleReader struct {
	timestamps []int64
	first      *domain.CandleV1
	last       *domain.CandleV1
}

func (f *fakeGapCandleReader) GetCandleRange(_ context.Context, _, _, _ string, _, _ int64, _ int) ([]domain.CandleV1, *problem.Problem) {
	return nil, nil
}

func (f *fakeGapCandleReader) GetCandleTimestamps(_ context.Context, _, _, _ string, _, _ int64) ([]int64, *problem.Problem) {
	return append([]int64(nil), f.timestamps...), nil
}

func (f *fakeGapCandleReader) GetFirstCandle(_ context.Context, _, _, _ string) (*domain.CandleV1, *problem.Problem) {
	return f.first, nil
}

func (f *fakeGapCandleReader) GetLastCandle(_ context.Context, _, _, _ string) (*domain.CandleV1, *problem.Problem) {
	return f.last, nil
}

func TestDetectCandleGaps_NoGaps(t *testing.T) {
	r := &fakeGapCandleReader{timestamps: []int64{60_000, 120_000, 180_000}}

	reports, p := app.DetectCandleGaps(context.Background(), r, app.GapDetectorConfig{
		Venue:          "BINANCE",
		Instrument:     "BTCUSDT",
		Timeframe:      "1m",
		FromMs:         60_000,
		ToMs:           180_000,
		ExpectedStepMs: 60_000,
	})
	if p != nil {
		t.Fatalf("DetectCandleGaps: %v", p)
	}
	if len(reports) != 0 {
		t.Fatalf("gaps=%+v want=none", reports)
	}
}

func TestDetectCandleGaps_SingleGap(t *testing.T) {
	r := &fakeGapCandleReader{
		timestamps: []int64{0, 60_000, 120_000, 1_080_000},
		first:      &domain.CandleV1{WindowStartTs: 0},
	}

	reports, p := app.DetectCandleGaps(context.Background(), r, app.GapDetectorConfig{
		Venue:          "BINANCE",
		Instrument:     "BTCUSDT",
		Timeframe:      "1m",
		FromMs:         0,
		ToMs:           1_080_000,
		ExpectedStepMs: 60_000,
	})
	if p != nil {
		t.Fatalf("DetectCandleGaps: %v", p)
	}
	if len(reports) != 1 {
		t.Fatalf("len=%d want=1", len(reports))
	}
	if reports[0].GapStartMs != 180_000 || reports[0].GapEndMs != 1_020_000 || reports[0].Missing != 15 {
		t.Fatalf("gap=%+v", reports[0])
	}
}

func TestDetectCandleGaps_MultipleGaps(t *testing.T) {
	r := &fakeGapCandleReader{
		timestamps: []int64{0, 60_000, 240_000, 300_000, 600_000},
		first:      &domain.CandleV1{WindowStartTs: 0},
	}

	reports, p := app.DetectCandleGaps(context.Background(), r, app.GapDetectorConfig{
		Venue:          "BINANCE",
		Instrument:     "ETHUSDT",
		Timeframe:      "1m",
		FromMs:         0,
		ToMs:           600_000,
		ExpectedStepMs: 60_000,
	})
	if p != nil {
		t.Fatalf("DetectCandleGaps: %v", p)
	}

	want := []app.GapReport{
		{Venue: "BINANCE", Instrument: "ETHUSDT", Timeframe: "1m", GapStartMs: 120_000, GapEndMs: 180_000, Missing: 2},
		{Venue: "BINANCE", Instrument: "ETHUSDT", Timeframe: "1m", GapStartMs: 360_000, GapEndMs: 540_000, Missing: 4},
	}
	if !reflect.DeepEqual(reports, want) {
		t.Fatalf("reports=%+v want=%+v", reports, want)
	}
}

func TestDetectCandleGaps_EmptyStorage(t *testing.T) {
	r := &fakeGapCandleReader{}

	reports, p := app.DetectCandleGaps(context.Background(), r, app.GapDetectorConfig{
		Venue:          "BINANCE",
		Instrument:     "BTCUSDT",
		Timeframe:      "1m",
		FromMs:         0,
		ToMs:           0,
		ExpectedStepMs: 60_000,
	})
	if p != nil {
		t.Fatalf("DetectCandleGaps: %v", p)
	}
	if len(reports) != 0 {
		t.Fatalf("reports=%+v want=none", reports)
	}
}

func TestDetectCandleGaps_AutoAnchor(t *testing.T) {
	r := &fakeGapCandleReader{
		timestamps: []int64{60_000, 120_000, 240_000},
		first:      &domain.CandleV1{WindowStartTs: 60_000},
		last:       &domain.CandleV1{WindowStartTs: 240_000},
	}

	reports, p := app.DetectCandleGaps(context.Background(), r, app.GapDetectorConfig{
		Venue:          "BINANCE",
		Instrument:     "BTCUSDT",
		Timeframe:      "1m",
		FromMs:         0,
		ToMs:           0,
		ExpectedStepMs: 60_000,
	})
	if p != nil {
		t.Fatalf("DetectCandleGaps: %v", p)
	}
	if len(reports) != 1 {
		t.Fatalf("len=%d want=1", len(reports))
	}
	if reports[0].GapStartMs != 180_000 || reports[0].GapEndMs != 180_000 || reports[0].Missing != 1 {
		t.Fatalf("gap=%+v", reports[0])
	}
}
