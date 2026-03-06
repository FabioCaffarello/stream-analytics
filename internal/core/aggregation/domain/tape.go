package domain

import (
	"math"
	"strings"

	"github.com/market-raccoon/internal/shared/problem"
	"github.com/market-raccoon/internal/shared/validation"
)

const tapeNotionalFixedScale int64 = candleFixedScale

// AllowedTapeTimeframes defines fixed tape aggregation windows for v1.
var AllowedTapeTimeframes = []string{"250ms", "1s", "5s"}

// TapeWindowDurations maps supported tape windows to milliseconds.
var TapeWindowDurations = map[string]int64{
	"250ms": 250,
	"1s":    1_000,
	"5s":    5_000,
}

// TapeKey identifies one open tape window state.
type TapeKey struct {
	Venue      string
	Instrument string
	Timeframe  string
}

// TapeWindowV1 accumulates deterministic trade-flow metrics in one fixed window.
type TapeWindowV1 struct {
	Venue         string `json:"Venue"`
	Instrument    string `json:"Instrument"`
	Timeframe     string `json:"Timeframe"`
	WindowStartTs int64  `json:"WindowStartTs"`
	WindowEndTs   int64  `json:"WindowEndTs"`

	TradeCount int64 `json:"TradeCount"`
	BuyCount   int64 `json:"BuyCount"`
	SellCount  int64 `json:"SellCount"`

	BuyVolume   float64 `json:"BuyVolume"`
	SellVolume  float64 `json:"SellVolume"`
	TotalVolume float64 `json:"TotalVolume"`

	BuyNotional  float64 `json:"BuyNotional"`
	SellNotional float64 `json:"SellNotional"`

	VwapPrice float64 `json:"VwapPrice"`

	MaxPrice  float64 `json:"MaxPrice"`
	MinPrice  float64 `json:"MinPrice"`
	LastPrice float64 `json:"LastPrice"`

	MaxTradeSize float64 `json:"MaxTradeSize"`

	LastSeq int64 `json:"LastSeq"`

	RateTradesPerSec float64 `json:"RateTradesPerSec"`
	VolumeImbalance  float64 `json:"VolumeImbalance"`

	IsClosed bool `json:"IsClosed"`

	buyVolumeFixed   int64
	sellVolumeFixed  int64
	totalVolumeFixed int64

	buyNotionalFixed  int64
	sellNotionalFixed int64

	maxPriceFixed     int64
	minPriceFixed     int64
	lastPriceFixed    int64
	maxTradeSizeFixed int64

	hasTrades bool
}

// TapeClosed is emitted when one tape window is finalized.
type TapeClosed struct {
	Window  TapeWindowV1
	IsBurst bool
}

// EventName returns the stable event name.
func (TapeClosed) EventName() string { return "TapeClosed" }

// NewTapeClosed wraps a finalized tape window into a domain event.
func NewTapeClosed(w TapeWindowV1, isBurst bool) TapeClosed {
	return TapeClosed{Window: w, IsBurst: isBurst}
}

// NewTapeWindowV1 creates one open tape window.
func NewTapeWindowV1(venue, instrument, timeframe string, windowStartTs int64) (*TapeWindowV1, *problem.Problem) {
	if p := validation.Collect(
		validation.NonEmptyString("venue", venue),
		validation.NonEmptyString("instrument", instrument),
		validation.OneOf("timeframe", timeframe, AllowedTapeTimeframes),
		validation.NonNegativeInt("window_start_ts", windowStartTs),
	); p != nil {
		return nil, p
	}
	return &TapeWindowV1{
		Venue:         strings.TrimSpace(venue),
		Instrument:    strings.TrimSpace(instrument),
		Timeframe:     strings.TrimSpace(timeframe),
		WindowStartTs: windowStartTs,
	}, nil
}

// ApplyTrade applies one trade to the open window.
func (w *TapeWindowV1) ApplyTrade(price, size float64, isBuy bool, seq int64) *problem.Problem {
	if p := w.checkMutable(); p != nil {
		return p
	}
	if p := validation.PositiveInt("seq", seq); p != nil {
		return p
	}
	priceFixed, sizeFixed, notionalFixed, p := toTradeFixedValues(price, size)
	if p != nil {
		return p
	}
	w.TradeCount++
	w.applySide(isBuy, sizeFixed, notionalFixed)
	w.totalVolumeFixed = w.buyVolumeFixed + w.sellVolumeFixed
	w.updateExtrema(priceFixed, sizeFixed, seq)
	w.syncFromFixed()
	return w.Validate()
}

