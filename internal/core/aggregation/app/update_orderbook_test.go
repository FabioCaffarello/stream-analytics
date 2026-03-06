package app_test

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/market-raccoon/internal/core/aggregation/app"
	"github.com/market-raccoon/internal/core/aggregation/domain"
	"github.com/market-raccoon/internal/shared/clock"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/problem"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// --- fakes ---

type fakePublisher struct {
	snaps        []domain.SnapshotProduced
	inconsistent []domain.OrderBookInconsistentDetected
	candles      []domain.CandleClosed
	stats        []domain.StatsWindowClosed
	tapes        []domain.TapeClosed
	openInterest []domain.OpenInterestClosed
	deltaVolume  []domain.DeltaVolumeClosed
	cvd          []domain.CVDClosed
	barStats     []domain.BarStatsClosed
	snapErr      *problem.Problem
}

func (f *fakePublisher) PublishSnapshot(_ context.Context, s domain.SnapshotProduced) *problem.Problem {
	if f.snapErr != nil {
		return f.snapErr
	}
	f.snaps = append(f.snaps, s)
	return nil
}

func (f *fakePublisher) PublishInconsistent(_ context.Context, e domain.OrderBookInconsistentDetected) *problem.Problem {
	f.inconsistent = append(f.inconsistent, e)
	return nil
}

func (f *fakePublisher) PublishCandleClosed(_ context.Context, e domain.CandleClosed) *problem.Problem {
	f.candles = append(f.candles, e)
	return nil
}

func (f *fakePublisher) PublishStatsClosed(_ context.Context, e domain.StatsWindowClosed) *problem.Problem {
	f.stats = append(f.stats, e)
	return nil
}

func (f *fakePublisher) PublishTapeClosed(_ context.Context, e domain.TapeClosed) *problem.Problem {
	f.tapes = append(f.tapes, e)
	return nil
}

func (f *fakePublisher) PublishOpenInterest(_ context.Context, e domain.OpenInterestClosed) *problem.Problem {
	f.openInterest = append(f.openInterest, e)
	return nil
}

func (f *fakePublisher) PublishDeltaVolume(_ context.Context, e domain.DeltaVolumeClosed) *problem.Problem {
	f.deltaVolume = append(f.deltaVolume, e)
	return nil
}

func (f *fakePublisher) PublishCVD(_ context.Context, e domain.CVDClosed) *problem.Problem {
	f.cvd = append(f.cvd, e)
	return nil
}

func (f *fakePublisher) PublishBarStats(_ context.Context, e domain.BarStatsClosed) *problem.Problem {
	f.barStats = append(f.barStats, e)
	return nil
}

type fakeStore struct {
	saved   []domain.SnapshotProduced
	saveErr *problem.Problem
}

func (f *fakeStore) Save(_ context.Context, s domain.SnapshotProduced) *problem.Problem {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.saved = append(f.saved, s)
	return nil
}

// --- helpers ---

func newUC() (*app.UpdateOrderBookFromEvents, *fakePublisher, *fakeStore) {
	pub := &fakePublisher{}
	store := &fakeStore{}
	uc := app.NewUpdateOrderBookFromEvents(pub, store)
	return uc, pub, store
}

func applyDelta(t *testing.T, uc *app.UpdateOrderBookFromEvents, seq int64, bids, asks []domain.Level) app.UpdateResponse {
	t.Helper()
	r := uc.Execute(context.Background(), app.UpdateRequest{
		Venue:      "binance",
		Instrument: "BTCUSDT",
		Seq:        seq,
		Bids:       bids,
		Asks:       asks,
	})
	if r.IsFail() {
		t.Fatalf("Execute seq=%d: %s", seq, r.Problem())
	}
	return r.Value()
}

// --- tests ---

func TestUpdateOrderBook_success(t *testing.T) {
	uc, pub, store := newUC()
	resp := applyDelta(t, uc, 1,
		[]domain.Level{{Price: 50_000, Quantity: 1}},
		[]domain.Level{{Price: 50_100, Quantity: 1}},
	)
	if resp.Seq != 1 {
		t.Errorf("Seq = %d; want 1", resp.Seq)
	}
	if resp.Spread != 100 {
		t.Errorf("Spread = %f; want 100", resp.Spread)
	}
	if len(pub.snaps) != 1 {
		t.Errorf("published %d snapshots; want 1", len(pub.snaps))
	}
	if len(store.saved) != 1 {
		t.Errorf("stored %d snapshots; want 1", len(store.saved))
	}
}

