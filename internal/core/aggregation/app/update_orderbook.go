// Package app contains the application use cases for the aggregation context.
package app

import (
	"context"

	"github.com/market-raccoon/internal/core/aggregation/domain"
	"github.com/market-raccoon/internal/core/aggregation/ports"
	"github.com/market-raccoon/internal/shared/problem"
	"github.com/market-raccoon/internal/shared/result"
	"github.com/market-raccoon/internal/shared/validation"
)

// UpdateRequest carries the delta to apply to a specific order book.
type UpdateRequest struct {
	Venue      string
	Instrument string
	Seq        int64
	Bids       []domain.Level
	Asks       []domain.Level
}

// UpdateResponse is returned on success.
type UpdateResponse struct {
	Seq    int64
	Spread float64
}

// UpdateOrderBookFromEvents applies incremental deltas and publishes snapshots.
//
// Steps:
//  1. Validate inputs
//  2. Get or create OrderBook aggregate
//  3. ApplyDelta — on crossed book, emit inconsistency event
//  4. Publish updated snapshot via ArtifactPublisher
//  5. Persist snapshot in hot read model
type UpdateOrderBookFromEvents struct {
	publisher ports.ArtifactPublisher
	store     ports.HotReadModelStore
	books     map[domain.BookID]*domain.OrderBook
}

// NewUpdateOrderBookFromEvents constructs the use case.
func NewUpdateOrderBookFromEvents(
	pub ports.ArtifactPublisher,
	store ports.HotReadModelStore,
) *UpdateOrderBookFromEvents {
	return &UpdateOrderBookFromEvents{
		publisher: pub,
		store:     store,
		books:     make(map[domain.BookID]*domain.OrderBook),
	}
}

// Execute applies the delta and returns the updated spread.
func (uc *UpdateOrderBookFromEvents) Execute(ctx context.Context, req UpdateRequest) result.Result[UpdateResponse] {
	// 1. Validate inputs.
	if p := validation.Collect(
		validation.NonEmptyString("venue", req.Venue),
		validation.NonEmptyString("instrument", req.Instrument),
		validation.PositiveInt("seq", req.Seq),
	); p != nil {
		return result.FailProblem[UpdateResponse](p)
	}

	// 2. Get or create aggregate.
	book, p := uc.getOrCreateBook(req.Venue, req.Instrument)
	if p != nil {
		return result.FailProblem[UpdateResponse](p)
	}

	// 3. Apply delta.
	if p := book.ApplyDelta(req.Seq, req.Bids, req.Asks); p != nil {
		if p.Code == problem.IntegrityViolation {
			events := book.PullDomainEvents()
			for _, evt := range events {
				inconsistent, ok := evt.(domain.OrderBookInconsistentDetected)
				if !ok {
					continue
				}
				if pubErr := uc.publisher.PublishInconsistent(ctx, inconsistent); pubErr != nil {
					return result.FailProblem[UpdateResponse](pubErr)
				}
			}
			return result.FailProblem[UpdateResponse](p)
		}
		return result.FailProblem[UpdateResponse](p)
	}

	// 4. Publish snapshot.
	snap := domain.NewSnapshotProduced(book)
	if p := uc.publisher.PublishSnapshot(ctx, snap); p != nil {
		return result.FailProblem[UpdateResponse](p)
	}

	// 5. Persist in hot read model.
	if p := uc.store.Save(ctx, snap); p != nil {
		return result.FailProblem[UpdateResponse](p)
	}

	return result.Ok(UpdateResponse{
		Seq:    book.LastSeq(),
		Spread: book.Spread(),
	})
}

// getOrCreateBook lazily initialises an OrderBook for the given identity.
func (uc *UpdateOrderBookFromEvents) getOrCreateBook(venue, instrument string) (*domain.OrderBook, *problem.Problem) {
	id := domain.BookID{Venue: venue, Instrument: instrument}
	if b, ok := uc.books[id]; ok {
		return b, nil
	}
	b, p := domain.NewOrderBook(venue, instrument)
	if p != nil {
		return nil, p
	}
	uc.books[id] = b
	return b, nil
}
