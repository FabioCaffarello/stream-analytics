package app

import (
	"context"
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/core/insights/domain"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
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

func TestInsightsService_SnapshotSessionVolumeProfile(t *testing.T) {
	svc := NewInsightsService(InsightsServiceConfig{
		SessionVolumeProfile: BuildSessionVolumeProfileConfig{EmitCadence: 1},
	})

	anchor := domain.SessionPresets["UTC_DAILY"]
	for i := 0; i < 3; i++ {
		res := svc.SessionVolumeProfile.Execute(context.Background(), BuildSessionVolumeProfileRequest{
			Venue:      "binance",
			Instrument: "BTC-USDT",
			Anchor:     anchor,
			TickSize:   0.5,
			Open:       100,
			High:       105,
			Low:        95,
			Close:      102,
			BuyVolume:  10,
			SellVolume: 5,
			TradeCount: 3,
			TsIngest:   1_710_000_000_000 + int64(i)*60_000,
			SeqFirst:   int64(i*10 + 1),
			SeqLast:    int64(i*10 + 10),
		})
		if res.IsFail() {
			t.Fatalf("svp execute #%d failed: %v", i, res.Problem())
		}
	}

	snapRes := svc.SnapshotSessionVolumeProfile(context.Background(), SessionVolumeProfileSnapshotKey{
		Venue:       "binance",
		Instrument:  "BTC-USDT",
		AnchorLabel: "UTC_DAILY",
	})
	if snapRes.IsFail() {
		t.Fatalf("svp snapshot failed: %v", snapRes.Problem())
	}
	snap := snapRes.Value()
	if got, want := snap.Venue, "BINANCE"; got != want {
		t.Fatalf("venue=%q want=%q", got, want)
	}
	if len(snap.Buckets) == 0 {
		t.Fatal("expected at least one svp bucket")
	}
}

func TestInsightsService_SnapshotTPOProfile(t *testing.T) {
	svc := NewInsightsService(InsightsServiceConfig{
		TPOProfile: BuildTPOProfileConfig{EmitCadence: 1},
	})

	anchor := domain.SessionPresets["UTC_DAILY"]
	for i := 0; i < 3; i++ {
		res := svc.TPOProfile.Execute(context.Background(), BuildTPOProfileRequest{
			Venue:      "binance",
			Instrument: "BTC-USDT",
			Anchor:     anchor,
			TickSize:   0.5,
			High:       105,
			Low:        95,
			TsIngest:   1_710_000_000_000 + int64(i)*60_000,
			SeqLast:    int64(i*10 + 10),
		})
		if res.IsFail() {
			t.Fatalf("tpo execute #%d failed: %v", i, res.Problem())
		}
	}

	snapRes := svc.SnapshotTPOProfile(context.Background(), TPOProfileSnapshotKey{
		Venue:       "binance",
		Instrument:  "BTC-USDT",
		AnchorLabel: "UTC_DAILY",
	})
	if snapRes.IsFail() {
		t.Fatalf("tpo snapshot failed: %v", snapRes.Problem())
	}
	snap := snapRes.Value()
	if got, want := snap.Venue, "BINANCE"; got != want {
		t.Fatalf("venue=%q want=%q", got, want)
	}
	if len(snap.Levels) == 0 {
		t.Fatal("expected at least one tpo level")
	}
	if len(snap.Periods) == 0 {
		t.Fatal("expected at least one tpo period")
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

	svp := svc.SnapshotSessionVolumeProfile(context.Background(), SessionVolumeProfileSnapshotKey{
		Venue:       "binance",
		Instrument:  "BTC-USDT",
		AnchorLabel: "UTC_DAILY",
	})
	if svp.IsOk() {
		t.Fatal("expected svp snapshot miss")
	}
	if got, want := svp.Problem().Code, problem.NotFound; got != want {
		t.Fatalf("svp code=%s want=%s", got, want)
	}

	tpo := svc.SnapshotTPOProfile(context.Background(), TPOProfileSnapshotKey{
		Venue:       "binance",
		Instrument:  "BTC-USDT",
		AnchorLabel: "UTC_DAILY",
	})
	if tpo.IsOk() {
		t.Fatal("expected tpo snapshot miss")
	}
	if got, want := tpo.Problem().Code, problem.NotFound; got != want {
		t.Fatalf("tpo code=%s want=%s", got, want)
	}
}