func TestUpdateOrderBook_monotonic(t *testing.T) {
	uc, pub, store := newUC()
	applyDelta(t, uc, 1,
		[]domain.Level{{Price: 100, Quantity: 1}},
		[]domain.Level{{Price: 101, Quantity: 1}},
	)

	r := uc.Execute(context.Background(), app.UpdateRequest{
		Venue: "binance", Instrument: "BTCUSDT",
		Seq:  1,
		Bids: []domain.Level{{Price: 100, Quantity: 1}},
	})
	if r.IsOk() {
		t.Fatal("expected OUT_OF_ORDER")
	}
	if r.Problem().Code != problem.OutOfOrder {
		t.Errorf("code = %s; want OUT_OF_ORDER", r.Problem().Code)
	}
	if len(pub.inconsistent) != 0 {
		t.Errorf("unexpected inconsistent events on OUT_OF_ORDER: %d", len(pub.inconsistent))
	}
	if len(pub.snaps) != 1 || len(store.saved) != 1 {
		t.Errorf("out-of-order must not publish/save new snapshots; got snaps=%d saved=%d", len(pub.snaps), len(store.saved))
	}
}

func TestUpdateOrderBook_crossedBook(t *testing.T) {
	uc, pub, store := newUC()
	r := uc.Execute(context.Background(), app.UpdateRequest{
		Venue: "binance", Instrument: "BTCUSDT",
		Seq:  1,
		Bids: []domain.Level{{Price: 200, Quantity: 1}},
		Asks: []domain.Level{{Price: 100, Quantity: 1}},
	})
	if r.IsOk() {
		t.Fatal("expected integrity violation")
	}
	if r.Problem().Code != problem.IntegrityViolation {
		t.Errorf("code = %s; want INTEGRITY_VIOLATION", r.Problem().Code)
	}
	if len(pub.inconsistent) != 1 {
		t.Errorf("expected 1 inconsistent event, got %d", len(pub.inconsistent))
	}
	if len(pub.snaps) != 0 {
		t.Errorf("expected 0 snapshots on crossed book, got %d", len(pub.snaps))
	}
	if len(store.saved) != 0 {
		t.Errorf("expected 0 persisted snapshots on crossed book, got %d", len(store.saved))
	}
	if pub.inconsistent[0].Seq != 1 {
		t.Errorf("inconsistent event seq = %d; want 1", pub.inconsistent[0].Seq)
	}
}

func TestUpdateOrderBook_missingVenue(t *testing.T) {
	uc, _, _ := newUC()
	r := uc.Execute(context.Background(), app.UpdateRequest{
		Venue: "", Instrument: "BTCUSDT", Seq: 1,
	})
	if r.IsOk() {
		t.Fatal("expected validation failure")
	}
	if r.Problem().Code != problem.ValidationFailed {
		t.Errorf("code = %s; want VALIDATION_FAILED", r.Problem().Code)
	}
}

func TestUpdateOrderBook_snapshotContainsLevels(t *testing.T) {
	uc, pub, _ := newUC()
	applyDelta(t, uc, 1,
		[]domain.Level{{Price: 100, Quantity: 5}, {Price: 99, Quantity: 3}},
		[]domain.Level{{Price: 101, Quantity: 2}},
	)
	snap := pub.snaps[0]
	if len(snap.Bids) != 2 {
		t.Errorf("snapshot bids = %d; want 2", len(snap.Bids))
	}
	if len(snap.Asks) != 1 {
		t.Errorf("snapshot asks = %d; want 1", len(snap.Asks))
	}
}

func TestUpdateOrderBook_V2_Checksum_Populated(t *testing.T) {
	uc, _, _ := newUC()
	resp := applyDelta(t, uc, 1,
		[]domain.Level{{Price: 100, Quantity: 1}},
		[]domain.Level{{Price: 101, Quantity: 1}},
	)
	if resp.Checksum == 0 {
		t.Fatal("checksum must be populated")
	}
	if resp.BestBidPrice != 100 {
		t.Fatalf("best bid=%f want=100", resp.BestBidPrice)
	}
	if resp.BestAskPrice != 101 {
		t.Fatalf("best ask=%f want=101", resp.BestAskPrice)
	}
	if resp.SpreadBPS <= 0 {
		t.Fatalf("spread_bps=%f want>0", resp.SpreadBPS)
	}
}

