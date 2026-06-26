package domain

import "github.com/FabioCaffarello/stream-analytics/internal/shared/problem"

// OrderBookV2 defines the deterministic order book contract.
type OrderBookV2 interface {
	ID() BookID
	BestBid() *Level
	BestAsk() *Level
	Spread() float64
	Bids() []Level
	Asks() []Level
	TopN(n int) (bids, asks []Level)
	Checksum() uint32
	ApplyDelta(seq int64, bids, asks []Level) *problem.Problem
	ApplySnapshot(seq int64, bids, asks []Level) *problem.Problem
	LastSeq() int64
	State() HealthState
	IsHealthy() bool
	NeedsResync() bool
	PullDomainEvents() []DomainEvent
	LastPrunedLevels() int
}
