package domain

import (
	"strings"

	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/problem"
)

const DefaultTimeframe = "raw"

// Subject is the canonical delivery routing key.
//
// Format: <stream_type>/<venue>/<symbol>/<timeframe>
// Example: marketdata.trade/binance/BTC-USDT/1m
//
// stream_type may include dots because envelope types are namespaced.
type Subject struct {
	StreamType string
	Venue      string
	Symbol     string
	Timeframe  string
}

func ParseSubject(raw string) (Subject, *problem.Problem) {
	parts := strings.Split(strings.TrimSpace(raw), "/")
	if len(parts) != 4 {
		return Subject{}, problem.Newf(problem.ValidationFailed,
			"subject must have 4 segments <stream_type>/<venue>/<symbol>/<timeframe>, got %q", raw,
		)
	}
	return NewSubject(parts[0], parts[1], parts[2], parts[3])
}

func NewSubject(streamType, venue, symbol, timeframe string) (Subject, *problem.Problem) {
	streamType = strings.ToLower(strings.TrimSpace(streamType))
	venue = strings.ToLower(strings.TrimSpace(venue))
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	timeframe = strings.ToLower(strings.TrimSpace(timeframe))

	if streamType == "" {
		return Subject{}, problem.New(problem.ValidationFailed, "subject stream_type must not be empty")
	}
	if venue == "" {
		return Subject{}, problem.New(problem.ValidationFailed, "subject venue must not be empty")
	}
	if symbol == "" {
		return Subject{}, problem.New(problem.ValidationFailed, "subject symbol must not be empty")
	}
	if timeframe == "" {
		timeframe = DefaultTimeframe
	}

	return Subject{
		StreamType: streamType,
		Venue:      venue,
		Symbol:     symbol,
		Timeframe:  timeframe,
	}, nil
}

func SubjectFromEnvelope(env envelope.Envelope, timeframe string) (Subject, *problem.Problem) {
	return NewSubject(env.Type, env.Venue, env.Instrument, timeframe)
}

func (s Subject) String() string {
	return s.StreamType + "/" + s.Venue + "/" + s.Symbol + "/" + s.Timeframe
}
