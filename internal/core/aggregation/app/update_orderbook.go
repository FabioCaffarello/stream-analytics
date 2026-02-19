// Package app contains the application use cases for the aggregation context.
package app

import (
	"context"
	"time"

	"github.com/market-raccoon/internal/core/aggregation/domain"
	"github.com/market-raccoon/internal/core/aggregation/ports"
	"github.com/market-raccoon/internal/shared/clock"
	"github.com/market-raccoon/internal/shared/ds"
	"github.com/market-raccoon/internal/shared/metrics"
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
	IsSnapshot bool // true when the message is a full L2 snapshot
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
//  4. Persist snapshot in hot read model
//  5. Publish updated snapshot via ArtifactPublisher
type UpdateOrderBookFromEvents struct {
	publisher ports.ArtifactPublisher
	store     ports.HotReadModelStore
	books     *ds.BoundedMap[domain.BookID, *domain.OrderBook]
	maxLevels int
}

type UpdateConfig struct {
	MaxBooks  int
	BookTTL   time.Duration
	MaxLevels int
	Clock     clock.Clock
}

// NewUpdateOrderBookFromEvents constructs the use case.
func NewUpdateOrderBookFromEvents(
	pub ports.ArtifactPublisher,
	store ports.HotReadModelStore,
) *UpdateOrderBookFromEvents {
	return NewUpdateOrderBookFromEventsWithConfig(pub, store, UpdateConfig{
		MaxBooks:  10_000,
		BookTTL:   time.Hour,
		MaxLevels: 1_000,
		Clock:     clock.NewSystemClock(),
	})
}

func NewUpdateOrderBookFromEventsWithConfig(
	pub ports.ArtifactPublisher,
	store ports.HotReadModelStore,
	cfg UpdateConfig,
) *UpdateOrderBookFromEvents {
	if cfg.MaxBooks <= 0 {
		cfg.MaxBooks = 10_000
	}
	if cfg.BookTTL <= 0 {
		cfg.BookTTL = time.Hour
	}
	if cfg.MaxLevels <= 0 {
		cfg.MaxLevels = 1_000
	}
	if cfg.Clock == nil {
		cfg.Clock = clock.NewSystemClock()
	}

	books := ds.NewBoundedMap[domain.BookID, *domain.OrderBook](cfg.MaxBooks, cfg.BookTTL, cfg.Clock)
	books.SetSweepEveryOps(1024)
	books.SetSweepMinInterval(time.Second)
	books.SetOnEvict(func(_ domain.BookID, _ *domain.OrderBook, reason string) {
		metrics.IncBooksEvicted(reason)
	})
	return &UpdateOrderBookFromEvents{
		publisher: pub,
		store:     store,
		books:     books,
		maxLevels: cfg.MaxLevels,
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

	// 3. Apply delta (or snapshot).
	applyFn := book.ApplyDelta
	if req.IsSnapshot {
		applyFn = book.ApplySnapshot
	}
	if p := applyFn(req.Seq, req.Bids, req.Asks); p != nil {
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

	// 4. Persist snapshot in hot read model.
	snap := domain.NewSnapshotProduced(book)
	if p := uc.store.Save(ctx, snap); p != nil {
		return result.FailProblem[UpdateResponse](p)
	}

	// 5. Publish snapshot.
	if p := uc.publisher.PublishSnapshot(ctx, snap); p != nil {
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
	metrics.AggregationBooksActive.Set(float64(uc.books.Len()))
	if b, ok := uc.books.Get(id); ok {
		return b, nil
	}
	b, p := domain.NewOrderBookWithMaxLevels(venue, instrument, uc.maxLevels)
	if p != nil {
		return nil, p
	}
	uc.books.Put(id, b)
	metrics.AggregationBooksActive.Set(float64(uc.books.Len()))
	return b, nil
}

func (uc *UpdateOrderBookFromEvents) ActiveBooks() int {
	return uc.books.Len()
}

// Snapshot returns the current in-memory snapshot for a book key.
// It performs no writes and does not publish artifacts.
func (uc *UpdateOrderBookFromEvents) Snapshot(venue, instrument string) (domain.SnapshotProduced, *problem.Problem) {
	if p := validation.Collect(
		validation.NonEmptyString("venue", venue),
		validation.NonEmptyString("instrument", instrument),
	); p != nil {
		return domain.SnapshotProduced{}, p
	}

	id := domain.BookID{Venue: venue, Instrument: instrument}
	book, ok := uc.books.Get(id)
	if !ok {
		return domain.SnapshotProduced{}, problem.Newf(problem.NotFound, "orderbook snapshot not found for %s/%s", venue, instrument)
	}
	return domain.NewSnapshotProduced(book), nil
}
