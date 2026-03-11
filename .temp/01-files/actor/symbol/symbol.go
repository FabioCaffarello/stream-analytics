package symbol

import (
	"marketmonkey/actor/orderbook"
	"marketmonkey/actor/stat"
	"marketmonkey/actor/trade"
	"marketmonkey/actor/volume"
	"marketmonkey/event"
	"marketmonkey/pkg/tick"
	"marketmonkey/types"
	"time"

	"github.com/anthdm/hollywood/actor"
)

type Symbol struct {
	pair       *event.Pair
	statPID    *actor.PID
	bookPID    *actor.PID
	publishPID *actor.PID
	tradePID   *actor.PID
	volumePID  *actor.PID
	ctx        *actor.Context
	tickers    *tick.Tickers
}

func New(pair *event.Pair) actor.Producer {
	return func() actor.Receiver {
		return &Symbol{
			pair:    pair,
			tickers: tick.NewTickers(),
		}
	}
}

func (s *Symbol) Receive(c *actor.Context) {
	switch c.Message().(type) {
	case actor.Started:
		s.start(c)
		s.startTickers()
	case *event.Trade:
		c.Forward(s.bookPID)
		c.Forward(s.statPID)
		c.Forward(s.tradePID)
		c.Forward(s.volumePID)
	case *event.Stat:
		c.Forward(s.statPID)
	case *event.BookUpdate:
		c.Forward(s.bookPID)
	case *event.LiquidationUpdate:
		c.Forward(s.statPID)
	case actor.Stopped:
		s.tickers.Stop()
	}
}

func (s *Symbol) startTickers() {
	s.tickers.AddTicker(time.Minute*1, func(t time.Time) {
		s.ctx.Send(s.bookPID, types.Tick{Value: 60, T: t})
	})
	s.tickers.Start()
}

func (s *Symbol) start(c *actor.Context) {
	s.ctx = c
	s.statPID = c.SpawnChild(stat.New(s.pair), "stat", actor.WithID(s.pair.Symbol), actor.WithContext(c.Context()))
	s.bookPID = c.SpawnChild(orderbook.New(s.pair), "book", actor.WithID(s.pair.Symbol), actor.WithContext(c.Context()))
	s.tradePID = c.SpawnChild(trade.New(s.pair), "trade", actor.WithID(s.pair.Symbol), actor.WithContext(c.Context()))
	s.volumePID = c.SpawnChild(volume.New(s.pair), "volume", actor.WithID(s.pair.Symbol), actor.WithContext(c.Context()))
}
