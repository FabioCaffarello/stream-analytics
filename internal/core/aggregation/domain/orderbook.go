// Package domain contains the aggregation bounded context domain model.
package domain

import (
	"math"

	"github.com/market-raccoon/internal/shared/problem"
	"github.com/market-raccoon/internal/shared/validation"
)

// BookID uniquely identifies an order book by venue and instrument.
type BookID struct {
	Venue      string `json:"Venue"`
	Instrument string `json:"Instrument"`
}

// Price is a strictly positive market price.
type Price float64

// Quantity is a non-negative size/amount.
type Quantity float64

// Level represents a single price level in the order book.
type Level struct {
	Price    Price    `json:"Price"`
	Quantity Quantity `json:"Quantity"`
}

// HealthState represents consistency state of the aggregate.
type HealthState string

const (
	// Healthy means no inconsistency was detected in the latest applied state.
	Healthy HealthState = "HEALTHY"
	// NeedsResync means a crossed book/integrity issue was detected and snapshot reload is needed.
	NeedsResync HealthState = "NEEDS_RESYNC"
)

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
	policy, p := NewOrderBookLimitsPolicy(maxLevels)
	if p != nil {
		return nil, p
	}
	return NewBTreeOrderBook(venue, instrument, policy, FurthestFromMidPruneStrategy{})
}

// validateLevel checks a single level's price and size constraints.
func validateLevel(side string, idx int, l Level) *problem.Problem {
	price := float64(l.Price)
	qty := float64(l.Quantity)
	if math.IsNaN(price) {
		return problem.WithDetail(
			problem.WithDetail(
				problem.WithDetail(
					problem.Newf(problem.ValidationFailed,
						"%s level[%d] price must be a finite positive number, got %g", side, idx, price),
					"side", side,
				),
				"index", idx,
			),
			"reason", "nan",
		)
	}
	if math.IsInf(price, 0) {
		return problem.WithDetail(
			problem.WithDetail(
				problem.WithDetail(
					problem.Newf(problem.ValidationFailed,
						"%s level[%d] price must be a finite positive number, got %g", side, idx, price),
					"side", side,
				),
				"index", idx,
			),
			"reason", "inf",
		)
	}
	if l.Price < 0 {
		return problem.WithDetail(
			problem.WithDetail(
				problem.WithDetail(
					problem.Newf(problem.ValidationFailed,
						"%s level[%d] price must be a finite positive number, got %g", side, idx, price),
					"side", side,
				),
				"index", idx,
			),
			"reason", "neg_price",
		)
	}
	if l.Price == 0 {
		return problem.WithDetail(
			problem.WithDetail(
				problem.WithDetail(
					problem.Newf(problem.ValidationFailed,
						"%s level[%d] price must be a finite positive number, got %g", side, idx, price),
					"side", side,
				),
				"index", idx,
			),
			"reason", "zero_price",
		)
	}
	if math.IsNaN(qty) {
		return problem.WithDetail(
			problem.WithDetail(
				problem.WithDetail(
					problem.Newf(problem.ValidationFailed,
						"%s level[%d] quantity must be a finite non-negative number, got %g", side, idx, qty),
					"side", side,
				),
				"index", idx,
			),
			"reason", "nan",
		)
	}
	if math.IsInf(qty, 0) {
		return problem.WithDetail(
			problem.WithDetail(
				problem.WithDetail(
					problem.Newf(problem.ValidationFailed,
						"%s level[%d] quantity must be a finite non-negative number, got %g", side, idx, qty),
					"side", side,
				),
				"index", idx,
			),
			"reason", "inf",
		)
	}
	if l.Quantity < 0 {
		return problem.WithDetail(
			problem.WithDetail(
				problem.WithDetail(
					problem.Newf(problem.ValidationFailed,
						"%s level[%d] quantity must be a finite non-negative number, got %g", side, idx, qty),
					"side", side,
				),
				"index", idx,
			),
			"reason", "neg_qty",
		)
	}
	return nil
}
