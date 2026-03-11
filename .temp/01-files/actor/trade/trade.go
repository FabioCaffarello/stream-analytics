package trade

import (
	"log/slog"
	"math"
	"time"

	"marketmonkey/common"
	"marketmonkey/config"
	"marketmonkey/event"
	"marketmonkey/pkg/metrics"
	"marketmonkey/pkg/nats"

	"github.com/anthdm/hollywood/actor"
	"github.com/fxamacker/cbor/v2"
	"github.com/google/uuid"
)

type Trade struct {
	pair      *event.Pair
	samplers  map[int64]*CandleSampler
	lastUnix  int64
	lastPrice float64
	ctx       *actor.Context
	producer  *nats.NatsProducer
}

func New(pair *event.Pair) actor.Producer {
	return func() actor.Receiver {
		return &Trade{
			pair:     pair,
			samplers: make(map[int64]*CandleSampler),
		}
	}
}

func (t *Trade) Receive(c *actor.Context) {
	switch msg := c.Message().(type) {
	case actor.Started:
		t.producer = c.Context().Value("producer").(*nats.NatsProducer)
		t.ctx = c
		for _, interval := range config.Intervals() {
			t.samplers[interval] = NewCandleSampler(interval, t.onCandle)
		}
	case *event.Trade:
		st := time.Now()
		if msg.Unix > t.lastUnix || t.lastPrice == 0 {
			t.lastUnix = msg.Unix
			t.lastPrice = msg.Price
		}
		for _, sampler := range t.samplers {
			sampler.ProcessTrades([]*event.Trade{msg})
		}
		metrics.ReportProcessTrade("trade", t.pair.Exchange, st)
	}
}

func (t *Trade) onCandle(candle *event.Candle, timeframe int64) {
	candles := &event.Candles{
		Values:    []*event.Candle{candle},
		Pair:      t.pair,
		Timeframe: timeframe,
	}

	if candle.Final && timeframe == 60 && candle.Unix > 0 {
		t.publish(candles, nats.StreamTypeStoreCandle)
	}

	t.publish(candles, nats.StreamTypeRealTimeCandle)
}

func (t *Trade) publish(candles *event.Candles, stream nats.StreamType) {
	b, err := cbor.Marshal(candles)
	if err != nil {
		slog.Error("failed to marshal candles", "error", err)
		return
	}

	// realtime data cant use candles.Key()
	// because it pushes updates to the same key
	// so we need to generate a new uuid for each message
	key := candles.Key()
	if stream == nats.StreamTypeRealTimeCandle {
		key = uuid.NewString()
	}

	t.producer.Publish(t.ctx.Context(), nats.PublishParams{
		Subject: nats.Subject{
			StreamType: stream,
			Exchange:   t.pair.Exchange,
			Symbol:     t.pair.Symbol,
			Timeframe:  candles.Timeframe,
		}.PubString(),
		Msg:   b,
		MsgID: key,
	})
}

type CandleSampler struct {
	timeframe  int64
	candle     *event.Candle
	handleFunc func(*event.Candle, int64)
	candleEnd  int64
}

func NewCandleSampler(timeframe int64, fn func(c *event.Candle, tf int64)) *CandleSampler {
	return &CandleSampler{
		timeframe:  timeframe,
		candle:     &event.Candle{},
		handleFunc: fn,
	}
}

func (s *CandleSampler) finalizeCandle() {
	if s.candle == nil {
		return
	}
	final := &event.Candle{
		Unix:  s.candle.Unix,
		Open:  s.candle.Open,
		Close: s.candle.Close,
		High:  s.candle.High,
		Low:   s.candle.Low,
		Vbuy:  s.candle.Vbuy,
		Vsell: s.candle.Vsell,
		Tbuy:  s.candle.Tbuy,
		Tsell: s.candle.Tsell,
		Final: true,
	}
	s.handleFunc(final, s.timeframe)
	s.candle = nil
	s.candleEnd = 0
}

func (s *CandleSampler) ProcessTrades(trades []*event.Trade) {
	for _, trade := range trades {
		sec := trade.Unix / 1000
		if s.candle == nil || sec >= s.candleEnd {
			s.finalizeCandle()
			start := sec - (sec % s.timeframe)
			s.candle = &event.Candle{
				Unix:  start,
				Open:  trade.Price,
				Close: trade.Price,
				High:  trade.Price,
				Low:   trade.Price,
			}
			s.candleEnd = start + s.timeframe
		}

		s.candle.Close = trade.Price
		s.candle.High = math.Max(s.candle.High, trade.Price)
		s.candle.Low = math.Min(s.candle.Low, trade.Price)
		if trade.IsBuy {
			s.candle.Vbuy = common.Round(s.candle.Vbuy + trade.Qty)
			s.candle.Tbuy++
		} else {
			s.candle.Vsell = common.Round(s.candle.Vsell + trade.Qty)
			s.candle.Tsell++
		}

		partial := &event.Candle{
			Unix:  s.candle.Unix,
			Open:  s.candle.Open,
			Close: s.candle.Close,
			High:  s.candle.High,
			Low:   s.candle.Low,
			Vbuy:  s.candle.Vbuy,
			Vsell: s.candle.Vsell,
			Tbuy:  s.candle.Tbuy,
			Tsell: s.candle.Tsell,
			Final: false,
		}
		s.handleFunc(partial, s.timeframe)
	}
}
