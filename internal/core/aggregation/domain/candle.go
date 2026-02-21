package domain

import (
	"math"
	"strings"

	"github.com/market-raccoon/internal/shared/problem"
	"github.com/market-raccoon/internal/shared/validation"
)

const candleFixedScale int64 = 100_000_000

// AllowedCandleTimeframes defines the fixed candle timeframe set in v1.
var AllowedCandleTimeframes = []string{"1m", "5m", "15m", "30m", "1h", "4h", "1d"}

// CandleKey identifies one open candle state.
type CandleKey struct {
	Venue      string
	Instrument string
	Timeframe  string
}

// NewCandleKey validates and constructs a candle key.
func NewCandleKey(venue, instrument, timeframe string) (CandleKey, *problem.Problem) {
	if p := validation.Collect(
		validation.NonEmptyString("venue", venue),
		validation.NonEmptyString("instrument", instrument),
		validation.OneOf("timeframe", timeframe, AllowedCandleTimeframes),
	); p != nil {
		return CandleKey{}, p
	}
	return CandleKey{
		Venue:      strings.TrimSpace(venue),
		Instrument: strings.TrimSpace(instrument),
		Timeframe:  strings.TrimSpace(timeframe),
	}, nil
}

// CandleV1 is the v1 OHLCV aggregate for one timeframe window.
type CandleV1 struct {
	Venue         string
	Instrument    string
	Timeframe     string
	WindowStartTs int64
	WindowEndTs   int64
	Open          float64
	High          float64
	Low           float64
	ClosePrice    float64
	Volume        float64
	BuyVolume     float64
	SellVolume    float64
	TradeCount    int64
	SeqFirst      int64
	SeqLast       int64
	IsClosed      bool

	openFixed       int64
	highFixed       int64
	lowFixed        int64
	closeFixed      int64
	volumeFixed     int64
	buyVolumeFixed  int64
	sellVolumeFixed int64
}

// CandleClosed is emitted when one candle window is finalized.
type CandleClosed struct {
	Candle CandleV1
}

// EventName returns the stable event name.
func (CandleClosed) EventName() string { return "CandleClosed" }

// NewCandleClosed wraps a finalized candle into a domain event.
func NewCandleClosed(c CandleV1) CandleClosed {
	return CandleClosed{Candle: c}
}

// NewCandleV1 creates one open candle for the given identity and window start.
func NewCandleV1(venue, instrument, timeframe string, windowStartTs int64) (*CandleV1, *problem.Problem) {
	if p := validation.Collect(
		validation.NonEmptyString("venue", venue),
		validation.NonEmptyString("instrument", instrument),
		validation.OneOf("timeframe", timeframe, AllowedCandleTimeframes),
		validation.NonNegativeInt("window_start_ts", windowStartTs),
	); p != nil {
		return nil, p
	}
	c := &CandleV1{
		Venue:         strings.TrimSpace(venue),
		Instrument:    strings.TrimSpace(instrument),
		Timeframe:     strings.TrimSpace(timeframe),
		WindowStartTs: windowStartTs,
	}
	return c, nil
}

// Key returns the bounded-map identity for this candle.
func (c *CandleV1) Key() CandleKey {
	return CandleKey{
		Venue:      c.Venue,
		Instrument: c.Instrument,
		Timeframe:  c.Timeframe,
	}
}

// ApplyTrade applies one trade tick to the open candle.
func (c *CandleV1) ApplyTrade(price, qty float64, isBuy bool, seq int64) *problem.Problem {
	if c.IsClosed {
		return problem.New(problem.Conflict, "cannot mutate closed candle")
	}
	if p := validation.PositiveInt("seq", seq); p != nil {
		return p
	}
	priceFixed, p := toPositiveFixed("price", price, candleFixedScale)
	if p != nil {
		return p
	}
	qtyFixed, p := toPositiveFixed("qty", qty, candleFixedScale)
	if p != nil {
		return p
	}
	if c.SeqLast > 0 && seq < c.SeqLast {
		return problem.Newf(problem.OutOfOrder, "seq must be monotonic: got=%d last=%d", seq, c.SeqLast)
	}

	if c.TradeCount == 0 {
		c.openFixed = priceFixed
		c.highFixed = priceFixed
		c.lowFixed = priceFixed
		c.closeFixed = priceFixed
		c.SeqFirst = seq
	} else {
		if priceFixed > c.highFixed {
			c.highFixed = priceFixed
		}
		if priceFixed < c.lowFixed {
			c.lowFixed = priceFixed
		}
		c.closeFixed = priceFixed
	}

	c.volumeFixed += qtyFixed
	if isBuy {
		c.buyVolumeFixed += qtyFixed
	} else {
		c.sellVolumeFixed += qtyFixed
	}
	c.SeqLast = seq
	c.TradeCount++
	c.syncFromFixed()
	return c.Validate()
}

