package domain

import marketmodel "github.com/market-raccoon/internal/core/marketmodel"

// TradeTickV1 is the canonical trade payload (CMM v1).
// Deprecated alias kept only for package-compatibility; do not introduce new local models.
type TradeTickV1 = marketmodel.Trade

// BookSnapshotV1 is the canonical full-book payload (CMM v1).
type BookSnapshotV1 = marketmodel.BookSnapshot

// BookDeltaV1 is the canonical incremental book payload (CMM v1).
// Deprecated alias kept only for package-compatibility; do not introduce new local models.
type BookDeltaV1 = marketmodel.BookDelta

// PriceLevel is the canonical order-book level payload (CMM v1).
// Deprecated alias kept only for package-compatibility; do not introduce new local models.
type PriceLevel = marketmodel.Level

// MarkPriceTickV1 is the canonical mark-price payload (CMM v1).
// Deprecated alias kept only for package-compatibility; do not introduce new local models.
type MarkPriceTickV1 = marketmodel.MarkPrice

// LiquidationTickV1 is the canonical liquidation payload (CMM v1).
// Deprecated alias kept only for package-compatibility; do not introduce new local models.
type LiquidationTickV1 = marketmodel.Liquidation

// OpenInterestTickV1 is the canonical open-interest payload (CMM v1).
// Deprecated alias kept only for package-compatibility; do not introduce new local models.
type OpenInterestTickV1 = marketmodel.OpenInterest
