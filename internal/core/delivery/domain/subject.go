package domain

import (
	"strings"

	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/naming"
	"github.com/market-raccoon/internal/shared/problem"
)

const DefaultTimeframe = "raw"

const (
	signalStreamType         = "signal"
	signalCompositeEventType = "signal.composite"
	unknownSignalSubjectKind = "unknown"
)

// Subject is the canonical delivery routing key.
//
// Format: <stream_type>/<venue>/<symbol>/<timeframe>
// Example: marketdata.trade/binance/BTC-USDT/1m
//
// stream_type may include dots because envelope types are namespaced.
type Subject struct {
	StreamType string
	Kind       string
	Venue      string
	Symbol     string
	Timeframe  string
}

func ParseSubject(raw string) (Subject, *problem.Problem) {
	parts := strings.Split(strings.TrimSpace(raw), "/")
	if len(parts) == 5 && strings.EqualFold(strings.TrimSpace(parts[0]), signalStreamType) {
		return NewSignalSubject(parts[1], parts[2], parts[3], parts[4])
	}
	if len(parts) != 4 {
		return Subject{}, problem.Newf(problem.ValidationFailed,
			"subject must have 4 segments <stream_type>/<venue>/<symbol>/<timeframe> or 5 segments signal/<kind>/<venue>/<symbol>/<timeframe>, got %q", raw,
		)
	}
	return NewSubject(parts[0], parts[1], parts[2], parts[3])
}

func NewSubject(streamType, venue, symbol, timeframe string) (Subject, *problem.Problem) {
	streamType = strings.ToLower(strings.TrimSpace(streamType))
	venue = strings.ToLower(strings.TrimSpace(venue))
	symbol = NormalizeSymbol(symbol)
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
		Kind:       "",
		Venue:      venue,
		Symbol:     symbol,
		Timeframe:  timeframe,
	}, nil
}

func NewSignalSubject(kind, venue, symbol, timeframe string) (Subject, *problem.Problem) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "" {
		return Subject{}, problem.New(problem.ValidationFailed, "subject signal kind must not be empty")
	}
	venue = strings.ToLower(strings.TrimSpace(venue))
	symbol = NormalizeSymbol(symbol)
	timeframe = strings.ToLower(strings.TrimSpace(timeframe))
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
		StreamType: signalStreamType,
		Kind:       kind,
		Venue:      venue,
		Symbol:     symbol,
		Timeframe:  timeframe,
	}, nil
}

// NormalizeSymbol converts WS symbol tokens to canonical event-bus instrument
// representation for deterministic routing and parity checks.
func NormalizeSymbol(raw string) string {
	return naming.CanonicalInstrument(raw)
}

// IsInstrumentSymbolEquivalent reports whether WS symbol and bus instrument
// refer to the same canonical token.
func IsInstrumentSymbolEquivalent(symbol, instrument string) bool {
	return NormalizeSymbol(symbol) == NormalizeSymbol(instrument)
}

func SubjectFromEnvelope(env envelope.Envelope, timeframe string) (Subject, *problem.Problem) {
	if strings.EqualFold(strings.TrimSpace(env.Type), signalCompositeEventType) {
		kind := unknownSignalSubjectKind
		if len(env.Meta) > 0 {
			if rawKind := strings.ToLower(strings.TrimSpace(env.Meta["kind"])); rawKind != "" {
				kind = rawKind
			}
		}
		return NewSignalSubject(kind, env.Venue, env.Instrument, timeframe)
	}
	return NewSubject(env.Type, env.Venue, env.Instrument, timeframe)
}

// TimeframeFromEnvelopeMeta returns the timeframe from env.Meta["timeframe"]
// if present, otherwise returns the provided fallback.
func TimeframeFromEnvelopeMeta(env envelope.Envelope, fallback string) string {
	if len(env.Meta) > 0 {
		if tf := strings.TrimSpace(env.Meta["timeframe"]); tf != "" {
			return tf
		}
	}
	return fallback
}

func (s Subject) String() string {
	if s.IsSignal() {
		kind := strings.ToLower(strings.TrimSpace(s.Kind))
		if kind == "" {
			kind = unknownSignalSubjectKind
		}
		return signalStreamType + "/" + kind + "/" + s.Venue + "/" + s.Symbol + "/" + s.Timeframe
	}
	return s.StreamType + "/" + s.Venue + "/" + s.Symbol + "/" + s.Timeframe
}

func (s Subject) IsSignal() bool {
	return strings.EqualFold(strings.TrimSpace(s.StreamType), signalStreamType)
}
