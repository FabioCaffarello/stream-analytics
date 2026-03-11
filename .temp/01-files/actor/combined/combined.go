package combined

import (
	"marketmonkey/event"
	"marketmonkey/types"
	"time"

	"github.com/anthdm/hollywood/actor"
	"github.com/tidwall/btree"
)

type Orderbook struct {
	pair       *event.Pair
	asks       *btree.Map[float64, float64]
	bids       *btree.Map[float64, float64]
	lastPrice  float64
	upperPrice float64
	lowerPrice float64

	publishPID *actor.PID
	lastUnix   int64
}

func NewOrderbook(pair *event.Pair) actor.Producer {
	return func() actor.Receiver {
		return &Orderbook{
			pair: pair,
			asks: btree.NewMap[float64, float64](0),
			bids: btree.NewMap[float64, float64](0),
		}
	}
}

type tickHeatmap struct{}

func (o *Orderbook) Receive(c *actor.Context) {
	switch msg := c.Message().(type) {
	case actor.Started:
		c.SendRepeat(c.PID(), types.Tick{}, time.Millisecond*300)
		c.SendRepeat(c.PID(), tickHeatmap{}, time.Millisecond*300)
		o.publishPID = c.Parent().Child("publish/" + o.pair.Symbol)
	case *event.Trade:
		if o.lastPrice == 0 {
			o.lastPrice = msg.Price
			o.calculateDepth()
		}
		o.lastPrice = msg.Price
	case *event.BookUpdate:
		if o.lastPrice > 0 {
			o.processUpdate(msg)
		}
		o.lastUnix = msg.Unix
	case types.Tick:
		o.publish(c)
	case tickHeatmap:
		o.publishHeatmap(c)
	}
}

// TODO: this depends on the symbol I guess...
func (o *Orderbook) calculateDepth() {
	o.upperPrice = o.lastPrice + 2000
	o.lowerPrice = o.lastPrice - 2000
}

func (o *Orderbook) processUpdate(msg *event.BookUpdate) {
	for _, ask := range msg.Asks {
		if ask.Size == 0 {
			o.asks.Delete(ask.Price)
			continue
		}
		if ask.Price <= o.upperPrice && ask.Price >= o.lowerPrice {
			o.asks.Set(ask.Price, ask.Size)
		}
	}
	for _, bid := range msg.Bids {
		if bid.Size == 0 {
			o.bids.Delete(bid.Price)
			continue
		}
		if bid.Price <= o.upperPrice && bid.Price >= o.lowerPrice {
			o.bids.Set(bid.Price, bid.Size)
		}
	}
}

func (o *Orderbook) publishHeatmap(c *actor.Context) {
	if o.asks.Len() == 0 || o.bids.Len() == 0 {
		return
	}

	// msg := o.calculateHeatmap()

	///c.Send(o.publishPID, msg)
}

func (o *Orderbook) publish(c *actor.Context) {
	if o.asks.Len() == 0 || o.bids.Len() == 0 {
		return
	}
	msg := &event.Orderbook{
		Pair:      o.pair,
		LastPrice: o.lastPrice,
		AskPrices: make([]float64, 0),
		AskSizes:  make([]float64, 0),
		BidPrices: make([]float64, 0),
		BidSizes:  make([]float64, 0),
	}
	depth := 7
	i := 0
	sum := 0.0
	o.bids.Descend(1000000, func(price float64, size float64) bool {
		if i == depth {
			return false
		}
		sum += size
		msg.BidPrices = append(msg.BidPrices, price)
		msg.BidSizes = append(msg.BidSizes, size)
		i++
		return true
	})
	sum = 0
	i = 0
	o.asks.Ascend(0, func(price float64, size float64) bool {
		if i == depth {
			return false
		}
		sum += size
		msg.AskPrices = append(msg.AskPrices, price)
		msg.AskSizes = append(msg.AskSizes, size)
		i++
		return true
	})
	c.Send(o.publishPID, msg)
}
