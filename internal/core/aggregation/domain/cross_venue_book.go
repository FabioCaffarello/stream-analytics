package domain

import (
	"math"
	"slices"
	"strings"

	"github.com/market-raccoon/internal/shared/problem"
	"github.com/market-raccoon/internal/shared/validation"
)

const crossVenueFixedPointScale = 100_000_000

// CrossVenueVenueBook is one venue top-of-book input used by the merger.
type CrossVenueVenueBook struct {
	Venue    string
	TsIngest int64
	Seq      int64
	BestBid  *Level
	BestAsk  *Level
}

// VenueLevel stores one venue level in fixed-point format.
type VenueLevel struct {
	Venue   string `json:"venue"`
	PriceFP int64  `json:"price_fp"`
	SizeFP  int64  `json:"size_fp"`
}

// CrossVenueBookSnapshotV1 is the deterministic merged top-of-book view.
type CrossVenueBookSnapshotV1 struct {
	Instrument         string       `json:"instrument"`
	TsServerMs         int64        `json:"ts_server_ms"`
	BestBids           []VenueLevel `json:"best_bids"`
	BestAsks           []VenueLevel `json:"best_asks"`
	GlobalSpreadBPS    float64      `json:"global_spread_bps"`
	VenueDivergenceBPS float64      `json:"venue_divergence_bps"`
}

// CrossVenueBookMerger merges per-venue top-of-book inputs into one synthetic view.
type CrossVenueBookMerger interface {
	Merge(instrument string, nowMs int64, books []CrossVenueVenueBook, staleThresholdMs int64) (CrossVenueBookSnapshotV1, *problem.Problem)
}

// DeterministicCrossVenueBookMerger is the default merger implementation.
type DeterministicCrossVenueBookMerger struct{}

// Merge merges venue books deterministically and excludes stale venues.
//
//nolint:gocyclo // Explicit branching keeps the deterministic ranking and staleness rules readable.
func (DeterministicCrossVenueBookMerger) Merge(
	instrument string,
	nowMs int64,
	books []CrossVenueVenueBook,
	staleThresholdMs int64,
) (CrossVenueBookSnapshotV1, *problem.Problem) {
	if p := validation.Collect(
		validation.NonEmptyString("instrument", instrument),
		validation.PositiveInt("now_ms", nowMs),
	); p != nil {
		return CrossVenueBookSnapshotV1{}, p
	}
	if staleThresholdMs <= 0 {
		return CrossVenueBookSnapshotV1{}, problem.New(problem.ValidationFailed, "stale_threshold_ms must be > 0")
	}

	normalizedInstrument := strings.TrimSpace(instrument)
	active := make([]CrossVenueVenueBook, 0, len(books))
	for _, book := range books {
		if strings.TrimSpace(book.Venue) == "" {
			continue
		}
		if book.TsIngest <= 0 || nowMs-book.TsIngest > staleThresholdMs {
			continue
		}
		if book.BestBid == nil || book.BestAsk == nil {
			continue
		}
		if book.BestBid.Price <= 0 || book.BestAsk.Price <= 0 {
			continue
		}
		active = append(active, book)
	}
	slices.SortFunc(active, func(a, b CrossVenueVenueBook) int {
		return strings.Compare(strings.ToLower(a.Venue), strings.ToLower(b.Venue))
	})

	snapshot := CrossVenueBookSnapshotV1{
		Instrument: normalizedInstrument,
		TsServerMs: nowMs,
		BestBids:   make([]VenueLevel, 0, len(active)),
		BestAsks:   make([]VenueLevel, 0, len(active)),
	}
	if len(active) == 0 {
		return snapshot, nil
	}

	for _, book := range active {
		snapshot.BestBids = append(snapshot.BestBids, VenueLevel{
			Venue:   book.Venue,
			PriceFP: priceToFixedPoint(book.BestBid.Price),
			SizeFP:  quantityToFixedPoint(book.BestBid.Quantity),
		})
		snapshot.BestAsks = append(snapshot.BestAsks, VenueLevel{
			Venue:   book.Venue,
			PriceFP: priceToFixedPoint(book.BestAsk.Price),
			SizeFP:  quantityToFixedPoint(book.BestAsk.Quantity),
		})
	}

	slices.SortFunc(snapshot.BestBids, func(a, b VenueLevel) int {
		switch {
		case a.PriceFP > b.PriceFP:
			return -1
		case a.PriceFP < b.PriceFP:
			return 1
		default:
			return strings.Compare(strings.ToLower(a.Venue), strings.ToLower(b.Venue))
		}
	})
	slices.SortFunc(snapshot.BestAsks, func(a, b VenueLevel) int {
		switch {
		case a.PriceFP < b.PriceFP:
			return -1
		case a.PriceFP > b.PriceFP:
			return 1
		default:
			return strings.Compare(strings.ToLower(a.Venue), strings.ToLower(b.Venue))
		}
	})

	bestBidPrice := float64(snapshot.BestBids[0].PriceFP) / crossVenueFixedPointScale
	bestAskPrice := float64(snapshot.BestAsks[0].PriceFP) / crossVenueFixedPointScale
	snapshot.GlobalSpreadBPS = spreadBPS(bestBidPrice, bestAskPrice)

	minSpread := math.MaxFloat64
	maxSpread := -math.MaxFloat64
	for _, book := range active {
		spread := spreadBPS(float64(book.BestBid.Price), float64(book.BestAsk.Price))
		if spread < minSpread {
			minSpread = spread
		}
		if spread > maxSpread {
			maxSpread = spread
		}
	}
	if minSpread == math.MaxFloat64 || maxSpread == -math.MaxFloat64 {
		snapshot.VenueDivergenceBPS = 0
	} else {
		snapshot.VenueDivergenceBPS = maxSpread - minSpread
	}
	return snapshot, nil
}

func priceToFixedPoint(price Price) int64 {
	return int64(math.Round(float64(price) * crossVenueFixedPointScale))
}

func quantityToFixedPoint(quantity Quantity) int64 {
	return int64(math.Round(float64(quantity) * crossVenueFixedPointScale))
}

func spreadBPS(bid, ask float64) float64 {
	if bid <= 0 || ask <= 0 || ask <= bid {
		return 0
	}
	mid := (bid + ask) / 2
	if mid <= 0 {
		return 0
	}
	return ((ask - bid) / mid) * 10_000
}