func TestUpdateOrderBook_V2_DepthCap_Respected(t *testing.T) {
	pub := &fakePublisher{}
	store := &fakeStore{}
	uc := app.NewUpdateOrderBookFromEventsWithConfig(pub, store, app.UpdateConfig{
		MaxBooks:        16,
		BookTTL:         time.Hour,
		MaxLevels:       1000,
		PublishDepthCap: 25,
		Clock:           clock.NewFakeClock(time.UnixMilli(1_700_000_000_000)),
	})

	bids := make([]domain.Level, 0, 100)
	asks := make([]domain.Level, 0, 100)
	for i := 0; i < 100; i++ {
		bids = append(bids, domain.Level{Price: domain.Price(1000 - i), Quantity: 1})
		asks = append(asks, domain.Level{Price: domain.Price(1001 + i), Quantity: 1})
	}
	applyDelta(t, uc, 1, bids, asks)

	if len(pub.snaps) != 1 {
		t.Fatalf("published snapshots=%d want=1", len(pub.snaps))
	}
	if got, want := len(pub.snaps[0].Bids), 25; got != want {
		t.Fatalf("published bids=%d want=%d", got, want)
	}
	if got, want := len(pub.snaps[0].Asks), 25; got != want {
		t.Fatalf("published asks=%d want=%d", got, want)
	}
	if got, want := pub.snaps[0].BidCount, 100; got != want {
		t.Fatalf("published bid_count=%d want=%d", got, want)
	}
	if got, want := pub.snaps[0].AskCount, 100; got != want {
		t.Fatalf("published ask_count=%d want=%d", got, want)
	}
	if got, want := pub.snaps[0].DepthCap, 25; got != want {
		t.Fatalf("published depth_cap=%d want=%d", got, want)
	}
}

func TestBadLevelMetric_NaN(t *testing.T) {
	uc, _, _ := newUC()
	bucket := metrics.InstrumentBucket("BTCUSDT")
	before := testutil.ToFloat64(metrics.MROrderBookBadLevelTotal.WithLabelValues("binance", bucket, "nan"))

	res := uc.Execute(context.Background(), app.UpdateRequest{
		Venue:      "binance",
		Instrument: "BTCUSDT",
		Seq:        1,
		Bids:       []domain.Level{{Price: domain.Price(math.NaN()), Quantity: 1}},
	})
	if res.IsOk() {
		t.Fatal("expected validation failure")
	}

	after := testutil.ToFloat64(metrics.MROrderBookBadLevelTotal.WithLabelValues("binance", bucket, "nan"))
	if after < before+1 {
		t.Fatalf("bad_level nan metric not incremented: before=%f after=%f", before, after)
	}
}

func TestBadLevelMetric_NegativePrice(t *testing.T) {
	uc, _, _ := newUC()
	bucket := metrics.InstrumentBucket("BTCUSDT")
	before := testutil.ToFloat64(metrics.MROrderBookBadLevelTotal.WithLabelValues("binance", bucket, "neg_price"))

	res := uc.Execute(context.Background(), app.UpdateRequest{
		Venue:      "binance",
		Instrument: "BTCUSDT",
		Seq:        1,
		Bids:       []domain.Level{{Price: -1, Quantity: 1}},
	})
	if res.IsOk() {
		t.Fatal("expected validation failure")
	}

	after := testutil.ToFloat64(metrics.MROrderBookBadLevelTotal.WithLabelValues("binance", bucket, "neg_price"))
	if after < before+1 {
		t.Fatalf("bad_level neg_price metric not incremented: before=%f after=%f", before, after)
	}
}

func TestBadLevelMetric_InfQuantity(t *testing.T) {
	uc, _, _ := newUC()
	bucket := metrics.InstrumentBucket("BTCUSDT")
	before := testutil.ToFloat64(metrics.MROrderBookBadLevelTotal.WithLabelValues("binance", bucket, "inf"))

	res := uc.Execute(context.Background(), app.UpdateRequest{
		Venue:      "binance",
		Instrument: "BTCUSDT",
		Seq:        1,
		Asks:       []domain.Level{{Price: 100, Quantity: domain.Quantity(math.Inf(1))}},
	})
	if res.IsOk() {
		t.Fatal("expected validation failure")
	}

	after := testutil.ToFloat64(metrics.MROrderBookBadLevelTotal.WithLabelValues("binance", bucket, "inf"))
	if after < before+1 {
		t.Fatalf("bad_level inf metric not incremented: before=%f after=%f", before, after)
	}
}

