package domain

import (
	"math"
	"slices"
	"strings"

	"github.com/market-raccoon/internal/shared/problem"
)

const (
	// CrossVenueTradeSnapshotType is the derived output event type for join snapshots.
	CrossVenueTradeSnapshotType = "insights.crossvenue.trade_snapshot"
	// CrossVenueTradeSnapshotVersion is the schema version for CrossVenueTradeSnapshotV1.
	CrossVenueTradeSnapshotVersion = 1
	// CrossVenueSpreadSignalType is the optional derived output event type for spread signals.
	CrossVenueSpreadSignalType = "insights.crossvenue.spread_signal"
	// CrossVenueSpreadSignalVersion is the schema version for CrossVenueSpreadSignalV1.
	CrossVenueSpreadSignalVersion = 1
	// CrossVenueSnapshotVenue is the synthetic venue used for derived cross-venue events.
	CrossVenueSnapshotVenue = "GLOBAL"
)

// JoinKey is the stable identity for cross-venue joins.
// Keyed by canonical instrument and optional market_type partition.
type JoinKey struct {
	Instrument string
	MarketType string
}

// NewJoinKey normalizes and validates join identity fields.
func NewJoinKey(instrument, marketType string) (JoinKey, *problem.Problem) {
	inst := strings.ToUpper(strings.TrimSpace(instrument))
	if inst == "" {
		return JoinKey{}, problem.New(problem.ValidationFailed, "instrument must not be empty")
	}
	return JoinKey{
		Instrument: inst,
		MarketType: strings.ToUpper(strings.TrimSpace(marketType)),
	}, nil
}

// HasMarketType reports whether this key is partitioned by market type.
func (k JoinKey) HasMarketType() bool { return k.MarketType != "" }

// LastTrade stores the latest observed trade for one venue within a join key.
type LastTrade struct {
	Venue          string
	Price          float64
	Size           float64
	Side           string
	TradeID        string
	TsExchange     int64
	TsIngest       int64
	Seq            int64
	IdempotencyKey string
}

// SnapshotVenueTradeV1 is one venue row in the joined snapshot payload.
type SnapshotVenueTradeV1 struct {
	Venue      string  `json:"venue"`
	Price      float64 `json:"price"`
	Size       float64 `json:"size"`
	Side       string  `json:"side"`
	TradeID    string  `json:"trade_id,omitempty"`
	TsExchange int64   `json:"ts_exchange"`
	TsIngest   int64   `json:"ts_ingest"`
	Seq        int64   `json:"seq"`
}

// CrossVenueTradeSnapshotV1 is the deterministic payload emitted by W10-1 join.
type CrossVenueTradeSnapshotV1 struct {
	Instrument        string                 `json:"instrument"`
	MarketType        string                 `json:"market_type,omitempty"`
	WatermarkTsIngest int64                  `json:"watermark_ts_ingest"`
	Venues            []SnapshotVenueTradeV1 `json:"venues"`
	MinPrice          float64                `json:"min_price"`
	MinPriceVenue     string                 `json:"min_price_venue"`
	MaxPrice          float64                `json:"max_price"`
	MaxPriceVenue     string                 `json:"max_price_venue"`
	SpreadAbs         float64                `json:"spread_abs"`
	SpreadBps         float64                `json:"spread_bps"`
	MidPrice          float64                `json:"mid_price"`
}

// Validate enforces deterministic snapshot invariants.
func (s CrossVenueTradeSnapshotV1) Validate() *problem.Problem {
	if strings.TrimSpace(s.Instrument) == "" {
		return problem.New(problem.ValidationFailed, "snapshot instrument must not be empty")
	}
	if s.WatermarkTsIngest <= 0 {
		return problem.New(problem.ValidationFailed, "snapshot watermark_ts_ingest must be positive")
	}
	if len(s.Venues) < 2 {
		return problem.New(problem.ValidationFailed, "snapshot requires at least two venues")
	}
	if p := validateSnapshotDerivedFields(s); p != nil {
		return p
	}
	return validateSnapshotVenueRows(s.Venues)
}

