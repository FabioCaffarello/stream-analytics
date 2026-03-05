package domain

import (
	"math"
	"strings"

	"github.com/market-raccoon/internal/shared/problem"
	"github.com/market-raccoon/internal/shared/validation"
)

const statsFundingFixedScale int64 = 1_000_000_000

const (
	StatsQualityFlagMissingLiquidation uint32 = 1 << iota
	StatsQualityFlagMissingMarkPrice
	StatsQualityFlagMissingFunding
	StatsQualityFlagForcedClose
)

// AllowedStatsTimeframes defines the fixed stats timeframe set in v1.
var AllowedStatsTimeframes = []string{"1s", "5s", "1m", "5m", "15m", "30m", "1h", "4h", "1d"}

// StatsKey identifies one open stats window state.
type StatsKey struct {
	Venue      string
	Instrument string
	Timeframe  string
}

// NewStatsKey validates and constructs a stats key.
func NewStatsKey(venue, instrument, timeframe string) (StatsKey, *problem.Problem) {
	if p := validation.Collect(
		validation.NonEmptyString("venue", venue),
		validation.NonEmptyString("instrument", instrument),
		validation.OneOf("timeframe", timeframe, AllowedStatsTimeframes),
	); p != nil {
		return StatsKey{}, p
	}
	return StatsKey{
		Venue:      strings.TrimSpace(venue),
		Instrument: strings.TrimSpace(instrument),
		Timeframe:  strings.TrimSpace(timeframe),
	}, nil
}

// StatsWindowV1 is the v1 stats aggregate for one timeframe window.
type StatsWindowV1 struct {
	Venue           string
	Instrument      string
	Timeframe       string
	WindowStartTs   int64
	WindowEndTs     int64
	WindowMs        int64
	TsIngestMs      int64
	QualityFlags    uint32
	LiqBuyVolume    float64
	LiqSellVolume   float64
	LiqTotalVolume  float64
	LiqCount        int64
	MarkPriceOpen   float64
	MarkPriceHigh   float64
	MarkPriceLow    float64
	MarkPriceClose  float64
	FundingRateAvg  float64
	FundingRateLast float64
	SeqFirst        int64
	SeqLast         int64
	IsClosed        bool

	liqBuyVolumeFixed   int64
	liqSellVolumeFixed  int64
	liqTotalVolumeFixed int64

	markPriceOpenFixed  int64
	markPriceHighFixed  int64
	markPriceLowFixed   int64
	markPriceCloseFixed int64
	markPriceSamples    int64

	fundingRateSumFixed  int64
	fundingRateAvgFixed  int64
	fundingRateLastFixed int64
	fundingRateSamples   int64
}

// StatsWindowClosed is emitted when one stats window is finalized.
type StatsWindowClosed struct {
	Stats StatsWindowV1
}

// EventName returns the stable event name.
func (StatsWindowClosed) EventName() string { return "StatsWindowClosed" }

// NewStatsWindowClosed wraps a finalized stats window into a domain event.
func NewStatsWindowClosed(s StatsWindowV1) StatsWindowClosed {
	return StatsWindowClosed{Stats: s}
}

// NewStatsWindowV1 creates one open stats window for the given identity.
func NewStatsWindowV1(venue, instrument, timeframe string, windowStartTs int64) (*StatsWindowV1, *problem.Problem) {
	if p := validation.Collect(
		validation.NonEmptyString("venue", venue),
		validation.NonEmptyString("instrument", instrument),
		validation.OneOf("timeframe", timeframe, AllowedStatsTimeframes),
		validation.NonNegativeInt("window_start_ts", windowStartTs),
	); p != nil {
		return nil, p
	}
	return &StatsWindowV1{
		Venue:         strings.TrimSpace(venue),
		Instrument:    strings.TrimSpace(instrument),
		Timeframe:     strings.TrimSpace(timeframe),
		WindowStartTs: windowStartTs,
	}, nil
}

// Key returns the bounded-map identity for this window.
func (w *StatsWindowV1) Key() StatsKey {
	return StatsKey{
		Venue:      w.Venue,
		Instrument: w.Instrument,
		Timeframe:  w.Timeframe,
	}
}