// Close finalizes this tape window. Closed windows are immutable.
func (w *TapeWindowV1) Close(windowEndTs int64) *problem.Problem {
	if p := w.checkMutable(); p != nil {
		return p
	}
	if windowEndTs <= w.WindowStartTs {
		return problem.Newf(problem.ValidationFailed,
			"window_end_ts must be > window_start_ts: start=%d end=%d",
			w.WindowStartTs,
			windowEndTs,
		)
	}
	w.WindowEndTs = windowEndTs
	w.IsClosed = true

	if w.totalVolumeFixed > 0 {
		totalNotionalFixed := w.buyNotionalFixed + w.sellNotionalFixed
		w.VwapPrice = float64(totalNotionalFixed) / float64(w.totalVolumeFixed)
	}

	durationMs := TapeWindowDurations[w.Timeframe]
	if durationMs > 0 {
		w.RateTradesPerSec = float64(w.TradeCount) / (float64(durationMs) / 1000.0)
	}
	if w.totalVolumeFixed > 0 {
		w.VolumeImbalance = float64(w.buyVolumeFixed-w.sellVolumeFixed) / float64(w.totalVolumeFixed)
		if w.VolumeImbalance > 1 {
			w.VolumeImbalance = 1
		}
		if w.VolumeImbalance < -1 {
			w.VolumeImbalance = -1
		}
	}
	w.syncFromFixed()
	return w.Validate()
}

// Rate returns trades-per-second for this window.
func (w *TapeWindowV1) Rate() float64 {
	if w == nil {
		return 0
	}
	return w.RateTradesPerSec
}

// Imbalance returns (BuyVolume-SellVolume)/TotalVolume in [-1,+1].
func (w *TapeWindowV1) Imbalance() float64 {
	if w == nil {
		return 0
	}
	return w.VolumeImbalance
}

// IsBurst returns true when trade count exceeds threshold.
func (w *TapeWindowV1) IsBurst(threshold int64) bool {
	if w == nil || threshold < 0 {
		return false
	}
	return w.TradeCount > threshold
}

// Validate enforces tape invariants.
func (w *TapeWindowV1) Validate() *problem.Problem {
	if p := w.validateStaticFields(); p != nil {
		return p
	}
	if p := w.validateWindowBounds(); p != nil {
		return p
	}
	if w.TradeCount == 0 {
		return w.validateEmptyWindow()
	}
	return w.validateNonEmptyWindow()
}

func toTradeFixedValues(price, size float64) (priceFixed, sizeFixed, notionalFixed int64, p *problem.Problem) {
	priceFixed, p = toPositiveFixed("price", price, candleFixedScale)
	if p != nil {
		return 0, 0, 0, p
	}
	sizeFixed, p = toPositiveFixed("size", size, candleFixedScale)
	if p != nil {
		return 0, 0, 0, p
	}
	notional := price * size
	if math.IsNaN(notional) || math.IsInf(notional, 0) || notional <= 0 {
		return 0, 0, 0, problem.New(problem.ValidationFailed, "notional must be a positive finite number")
	}
	notionalFixed = int64(math.RoundToEven(notional * float64(tapeNotionalFixedScale)))
	if notionalFixed <= 0 {
		return 0, 0, 0, problem.New(problem.ValidationFailed, "notional must be > 0")
	}
	return priceFixed, sizeFixed, notionalFixed, nil
}

func (w *TapeWindowV1) applySide(isBuy bool, sizeFixed, notionalFixed int64) {
	if isBuy {
		w.BuyCount++
		w.buyVolumeFixed += sizeFixed
		w.buyNotionalFixed += notionalFixed
		return
	}
	w.SellCount++
	w.sellVolumeFixed += sizeFixed
	w.sellNotionalFixed += notionalFixed
}

func (w *TapeWindowV1) updateExtrema(priceFixed, sizeFixed, seq int64) {
	if !w.hasTrades {
		w.maxPriceFixed = priceFixed
		w.minPriceFixed = priceFixed
		w.lastPriceFixed = priceFixed
		w.maxTradeSizeFixed = sizeFixed
		w.LastSeq = seq
		w.hasTrades = true
		return
	}
	if priceFixed > w.maxPriceFixed {
		w.maxPriceFixed = priceFixed
	}
	if priceFixed < w.minPriceFixed {
		w.minPriceFixed = priceFixed
	}
	if sizeFixed > w.maxTradeSizeFixed {
		w.maxTradeSizeFixed = sizeFixed
	}
	if seq > w.LastSeq || (seq == w.LastSeq && priceFixed > w.lastPriceFixed) {
		w.LastSeq = seq
		w.lastPriceFixed = priceFixed
	}
}

