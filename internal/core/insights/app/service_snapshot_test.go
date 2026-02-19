package app

import (
	"context"
	"testing"

	"github.com/market-raccoon/internal/shared/problem"
)

func TestInsightsService_SnapshotHeatmap(t *testing.T) {
	svc := NewInsightsService(InsightsServiceConfig{})

	res := svc.Heatmap.Execute(context.Background(), BuildHeatmapRequest{
		EventType:  "marketdata.trade",
		Venue:      "binance",
		Instrument: "BTC-USDT",
		Timeframe:  "1m",
		TickSize:   0.5,
		Price:      100,
		Size:       2,
		Side:       "buy",
		TsIngest:   1_710_000_000_000,
		Seq:        1,
	})
	if res.IsFail() {
		t.Fatalf("heatmap execute failed: %v", res.Problem())
	}

	snapRes := svc.SnapshotHeatmap(context.Background(), HeatmapSnapshotKey{
		Venue:      "binance",
		Instrument: "BTC-USDT",
		Timeframe:  "1m",
	})
	if snapRes.IsFail() {
		t.Fatalf("snapshot failed: %v", snapRes.Problem())
	}
	snap := snapRes.Value()
	if got, want := snap.Venue, "BINANCE"; got != want {
		t.Fatalf("venue=%q want=%q", got, want)
	}
	if got, want := snap.Instrument, "BTCUSDT"; got != want {
		t.Fatalf("instrument=%q want=%q", got, want)
	}
	if len(snap.Cells) == 0 {
		t.Fatal("expected at least one heatmap cell")
	}
}

func TestInsightsService_SnapshotVolumeProfile(t *testing.T) {
	svc := NewInsightsService(InsightsServiceConfig{})

	res := svc.VolumeProfile.Execute(context.Background(), BuildVolumeProfileRequest{
		EventType:  "marketdata.trade",
		Venue:      "binance",
		Instrument: "BTC-USDT",
		Timeframe:  "1m",
		TickSize:   0.5,
		Price:      100.2,
		Size:       1.5,
		Side:       "buy",
		TsIngest:   1_710_000_000_000,
		Seq:        1,
	})
	if res.IsFail() {
		t.Fatalf("volume profile execute failed: %v", res.Problem())
	}

	snapRes := svc.SnapshotVolumeProfile(context.Background(), VolumeProfileSnapshotKey{
		Venue:      "binance",
		Instrument: "BTC-USDT",
		Timeframe:  "1m",
	})
	if snapRes.IsFail() {
		t.Fatalf("snapshot failed: %v", snapRes.Problem())
	}
	snap := snapRes.Value()
	if got, want := snap.Venue, "BINANCE"; got != want {
		t.Fatalf("venue=%q want=%q", got, want)
	}
	if got, want := snap.Instrument, "BTCUSDT"; got != want {
		t.Fatalf("instrument=%q want=%q", got, want)
	}
	if len(snap.Buckets) == 0 {
		t.Fatal("expected at least one volume profile bucket")
	}
}

func TestInsightsService_SnapshotQueries_NotFound(t *testing.T) {
	svc := NewInsightsService(InsightsServiceConfig{})

	hm := svc.SnapshotHeatmap(context.Background(), HeatmapSnapshotKey{
		Venue:      "binance",
		Instrument: "BTC-USDT",
		Timeframe:  "1m",
	})
	if hm.IsOk() {
		t.Fatal("expected heatmap snapshot miss")
	}
	if got, want := hm.Problem().Code, problem.NotFound; got != want {
		t.Fatalf("heatmap code=%s want=%s", got, want)
	}

	vp := svc.SnapshotVolumeProfile(context.Background(), VolumeProfileSnapshotKey{
		Venue:      "binance",
		Instrument: "BTC-USDT",
		Timeframe:  "1m",
	})
	if vp.IsOk() {
		t.Fatal("expected volume profile snapshot miss")
	}
	if got, want := vp.Problem().Code, problem.NotFound; got != want {
		t.Fatalf("vpvr code=%s want=%s", got, want)
	}
}