// ApplyLiquidation applies one liquidation tick to this stats window.
func (w *StatsWindowV1) ApplyLiquidation(side string, qty float64, seq int64) *problem.Problem {
	if p := w.checkMutable(); p != nil {
		return p
	}
	qtyFixed, p := toPositiveFixed("liquidation_qty", qty, candleFixedScale)
	if p != nil {
		return p
	}
	if p := w.bumpSeq(seq); p != nil {
		return p
	}
	side = strings.ToLower(strings.TrimSpace(side))
	switch side {
	case "buy":
		w.liqBuyVolumeFixed += qtyFixed
	case "sell":
		w.liqSellVolumeFixed += qtyFixed
	default:
		return problem.Newf(problem.ValidationFailed, "liquidation side must be buy|sell, got %q", side)
	}
	w.liqTotalVolumeFixed = w.liqBuyVolumeFixed + w.liqSellVolumeFixed
	w.LiqCount++
	w.syncFromFixed()
	return w.Validate()
}

// ApplyMarkPrice applies one mark price update to this stats window.
func (w *StatsWindowV1) ApplyMarkPrice(markPrice float64, seq int64) *problem.Problem {
	if p := w.checkMutable(); p != nil {
		return p
	}
	priceFixed, p := toPositiveFixed("mark_price", markPrice, candleFixedScale)
	if p != nil {
		return p
	}
	if p := w.bumpSeq(seq); p != nil {
		return p
	}
	if w.markPriceSamples == 0 {
		w.markPriceOpenFixed = priceFixed
		w.markPriceHighFixed = priceFixed
		w.markPriceLowFixed = priceFixed
		w.markPriceCloseFixed = priceFixed
	} else {
		if priceFixed > w.markPriceHighFixed {
			w.markPriceHighFixed = priceFixed
		}
		if priceFixed < w.markPriceLowFixed {
			w.markPriceLowFixed = priceFixed
		}
		w.markPriceCloseFixed = priceFixed
	}
	w.markPriceSamples++
	w.syncFromFixed()
	return w.Validate()
}

// ApplyFundingRate applies one funding rate update to this stats window.
func (w *StatsWindowV1) ApplyFundingRate(rate float64, seq int64) *problem.Problem {
	if p := w.checkMutable(); p != nil {
		return p
	}
	if math.IsNaN(rate) || math.IsInf(rate, 0) {
		return problem.New(problem.ValidationFailed, "funding_rate must be finite")
	}
	rateFixed := int64(math.RoundToEven(rate * float64(statsFundingFixedScale)))
	if p := w.bumpSeq(seq); p != nil {
		return p
	}
	w.fundingRateSamples++
	w.fundingRateSumFixed += rateFixed
	w.fundingRateLastFixed = rateFixed
	w.fundingRateAvgFixed = w.fundingRateSumFixed / w.fundingRateSamples
	w.syncFromFixed()
	return w.Validate()
}

// Close finalizes this stats window. Closed windows are immutable.
func (w *StatsWindowV1) Close(windowEndTs int64) *problem.Problem {
	if p := w.checkMutable(); p != nil {
		return p
	}
	if w.SeqFirst == 0 {
		return problem.New(problem.ValidationFailed, "cannot close stats window without any input")
	}
	if windowEndTs <= w.WindowStartTs {
		return problem.Newf(problem.ValidationFailed,
			"window_end_ts must be > window_start_ts: start=%d end=%d",
			w.WindowStartTs,
			windowEndTs,
		)
	}
	w.WindowEndTs = windowEndTs
	w.WindowMs = windowEndTs - w.WindowStartTs
	w.IsClosed = true
	w.setQualityFlags(false)
	return w.Validate()
}