func (w *TapeWindowV1) validateStaticFields() *problem.Problem {
	if p := validation.Collect(
		validation.NonEmptyString("venue", w.Venue),
		validation.NonEmptyString("instrument", w.Instrument),
		validation.OneOf("timeframe", w.Timeframe, AllowedTapeTimeframes),
		validation.NonNegativeInt("window_start_ts", w.WindowStartTs),
		validation.NonNegativeInt("trade_count", w.TradeCount),
	); p != nil {
		return p
	}
	return nil
}

func (w *TapeWindowV1) validateWindowBounds() *problem.Problem {
	if w.IsClosed && w.WindowEndTs <= w.WindowStartTs {
		return problem.New(problem.IntegrityViolation, "closed tape window must have valid window bounds")
	}
	if !w.IsClosed && w.WindowEndTs != 0 {
		return problem.New(problem.IntegrityViolation, "open tape window must not have window_end_ts set")
	}
	return nil
}

func (w *TapeWindowV1) validateEmptyWindow() *problem.Problem {
	if w.BuyCount != 0 || w.SellCount != 0 {
		return problem.New(problem.IntegrityViolation, "empty tape window cannot have side counts")
	}
	return nil
}

func (w *TapeWindowV1) validateNonEmptyWindow() *problem.Problem {
	if w.BuyCount+w.SellCount != w.TradeCount {
		return problem.New(problem.IntegrityViolation, "trade_count must equal buy_count + sell_count")
	}
	if w.totalVolumeFixed <= 0 || w.buyVolumeFixed < 0 || w.sellVolumeFixed < 0 {
		return problem.New(problem.IntegrityViolation, "volume totals are invalid")
	}
	if w.totalVolumeFixed != w.buyVolumeFixed+w.sellVolumeFixed {
		return problem.New(problem.IntegrityViolation, "total_volume must equal buy_volume + sell_volume")
	}
	if w.maxPriceFixed <= 0 || w.minPriceFixed <= 0 || w.lastPriceFixed <= 0 {
		return problem.New(problem.IntegrityViolation, "price extrema must be positive")
	}
	if w.maxPriceFixed < w.minPriceFixed {
		return problem.New(problem.IntegrityViolation, "max_price must be >= min_price")
	}
	if w.maxTradeSizeFixed <= 0 {
		return problem.New(problem.IntegrityViolation, "max_trade_size must be positive")
	}
	if w.LastSeq <= 0 {
		return problem.New(problem.IntegrityViolation, "last_seq must be positive when trades exist")
	}
	if w.VolumeImbalance < -1 || w.VolumeImbalance > 1 {
		return problem.New(problem.IntegrityViolation, "imbalance must be in [-1,+1]")
	}
	return nil
}

func (w *TapeWindowV1) checkMutable() *problem.Problem {
	if w.IsClosed {
		return problem.New(problem.Conflict, "cannot mutate closed tape window")
	}
	return nil
}

func (w *TapeWindowV1) syncFromFixed() {
	w.BuyVolume = fromFixed(w.buyVolumeFixed, candleFixedScale)
	w.SellVolume = fromFixed(w.sellVolumeFixed, candleFixedScale)
	w.TotalVolume = fromFixed(w.totalVolumeFixed, candleFixedScale)
	w.BuyNotional = fromFixed(w.buyNotionalFixed, tapeNotionalFixedScale)
	w.SellNotional = fromFixed(w.sellNotionalFixed, tapeNotionalFixedScale)
	w.MaxPrice = fromFixed(w.maxPriceFixed, candleFixedScale)
	w.MinPrice = fromFixed(w.minPriceFixed, candleFixedScale)
	w.LastPrice = fromFixed(w.lastPriceFixed, candleFixedScale)
	w.MaxTradeSize = fromFixed(w.maxTradeSizeFixed, candleFixedScale)
	if w.totalVolumeFixed > 0 {
		w.VwapPrice = float64(w.buyNotionalFixed+w.sellNotionalFixed) / float64(w.totalVolumeFixed)
	}
}
