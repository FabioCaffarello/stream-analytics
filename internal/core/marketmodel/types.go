package marketmodel

import (
	"fmt"
	"math"
	"strings"

	"github.com/FabioCaffarello/stream-analytics/internal/shared/naming"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

const SchemaVersionV1 uint16 = 1

type Venue string

type Symbol string

type Channel string

const (
	ChannelTrade        Channel = "trade"
	ChannelBookSnapshot Channel = "book_snapshot"
	ChannelBookDelta    Channel = "book_delta"
	ChannelCandle       Channel = "candle"
	ChannelStats        Channel = "stats"
	ChannelEvidence     Channel = "evidence"
	ChannelSignal       Channel = "signal"
	ChannelMarkPrice    Channel = "mark_price"
	ChannelLiquidation  Channel = "liquidation"
)

type StreamKey struct {
	Venue   Venue
	Symbol  Symbol
	Channel Channel
}

type Seq int64

type ServerTS int64

type Price float64

type Size float64

type Side string

const (
	SideBuy  Side = "buy"
	SideSell Side = "sell"
)

func NewVenue(raw string) (Venue, *problem.Problem) {
	v := naming.CanonicalVenue(raw)
	if strings.TrimSpace(v) == "" {
		return "", problem.WithDetail(problem.New(problem.ValidationFailed, "venue must not be empty"), "field", "venue")
	}
	return Venue(v), nil
}

func NewSymbol(raw string) (Symbol, *problem.Problem) {
	s := naming.CanonicalInstrument(raw)
	if strings.TrimSpace(s) == "" {
		return "", problem.WithDetail(problem.New(problem.ValidationFailed, "symbol must not be empty"), "field", "symbol")
	}
	return Symbol(s), nil
}

func NewChannel(raw string) (Channel, *problem.Problem) {
	c := Channel(strings.ToLower(strings.TrimSpace(raw)))
	switch c {
	case ChannelTrade,
		ChannelBookSnapshot,
		ChannelBookDelta,
		ChannelCandle,
		ChannelStats,
		ChannelEvidence,
		ChannelSignal,
		ChannelMarkPrice,
		ChannelLiquidation:
		return c, nil
	default:
		return "", problem.WithDetail(problem.Newf(problem.ValidationFailed, "unsupported channel %q", raw), "field", "channel")
	}
}

func NewStreamKey(venue, symbol string, channel Channel) (StreamKey, *problem.Problem) {
	v, p := NewVenue(venue)
	if p != nil {
		return StreamKey{}, p
	}
	s, p := NewSymbol(symbol)
	if p != nil {
		return StreamKey{}, p
	}
	if _, p := NewChannel(string(channel)); p != nil {
		return StreamKey{}, p
	}
	return StreamKey{Venue: v, Symbol: s, Channel: channel}, nil
}

func NewSeq(v int64) (Seq, *problem.Problem) {
	if v <= 0 {
		return 0, problem.WithDetail(problem.Newf(problem.ValidationFailed, "seq must be > 0, got %d", v), "field", "seq")
	}
	return Seq(v), nil
}

func NewServerTS(ms int64) (ServerTS, *problem.Problem) {
	if ms <= 0 {
		return 0, problem.WithDetail(problem.Newf(problem.ValidationFailed, "server_ts must be > 0, got %d", ms), "field", "server_ts")
	}
	return ServerTS(ms), nil
}

func NewPrice(v float64) (Price, *problem.Problem) {
	if math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 {
		return 0, problem.WithDetail(problem.Newf(problem.ValidationFailed, "price must be finite and > 0, got %v", v), "field", "price")
	}
	return Price(v), nil
}

func NewSize(v float64, allowZero bool) (Size, *problem.Problem) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, problem.WithDetail(problem.Newf(problem.ValidationFailed, "size must be finite, got %v", v), "field", "size")
	}
	if allowZero {
		if v < 0 {
			return 0, problem.WithDetail(problem.Newf(problem.ValidationFailed, "size must be >= 0, got %v", v), "field", "size")
		}
	} else if v <= 0 {
		return 0, problem.WithDetail(problem.Newf(problem.ValidationFailed, "size must be > 0, got %v", v), "field", "size")
	}
	return Size(v), nil
}

func NewSide(raw string) (Side, *problem.Problem) {
	s := Side(strings.ToLower(strings.TrimSpace(raw)))
	switch s {
	case SideBuy, SideSell:
		return s, nil
	default:
		return "", problem.WithDetail(problem.Newf(problem.ValidationFailed, "unsupported side %q", raw), "field", "side")
	}
}

func (k StreamKey) String() string {
	return fmt.Sprintf("%s/%s/%s", k.Venue, k.Symbol, k.Channel)
}

func (s Seq) Int64() int64 {
	return int64(s)
}

func (ts ServerTS) UnixMilli() int64 {
	return int64(ts)
}

func (p Price) Float64() float64 {
	return float64(p)
}

func (s Size) Float64() float64 {
	return float64(s)
}
