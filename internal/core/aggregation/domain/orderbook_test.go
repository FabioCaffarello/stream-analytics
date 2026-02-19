package domain_test

import (
	"testing"

	"github.com/market-raccoon/internal/core/aggregation/domain"
	"github.com/market-raccoon/internal/shared/problem"
)

func newBook(t *testing.T) *domain.OrderBook {
	t.Helper()
	b, p := domain.NewOrderBook("binance", "BTCUSDT")
	if p != nil {
		t.Fatalf("NewOrderBook: %s", p)
	}
	return b
}

func TestOrderBook_emptyBook(t *testing.T) {
	b := newBook(t)
	if !b.IsHealthy() {
		t.Errorf("new book state = %s; want HEALTHY", b.State())
	}
	if b.BestBid() != nil {
		t.Error("empty book should have no best bid")
	}
	if b.BestAsk() != nil {
		t.Error("empty book should have no best ask")
	}
	if b.Spread() != -1 {
		t.Errorf("spread = %f; want -1 for incomplete book", b.Spread())
	}
}

func TestOrderBook_applyDelta_basic(t *testing.T) {
	b := newBook(t)
	p := b.ApplyDelta(1,
		[]domain.Level{{Price: 49_900, Quantity: 2}, {Price: 50_000, Quantity: 1}},
		[]domain.Level{{Price: 50_100, Quantity: 1}, {Price: 50_200, Quantity: 3}},
	)
	if p != nil {
		t.Fatalf("ApplyDelta: %s", p)
	}

	if got := b.BestBid(); got == nil || float64(got.Price) != 50_000 {
		t.Errorf("best bid = %v; want 50000", got)
	}
	if got := b.BestAsk(); got == nil || float64(got.Price) != 50_100 {
		t.Errorf("best ask = %v; want 50100", got)
	}
	if s := b.Spread(); s != 100 {
		t.Errorf("spread = %f; want 100", s)
	}
}

func TestOrderBook_applyDelta_seqMonotonic(t *testing.T) {
	b := newBook(t)
	_ = b.ApplyDelta(5,
		[]domain.Level{{Price: 100, Quantity: 1}},
		[]domain.Level{{Price: 101, Quantity: 1}},
	)
	if b.LastSeq() != 5 {
		t.Fatalf("lastSeq = %d; want 5", b.LastSeq())
	}

	// Same seq → OUT_OF_ORDER
	p := b.ApplyDelta(5, nil, nil)
	if p == nil {
		t.Fatal("expected OUT_OF_ORDER")
	}
	if p.Code != problem.OutOfOrder {
		t.Errorf("code = %s; want OUT_OF_ORDER", p.Code)
	}

	// Lower seq → OUT_OF_ORDER
	p = b.ApplyDelta(3, nil, nil)
	if p == nil || p.Code != problem.OutOfOrder {
		t.Errorf("expected OUT_OF_ORDER, got %v", p)
	}
	if b.LastSeq() != 5 {
		t.Errorf("lastSeq changed after out-of-order, got %d", b.LastSeq())
	}
	if !b.IsHealthy() {
		t.Errorf("out-of-order should keep current health state, got %s", b.State())
	}
}

func TestOrderBook_applySnapshot_allowsSeqReanchor(t *testing.T) {
	b := newBook(t)
	p := b.ApplySnapshot(10,
		[]domain.Level{{Price: 100, Quantity: 1}},
		[]domain.Level{{Price: 101, Quantity: 1}},
	)
	if p != nil {
		t.Fatalf("initial snapshot failed: %v", p)
	}

	p = b.ApplySnapshot(7,
		[]domain.Level{{Price: 99, Quantity: 1}},
		[]domain.Level{{Price: 100, Quantity: 1}},
	)
	if p != nil {
		t.Fatalf("expected snapshot seq re-anchor to be accepted, got %v", p)
	}
	if b.LastSeq() != 7 {
		t.Fatalf("lastSeq=%d want=7", b.LastSeq())
	}
}

func TestOrderBook_applyDelta_removeLevel(t *testing.T) {
	b := newBook(t)
	_ = b.ApplyDelta(1,
		[]domain.Level{{Price: 100, Quantity: 5}, {Price: 99, Quantity: 3}},
		[]domain.Level{{Price: 101, Quantity: 2}},
	)
	// Remove the 100 bid.
	p := b.ApplyDelta(2,
		[]domain.Level{{Price: 100, Quantity: 0}},
		nil,
	)
	if p != nil {
		t.Fatalf("ApplyDelta remove: %s", p)
	}
	if got := b.BestBid(); got == nil || float64(got.Price) != 99 {
		t.Errorf("after removal best bid = %v; want 99", got)
	}
}

