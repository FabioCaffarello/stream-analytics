package domain_test

import (
	"hash/crc32"
	"testing"

	"github.com/market-raccoon/internal/core/aggregation/domain"
)

func TestOrderBook_Checksum_Deterministic(t *testing.T) {
	book := newBook(t)
	if p := book.ApplyDelta(1,
		[]domain.Level{{Price: 65000, Quantity: 1.2}, {Price: 64990, Quantity: 2.3}},
		[]domain.Level{{Price: 65010, Quantity: 0.9}, {Price: 65020, Quantity: 1.1}},
	); p != nil {
		t.Fatalf("ApplyDelta: %v", p)
	}

	first := book.Checksum()
	second := book.Checksum()
	if first != second {
		t.Fatalf("checksum is not deterministic: first=%d second=%d", first, second)
	}
}

func TestOrderBook_Checksum_DifferentOrder_SameResult(t *testing.T) {
	left := newBook(t)
	right := newBook(t)

	if p := left.ApplyDelta(1,
		[]domain.Level{{Price: 65000, Quantity: 1}, {Price: 64995, Quantity: 2}},
		[]domain.Level{{Price: 65010, Quantity: 1}, {Price: 65015, Quantity: 2}},
	); p != nil {
		t.Fatalf("left ApplyDelta[1]: %v", p)
	}
	if p := left.ApplyDelta(2,
		[]domain.Level{{Price: 64990, Quantity: 3}},
		[]domain.Level{{Price: 65020, Quantity: 4}},
	); p != nil {
		t.Fatalf("left ApplyDelta[2]: %v", p)
	}

	if p := right.ApplyDelta(1,
		[]domain.Level{{Price: 64990, Quantity: 3}, {Price: 65000, Quantity: 1}, {Price: 64995, Quantity: 2}},
		[]domain.Level{{Price: 65020, Quantity: 4}, {Price: 65010, Quantity: 1}, {Price: 65015, Quantity: 2}},
	); p != nil {
		t.Fatalf("right ApplyDelta[1]: %v", p)
	}

	if got, want := left.Checksum(), right.Checksum(); got != want {
		t.Fatalf("checksum mismatch got=%d want=%d", got, want)
	}
}

func TestOrderBook_Checksum_EmptyBook(t *testing.T) {
	book := newBook(t)
	want := crc32.Checksum([]byte{0xFF}, crc32.MakeTable(crc32.Castagnoli))
	if got := book.Checksum(); got != want {
		t.Fatalf("empty checksum=%d want=%d", got, want)
	}
}

func TestOrderBook_TopN_Bounded(t *testing.T) {
	book, p := domain.NewOrderBookWithMaxLevels("binance", "BTCUSDT", 1000)
	if p != nil {
		t.Fatalf("NewOrderBookWithMaxLevels: %v", p)
	}

	bids := make([]domain.Level, 0, 100)
	asks := make([]domain.Level, 0, 100)
	for i := 0; i < 100; i++ {
		bids = append(bids, domain.Level{Price: domain.Price(65000 - i), Quantity: 1})
		asks = append(asks, domain.Level{Price: domain.Price(65001 + i), Quantity: 1})
	}
	if p := book.ApplyDelta(1, bids, asks); p != nil {
		t.Fatalf("ApplyDelta: %v", p)
	}

	topBids, topAsks := book.TopN(25)
	if got, want := len(topBids), 25; got != want {
		t.Fatalf("top bids len=%d want=%d", got, want)
	}
	if got, want := len(topAsks), 25; got != want {
		t.Fatalf("top asks len=%d want=%d", got, want)
	}
}

func TestOrderBook_TopN_LessThanN(t *testing.T) {
	book := newBook(t)
	if p := book.ApplyDelta(1,
		[]domain.Level{{Price: 10, Quantity: 1}, {Price: 9, Quantity: 1}},
		[]domain.Level{{Price: 11, Quantity: 1}},
	); p != nil {
		t.Fatalf("ApplyDelta: %v", p)
	}

	topBids, topAsks := book.TopN(100)
	if got, want := len(topBids), 2; got != want {
		t.Fatalf("top bids len=%d want=%d", got, want)
	}
	if got, want := len(topAsks), 1; got != want {
		t.Fatalf("top asks len=%d want=%d", got, want)
	}
}
