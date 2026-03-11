package volume

import (
	"log"
	"log/slog"
	"marketmonkey/common"
	"marketmonkey/config"
	"marketmonkey/event"
	"marketmonkey/pkg/metrics"
	"marketmonkey/pkg/nats"
	"time"

	"github.com/anthdm/hollywood/actor"
	"github.com/fxamacker/cbor/v2"
	"github.com/google/uuid"
)

type publishVolumes struct{}

type VSB struct {
	Vsell float64
	Vbuy  float64
}

type Volume struct {
	pair       *event.Pair
	priceGroup float64
	tickSize   float64
	volumes    map[float64]VSB
	ctx        *actor.Context
	producer   *nats.NatsProducer
	samplers   map[int64]*VolumeSampler
}

func New(pair *event.Pair) actor.Producer {
	return func() actor.Receiver {
		market, ok := config.GetMarket(pair.Exchange)
		if !ok {
			log.Fatal("failed to get market from config")
		}
		symbol, ok := market.GetSymbol(pair.Symbol)
		if !ok {
			log.Fatal("failed to get symbol from config")
		}
		return &Volume{
			pair:     pair,
			volumes:  map[float64]VSB{},
			tickSize: symbol.TickSize,
			samplers: make(map[int64]*VolumeSampler),
		}
	}
}

func (v *Volume) Receive(c *actor.Context) {
	switch msg := c.Message().(type) {
	case actor.Started:
		v.producer = c.Context().Value("producer").(*nats.NatsProducer)
		v.ctx = c
		for _, interval := range config.Intervals() {
			v.samplers[interval] = NewVolumeSampler(v.pair, interval, v.onVolume)
		}
		c.SendRepeat(c.PID(), publishVolumes{}, time.Millisecond*250)
	case *event.Trade:
		st := time.Now()
		v.processTrade(msg)
		metrics.ReportProcessTrade("volume", v.pair.Exchange, st)
	case publishVolumes:
		for _, sampler := range v.samplers {
			if sampler.volume == nil {
				return
			}
			m := &event.Volume{
				Pair:       sampler.volume.Pair,
				Unix:       sampler.volume.Unix,
				Timeframe:  sampler.volume.Timeframe,
				Prices:     append([]float64{}, sampler.volume.Prices...),
				Buys:       append([]float64{}, sampler.volume.Buys...),
				Sells:      append([]float64{}, sampler.volume.Sells...),
				PriceGroup: sampler.volume.PriceGroup,
				Final:      sampler.volume.Final,
			}
			volumes := &event.Volumes{
				Pair:      v.pair,
				Timeframe: sampler.interval,
				Values:    []*event.Volume{m},
			}
			v.publish(volumes, nats.StreamTypeRealTimeVolume)
		}
	}
}

func (v *Volume) processTrade(trade *event.Trade) {
	if v.priceGroup == 0 {
		v.priceGroup = common.CalculateVolumeBinSize(trade.Price, v.tickSize)
	}

	for _, sampler := range v.samplers {
		sampler.processTrade(trade, v.priceGroup)
	}
}

func (v *Volume) onVolume(volume *event.Volume, timeframe int64) {
	volumes := &event.Volumes{
		Pair:      v.pair,
		Timeframe: timeframe,
		Values:    []*event.Volume{volume},
	}

	if volume.Final {
		v.publish(volumes, nats.StreamTypeRealTimeVolume)
	}

	if volume.Final && timeframe == 60 && volume.Unix > 0 {
		v.publish(volumes, nats.StreamTypeStoreVolume)
	}

}

type VolumeSampler struct {
	pair      *event.Pair
	interval  int64
	volume    *event.Volume
	handler   func(*event.Volume, int64)
	volumeEnd int64
}

func NewVolumeSampler(pair *event.Pair, interval int64, handler func(*event.Volume, int64)) *VolumeSampler {
	return &VolumeSampler{
		pair:     pair,
		interval: interval,
		handler:  handler,
	}
}

func (vs *VolumeSampler) finalize() {
	if vs.volume == nil {
		return
	}
	final := &event.Volume{
		Pair:       vs.volume.Pair,
		Unix:       vs.volume.Unix,
		Timeframe:  vs.volume.Timeframe,
		Prices:     append([]float64{}, vs.volume.Prices...),
		Buys:       append([]float64{}, vs.volume.Buys...),
		Sells:      append([]float64{}, vs.volume.Sells...),
		PriceGroup: vs.volume.PriceGroup,
		Final:      true,
	}
	vs.handler(final, vs.interval)
	vs.volume = nil
	vs.volumeEnd = 0
}

func (vs *VolumeSampler) processTrade(trade *event.Trade, pg float64) {
	sec := trade.Unix / 1000
	if vs.volume == nil || sec >= vs.volumeEnd {
		vs.finalize()
		start := sec - (sec % vs.interval)
		vs.volume = &event.Volume{
			Pair:       vs.pair,
			Unix:       start,
			Prices:     []float64{},
			Buys:       []float64{},
			Sells:      []float64{},
			Timeframe:  vs.interval,
			PriceGroup: pg,
		}
		vs.volumeEnd = start + vs.interval
	}

	price := common.RoundDown(trade.Price, pg)
	addVolume := func(vbuy, vsell float64) {
		if i := common.IndexOfFloats(vs.volume.Prices, price); i != -1 {
			vs.volume.Buys[i] = common.Round(vs.volume.Buys[i] + vbuy)
			vs.volume.Sells[i] = common.Round(vs.volume.Sells[i] + vsell)
		} else {
			vs.volume.Prices = append(vs.volume.Prices, price)
			vs.volume.Buys = append(vs.volume.Buys, vbuy)
			vs.volume.Sells = append(vs.volume.Sells, vsell)
		}
	}

	if trade.IsBuy {
		addVolume(trade.Qty, 0)
	} else {
		addVolume(0, trade.Qty)
	}

	partial := &event.Volume{
		Pair:       vs.volume.Pair,
		Unix:       vs.volume.Unix,
		Timeframe:  vs.volume.Timeframe,
		Prices:     append([]float64{}, vs.volume.Prices...),
		Buys:       append([]float64{}, vs.volume.Buys...),
		Sells:      append([]float64{}, vs.volume.Sells...),
		PriceGroup: vs.volume.PriceGroup,
		Final:      false,
	}
	vs.handler(partial, vs.interval)
}

func (v *Volume) publish(volumes *event.Volumes, stream nats.StreamType) {
	b, err := cbor.Marshal(volumes)
	if err != nil {
		slog.Error("failed to marshal volumes", "error", err)
		return
	}

	// realtime data cant use volumes.Key()
	// because it pushes updates to the same key
	// so we need to generate a new uuid for each message
	key := volumes.Key()
	if stream == nats.StreamTypeRealTimeVolume {
		key = uuid.NewString()
	}

	v.producer.Publish(v.ctx.Context(), nats.PublishParams{
		Subject: nats.Subject{
			StreamType: stream,
			Exchange:   v.pair.Exchange,
			Symbol:     v.pair.Symbol,
			Timeframe:  volumes.Timeframe,
		}.PubString(),
		Msg:   b,
		MsgID: key,
	})
}
