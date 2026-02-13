package timescale_test

import (
	"context"
	"testing"

	"github.com/market-raccoon/internal/adapters/storage/timescale"
	insightsports "github.com/market-raccoon/internal/core/insights/ports"
)

func TestVolumeProfileWriter_IdempotentUpsertSamePayload(t *testing.T) {
	w := timescale.NewVolumeProfileWriter()
	upsert := testVPVRUpsert(1.5, 2.5, 4.0, 10, 12)

	if p := w.UpsertVolumeProfileBucket(context.Background(), upsert); p != nil {
		t.Fatalf("first upsert failed: %v", p)
	}
	if p := w.UpsertVolumeProfileBucket(context.Background(), upsert); p != nil {
		t.Fatalf("second upsert failed: %v", p)
	}

	if got := w.RowCount(); got != 1 {
		t.Fatalf("row count mismatch: got=%d want=1", got)
	}
	row, ok := w.ReadByKey(upsert.Venue, upsert.Instrument, upsert.Timeframe, upsert.WindowStartTs, upsert.BucketLow, upsert.BucketHigh)
	if !ok {
		t.Fatal("expected row to exist")
	}
	if row.BuyVolume != 1.5 || row.SellVolume != 2.5 || row.TotalVolume != 4.0 {
		t.Fatalf("unexpected volumes after idempotent upsert: %+v", row)
	}
	if row.SeqMin != 10 || row.SeqMax != 12 {
		t.Fatalf("unexpected seq bounds after idempotent upsert: %+v", row)
	}
	if got := w.CommitCount(); got != 1 {
		t.Fatalf("commit count mismatch: got=%d want=1", got)
	}
}

func TestVolumeProfileWriter_MergeSemanticsDeterministic(t *testing.T) {
	w := timescale.NewVolumeProfileWriter()
	a := testVPVRUpsert(2.0, 1.0, 3.0, 8, 11)
	b := testVPVRUpsert(1.5, 2.5, 4.0, 6, 15)

	if p := w.UpsertVolumeProfileBucket(context.Background(), a); p != nil {
		t.Fatalf("upsert A failed: %v", p)
	}
	if p := w.UpsertVolumeProfileBucket(context.Background(), b); p != nil {
		t.Fatalf("upsert B failed: %v", p)
	}

	row, ok := w.ReadByKey(a.Venue, a.Instrument, a.Timeframe, a.WindowStartTs, a.BucketLow, a.BucketHigh)
	if !ok {
		t.Fatal("expected merged row to exist")
	}
	if row.BuyVolume != 3.5 || row.SellVolume != 3.5 || row.TotalVolume != 7.0 {
		t.Fatalf("unexpected merged volumes: %+v", row)
	}
	if row.SeqMin != 6 || row.SeqMax != 15 {
		t.Fatalf("unexpected merged seq bounds: %+v", row)
	}
	if got := w.RowCount(); got != 1 {
		t.Fatalf("row count mismatch: got=%d want=1", got)
	}
}

func TestVolumeProfileWriter_ReplaySafetyDeterministic(t *testing.T) {
	a := testVPVRUpsert(1.0, 2.0, 3.0, 10, 20)
	b := testVPVRUpsert(0.5, 0.5, 1.0, 9, 22)

	run := func(applyARepeat bool) insightsports.VolumeProfileBucketUpsert {
		w := timescale.NewVolumeProfileWriter()
		if p := w.UpsertVolumeProfileBucket(context.Background(), a); p != nil {
			t.Fatalf("upsert A failed: %v", p)
		}
		if applyARepeat {
			if p := w.UpsertVolumeProfileBucket(context.Background(), a); p != nil {
				t.Fatalf("upsert A repeat failed: %v", p)
			}
		}
		if p := w.UpsertVolumeProfileBucket(context.Background(), b); p != nil {
			t.Fatalf("upsert B failed: %v", p)
		}
		row, ok := w.ReadByKey(a.Venue, a.Instrument, a.Timeframe, a.WindowStartTs, a.BucketLow, a.BucketHigh)
		if !ok {
			t.Fatal("expected replay-safe row to exist")
		}
		if got := w.RowCount(); got != 1 {
			t.Fatalf("row count mismatch: got=%d want=1", got)
		}
		return row
	}

	stateAB := run(false)
	stateAAB := run(true)
	if stateAB != stateAAB {
		t.Fatalf("replay safety mismatch: AB=%+v AAB=%+v", stateAB, stateAAB)
	}
}

func TestVolumeProfileWriter_ReplaySameWindow_FinalStateStable(t *testing.T) {
	a := testVPVRUpsert(1.2, 0.8, 2.0, 10, 12)
	b := testVPVRUpsert(0.4, 0.6, 1.0, 9, 15)

	run := func(repeat bool) insightsports.VolumeProfileBucketUpsert {
		w := timescale.NewVolumeProfileWriter()
		if p := w.UpsertVolumeProfileBucket(context.Background(), a); p != nil {
			t.Fatalf("upsert A failed: %v", p)
		}
		if p := w.UpsertVolumeProfileBucket(context.Background(), b); p != nil {
			t.Fatalf("upsert B failed: %v", p)
		}
		if repeat {
			if p := w.UpsertVolumeProfileBucket(context.Background(), a); p != nil {
				t.Fatalf("upsert A repeat failed: %v", p)
			}
			if p := w.UpsertVolumeProfileBucket(context.Background(), b); p != nil {
				t.Fatalf("upsert B repeat failed: %v", p)
			}
		}
		row, ok := w.ReadByKey(a.Venue, a.Instrument, a.Timeframe, a.WindowStartTs, a.BucketLow, a.BucketHigh)
		if !ok {
			t.Fatal("expected row to exist")
		}
		return row
	}

	state1 := run(false)
	state2 := run(true)
	if state1 != state2 {
		t.Fatalf("final state mismatch on replay same window: base=%+v replay=%+v", state1, state2)
	}
}

func testVPVRUpsert(buy, sell, total float64, seqMin, seqMax int64) insightsports.VolumeProfileBucketUpsert {
	return insightsports.VolumeProfileBucketUpsert{
		Venue:         "binance",
		Instrument:    "BTC-USDT",
		Timeframe:     "1m",
		WindowStartTs: 1_710_000_000_000,
		BucketLow:     100.0,
		BucketHigh:    100.5,
		BuyVolume:     buy,
		SellVolume:    sell,
		TotalVolume:   total,
		SeqMin:        seqMin,
		SeqMax:        seqMax,
	}
}
