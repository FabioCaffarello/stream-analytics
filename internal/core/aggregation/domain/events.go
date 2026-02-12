package domain

// DomainEvent is a marker interface for aggregate-emitted domain events.
//
//nolint:revive // DomainEvent follows DDD ubiquitous language for aggregate events.
type DomainEvent interface {
	EventName() string
}

// OrderBookUpdated is emitted after a successful ApplyDelta.
type OrderBookUpdated struct {
	BookID BookID
	Seq    int64
	BidTop *Level // nil if no bids
	AskTop *Level // nil if no asks
	Spread float64
	BidLen int
	AskLen int
}

// OrderBookInconsistentDetected is emitted when ApplyDelta detects a crossed book.
// The consumer should trigger a snapshot refresh.
type OrderBookInconsistentDetected struct {
	BookID BookID
	Seq    int64
	Reason string
}

// SnapshotProduced is emitted when a full snapshot of the book is available.
type SnapshotProduced struct {
	BookID BookID
	Seq    int64
	Bids   []Level
	Asks   []Level
}

// NewOrderBookUpdated builds an OrderBookUpdated event from a book.
func NewOrderBookUpdated(book *OrderBook, seq int64) OrderBookUpdated {
	return OrderBookUpdated{
		BookID: book.ID(),
		Seq:    seq,
		BidTop: book.BestBid(),
		AskTop: book.BestAsk(),
		Spread: book.Spread(),
		BidLen: len(book.Bids()),
		AskLen: len(book.Asks()),
	}
}

// NewSnapshotProduced builds a SnapshotProduced event from the current book state.
func NewSnapshotProduced(book *OrderBook) SnapshotProduced {
	return SnapshotProduced{
		BookID: book.ID(),
		Seq:    book.LastSeq(),
		Bids:   book.Bids(),
		Asks:   book.Asks(),
	}
}

// EventName returns the stable event name.
func (OrderBookUpdated) EventName() string { return "OrderBookUpdated" }

// EventName returns the stable event name.
func (OrderBookInconsistentDetected) EventName() string { return "OrderBookInconsistentDetected" }

// EventName returns the stable event name.
func (SnapshotProduced) EventName() string { return "SnapshotProduced" }
