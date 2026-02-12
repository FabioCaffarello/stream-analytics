// Package domain contains the aggregation bounded context domain model.
package domain

import (
	"sort"

	"github.com/market-raccoon/internal/shared/problem"
	"github.com/market-raccoon/internal/shared/validation"
)

// BookID uniquely identifies an order book by venue and instrument.
type BookID struct {
	Venue      string
	Instrument string
}

// Price is a strictly positive market price.
type Price float64

// Quantity is a non-negative size/amount.
type Quantity float64

// Level represents a single price level in the order book.
type Level struct {
	Price    Price
	Quantity Quantity
}

// HealthState represents consistency state of the aggregate.
type HealthState string

const (
	// Healthy means no inconsistency was detected in the latest applied state.
	Healthy HealthState = "HEALTHY"
	// NeedsResync means a crossed book/integrity issue was detected and snapshot reload is needed.
	NeedsResync HealthState = "NEEDS_RESYNC"
)

// OrderBook is the aggregate for a live limit order book.
//
// Invariants:
//   - All bid prices < all ask prices (no negative spread).
//   - Bid levels are sorted descending by price.
//   - Ask levels are sorted ascending by price.
//   - All prices are strictly positive.
//   - All quantities are non-negative (zero means removed).
//   - Sequence is monotonic.
type OrderBook struct {
	id      BookID
	lastSeq int64
	bids    []Level // sorted desc by price
	asks    []Level // sorted asc by price
	maxLvls int
	state   HealthState
	events  []DomainEvent
}

// NewOrderBook creates an empty OrderBook for the given identity.
func NewOrderBook(venue, instrument string) (*OrderBook, *problem.Problem) {
	return NewOrderBookWithMaxLevels(venue, instrument, 0)
}

// NewOrderBookWithMaxLevels creates an order book with optional level bound per side.
// maxLevels <= 0 means unbounded.
func NewOrderBookWithMaxLevels(venue, instrument string, maxLevels int) (*OrderBook, *problem.Problem) {
	if p := validation.Collect(
		validation.NonEmptyString("venue", venue),
		validation.NonEmptyString("instrument", instrument),
	); p != nil {
		return nil, p
	}
	if maxLevels < 0 {
		return nil, problem.New(problem.ValidationFailed, "max_levels must be >= 0")
	}
	return &OrderBook{
		id:      BookID{Venue: venue, Instrument: instrument},
		maxLvls: maxLevels,
		state:   Healthy,
	}, nil
}

// ID returns the book identity.
func (b *OrderBook) ID() BookID { return b.id }

// BestBid returns the highest bid level, or nil if no bids.
func (b *OrderBook) BestBid() *Level {
	if len(b.bids) == 0 {
		return nil
	}
	l := b.bids[0]
	return &l
}

// BestAsk returns the lowest ask level, or nil if no asks.
func (b *OrderBook) BestAsk() *Level {
	if len(b.asks) == 0 {
		return nil
	}
	l := b.asks[0]
	return &l
}

// Spread returns the current bid-ask spread. Returns -1 if incomplete book.
func (b *OrderBook) Spread() float64 {
	bid := b.BestBid()
	ask := b.BestAsk()
	if bid == nil || ask == nil {
		return -1
	}
	return float64(ask.Price - bid.Price)
}

// Bids returns a copy of all bid levels (desc by price).
func (b *OrderBook) Bids() []Level {
	out := make([]Level, len(b.bids))
	copy(out, b.bids)
	return out
}

// Asks returns a copy of all ask levels (asc by price).
func (b *OrderBook) Asks() []Level {
	out := make([]Level, len(b.asks))
	copy(out, b.asks)
	return out
}