// ApplyClosedCandle folds one closed lower-timeframe candle into this candle.
func (c *CandleV1) ApplyClosedCandle(child CandleV1) *problem.Problem {
	if c.IsClosed {
		return problem.New(problem.Conflict, "cannot mutate closed candle")
	}
	if !child.IsClosed {
		return problem.New(problem.ValidationFailed, "child candle must be closed")
	}
	if strings.TrimSpace(child.Venue) != c.Venue {
		return problem.New(problem.ValidationFailed, "child venue mismatch")
	}
	if strings.TrimSpace(child.Instrument) != c.Instrument {
		return problem.New(problem.ValidationFailed, "child instrument mismatch")
	}
	if child.TradeCount <= 0 {
		return problem.New(problem.ValidationFailed, "child candle must contain trades")
	}
	if p := child.Validate(); p != nil {
		return p
	}
	if p := child.hydrateFixedFromFields(); p != nil {
		return p
	}
	if c.SeqLast > 0 && child.SeqFirst < c.SeqLast {
		return problem.Newf(problem.OutOfOrder, "child seq out of order: child_first=%d last=%d", child.SeqFirst, c.SeqLast)
	}

	if c.TradeCount == 0 {
		c.openFixed = child.openFixed
		c.highFixed = child.highFixed
		c.lowFixed = child.lowFixed
		c.closeFixed = child.closeFixed
		c.SeqFirst = child.SeqFirst
	} else {
		if child.highFixed > c.highFixed {
			c.highFixed = child.highFixed
		}
		if child.lowFixed < c.lowFixed {
			c.lowFixed = child.lowFixed
		}
		c.closeFixed = child.closeFixed
	}

	c.volumeFixed += child.volumeFixed
	c.buyVolumeFixed += child.buyVolumeFixed
	c.sellVolumeFixed += child.sellVolumeFixed
	c.TradeCount += child.TradeCount
	c.SeqLast = child.SeqLast
	c.syncFromFixed()
	return c.Validate()
}

// Close finalizes the candle window. Closed candles are immutable.
func (c *CandleV1) Close(windowEndTs int64) *problem.Problem {
	if c.IsClosed {
		return problem.New(problem.Conflict, "candle already closed")
	}
	if c.TradeCount <= 0 {
		return problem.New(problem.ValidationFailed, "cannot close candle without trades")
	}
	if windowEndTs <= c.WindowStartTs {
		return problem.Newf(problem.ValidationFailed,
			"window_end_ts must be > window_start_ts: start=%d end=%d",
			c.WindowStartTs,
			windowEndTs,
		)
	}
	c.WindowEndTs = windowEndTs
	c.IsClosed = true
	return c.Validate()
}

// Validate enforces CA-1..CA-7 state invariants.
//
//nolint:gocyclo // explicit invariant checks are intentionally verbose.
func (c *CandleV1) Validate() *problem.Problem {
	if p := validation.Collect(
		validation.NonEmptyString("venue", c.Venue),
		validation.NonEmptyString("instrument", c.Instrument),
		validation.OneOf("timeframe", c.Timeframe, AllowedCandleTimeframes),
		validation.NonNegativeInt("window_start_ts", c.WindowStartTs),
		validation.NonNegativeInt("trade_count", c.TradeCount),
	); p != nil {
		return p
	}
	if c.IsClosed && c.WindowEndTs <= c.WindowStartTs {
		return problem.New(problem.IntegrityViolation, "closed candle must have window_end_ts > window_start_ts")
	}
	if !c.IsClosed && c.WindowEndTs != 0 {
		return problem.New(problem.IntegrityViolation, "open candle must not have window_end_ts set")
	}
	if c.TradeCount == 0 {
		return nil
	}
	if p := c.hydrateFixedFromFields(); p != nil {
		return p
	}
	if c.SeqFirst <= 0 || c.SeqLast < c.SeqFirst {
		return problem.New(problem.IntegrityViolation, "seq bounds are invalid")
	}
	if c.openFixed <= 0 || c.highFixed <= 0 || c.lowFixed <= 0 || c.closeFixed <= 0 {
		return problem.New(problem.IntegrityViolation, "ohlc must be strictly positive")
	}
	if c.highFixed < c.lowFixed {
		return problem.New(problem.IntegrityViolation, "high must be >= low")
	}
	if c.highFixed < c.openFixed || c.highFixed < c.closeFixed {
		return problem.New(problem.IntegrityViolation, "high must be >= open and close")
	}
	if c.lowFixed > c.openFixed || c.lowFixed > c.closeFixed {
		return problem.New(problem.IntegrityViolation, "low must be <= open and close")
	}
	if c.volumeFixed < 0 || c.buyVolumeFixed < 0 || c.sellVolumeFixed < 0 {
		return problem.New(problem.IntegrityViolation, "volumes must be non-negative")
	}
	if c.volumeFixed != c.buyVolumeFixed+c.sellVolumeFixed {
		return problem.New(problem.IntegrityViolation, "volume must equal buy_volume + sell_volume")
	}
	return nil
}