func validateSnapshotDerivedFields(s CrossVenueTradeSnapshotV1) *problem.Problem {
	if strings.TrimSpace(s.MinPriceVenue) == "" {
		return problem.New(problem.ValidationFailed, "snapshot min_price_venue must not be empty")
	}
	if strings.TrimSpace(s.MaxPriceVenue) == "" {
		return problem.New(problem.ValidationFailed, "snapshot max_price_venue must not be empty")
	}
	if !finiteFloat64(s.MinPrice) || !finiteFloat64(s.MaxPrice) || !finiteFloat64(s.SpreadAbs) || !finiteFloat64(s.SpreadBps) || !finiteFloat64(s.MidPrice) {
		return problem.New(problem.ValidationFailed, "snapshot derived price fields must be finite numbers")
	}
	if s.SpreadAbs < 0 {
		return problem.New(problem.ValidationFailed, "snapshot spread_abs must be >= 0")
	}
	if s.MaxPrice < s.MinPrice {
		return problem.New(problem.ValidationFailed, "snapshot max_price must be >= min_price")
	}
	return nil
}

func validateSnapshotVenueRows(rows []SnapshotVenueTradeV1) *problem.Problem {
	seen := make(map[string]struct{}, len(rows))
	venues := make([]string, 0, len(rows))
	for _, row := range rows {
		v := strings.ToUpper(strings.TrimSpace(row.Venue))
		if v == "" {
			return problem.New(problem.ValidationFailed, "snapshot venue must not be empty")
		}
		if _, exists := seen[v]; exists {
			return problem.Newf(problem.ValidationFailed, "duplicate snapshot venue %q", v)
		}
		seen[v] = struct{}{}
		venues = append(venues, v)
	}
	if !slices.IsSorted(venues) {
		return problem.New(problem.ValidationFailed, "snapshot venues must be sorted lexicographically")
	}
	return nil
}

// CrossVenueSpreadSignalV1 is the optional deterministic payload emitted when
// spread thresholds are met.
type CrossVenueSpreadSignalV1 struct {
	Instrument        string  `json:"instrument"`
	MarketType        string  `json:"market_type,omitempty"`
	WatermarkTsIngest int64   `json:"watermark_ts_ingest"`
	MinPrice          float64 `json:"min_price"`
	MinPriceVenue     string  `json:"min_price_venue"`
	MaxPrice          float64 `json:"max_price"`
	MaxPriceVenue     string  `json:"max_price_venue"`
	SpreadAbs         float64 `json:"spread_abs"`
	SpreadBps         float64 `json:"spread_bps"`
}

// Validate enforces deterministic signal invariants.
func (s CrossVenueSpreadSignalV1) Validate() *problem.Problem {
	if strings.TrimSpace(s.Instrument) == "" {
		return problem.New(problem.ValidationFailed, "spread signal instrument must not be empty")
	}
	if s.WatermarkTsIngest <= 0 {
		return problem.New(problem.ValidationFailed, "spread signal watermark_ts_ingest must be positive")
	}
	if strings.TrimSpace(s.MinPriceVenue) == "" {
		return problem.New(problem.ValidationFailed, "spread signal min_price_venue must not be empty")
	}
	if strings.TrimSpace(s.MaxPriceVenue) == "" {
		return problem.New(problem.ValidationFailed, "spread signal max_price_venue must not be empty")
	}
	if !finiteFloat64(s.MinPrice) || !finiteFloat64(s.MaxPrice) || !finiteFloat64(s.SpreadAbs) || !finiteFloat64(s.SpreadBps) {
		return problem.New(problem.ValidationFailed, "spread signal price fields must be finite numbers")
	}
	if s.SpreadAbs < 0 {
		return problem.New(problem.ValidationFailed, "spread signal spread_abs must be >= 0")
	}
	if s.MaxPrice < s.MinPrice {
		return problem.New(problem.ValidationFailed, "spread signal max_price must be >= min_price")
	}
	return nil
}

func finiteFloat64(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