// ApplyDelta applies an incremental update to the order book.
//
// seq must be strictly greater than the last applied seq (monotonicity).
// Each level with quantity=0 is removed; otherwise upserted.
// After applying, invariants are validated.
func (b *OrderBook) ApplyDelta(seq int64, bids, asks []Level) *problem.Problem {
	if p := validation.PositiveInt("seq", seq); p != nil {
		return p
	}
	if seq <= b.lastSeq {
		return problem.WithDetail(
			problem.WithDetail(
				problem.Newf(problem.OutOfOrder,
					"seq %d is not greater than last seq %d", seq, b.lastSeq),
				"seq", seq,
			),
			"last_seq", b.lastSeq,
		)
	}

	// Validate input levels before mutating.
	for i, l := range bids {
		if p := validateLevel("bid", i, l); p != nil {
			return p
		}
	}
	for i, l := range asks {
		if p := validateLevel("ask", i, l); p != nil {
			return p
		}
	}

	// Apply updates.
	b.bids = applyLevels(b.bids, bids)
	b.asks = applyLevels(b.asks, asks)

	// Re-sort.
	sort.Slice(b.bids, func(i, j int) bool { return b.bids[i].Price > b.bids[j].Price })
	sort.Slice(b.asks, func(i, j int) bool { return b.asks[i].Price < b.asks[j].Price })
	b.trimLevels()

	// Invariant: no crossed book.
	if p := b.checkSpread(); p != nil {
		b.state = NeedsResync
		b.events = append(b.events, OrderBookInconsistentDetected{
			BookID: b.ID(),
			Seq:    seq,
			Reason: p.Message,
		})
		return p
	}

	b.state = Healthy
	b.lastSeq = seq
	return nil
}

func (b *OrderBook) trimLevels() {
	if b.maxLvls <= 0 {
		return
	}
	if len(b.bids) > b.maxLvls {
		b.bids = b.bids[:b.maxLvls]
	}
	if len(b.asks) > b.maxLvls {
		b.asks = b.asks[:b.maxLvls]
	}
}

// LastSeq returns the last successfully applied sequence number.
func (b *OrderBook) LastSeq() int64 { return b.lastSeq }

// State returns the current aggregate consistency state.
func (b *OrderBook) State() HealthState { return b.state }

// IsHealthy reports if the aggregate is consistent.
func (b *OrderBook) IsHealthy() bool { return b.state == Healthy }

// NeedsResync reports if the aggregate requires snapshot resync.
func (b *OrderBook) NeedsResync() bool { return b.state == NeedsResync }

// PullDomainEvents returns and clears pending domain events.
func (b *OrderBook) PullDomainEvents() []DomainEvent {
	out := make([]DomainEvent, len(b.events))
	copy(out, b.events)
	b.events = b.events[:0]
	return out
}

// checkSpread validates that best bid < best ask (no negative spread).
func (b *OrderBook) checkSpread() *problem.Problem {
	bid := b.BestBid()
	ask := b.BestAsk()
	if bid == nil || ask == nil {
		return nil // incomplete book is allowed
	}
	if bid.Price >= ask.Price {
		return problem.WithDetail(
			problem.WithDetail(
				problem.Newf(problem.IntegrityViolation,
					"crossed book: best bid %.8f >= best ask %.8f",
					float64(bid.Price), float64(ask.Price)),
				"best_bid", float64(bid.Price),
			),
			"best_ask", float64(ask.Price),
		)
	}
	return nil
}

// applyLevels upserts/removes levels from the current side.
func applyLevels(current []Level, updates []Level) []Level {
	// Build a map for O(1) lookup.
	index := make(map[Price]int, len(current))
	for i, l := range current {
		index[l.Price] = i
	}

	for _, u := range updates {
		if u.Quantity == 0 {
			// Remove level.
			if i, ok := index[u.Price]; ok {
				current[i] = current[len(current)-1]
				current = current[:len(current)-1]
				// Rebuild index (simple approach).
				index = make(map[Price]int, len(current))
				for j, l := range current {
					index[l.Price] = j
				}
			}
		} else {
			// Upsert.
			if i, ok := index[u.Price]; ok {
				current[i].Quantity = u.Quantity
			} else {
				current = append(current, u)
				index[u.Price] = len(current) - 1
			}
		}
	}
	return current
}

// validateLevel checks a single level's price and size constraints.
func validateLevel(side string, idx int, l Level) *problem.Problem {
	if l.Price <= 0 {
		return problem.WithDetail(
			problem.WithDetail(
				problem.Newf(problem.ValidationFailed,
					"%s level[%d] price must be positive, got %g", side, idx, float64(l.Price)),
				"side", side,
			),
			"index", idx,
		)
	}
	if l.Quantity < 0 {
		return problem.WithDetail(
			problem.WithDetail(
				problem.Newf(problem.ValidationFailed,
					"%s level[%d] quantity must be non-negative, got %g", side, idx, float64(l.Quantity)),
				"side", side,
			),
			"index", idx,
		)
	}
	return nil
}
