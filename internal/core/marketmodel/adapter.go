package marketmodel

import (
	"strings"

	"github.com/market-raccoon/internal/shared/problem"
)

type ExchangeAdapter interface {
	Venue() Venue
	Precision(symbol Symbol) PrecisionRule
	NormalizeTimestamp(raw int64, fallback ServerTS) ServerTS
	NormalizeSide(raw string) (Side, *problem.Problem)
}

type StaticExchangeAdapter struct {
	venue       Venue
	defaultRule PrecisionRule
	rules       map[Symbol]PrecisionRule
}

func NewStaticExchangeAdapter(venue string, defaultRule PrecisionRule, rules map[string]PrecisionRule) (*StaticExchangeAdapter, *problem.Problem) {
	v, p := NewVenue(venue)
	if p != nil {
		return nil, p
	}
	out := &StaticExchangeAdapter{
		venue:       v,
		defaultRule: defaultRule,
		rules:       make(map[Symbol]PrecisionRule, len(rules)),
	}
	for k, rule := range rules {
		s, p := NewSymbol(k)
		if p != nil {
			return nil, p
		}
		out.rules[s] = rule
	}
	return out, nil
}

func (a *StaticExchangeAdapter) Venue() Venue {
	return a.venue
}

func (a *StaticExchangeAdapter) Precision(symbol Symbol) PrecisionRule {
	if a == nil {
		return PrecisionRule{PriceDecimals: 8, SizeDecimals: 8}
	}
	if r, ok := a.rules[symbol]; ok {
		return r
	}
	return a.defaultRule
}

func (a *StaticExchangeAdapter) NormalizeTimestamp(raw int64, fallback ServerTS) ServerTS {
	if raw > 0 {
		return ServerTS(raw)
	}
	if fallback > 0 {
		return fallback
	}
	return 1
}

func (a *StaticExchangeAdapter) NormalizeSide(raw string) (Side, *problem.Problem) {
	s := strings.ToLower(strings.TrimSpace(raw))
	switch s {
	case "buy", "bid", "b":
		return SideBuy, nil
	case "sell", "ask", "a", "s":
		return SideSell, nil
	default:
		return "", problem.Newf(problem.ValidationFailed, "unsupported side %q for venue %s", raw, a.venue)
	}
}

type AdapterRegistry struct {
	adapters map[Venue]ExchangeAdapter
}

func NewAdapterRegistry(adapters ...ExchangeAdapter) *AdapterRegistry {
	out := &AdapterRegistry{adapters: make(map[Venue]ExchangeAdapter, len(adapters))}
	for _, adapter := range adapters {
		if adapter == nil {
			continue
		}
		out.adapters[adapter.Venue()] = adapter
	}
	return out
}

func (r *AdapterRegistry) Resolve(venue string) ExchangeAdapter {
	v, p := NewVenue(venue)
	if p != nil {
		return defaultAdapter()
	}
	if r != nil {
		if a, ok := r.adapters[v]; ok {
			return a
		}
	}
	return defaultAdapterForVenue(v)
}

func defaultAdapter() ExchangeAdapter {
	a, _ := NewStaticExchangeAdapter("unknown", PrecisionRule{PriceDecimals: 8, SizeDecimals: 8}, nil)
	return a
}

func defaultAdapterForVenue(venue Venue) ExchangeAdapter {
	rules := map[string]PrecisionRule{
		"BTC-USDT": {PriceDecimals: 2, SizeDecimals: 6},
		"ETH-USDT": {PriceDecimals: 2, SizeDecimals: 6},
		"SOL-USDT": {PriceDecimals: 3, SizeDecimals: 4},
	}
	defaultRule := PrecisionRule{PriceDecimals: 8, SizeDecimals: 8}
	switch venue {
	case "BINANCE", "BYBIT", "COINBASE", "KRAKEN", "KRAKENF", "HYPERLIQUID":
		defaultRule = PrecisionRule{PriceDecimals: 8, SizeDecimals: 8}
	}
	a, p := NewStaticExchangeAdapter(string(venue), defaultRule, rules)
	if p != nil {
		return defaultAdapter()
	}
	return a
}
