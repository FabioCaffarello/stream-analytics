package marketmodel

import (
	"sort"
	"strings"

	"github.com/market-raccoon/internal/shared/problem"
)

const (
	TradeVersion        uint16 = SchemaVersionV1
	BookSnapshotVersion uint16 = SchemaVersionV1
	BookDeltaVersion    uint16 = SchemaVersionV1
	CandleVersion       uint16 = SchemaVersionV1
	StatsVersion        uint16 = SchemaVersionV1
	EvidenceVersion     uint16 = SchemaVersionV1
)

type Level struct {
	Price float64 `json:"price"`
	Size  float64 `json:"size"`
}

type Trade struct {
	Price     float64 `json:"price"`
	Size      float64 `json:"size"`
	Side      string  `json:"side"`
	TradeID   string  `json:"trade_id"`
	Timestamp int64   `json:"timestamp"`
}

type BookSnapshot struct {
	Bids      []Level `json:"bids"`
	Asks      []Level `json:"asks"`
	Timestamp int64   `json:"timestamp"`
}

type BookDelta struct {
	Bids       []Level `json:"bids"`
	Asks       []Level `json:"asks"`
	FirstID    int64   `json:"first_id"`
	FinalID    int64   `json:"final_id"`
	PrevFinal  int64   `json:"prev_final"`
	Timestamp  int64   `json:"timestamp"`
	IsSnapshot bool    `json:"is_snapshot"`
}

type MarkPrice struct {
	MarkPrice   float64 `json:"mark_price"`
	IndexPrice  float64 `json:"index_price"`
	FundingRate float64 `json:"funding_rate"`
	Timestamp   int64   `json:"timestamp"`
}

type Liquidation struct {
	Side      string  `json:"side"`
	Price     float64 `json:"price"`
	Size      float64 `json:"size"`
	Timestamp int64   `json:"timestamp"`
}

type Candle struct {
	Venue         string  `json:"venue"`
	Instrument    string  `json:"instrument"`
	Timeframe     string  `json:"timeframe"`
	WindowStartTs int64   `json:"window_start_ts"`
	WindowEndTs   int64   `json:"window_end_ts"`
	Open          float64 `json:"open"`
	High          float64 `json:"high"`
	Low           float64 `json:"low"`
	ClosePrice    float64 `json:"close_price"`
	Volume        float64 `json:"volume"`
	BuyVolume     float64 `json:"buy_volume"`
	SellVolume    float64 `json:"sell_volume"`
	TradeCount    int64   `json:"trade_count"`
	SeqFirst      int64   `json:"seq_first"`
	SeqLast       int64   `json:"seq_last"`
	IsClosed      bool    `json:"is_closed"`
}

type Stats struct {
	Venue           string  `json:"venue"`
	Instrument      string  `json:"instrument"`
	Timeframe       string  `json:"timeframe"`
	WindowStartTs   int64   `json:"window_start_ts"`
	WindowEndTs     int64   `json:"window_end_ts"`
	LiqBuyVolume    float64 `json:"liq_buy_volume"`
	LiqSellVolume   float64 `json:"liq_sell_volume"`
	LiqTotalVolume  float64 `json:"liq_total_volume"`
	LiqCount        int64   `json:"liq_count"`
	MarkPriceOpen   float64 `json:"mark_price_open"`
	MarkPriceHigh   float64 `json:"mark_price_high"`
	MarkPriceLow    float64 `json:"mark_price_low"`
	MarkPriceClose  float64 `json:"mark_price_close"`
	FundingRateAvg  float64 `json:"funding_rate_avg"`
	FundingRateLast float64 `json:"funding_rate_last"`
	SeqFirst        int64   `json:"seq_first"`
	SeqLast         int64   `json:"seq_last"`
	IsClosed        bool    `json:"is_closed"`
}

type Evidence struct {
	Kind       string            `json:"kind"`
	Venue      string            `json:"venue"`
	Instrument string            `json:"instrument"`
	Severity   string            `json:"severity"`
	Score      float64           `json:"score"`
	Features   []EvidenceFeature `json:"features"`
	Timestamp  int64             `json:"timestamp"`
}

