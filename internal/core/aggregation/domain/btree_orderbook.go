package domain

import (
	"github.com/google/btree"
	"github.com/market-raccoon/internal/shared/problem"
	"github.com/market-raccoon/internal/shared/validation"
)

const orderBookBTreeDegree = 16

type levelNode struct {
	Price    Price
	Quantity Quantity
}

// OrderBookPruneStrategy encapsulates eviction rules for bounded books.
type OrderBookPruneStrategy interface {
	Prune(book *OrderBook) int
}

// FurthestFromMidPruneStrategy removes levels furthest from the mid:
// lowest bids and highest asks.
type FurthestFromMidPruneStrategy struct{}

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
	id               BookID
	lastSeq          int64
	bids             *btree.BTreeG[levelNode] // sorted descending by price
	asks             *btree.BTreeG[levelNode] // sorted ascending by price
	limits           OrderBookLimitsPolicy
	pruneStrategy    OrderBookPruneStrategy
	lastPrunedLevels int
	state            HealthState
	events           []DomainEvent
}

// NewBTreeOrderBook creates an empty order book backed by B-Tree indexes.
func NewBTreeOrderBook(
	venue, instrument string,
	limits OrderBookLimitsPolicy,
	strategy OrderBookPruneStrategy,
) (*OrderBook, *problem.Problem) {
	if p := validation.Collect(
		validation.NonEmptyString("venue", venue),
		validation.NonEmptyString("instrument", instrument),
	); p != nil {
		return nil, p
	}
	if strategy == nil {
		strategy = FurthestFromMidPruneStrategy{}
	}
	return &OrderBook{
		id:            BookID{Venue: venue, Instrument: instrument},
		bids:          newBidTree(),
		asks:          newAskTree(),
		limits:        limits,
		pruneStrategy: strategy,
		state:         Healthy,
	}, nil
}

func newBidTree() *btree.BTreeG[levelNode] {
	return btree.NewG(orderBookBTreeDegree, func(a, b levelNode) bool {
		return a.Price > b.Price
	})
}

func newAskTree() *btree.BTreeG[levelNode] {
	return btree.NewG(orderBookBTreeDegree, func(a, b levelNode) bool {
		return a.Price < b.Price
	})
}

func (FurthestFromMidPruneStrategy) Prune(book *OrderBook) int {
	if book == nil || !book.limits.IsBounded() {
		return 0
	}
	maxLevels := book.limits.MaxLevelsPerSide
	pruned := pruneBidTree(book, maxLevels)
	pruned += pruneAskTree(book, maxLevels)
	return pruned
}

func pruneBidTree(book *OrderBook, maxLevels int) int {
	if book == nil || book.bids == nil || book.bids.Len() <= maxLevels {
		return 0
	}
	kept := make([]levelNode, 0, maxLevels)
	book.bids.Ascend(func(node levelNode) bool {
		if len(kept) >= maxLevels {
			return false
		}
		kept = append(kept, node)
		return true
	})
	removed := book.bids.Len() - len(kept)
	book.bids = newBidTree()
	for _, node := range kept {
		book.bids.ReplaceOrInsert(node)
	}
	return removed
}

func pruneAskTree(book *OrderBook, maxLevels int) int {
	if book == nil || book.asks == nil || book.asks.Len() <= maxLevels {
		return 0
	}
	kept := make([]levelNode, 0, maxLevels)
	book.asks.Ascend(func(node levelNode) bool {
		if len(kept) >= maxLevels {
			return false
		}
		kept = append(kept, node)
		return true
	})
	removed := book.asks.Len() - len(kept)
	book.asks = newAskTree()
	for _, node := range kept {
		book.asks.ReplaceOrInsert(node)
	}
	return removed
}

// ID returns the book identity.
func (b *OrderBook) ID() BookID { return b.id }

// BestBid returns the highest bid level, or nil if no bids.
func (b *OrderBook) BestBid() *Level {
	return firstLevel(b.bids)
}

// BestAsk returns the lowest ask level, or nil if no asks.
func (b *OrderBook) BestAsk() *Level {
	return firstLevel(b.asks)
}

func firstLevel(tree *btree.BTreeG[levelNode]) *Level {
	if tree == nil || tree.Len() == 0 {
		return nil
	}
	var out *Level
	tree.Ascend(func(node levelNode) bool {
		l := Level{Price: node.Price, Quantity: node.Quantity}
		out = &l
		return false
	})
	return out
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
	return levelsFromTree(b.bids)
}

// Asks returns a copy of all ask levels (asc by price).
func (b *OrderBook) Asks() []Level {
	return levelsFromTree(b.asks)
}

func levelsFromTree(tree *btree.BTreeG[levelNode]) []Level {
	if tree == nil || tree.Len() == 0 {
		return nil
	}
	out := make([]Level, 0, tree.Len())
	tree.Ascend(func(node levelNode) bool {
		out = append(out, Level{
			Price:    node.Price,
			Quantity: node.Quantity,
		})
		return true
	})
	return out
}

// ApplyDelta applies an incremental update to the order book.
func (b *OrderBook) ApplyDelta(seq int64, bids, asks []Level) *problem.Problem {
	return b.applyDelta(seq, bids, asks, false)
}

// ApplySnapshot replaces the entire order book with the given levels.
func (b *OrderBook) ApplySnapshot(seq int64, bids, asks []Level) *problem.Problem {
	return b.applyDelta(seq, bids, asks, true)
}

func (b *OrderBook) applyDelta(seq int64, bids, asks []Level, isSnapshot bool) *problem.Problem {
	if p := validation.PositiveInt("seq", seq); p != nil {
		return p
	}
	if seq <= b.lastSeq && !isSnapshot {
		return problem.WithDetail(
			problem.WithDetail(
				problem.Newf(problem.OutOfOrder,
					"seq %d is not greater than last seq %d", seq, b.lastSeq),
				"seq", seq,
			),
			"last_seq", b.lastSeq,
		)
	}

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

	if isSnapshot {
		b.bids = newBidTree()
		b.asks = newAskTree()
	}

	applySideUpdates(b.bids, bids)
	applySideUpdates(b.asks, asks)
	b.lastPrunedLevels = b.pruneStrategy.Prune(b)

	if p := b.checkSpread(); p != nil {
		b.state = NeedsResync
		b.lastSeq = seq
		b.events = append(b.events, OrderBookInconsistentDetected{
			BookID: b.ID(),
			Seq:    seq,
			Reason: p.Message,
		})
		b.bids = newBidTree()
		b.asks = newAskTree()
		return p
	}

	b.state = Healthy
	b.lastSeq = seq
	return nil
}

func applySideUpdates(tree *btree.BTreeG[levelNode], updates []Level) {
	if tree == nil || len(updates) == 0 {
		return
	}
	for _, u := range updates {
		key := levelNode{Price: u.Price}
		if u.Quantity == 0 {
			tree.Delete(key)
			continue
		}
		tree.ReplaceOrInsert(levelNode{
			Price:    u.Price,
			Quantity: u.Quantity,
		})
	}
}

// LastSeq returns the last successfully applied sequence number.
func (b *OrderBook) LastSeq() int64 { return b.lastSeq }

// LastPrunedLevels reports the prune count from the latest apply operation.
func (b *OrderBook) LastPrunedLevels() int { return b.lastPrunedLevels }

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
		return nil
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