func TestOrderBook_crossedBook(t *testing.T) {
	b := newBook(t)
	// bid > ask → crossed → INTEGRITY_VIOLATION
	p := b.ApplyDelta(1,
		[]domain.Level{{Price: 200, Quantity: 1}},
		[]domain.Level{{Price: 100, Quantity: 1}}, // ask below bid
	)
	if p == nil {
		t.Fatal("expected integrity violation for crossed book")
	}
	if p.Code != problem.IntegrityViolation {
		t.Errorf("code = %s; want INTEGRITY_VIOLATION", p.Code)
	}
	if !b.NeedsResync() || b.IsHealthy() {
		t.Errorf("expected state NEEDS_RESYNC, got state=%s", b.State())
	}
	// lastSeq must advance to prevent infinite retry loop.
	if b.LastSeq() != 1 {
		t.Errorf("lastSeq = %d; want 1 (must advance on crossed book)", b.LastSeq())
	}
	// Book must be cleared to allow self-healing on next delta.
	if len(b.Bids()) != 0 || len(b.Asks()) != 0 {
		t.Errorf("book should be cleared after crossed detection, got bids=%d asks=%d", len(b.Bids()), len(b.Asks()))
	}
	evts := b.PullDomainEvents()
	if len(evts) != 1 {
		t.Fatalf("expected 1 domain event, got %d", len(evts))
	}
	evt, ok := evts[0].(domain.OrderBookInconsistentDetected)
	if !ok {
		t.Fatalf("unexpected event type %T", evts[0])
	}
	if evt.Seq != 1 {
		t.Errorf("event seq = %d; want 1", evt.Seq)
	}
	if len(b.PullDomainEvents()) != 0 {
		t.Error("expected domain events to be cleared after pull")
	}
}

func TestOrderBook_crossedBook_selfHeals(t *testing.T) {
	b := newBook(t)

	// Delta 1: crossed book.
	p := b.ApplyDelta(1,
		[]domain.Level{{Price: 200, Quantity: 1}},
		[]domain.Level{{Price: 100, Quantity: 1}},
	)
	if p == nil || p.Code != problem.IntegrityViolation {
		t.Fatalf("expected INTEGRITY_VIOLATION, got %v", p)
	}
	_ = b.PullDomainEvents()

	// Delta 2: valid levels on the now-empty book → self-heals.
	p = b.ApplyDelta(2,
		[]domain.Level{{Price: 50_000, Quantity: 1}},
		[]domain.Level{{Price: 50_100, Quantity: 1}},
	)
	if p != nil {
		t.Fatalf("expected self-heal, got %v", p)
	}
	if !b.IsHealthy() {
		t.Errorf("state = %s; want HEALTHY after self-heal", b.State())
	}
	if b.LastSeq() != 2 {
		t.Errorf("lastSeq = %d; want 2", b.LastSeq())
	}
	if s := b.Spread(); s != 100 {
		t.Errorf("spread = %f; want 100", s)
	}
}

func TestOrderBook_negativePriceRejected(t *testing.T) {
	b := newBook(t)
	p := b.ApplyDelta(1,
		[]domain.Level{{Price: -1, Quantity: 1}},
		nil,
	)
	if p == nil {
		t.Fatal("expected validation error for negative price")
	}
	if p.Code != problem.ValidationFailed {
		t.Errorf("code = %s; want VALIDATION_FAILED", p.Code)
	}
}

func TestOrderBook_negativeQuantityRejected(t *testing.T) {
	b := newBook(t)
	p := b.ApplyDelta(1,
		nil,
		[]domain.Level{{Price: 101, Quantity: -1}},
	)
	if p == nil {
		t.Fatal("expected validation error for negative quantity")
	}
}

func TestOrderBook_bidsDescending(t *testing.T) {
	b := newBook(t)
	_ = b.ApplyDelta(1,
		[]domain.Level{
			{Price: 100, Quantity: 1},
			{Price: 103, Quantity: 1},
			{Price: 101, Quantity: 1},
		},
		[]domain.Level{{Price: 110, Quantity: 1}},
	)
	bids := b.Bids()
	for i := 1; i < len(bids); i++ {
		if bids[i].Price > bids[i-1].Price {
			t.Errorf("bids not sorted descending at index %d", i)
		}
	}
}

func TestOrderBook_asksAscending(t *testing.T) {
	b := newBook(t)
	_ = b.ApplyDelta(1,
		[]domain.Level{{Price: 99, Quantity: 1}},
		[]domain.Level{
			{Price: 105, Quantity: 1},
			{Price: 102, Quantity: 1},
			{Price: 110, Quantity: 1},
		},
	)
	asks := b.Asks()
	for i := 1; i < len(asks); i++ {
		if asks[i].Price < asks[i-1].Price {
			t.Errorf("asks not sorted ascending at index %d", i)
		}
	}
}