type EvidenceFeature struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
}

func (t Trade) Validate() *problem.Problem {
	if _, p := NewPrice(t.Price); p != nil {
		return p
	}
	if _, p := NewSize(t.Size, false); p != nil {
		return p
	}
	if _, p := NewSide(t.Side); p != nil {
		return p
	}
	if strings.TrimSpace(t.TradeID) == "" {
		return problem.WithDetail(problem.New(problem.ValidationFailed, "trade_id must not be empty"), "field", "trade_id")
	}
	if _, p := NewServerTS(t.Timestamp); p != nil {
		return p
	}
	return nil
}

func (s BookSnapshot) Validate() *problem.Problem {
	if _, p := NewServerTS(s.Timestamp); p != nil {
		return p
	}
	if p := validateLevels(s.Bids, true); p != nil {
		return p
	}
	if p := validateLevels(s.Asks, true); p != nil {
		return p
	}
	if !isSorted(s.Bids, true) {
		return problem.New(problem.ValidationFailed, "snapshot bids must be sorted by price desc")
	}
	if !isSorted(s.Asks, false) {
		return problem.New(problem.ValidationFailed, "snapshot asks must be sorted by price asc")
	}
	return nil
}

func (d BookDelta) Validate() *problem.Problem {
	if _, p := NewServerTS(d.Timestamp); p != nil {
		return p
	}
	if p := validateLevels(d.Bids, false); p != nil {
		return p
	}
	if p := validateLevels(d.Asks, false); p != nil {
		return p
	}
	if !d.IsSnapshot {
		if d.FirstID <= 0 || d.FinalID <= 0 {
			return problem.New(problem.ValidationFailed, "book delta update ids must be > 0")
		}
		if d.FinalID < d.FirstID {
			return problem.New(problem.ValidationFailed, "book delta final_id must be >= first_id")
		}
	}
	if !isSorted(d.Bids, true) {
		return problem.New(problem.ValidationFailed, "delta bids must be sorted by price desc")
	}
	if !isSorted(d.Asks, false) {
		return problem.New(problem.ValidationFailed, "delta asks must be sorted by price asc")
	}
	return nil
}

func NormalizeBookOrdering(bids, asks []Level, delta bool) ([]Level, []Level, *problem.Problem) {
	nb := cloneLevels(bids)
	na := cloneLevels(asks)
	sortLevels(nb, true)
	sortLevels(na, false)
	nb = collapseByPrice(nb)
	na = collapseByPrice(na)
	if p := validateLevels(nb, !delta); p != nil {
		return nil, nil, p
	}
	if p := validateLevels(na, !delta); p != nil {
		return nil, nil, p
	}
	return nb, na, nil
}

func sortLevels(levels []Level, desc bool) {
	sort.SliceStable(levels, func(i, j int) bool {
		pi := levels[i].Price
		pj := levels[j].Price
		if pi == pj {
			return levels[i].Size > levels[j].Size
		}
		if desc {
			return pi > pj
		}
		return pi < pj
	})
}

func collapseByPrice(levels []Level) []Level {
	if len(levels) == 0 {
		return nil
	}
	out := make([]Level, 0, len(levels))
	for _, lvl := range levels {
		if len(out) > 0 && out[len(out)-1].Price == lvl.Price {
			continue
		}
		out = append(out, lvl)
	}
	return out
}

func validateLevels(levels []Level, strictPositiveSize bool) *problem.Problem {
	for _, lvl := range levels {
		if _, p := NewPrice(lvl.Price); p != nil {
			return p
		}
		if _, p := NewSize(lvl.Size, !strictPositiveSize); p != nil {
			return p
		}
	}
	return nil
}

func isSorted(levels []Level, desc bool) bool {
	for i := 1; i < len(levels); i++ {
		prev := levels[i-1].Price
		cur := levels[i].Price
		if desc {
			if prev < cur {
				return false
			}
			continue
		}
		if prev > cur {
			return false
		}
	}
	return true
}

func cloneLevels(in []Level) []Level {
	if len(in) == 0 {
		return nil
	}
	out := make([]Level, len(in))
	copy(out, in)
	return out
}