func TestUpdateOrderBook_saveFailureDoesNotPublishSnapshot(t *testing.T) {
	pub := &fakePublisher{}
	store := &fakeStore{
		saveErr: problem.New(problem.Unavailable, "store unavailable"),
	}
	uc := app.NewUpdateOrderBookFromEvents(pub, store)
	r := uc.Execute(context.Background(), app.UpdateRequest{
		Venue:      "binance",
		Instrument: "BTCUSDT",
		Seq:        1,
		Bids:       []domain.Level{{Price: 100, Quantity: 1}},
		Asks:       []domain.Level{{Price: 101, Quantity: 1}},
	})
	if r.IsOk() {
		t.Fatal("expected failure when store save fails")
	}
	if r.Problem().Code != problem.Unavailable {
		t.Fatalf("code=%s want=%s", r.Problem().Code, problem.Unavailable)
	}
	if len(pub.snaps) != 0 {
		t.Fatalf("published snapshots=%d want=0", len(pub.snaps))
	}
	if len(store.saved) != 0 {
		t.Fatalf("persisted snapshots=%d want=0", len(store.saved))
	}
}

func TestUpdateOrderBook_snapshotPublishMinIntervalThrottlesPublishOnly(t *testing.T) {
	pub := &fakePublisher{}
	store := &fakeStore{}
	clk := clock.NewFakeClock(time.UnixMilli(1_700_000_000_000))
	uc := app.NewUpdateOrderBookFromEventsWithConfig(pub, store, app.UpdateConfig{
		MaxBooks:                   8,
		BookTTL:                    time.Hour,
		MaxLevels:                  128,
		Clock:                      clk,
		SnapshotPublishMinInterval: 200 * time.Millisecond,
	})

	applyDelta(t, uc, 1,
		[]domain.Level{{Price: 100, Quantity: 1}},
		[]domain.Level{{Price: 101, Quantity: 1}},
	)
	clk.Advance(50 * time.Millisecond)
	applyDelta(t, uc, 2,
		[]domain.Level{{Price: 100, Quantity: 2}},
		[]domain.Level{{Price: 101, Quantity: 2}},
	)
	clk.Advance(250 * time.Millisecond)
	applyDelta(t, uc, 3,
		[]domain.Level{{Price: 100, Quantity: 3}},
		[]domain.Level{{Price: 101, Quantity: 3}},
	)

	if got, want := len(store.saved), 3; got != want {
		t.Fatalf("persisted snapshots=%d want=%d", got, want)
	}
	if got, want := len(pub.snaps), 2; got != want {
		t.Fatalf("published snapshots=%d want=%d", got, want)
	}
}

func TestUpdateOrderBook_boundedBooksEvictsOldest(t *testing.T) {
	pub := &fakePublisher{}
	store := &fakeStore{}
	clk := clock.NewFakeClock(time.Now())
	uc := app.NewUpdateOrderBookFromEventsWithConfig(pub, store, app.UpdateConfig{
		MaxBooks:  1,
		BookTTL:   time.Hour,
		MaxLevels: 10,
		Clock:     clk,
	})

	req := app.UpdateRequest{
		Venue:      "binance",
		Instrument: "BTCUSDT",
		Seq:        1,
		Bids:       []domain.Level{{Price: 100, Quantity: 1}},
		Asks:       []domain.Level{{Price: 101, Quantity: 1}},
	}
	if r := uc.Execute(context.Background(), req); r.IsFail() {
		t.Fatalf("first execute failed: %v", r.Problem())
	}
	req.Instrument = "ETHUSDT"
	clk.Advance(time.Millisecond)
	if r := uc.Execute(context.Background(), req); r.IsFail() {
		t.Fatalf("second execute failed: %v", r.Problem())
	}
	if got := uc.ActiveBooks(); got != 1 {
		t.Fatalf("active books=%d want=1", got)
	}
}