//nolint:gocyclo // fixed-point hydration validates each field independently.
func (c *CandleV1) hydrateFixedFromFields() *problem.Problem {
	if c.TradeCount <= 0 {
		return nil
	}
	var p *problem.Problem
	if c.openFixed == 0 {
		c.openFixed, p = toPositiveFixed("open", c.Open, candleFixedScale)
		if p != nil {
			return p
		}
	}
	if c.highFixed == 0 {
		c.highFixed, p = toPositiveFixed("high", c.High, candleFixedScale)
		if p != nil {
			return p
		}
	}
	if c.lowFixed == 0 {
		c.lowFixed, p = toPositiveFixed("low", c.Low, candleFixedScale)
		if p != nil {
			return p
		}
	}
	if c.closeFixed == 0 {
		c.closeFixed, p = toPositiveFixed("close", c.ClosePrice, candleFixedScale)
		if p != nil {
			return p
		}
	}
	if c.volumeFixed == 0 {
		c.volumeFixed, p = toNonNegativeFixed("volume", c.Volume, candleFixedScale)
		if p != nil {
			return p
		}
	}
	if c.buyVolumeFixed == 0 && c.BuyVolume > 0 {
		c.buyVolumeFixed, p = toNonNegativeFixed("buy_volume", c.BuyVolume, candleFixedScale)
		if p != nil {
			return p
		}
	}
	if c.sellVolumeFixed == 0 && c.SellVolume > 0 {
		c.sellVolumeFixed, p = toNonNegativeFixed("sell_volume", c.SellVolume, candleFixedScale)
		if p != nil {
			return p
		}
	}
	return nil
}

func (c *CandleV1) syncFromFixed() {
	c.Open = fromFixed(c.openFixed, candleFixedScale)
	c.High = fromFixed(c.highFixed, candleFixedScale)
	c.Low = fromFixed(c.lowFixed, candleFixedScale)
	c.ClosePrice = fromFixed(c.closeFixed, candleFixedScale)
	c.Volume = fromFixed(c.volumeFixed, candleFixedScale)
	c.BuyVolume = fromFixed(c.buyVolumeFixed, candleFixedScale)
	c.SellVolume = fromFixed(c.sellVolumeFixed, candleFixedScale)
}

func toPositiveFixed(field string, value float64, scale int64) (int64, *problem.Problem) {
	fixed, p := toNonNegativeFixed(field, value, scale)
	if p != nil {
		return 0, p
	}
	if fixed <= 0 {
		return 0, problem.Newf(problem.ValidationFailed, "%s must be positive", field)
	}
	return fixed, nil
}

func toNonNegativeFixed(field string, value float64, scale int64) (int64, *problem.Problem) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, problem.Newf(problem.ValidationFailed, "%s must be a finite number", field)
	}
	scaled := math.RoundToEven(value * float64(scale))
	if scaled > math.MaxInt64 || scaled < math.MinInt64 {
		return 0, problem.Newf(problem.ValidationFailed, "%s out of fixed precision range", field)
	}
	fixed := int64(scaled)
	if fixed < 0 {
		return 0, problem.Newf(problem.ValidationFailed, "%s must be non-negative", field)
	}
	return fixed, nil
}

func fromFixed(value, scale int64) float64 {
	return float64(value) / float64(scale)
}
