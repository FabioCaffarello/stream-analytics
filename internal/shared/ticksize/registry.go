package ticksize

import (
	"math"
	"sort"
	"strings"

	"github.com/market-raccoon/internal/shared/problem"
)

// TickSpec defines deterministic display-grouping bounds.
type TickSpec struct {
	MinPrice float64
	MaxPrice float64
	TickSize float64
}

// Registry holds per-venue tick specs plus a fallback strategy.
type Registry struct {
	venues   map[string][]TickSpec
	fallback func(price float64) float64
}

// NewRegistry creates a registry with deterministic default venue specs.
func NewRegistry() *Registry {
	r := &Registry{
		venues:   make(map[string][]TickSpec, 4),
		fallback: AutoGroupSize,
	}
	r.mustRegisterDefaults()
	return r
}

// RegisterVenue adds/overwrites a venue tick table. Specs must be sorted and valid.
func (r *Registry) RegisterVenue(venue string, specs []TickSpec) *problem.Problem {
	if r == nil {
		return problem.New(problem.ValidationFailed, "ticksize registry must not be nil")
	}
	venue = strings.ToLower(strings.TrimSpace(venue))
	if venue == "" {
		return problem.New(problem.ValidationFailed, "ticksize venue must not be empty")
	}
	if len(specs) == 0 {
		return problem.New(problem.ValidationFailed, "ticksize specs must not be empty")
	}
	if p := validateSpecs(specs); p != nil {
		return p
	}
	copied := make([]TickSpec, len(specs))
	copy(copied, specs)
	r.venues[venue] = copied
	return nil
}

// GroupSizeForPrice returns venue-specific group size or fallback when no match exists.
func (r *Registry) GroupSizeForPrice(venue string, price float64) float64 {
	if r == nil {
		return AutoGroupSize(price)
	}
	venue = strings.ToLower(strings.TrimSpace(venue))
	specs, ok := r.venues[venue]
	if !ok || len(specs) == 0 {
		return r.fallbackValue(price)
	}
	if !isFinite(price) || price < 0 {
		return r.fallbackValue(price)
	}
	idx := sort.Search(len(specs), func(i int) bool {
		return specs[i].MinPrice > price
	})
	if idx > 0 {
		s := specs[idx-1]
		if price >= s.MinPrice && (s.MaxPrice <= 0 || price < s.MaxPrice) {
			return s.TickSize
		}
	}
	return r.fallbackValue(price)
}

// AutoGroupSize mirrors client orderbook_auto_price_group:
// target = price * 0.0001; group = 10^floor(log10(target)).
func AutoGroupSize(price float64) float64 {
	if !isFinite(price) || price <= 0 {
		return 1
	}
	target := price * 0.0001
	if !isFinite(target) || target <= 0 {
		return 1
	}
	exp := math.Floor(math.Log10(target))
	group := math.Pow(10, exp)
	if !isFinite(group) || group <= 0 {
		return 1
	}
	return group
}

func (r *Registry) fallbackValue(price float64) float64 {
	if r.fallback == nil {
		return AutoGroupSize(price)
	}
	value := r.fallback(price)
	if !isFinite(value) || value <= 0 {
		return 1
	}
	return value
}

func validateSpecs(specs []TickSpec) *problem.Problem {
	for i := range specs {
		s := specs[i]
		if !isFinite(s.MinPrice) || s.MinPrice < 0 {
			return problem.Newf(problem.ValidationFailed, "ticksize spec[%d].min_price must be finite and >= 0", i)
		}
		if !isFinite(s.MaxPrice) {
			return problem.Newf(problem.ValidationFailed, "ticksize spec[%d].max_price must be finite", i)
		}
		if s.MaxPrice > 0 && s.MaxPrice <= s.MinPrice {
			return problem.Newf(problem.ValidationFailed, "ticksize spec[%d].max_price must be > min_price", i)
		}
		if !isFinite(s.TickSize) || s.TickSize <= 0 {
			return problem.Newf(problem.ValidationFailed, "ticksize spec[%d].tick_size must be finite and > 0", i)
		}
		if i > 0 && specs[i-1].MinPrice >= s.MinPrice {
			return problem.Newf(problem.ValidationFailed, "ticksize specs must be sorted by min_price ascending")
		}
	}
	return nil
}

func (r *Registry) mustRegisterDefaults() {
	_ = r.RegisterVenue("binance", []TickSpec{
		{MinPrice: 0, MaxPrice: 0.001, TickSize: 0.000001},
		{MinPrice: 0.001, MaxPrice: 0.01, TickSize: 0.00001},
		{MinPrice: 0.01, MaxPrice: 0.1, TickSize: 0.0001},
		{MinPrice: 0.1, MaxPrice: 1, TickSize: 0.001},
		{MinPrice: 1, MaxPrice: 10, TickSize: 0.01},
		{MinPrice: 10, MaxPrice: 100, TickSize: 0.1},
		{MinPrice: 100, MaxPrice: 1000, TickSize: 1},
		{MinPrice: 1000, MaxPrice: 10000, TickSize: 10},
		{MinPrice: 10000, MaxPrice: 100000, TickSize: 10},
		{MinPrice: 100000, MaxPrice: 0, TickSize: 100},
	})
	_ = r.RegisterVenue("bybit", []TickSpec{
		{MinPrice: 0, MaxPrice: 0.001, TickSize: 0.000001},
		{MinPrice: 0.001, MaxPrice: 0.01, TickSize: 0.00001},
		{MinPrice: 0.01, MaxPrice: 0.1, TickSize: 0.0001},
		{MinPrice: 0.1, MaxPrice: 1, TickSize: 0.001},
		{MinPrice: 1, MaxPrice: 10, TickSize: 0.01},
		{MinPrice: 10, MaxPrice: 100, TickSize: 0.1},
		{MinPrice: 100, MaxPrice: 1000, TickSize: 1},
		{MinPrice: 1000, MaxPrice: 10000, TickSize: 10},
		{MinPrice: 10000, MaxPrice: 100000, TickSize: 10},
		{MinPrice: 100000, MaxPrice: 0, TickSize: 100},
	})
	_ = r.RegisterVenue("coinbase", []TickSpec{
		{MinPrice: 0, MaxPrice: 1, TickSize: 0.0001},
		{MinPrice: 1, MaxPrice: 100, TickSize: 0.01},
		{MinPrice: 100, MaxPrice: 10000, TickSize: 0.01},
		{MinPrice: 10000, MaxPrice: 0, TickSize: 1},
	})
}

func isFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
