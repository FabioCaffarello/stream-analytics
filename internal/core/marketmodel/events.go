package marketmodel

import (
	"math"
	"sort"
	"strconv"
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
	SignalVersion       uint16 = SchemaVersionV1
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
	Type           string            `json:"type"`
	TsServer       int64             `json:"ts_server"`
	Venue          string            `json:"venue"`
	Symbol         string            `json:"symbol"`
	StreamID       string            `json:"stream_id"`
	Seq            int64             `json:"seq"`
	Severity       string            `json:"severity"`
	Confidence     float64           `json:"confidence"`
	Features       []EvidenceFeature `json:"features"`
	Explanation    string            `json:"explanation"`
	RuleVersion    string            `json:"rule_version"`
	InputWatermark InputWatermark    `json:"input_watermark"`
}

type EvidenceFeature struct {
	Key   string  `json:"key"`
	Value float64 `json:"value"`
}

type InputWatermark struct {
	SeqStart int64 `json:"seq_start"`
	SeqEnd   int64 `json:"seq_end"`
}

type SignalScope string

const (
	SignalScopeStream SignalScope = "stream"
	SignalScopeMarket SignalScope = "market"
)

type SignalFeature struct {
	Key   string  `json:"key"`
	Value float64 `json:"value"`
}

type SignalInputSeqRange struct {
	Venue    string `json:"venue"`
	Symbol   string `json:"symbol"`
	SeqStart int64  `json:"seq_start"`
	SeqEnd   int64  `json:"seq_end"`
}

type SignalEvent struct {
	Type           string                `json:"type"`
	TsServer       int64                 `json:"ts_server"`
	Scope          SignalScope           `json:"scope"`
	Venue          string                `json:"venue,omitempty"`
	Symbol         string                `json:"symbol,omitempty"`
	Severity       string                `json:"severity"`
	Confidence     float64               `json:"confidence"`
	Features       []SignalFeature       `json:"features"`
	Explanation    string                `json:"explanation"`
	RuleVersion    string                `json:"rule_version"`
	InputWatermark []SignalInputSeqRange `json:"input_watermark"`
	CorrelationID  string                `json:"correlation_id"`
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

//nolint:gocyclo // signal payload validation keeps explicit contract checks in one place.
func (s SignalEvent) Validate() *problem.Problem {
	if strings.TrimSpace(s.Type) == "" {
		return problem.New(problem.ValidationFailed, "signal type must not be empty")
	}
	if _, p := NewServerTS(s.TsServer); p != nil {
		return p
	}
	switch s.Scope {
	case SignalScopeStream:
		if strings.TrimSpace(s.Venue) == "" {
			return problem.New(problem.ValidationFailed, "signal venue must not be empty for stream scope")
		}
		if strings.TrimSpace(s.Symbol) == "" {
			return problem.New(problem.ValidationFailed, "signal symbol must not be empty for stream scope")
		}
	case SignalScopeMarket:
		if strings.TrimSpace(s.Venue) != "" || strings.TrimSpace(s.Symbol) != "" {
			return problem.New(problem.ValidationFailed, "market scope signal must not set venue/symbol")
		}
	default:
		return problem.New(problem.ValidationFailed, "signal scope must be stream or market")
	}
	if !isSignalSeverity(s.Severity) {
		return problem.New(problem.ValidationFailed, "signal severity must be one of low|medium|high|critical")
	}
	if !isFinite(s.Confidence) || s.Confidence < 0 || s.Confidence > 1 {
		return problem.New(problem.ValidationFailed, "signal confidence must be in [0,1]")
	}
	if len(s.Features) == 0 {
		return problem.New(problem.ValidationFailed, "signal features must not be empty")
	}
	for i := range s.Features {
		if strings.TrimSpace(s.Features[i].Key) == "" {
			return problem.New(problem.ValidationFailed, "signal feature key must not be empty")
		}
		if !isFinite(s.Features[i].Value) {
			return problem.New(problem.ValidationFailed, "signal feature values must be finite")
		}
		if i > 0 && strings.Compare(s.Features[i-1].Key, s.Features[i].Key) >= 0 {
			return problem.New(problem.ValidationFailed, "signal features must be sorted and unique by key")
		}
	}
	if strings.TrimSpace(s.Explanation) == "" {
		return problem.New(problem.ValidationFailed, "signal explanation must not be empty")
	}
	if strings.TrimSpace(s.RuleVersion) == "" {
		return problem.New(problem.ValidationFailed, "signal rule_version must not be empty")
	}
	if len(s.InputWatermark) == 0 {
		return problem.New(problem.ValidationFailed, "signal input_watermark must not be empty")
	}
	for i := range s.InputWatermark {
		w := s.InputWatermark[i]
		if strings.TrimSpace(w.Venue) == "" || strings.TrimSpace(w.Symbol) == "" {
			return problem.New(problem.ValidationFailed, "signal input_watermark venue/symbol must not be empty")
		}
		if w.SeqStart <= 0 || w.SeqEnd <= 0 || w.SeqEnd < w.SeqStart {
			return problem.New(problem.ValidationFailed, "signal input_watermark seq range is invalid")
		}
		if i > 0 {
			prev := s.InputWatermark[i-1]
			if cmp := strings.Compare(prev.Venue+"|"+prev.Symbol, w.Venue+"|"+w.Symbol); cmp >= 0 {
				return problem.New(problem.ValidationFailed, "signal input_watermark must be sorted and unique by venue/symbol")
			}
		}
	}
	if strings.TrimSpace(s.CorrelationID) == "" {
		return problem.New(problem.ValidationFailed, "signal correlation_id must not be empty")
	}
	return nil
}

func SignalScopeFromProtoValue(v int32) SignalScope {
	switch v {
	case 1:
		return SignalScopeStream
	case 2:
		return SignalScopeMarket
	default:
		return ""
	}
}

func (s SignalScope) ProtoValue() int32 {
	switch s {
	case SignalScopeStream:
		return 1
	case SignalScopeMarket:
		return 2
	default:
		return 0
	}
}

func FormatSignalFeatureValue(v float64) string {
	if !isFinite(v) {
		return "0"
	}
	return strconv.FormatFloat(v, 'f', 6, 64)
}

func isSignalSeverity(severity string) bool {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "low", "medium", "high", "critical":
		return true
	default:
		return false
	}
}

func isFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