func TestUpdateOrderBook_boundedBooksEvictionDeterministicVictim(t *testing.T) {
	run := func(t *testing.T) (ethWasEvicted bool, btcWasRetained bool) {
		t.Helper()

		pub := &fakePublisher{}
		store := &fakeStore{}
		clk := clock.NewFakeClock(time.Unix(0, 0))
		uc := app.NewUpdateOrderBookFromEventsWithConfig(pub, store, app.UpdateConfig{
			MaxBooks:  2,
			BookTTL:   time.Hour,
			MaxLevels: 10,
			Clock:     clk,
		})

		makeReq := func(symbol string, seq int64) app.UpdateRequest {
			return app.UpdateRequest{
				Venue:      "binance",
				Instrument: symbol,
				Seq:        seq,
				Bids:       []domain.Level{{Price: 100, Quantity: 1}},
				Asks:       []domain.Level{{Price: 101, Quantity: 1}},
			}
		}

		if r := uc.Execute(context.Background(), makeReq("BTCUSDT", 1)); r.IsFail() {
			t.Fatalf("btc first execute failed: %v", r.Problem())
		}
		clk.Advance(time.Millisecond)
		if r := uc.Execute(context.Background(), makeReq("ETHUSDT", 1)); r.IsFail() {
			t.Fatalf("eth first execute failed: %v", r.Problem())
		}
		clk.Advance(time.Millisecond)
		if r := uc.Execute(context.Background(), makeReq("BTCUSDT", 2)); r.IsFail() {
			t.Fatalf("btc touch execute failed: %v", r.Problem())
		}
		clk.Advance(time.Millisecond)
		if r := uc.Execute(context.Background(), makeReq("SOLUSDT", 1)); r.IsFail() {
			t.Fatalf("sol execute failed: %v", r.Problem())
		}

		// BTC should still be resident at this point, so lower seq remains out_of_order.
		clk.Advance(time.Millisecond)
		btcResult := uc.Execute(context.Background(), makeReq("BTCUSDT", 1))
		btcWasRetained = btcResult.IsFail() && btcResult.Problem().Code == problem.OutOfOrder

		// ETH should be the deterministic LRU victim and therefore accepted again.
		clk.Advance(time.Millisecond)
		ethResult := uc.Execute(context.Background(), makeReq("ETHUSDT", 1))
		ethWasEvicted = ethResult.IsOk()
		return ethWasEvicted, btcWasRetained
	}

	ethFirst, btcFirst := run(t)
	ethSecond, btcSecond := run(t)

	if !ethFirst || !btcFirst {
		t.Fatalf("unexpected eviction result first run: ethWasEvicted=%v btcWasRetained=%v", ethFirst, btcFirst)
	}
	if ethFirst != ethSecond || btcFirst != btcSecond {
		t.Fatalf(
			"non-deterministic eviction result first=(eth:%v btc:%v) second=(eth:%v btc:%v)",
			ethFirst,
			btcFirst,
			ethSecond,
			btcSecond,
		)
	}
}

func TestUpdateOrderBook_ThrottledSweepDoesNotRunEveryRequest(t *testing.T) {
	pub := &fakePublisher{}
	store := &fakeStore{}
	clk := clock.NewFakeClock(time.Unix(0, 0))
	uc := app.NewUpdateOrderBookFromEventsWithConfig(pub, store, app.UpdateConfig{
		MaxBooks:  16,
		BookTTL:   10 * time.Millisecond,
		MaxLevels: 10,
		Clock:     clk,
	})

	makeReq := func(symbol string, seq int64) app.UpdateRequest {
		return app.UpdateRequest{
			Venue:      "binance",
			Instrument: symbol,
			Seq:        seq,
			Bids:       []domain.Level{{Price: 100, Quantity: 1}},
			Asks:       []domain.Level{{Price: 101, Quantity: 1}},
		}
	}

	if r := uc.Execute(context.Background(), makeReq("BTCUSDT", 1)); r.IsFail() {
		t.Fatalf("first execute failed: %v", r.Problem())
	}
	if r := uc.Execute(context.Background(), makeReq("ETHUSDT", 1)); r.IsFail() {
		t.Fatalf("second execute failed: %v", r.Problem())
	}

	clk.Advance(20 * time.Millisecond)
	if r := uc.Execute(context.Background(), makeReq("SOLUSDT", 1)); r.IsFail() {
		t.Fatalf("third execute failed: %v", r.Problem())
	}

	if got := uc.ActiveBooks(); got < 2 {
		t.Fatalf("expected no full sweep on each request, active books=%d", got)
	}
}
