package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/market-raccoon/internal/core/aggregation/app"
	"github.com/market-raccoon/internal/core/aggregation/domain"
	"github.com/market-raccoon/internal/shared/clock"
	"github.com/market-raccoon/internal/shared/problem"
)

// --- fakes ---

type fakePublisher struct {
	snaps        []domain.SnapshotProduced
	inconsistent []domain.OrderBookInconsistentDetected
}

func (f *fakePublisher) PublishSnapshot(_ context.Context, s domain.SnapshotProduced) *problem.Problem {
	f.snaps = append(f.snaps, s)
	return nil
}

func (f *fakePublisher) PublishInconsistent(_ context.Context, e domain.OrderBookInconsistentDetected) *problem.Problem {
	f.inconsistent = append(f.inconsistent, e)
	return nil
}

type fakeStore struct{ saved []domain.SnapshotProduced }

func (f *fakeStore) Save(_ context.Context, s domain.SnapshotProduced) *problem.Problem {
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