// Validate enforces ST-1..ST-6 state invariants.
//
//nolint:gocyclo // explicit invariant checks are intentionally verbose.
func (w *StatsWindowV1) Validate() *problem.Problem {
	if p := validation.Collect(
		validation.NonEmptyString("venue", w.Venue),
		validation.NonEmptyString("instrument", w.Instrument),
		validation.OneOf("timeframe", w.Timeframe, AllowedStatsTimeframes),
		validation.NonNegativeInt("window_start_ts", w.WindowStartTs),
		validation.NonNegativeInt("liq_count", w.LiqCount),
	); p != nil {
		return p
	}
	if w.IsClosed && w.WindowEndTs <= w.WindowStartTs {
		return problem.New(problem.IntegrityViolation, "closed stats window must have valid window bounds")
	}
	if w.IsClosed && w.WindowMs <= 0 {
		return problem.New(problem.IntegrityViolation, "closed stats window must have window_ms > 0")
	}
	if !w.IsClosed && w.WindowEndTs != 0 {
		return problem.New(problem.IntegrityViolation, "open stats window must not have window_end_ts set")
	}
	if w.liqBuyVolumeFixed < 0 || w.liqSellVolumeFixed < 0 || w.liqTotalVolumeFixed < 0 {
		return problem.New(problem.IntegrityViolation, "liquidation volumes must be non-negative")
	}
	if w.liqTotalVolumeFixed != w.liqBuyVolumeFixed+w.liqSellVolumeFixed {
		return problem.New(problem.IntegrityViolation, "liq_total_volume must equal liq_buy_volume + liq_sell_volume")
	}
	if w.SeqFirst > 0 && w.SeqLast < w.SeqFirst {
		return problem.New(problem.IntegrityViolation, "seq bounds are invalid")
	}

	if w.markPriceSamples > 0 {
		if w.markPriceOpenFixed <= 0 || w.markPriceHighFixed <= 0 || w.markPriceLowFixed <= 0 || w.markPriceCloseFixed <= 0 {
			return problem.New(problem.IntegrityViolation, "mark price values must be positive when markprice input exists")
		}
		if w.markPriceHighFixed < w.markPriceLowFixed {
			return problem.New(problem.IntegrityViolation, "mark_price_high must be >= mark_price_low")
		}
		if w.markPriceHighFixed < w.markPriceOpenFixed || w.markPriceHighFixed < w.markPriceCloseFixed {
			return problem.New(problem.IntegrityViolation, "mark_price_high must be >= open and close")
		}
		if w.markPriceLowFixed > w.markPriceOpenFixed || w.markPriceLowFixed > w.markPriceCloseFixed {
			return problem.New(problem.IntegrityViolation, "mark_price_low must be <= open and close")
		}
	}
	return nil
}

func (w *StatsWindowV1) SetQualityFlags(forcedClose bool) {
	w.setQualityFlags(forcedClose)
}

func (w *StatsWindowV1) checkMutable() *problem.Problem {
	if w.IsClosed {
		return problem.New(problem.Conflict, "cannot mutate closed stats window")
	}
	return nil
}

func (w *StatsWindowV1) bumpSeq(seq int64) *problem.Problem {
	if p := validation.PositiveInt("seq", seq); p != nil {
		return p
	}
	if w.SeqLast > 0 && seq < w.SeqLast {
		return problem.Newf(problem.OutOfOrder, "seq must be monotonic: got=%d last=%d", seq, w.SeqLast)
	}
	if w.SeqFirst == 0 {
		w.SeqFirst = seq
	}
	w.SeqLast = seq
	return nil
}

func (w *StatsWindowV1) syncFromFixed() {
	w.LiqBuyVolume = fromFixed(w.liqBuyVolumeFixed, candleFixedScale)
	w.LiqSellVolume = fromFixed(w.liqSellVolumeFixed, candleFixedScale)
	w.LiqTotalVolume = fromFixed(w.liqTotalVolumeFixed, candleFixedScale)

	if w.markPriceSamples > 0 {
		w.MarkPriceOpen = fromFixed(w.markPriceOpenFixed, candleFixedScale)
		w.MarkPriceHigh = fromFixed(w.markPriceHighFixed, candleFixedScale)
		w.MarkPriceLow = fromFixed(w.markPriceLowFixed, candleFixedScale)
		w.MarkPriceClose = fromFixed(w.markPriceCloseFixed, candleFixedScale)
	}
	if w.fundingRateSamples > 0 {
		w.FundingRateAvg = fromFixed(w.fundingRateAvgFixed, statsFundingFixedScale)
		w.FundingRateLast = fromFixed(w.fundingRateLastFixed, statsFundingFixedScale)
	}
}

func (w *StatsWindowV1) setQualityFlags(forcedClose bool) {
	var flags uint32
	if w.LiqCount == 0 {
		flags |= StatsQualityFlagMissingLiquidation
	}
	if w.markPriceSamples == 0 {
		flags |= StatsQualityFlagMissingMarkPrice
	}
	if w.fundingRateSamples == 0 {
		flags |= StatsQualityFlagMissingFunding
	}
	if forcedClose {
		flags |= StatsQualityFlagForcedClose
	}
	w.QualityFlags = flags
}