func TestNewOrderBook_emptyVenue(t *testing.T) {
	_, p := domain.NewOrderBook("", "BTCUSDT")
	if p == nil {
		t.Fatal("expected problem for empty venue")
	}
}

func TestOrderBook_lastSeq(t *testing.T) {
	b := newBook(t)
	if b.LastSeq() != 0 {
		t.Errorf("initial lastSeq = %d; want 0", b.LastSeq())
	}
	_ = b.ApplyDelta(7,
		[]domain.Level{{Price: 100, Quantity: 1}},
		[]domain.Level{{Price: 101, Quantity: 1}},
	)
	if b.LastSeq() != 7 {
		t.Errorf("lastSeq = %d; want 7", b.LastSeq())
	}
}

func TestOrderBook_maxLevelsBoundedPerSide(t *testing.T) {
	b, p := domain.NewOrderBookWithMaxLevels("binance", "BTCUSDT", 2)
	if p != nil {
		t.Fatalf("NewOrderBookWithMaxLevels: %v", p)
	}

	err := b.ApplyDelta(1,
		[]domain.Level{
			{Price: 103, Quantity: 1},
			{Price: 102, Quantity: 1},
			{Price: 101, Quantity: 1},
		},
		[]domain.Level{
			{Price: 104, Quantity: 1},
			{Price: 105, Quantity: 1},
			{Price: 106, Quantity: 1},
		},
	)
	if err != nil {
		t.Fatalf("ApplyDelta: %v", err)
	}

	if got := len(b.Bids()); got != 2 {
		t.Fatalf("bids len=%d want=2", got)
	}
	if got := len(b.Asks()); got != 2 {
		t.Fatalf("asks len=%d want=2", got)
	}
}

func TestOrderBook_applySnapshot_replacesLevels(t *testing.T) {
	b := newBook(t)

	// Delta 1: populate the book.
	p := b.ApplyDelta(1,
		[]domain.Level{{Price: 42000, Quantity: 1}, {Price: 41990, Quantity: 2}},
		[]domain.Level{{Price: 42010, Quantity: 1}, {Price: 42020, Quantity: 2}},
	)
	if p != nil {
		t.Fatalf("ApplyDelta: %v", p)
	}
	if len(b.Bids()) != 2 || len(b.Asks()) != 2 {
		t.Fatalf("bids=%d asks=%d; want 2/2", len(b.Bids()), len(b.Asks()))
	}

	// Snapshot: completely different price range — old levels must NOT remain.
	p = b.ApplySnapshot(2,
		[]domain.Level{{Price: 50000, Quantity: 1}},
		[]domain.Level{{Price: 50100, Quantity: 1}},
	)
	if p != nil {
		t.Fatalf("ApplySnapshot: %v", p)
	}
	if len(b.Bids()) != 1 || len(b.Asks()) != 1 {
		t.Fatalf("after snapshot bids=%d asks=%d; want 1/1", len(b.Bids()), len(b.Asks()))
	}
	if got := b.BestBid(); got == nil || float64(got.Price) != 50000 {
		t.Errorf("best bid = %v; want 50000", got)
	}
	if got := b.BestAsk(); got == nil || float64(got.Price) != 50100 {
		t.Errorf("best ask = %v; want 50100", got)
	}
	if b.Spread() != 100 {
		t.Errorf("spread = %f; want 100", b.Spread())
	}
	if !b.IsHealthy() {
		t.Errorf("state = %s; want HEALTHY", b.State())
	}
}

func TestOrderBook_applySnapshot_preventsAccumulationCross(t *testing.T) {
	b := newBook(t)

	// Delta 1: normal book at 42000/42010.
	_ = b.ApplyDelta(1,
		[]domain.Level{{Price: 42000, Quantity: 1}},
		[]domain.Level{{Price: 42010, Quantity: 1}},
	)

	// Snapshot 2: price shifted down to 41000/41010.
	// Without snapshot clearing, old bid@42000 would cross new ask@41010.
	p := b.ApplySnapshot(2,
		[]domain.Level{{Price: 41000, Quantity: 1}},
		[]domain.Level{{Price: 41010, Quantity: 1}},
	)
	if p != nil {
		t.Fatalf("expected no cross with snapshot, got %v", p)
	}
	if !b.IsHealthy() {
		t.Errorf("state = %s; want HEALTHY", b.State())
	}
}
