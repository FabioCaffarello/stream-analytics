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
	BookID BookID  `json:"BookID"`
	Seq    int64   `json:"Seq"`
	Bids   []Level `json:"Bids"`
	Asks   []Level `json:"Asks"`
	// V2 metadata for deterministic replay verification and bounded WS delivery.
	BestBidPrice float64 `json:"BestBidPrice"`
	BestAskPrice float64 `json:"BestAskPrice"`
	SpreadBPS    float64 `json:"SpreadBPS"`
	Checksum     uint32  `json:"Checksum"`
	TsIngestMs   int64   `json:"TsIngestMs"`
	BidCount     int     `json:"BidCount"`
	AskCount     int     `json:"AskCount"`
	DepthCap     int     `json:"DepthCap"`
	Version      int     `json:"Version"`
}

// NewOrderBookUpdated builds an OrderBookUpdated event from a book.
func NewOrderBookUpdated(book OrderBookV2, seq int64) OrderBookUpdated {
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
func NewSnapshotProduced(book OrderBookV2) SnapshotProduced {
	bids := book.Bids()
	asks := book.Asks()
	bestBid := 0.0
	bestAsk := 0.0
	if len(bids) > 0 {
		bestBid = float64(bids[0].Price)
	}
	if len(asks) > 0 {
		bestAsk = float64(asks[0].Price)
	}
	return SnapshotProduced{
		BookID:       book.ID(),
		Seq:          book.LastSeq(),
		Bids:         bids,
		Asks:         asks,
		BestBidPrice: bestBid,
		BestAskPrice: bestAsk,
		SpreadBPS:    spreadBPSFromBest(bestBid, bestAsk),
		Checksum:     book.Checksum(),
		BidCount:     len(bids),
		AskCount:     len(asks),
		DepthCap:     0,
		Version:      2,
	}
}

// Capped returns a bounded snapshot payload view while preserving raw counts
// and full-book checksum metadata from the source snapshot.
func (s SnapshotProduced) Capped(depthCap int, tsIngestMs int64) SnapshotProduced {
	out := s
	out.TsIngestMs = tsIngestMs
	if out.Version <= 0 {
		out.Version = 2
	}
	if out.BidCount <= 0 {
		out.BidCount = len(out.Bids)
	}
	if out.AskCount <= 0 {
		out.AskCount = len(out.Asks)
	}
	if depthCap <= 0 {
		out.DepthCap = 0
		out.BestBidPrice = snapshotBestBid(out.Bids)
		out.BestAskPrice = snapshotBestAsk(out.Asks)
		out.SpreadBPS = spreadBPSFromBest(out.BestBidPrice, out.BestAskPrice)
		return out
	}
	out.DepthCap = depthCap
	out.Bids = capLevels(out.Bids, depthCap)
	out.Asks = capLevels(out.Asks, depthCap)
	out.BestBidPrice = snapshotBestBid(out.Bids)
	out.BestAskPrice = snapshotBestAsk(out.Asks)
	out.SpreadBPS = spreadBPSFromBest(out.BestBidPrice, out.BestAskPrice)
	return out
}

func capLevels(levels []Level, depthCap int) []Level {
	if depthCap <= 0 || len(levels) <= depthCap {
		if len(levels) == 0 {
			return nil
		}
		out := make([]Level, len(levels))
		copy(out, levels)
		return out
	}
	out := make([]Level, depthCap)
	copy(out, levels[:depthCap])
	return out
}

func snapshotBestBid(levels []Level) float64 {
	if len(levels) == 0 {
		return 0
	}
	return float64(levels[0].Price)
}

func snapshotBestAsk(levels []Level) float64 {
	if len(levels) == 0 {
		return 0
	}
	return float64(levels[0].Price)
}

func spreadBPSFromBest(bestBid, bestAsk float64) float64 {
	if bestBid <= 0 || bestAsk <= 0 {
		return -1
	}
	mid := (bestBid + bestAsk) * 0.5
	if mid <= 0 {
		return -1
	}
	return ((bestAsk - bestBid) / mid) * 10_000
}

// EventName returns the stable event name.
func (OrderBookUpdated) EventName() string { return "OrderBookUpdated" }

// EventName returns the stable event name.
func (OrderBookInconsistentDetected) EventName() string { return "OrderBookInconsistentDetected" }

// EventName returns the stable event name.
func (SnapshotProduced) EventName() string { return "SnapshotProduced" }
