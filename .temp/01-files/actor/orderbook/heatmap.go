package orderbook

import (
	"math"
	"sort"

	"marketmonkey/common"
	"marketmonkey/event"
)

func (o *Orderbook) calculateHeatmap() *event.Heatmap {
	if o.priceGroup == 0 || o.bids.Len() == 0 || o.asks.Len() == 0 {
		return nil
	}

	var (
		unix    = o.lastUnix / 1000
		m       = make(map[float64]float64, o.bids.Len()+o.asks.Len())
		maxSize = 0.0
		minSize = math.MaxFloat64
	)

	bids := o.bids.Iter()
	for bids.Next() {
		price := common.RoundDown(bids.Key(), o.priceGroup)
		size := bids.Value()
		m[price] = common.Round(m[price] + size)
		if size > maxSize {
			maxSize = size
		}
		if size < minSize {
			minSize = size
		}
	}

	asks := o.asks.Iter()
	for asks.Next() {
		price := common.RoundDown(asks.Key(), o.priceGroup)
		size := asks.Value()
		m[price] = common.Round(m[price] + size)
		if size > maxSize {
			maxSize = size
		}
		if size < minSize {
			minSize = size
		}
	}

	keys := make([]float64, len(m))
	it := 0
	for price := range m {
		keys[it] = price
		it++
	}

	sort.Float64s(keys)

	hm := &event.Heatmap{
		PriceGroup: o.priceGroup,
		Pair:       o.pair,
		Prices:     make([]float64, len(m)),
		Sizes:      make([]float64, len(m)),
		Unix:       unix,
	}

	for i := 0; i < len(m); i++ {
		hm.Prices[i] = keys[i]
		hm.Sizes[i] = m[keys[i]]
	}

	hm.MinPrice = keys[0]
	hm.MaxPrice = keys[len(keys)-1]
	hm.MaxSize = maxSize
	hm.MinSize = minSize

	return hm
}
