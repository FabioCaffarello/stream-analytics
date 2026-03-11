package stat

import (
	"log/slog"
	"marketmonkey/config"
	"marketmonkey/event"
	"marketmonkey/pkg/metrics"
	"marketmonkey/pkg/nats"
	"time"

	"github.com/anthdm/hollywood/actor"
	"github.com/fxamacker/cbor/v2"
	"github.com/google/uuid"
)

type Stat struct {
	pair     *event.Pair
	lastUnix int64

	samplers map[int64]*StatSampler
	ctx      *actor.Context
	producer *nats.NatsProducer
}

func New(pair *event.Pair) actor.Producer {
	return func() actor.Receiver {
		return &Stat{
			pair:     pair,
			samplers: make(map[int64]*StatSampler),
		}
	}
}

func (s *Stat) Receive(c *actor.Context) {
	switch msg := c.Message().(type) {
	case actor.Started:
		s.producer = c.Context().Value("producer").(*nats.NatsProducer)
		s.ctx = c
		for _, interval := range config.Intervals() {
			s.samplers[interval] = NewStatSampler(interval, s.onStat)
		}
	case *event.Stat:
		st := time.Now()
		for _, sampler := range s.samplers {
			sampler.processStatUpdate(msg)
		}
		metrics.ReportProcessStat("stat", s.pair.Exchange, st)
	case *event.Trade:
		st := time.Now()
		if msg.Unix > s.lastUnix {
			s.lastUnix = msg.Unix
		}
		for _, sampler := range s.samplers {
			sampler.processTrade(msg)
		}
		metrics.ReportProcessTrade("stat", s.pair.Exchange, st)
	case *event.LiquidationUpdate:
		st := time.Now()
		for _, sampler := range s.samplers {
			sampler.processLiquidation(msg)
		}
		metrics.ReportProcessLiquidation("stat", s.pair.Exchange, st)
	}
}

func (s *Stat) onStat(msg *event.Stat, tf int64) {
	stats := &event.Stats{
		Pair:      s.pair,
		Timeframe: tf,
		Values:    []*event.Stat{msg},
	}
	// Only store the 1 minute
	if msg.Final && tf == 60 && msg.Unix > 0 {
		s.publish(stats, nats.StreamTypeStoreStat)
		return
	}
	s.publish(stats, nats.StreamTypeRealTimeStat)
}

func (s *Stat) publish(stats *event.Stats, stream nats.StreamType) {
	b, err := cbor.Marshal(stats)
	if err != nil {
		slog.Error("failed to marshal stats", "error", err)
		return
	}

	// realtime data cant use stats.Key()
	// because it pushes updates to the same key
	// so we need to generate a new uuid for each message
	key := stats.Key()
	if stream == nats.StreamTypeRealTimeStat {
		key = uuid.NewString()
	}

	s.producer.Publish(s.ctx.Context(), nats.PublishParams{
		Subject: nats.Subject{
			StreamType: stream,
			Exchange:   s.pair.Exchange,
			Symbol:     s.pair.Symbol,
			Timeframe:  stats.Timeframe,
		}.PubString(),
		Msg:   b,
		MsgID: key,
	})
}

type StatSampler struct {
	timeframe  int64
	stat       *event.Stat
	statEnd    int64
	handleFunc func(*event.Stat, int64)
}

func NewStatSampler(tf int64, hanleFunc func(stat *event.Stat, tf int64)) *StatSampler {
	return &StatSampler{
		timeframe:  tf,
		stat:       &event.Stat{},
		handleFunc: hanleFunc,
	}
}

func (s *StatSampler) finalize() {
	if s.stat == nil {
		return
	}
	final := &event.Stat{
		Pair:      s.stat.Pair,
		Unix:      s.stat.Unix,
		LiqVsell:  s.stat.LiqVsell,
		LiqVbuy:   s.stat.LiqVbuy,
		MarkPrice: s.stat.MarkPrice,
		Funding:   s.stat.Funding,
		Tbuy:      s.stat.Tbuy,
		Tsell:     s.stat.Tsell,
		Final:     true,
	}
	s.handleFunc(final, s.timeframe)
	s.stat = nil
	s.statEnd = 0
}

func (s *StatSampler) processTrade(trade *event.Trade) {
	sec := trade.Unix / 1000

	if s.stat == nil || sec >= s.statEnd {
		s.finalize()
		start := sec - (sec % s.timeframe)
		s.stat = &event.Stat{
			Unix: start,
		}
		s.statEnd = start + s.timeframe
	}

	if trade.IsBuy {
		s.stat.Tbuy++
	} else {
		s.stat.Tsell++
	}

	partial := &event.Stat{
		Pair:      s.stat.Pair,
		Unix:      s.stat.Unix,
		LiqVsell:  s.stat.LiqVsell,
		LiqVbuy:   s.stat.LiqVbuy,
		MarkPrice: s.stat.MarkPrice,
		Funding:   s.stat.Funding,
		Tbuy:      s.stat.Tbuy,
		Tsell:     s.stat.Tsell,
		Final:     false,
	}
	s.handleFunc(partial, s.timeframe)
}

func (s *StatSampler) processStatUpdate(msg *event.Stat) {
	if msg.Funding != 0 {
		s.stat.Funding = msg.Funding
	}
	if msg.MarkPrice != 0 {
		s.stat.MarkPrice = msg.MarkPrice
	}
}

func (s *StatSampler) processLiquidation(liq *event.LiquidationUpdate) {
	if liq.IsBuy {
		s.stat.LiqVbuy += liq.Size
	} else {
		s.stat.LiqVsell += liq.Size
	}
}
