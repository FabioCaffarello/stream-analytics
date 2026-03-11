package processor

import (
	"context"
	"fmt"
	"log/slog"
	"marketmonkey/actor/symbol"
	"marketmonkey/config"
	"marketmonkey/event"
	"marketmonkey/pkg/metrics"
	"marketmonkey/pkg/nats"
	"strings"

	"github.com/anthdm/hollywood/actor"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
)

func consumerRegistry(p *Processor) []event.Registry {
	return []event.Registry{
		{
			Stream: nats.StreamTypeTrade,
			Fn:     event.CreateStreamHandler(p.handleTrade),
		},
		{
			Stream: nats.StreamTypeBookUpdate,
			Fn:     event.CreateStreamHandler(p.handleOrderbook),
		},
		{
			Stream: nats.StreamTypePreStat,
			Fn:     event.CreateStreamHandler(p.handlePreStat),
		},
		{
			Stream: nats.StreamTypeLiquidation,
			Fn:     event.CreateStreamHandler(p.handleLiquidation),
		},
	}
}

// producerStreams the processor will produce to
var producerStreams = []nats.StreamType{
	nats.StreamTypeRealTimeCandle,
	nats.StreamTypeRealTimeHeatmap,
	nats.StreamTypeRealTimeStat,
	nats.StreamTypeRealTimeVolume,
	nats.StreamTypeRealTimeOrderbook,
	nats.StreamTypeStoreCandle,
	nats.StreamTypeStoreHeatmap,
	nats.StreamTypeStoreStat,
	nats.StreamTypeStoreVolume,
}

type Processor struct {
	exchange     string
	symbols      map[string]*actor.PID
	quitch       chan struct{}
	ctx          *actor.Context
	natsConsumer *nats.NatsConsumer
	natsProducer *nats.NatsProducer

	// defaults to true, only used for backfilling
	// where we want to handle consumption manually
	consumeFromNats bool

	metrics *metrics.MetricsServer
}

func New(exchange string, opt ...bool) actor.Producer {
	consumeFromNats := true
	if len(opt) > 0 {
		consumeFromNats = opt[0]
	}
	return func() actor.Receiver {
		return &Processor{
			exchange:        exchange,
			quitch:          make(chan struct{}),
			symbols:         make(map[string]*actor.PID),
			consumeFromNats: consumeFromNats,
		}
	}
}

func (p *Processor) Receive(c *actor.Context) {
	switch msg := c.Message().(type) {
	case actor.Started:
		p.ctx = c
		p.setupMetrics()
		p.start(c)
	case *event.Trade:
		pid := p.symbols[strings.ToUpper(msg.Pair.Symbol)]
		c.Forward(pid)
	case *event.BookUpdate:
		pid := p.symbols[strings.ToUpper(msg.Pair.Symbol)]
		c.Forward(pid)
	case *event.LiquidationUpdate:
		pid := p.symbols[strings.ToUpper(msg.Pair.Symbol)]
		c.Forward(pid)
	case *event.Stat:
		pid := p.symbols[strings.ToUpper(msg.Pair.Symbol)]
		c.Forward(pid)
	case actor.Stopped:
		p.stop()
	}
}

func (p *Processor) setupMetrics() {
	serviceID := fmt.Sprintf("processor-%s-%s", p.exchange, uuid.New().String())
	ms, err := metrics.NewMetricsServer(metrics.Config{
		Tags:      []string{"processor", p.exchange},
		ServiceID: serviceID,
	}, p.quitch)
	if err != nil {
		slog.Error("failed to create metrics server", "error", err)
	}
	p.metrics = ms

	if err := ms.Start(); err != nil {
		slog.Error("failed to start metrics server", "error", err)
	}

	p.metrics.RegisterAll(metrics.ProcessorMetrics...)
}

func (p *Processor) start(c *actor.Context) {
	market, ok := config.GetMarket(p.exchange)
	if !ok {
		slog.Error("exchange not found", "exchange", p.exchange)
		panic(fmt.Errorf("exchange not found: %s", p.exchange))
	}

	p.natsProducer = nats.NewNatsProducer(p.getJetstreamConfig())
	if err := p.natsProducer.Setup(); err != nil {
		slog.Error("failed to setup nats", "error", err)
		panic(err)
	}

	ctx := context.WithValue(c.Context(), "producer", p.natsProducer)

	for _, sym := range market.Symbols {
		pair := event.NewPair(p.exchange, sym.Ticker)
		pid := c.SpawnChild(symbol.New(pair), "symbol", actor.WithID(pair.Symbol), actor.WithContext(ctx))
		symbolUpper := strings.ToUpper(pair.Symbol)
		p.symbols[symbolUpper] = pid
	}

	p.natsConsumer = nats.NewNatsConsumer(p.quitch)
	if !p.consumeFromNats {
		return
	}

	p.consume()
}

func (p *Processor) stop() {
	if p.natsConsumer != nil {
		p.natsConsumer.Close()
	}
	if p.natsProducer != nil {
		p.natsProducer.Close()
	}
	close(p.quitch)
}

func (p *Processor) getJetstreamConfig() []jetstream.StreamConfig {
	cfg := make([]jetstream.StreamConfig, 0, len(producerStreams))
	for _, stream := range producerStreams {
		cfg = append(cfg, stream.Config())
	}
	return cfg
}

func (p *Processor) consume() {
	for _, reg := range consumerRegistry(p) {
		subject := nats.Subject{
			StreamType: reg.Stream,
			Exchange:   p.exchange,
		}

		_, err := p.natsConsumer.NewConsumer(nats.ConsumerParams{
			Subject: subject,
			Durable: reg.Stream.Durable(p.exchange),
			Handler: reg.Fn,
		})
		if err != nil {
			slog.Error("failed to create durable consumer", "stream", reg.Stream, "error", err)
			panic(err)
		}
	}
}

func (p *Processor) handleTrade(trade *event.Trade, _ *jetstream.MsgMetadata) error {
	p.ctx.Send(p.ctx.PID(), trade)
	return nil
}

func (p *Processor) handleOrderbook(book *event.BookUpdate, _ *jetstream.MsgMetadata) error {
	p.ctx.Send(p.ctx.PID(), book)
	return nil
}

func (p *Processor) handlePreStat(stat *event.Stat, _ *jetstream.MsgMetadata) error {
	p.ctx.Send(p.ctx.PID(), stat)
	return nil
}

func (p *Processor) handleLiquidation(liquidation *event.LiquidationUpdate, _ *jetstream.MsgMetadata) error {
	p.ctx.Send(p.ctx.PID(), liquidation)
	return nil
}
