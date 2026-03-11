package ws

import (
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/anthdm/hollywood/actor"
	"github.com/google/uuid"
)

/*
The manager handles the setup and renewal of websocket consumers.

Websocket consumers have limited lifetime, and will be respawned when they expire.
They use a fail-fast strategy, and will panic if they encounter an error. Which
triggers hollywood to restart them.

We provide 2 options for how to setup the subscriptions in the websocket:

1. Endpoint based subscriptions - where the endpoint itself contains the subscriptions
   For example,
   wss://fstream.binance.com/ws/btcusdt@aggTrade

   just by using this url, you will be subscribed to the stream.

   To use this, pass an EndpointBuilder function to the manager.

2. Message based subscriptions - where you send messages on the ws to subscribe to streams
   For example,
   wss://fstream.binance.com/ws/

   then the manager will send a message to the ws to subscribe to the stream.
   {"method": "SUBSCRIBE", "params": ["btcusdt@aggTrade"]}

   To use this, pass a SubscriptionBuilder function to the manager.

*/

type FillStrategy string

const (
	// will fill streams until MaxStreamsPerWebsocket / StreamsPerTicker is reached
	// for example, with the config:
	// tickers = 10
	// MaxStreamsPerWebsocket = 20
	// StreamsPerTicker = 2
	// MaxWebsockets = 5
	// there will only be 1 websocket active, and it will have all 20 streams
	FillStrategyFirst FillStrategy = "first"

	// will distribute streams evenly across active websocket capacity
	// for example, with the config:
	// tickers = 10
	// MaxStreamsPerWebsocket = 20
	// StreamsPerTicker = 2
	// MaxWebsockets = 5
	// there will be 4 websockets active, and each will have 5 streams
	FillStrategyEvenly FillStrategy = "evenly"

	// will automatically determine the best fill strategy based on the number of tickers
	// tickers > 50: FillStrategyEvenly
	// tickers <= 50: FillStrategyFirst
	FillStrategyAuto FillStrategy = "auto"
)

func (f FillStrategy) isValid() bool {
	return f == FillStrategyFirst || f == FillStrategyEvenly || f == FillStrategyAuto
}

type Heartbeat struct {
	Interval time.Duration
	Message  []byte
}

type ManagerConfig struct {
	// PID to send messages to
	SendTo *actor.PID

	// exchange name
	Exchange string

	// tickers to subscribe to
	Tickers []string

	// how many streams per symbol, eg aggTrade, depth
	StreamsPerTicker int64

	// max streams per websocket
	// each ticker can have multiple streams, eg aggTrade, depth
	MaxStreamsPerWebsocket int64

	// fill strategy
	FillStrategy FillStrategy

	// max websockets
	MaxWebsockets int64

	// max time a ws can live
	MaxWebsocketLifetime time.Duration

	// how long to overlap respawning ws's
	RespawnOverlap time.Duration

	// for exchanges that allow you to subscribe via url query params
	// this function should build the endpoint for the given tickers
	// func EndpointBuilder(tickers []string) string
	EndpointBuilder func([]string) string

	// for exchanges where you must subscribe via ws api
	// this function must return subscription messages
	// as json bytes to be sent on the ws to subscribe
	// func SubscriptionBuilder(tickers []string) [][]byte
	// where each []byte is a seperate message to be sent
	SubscriptionBuilder func([]string) [][]byte

	// for exchanges that require a heartbeat
	// this function must return the heartbeat interval and the message to send
	// if the heartbeat interval is 0, no heartbeat will be sent
	Heartbeat func() Heartbeat

	// max active websockets, leaving one for respawn overlap
	websocketCapacity int64

	// how many tickers per websocket maximum
	tickerCapacityPerWebsocket int64

	// buckets of tickers
	buckets [][]string
}

// validate the config and return error if required values are not set
// light on the validation, hopefully no retard gives a config with bad values
func (sc ManagerConfig) Validate() (ManagerConfig, error) {
	// 1. check for required values
	if sc.SendTo == nil {
		return ManagerConfig{}, fmt.Errorf("sendTo is required")
	}

	if sc.Exchange == "" {
		return ManagerConfig{}, fmt.Errorf("exchange is required")
	}

	if sc.StreamsPerTicker == 0 {
		return ManagerConfig{}, fmt.Errorf("streams per ticker is required")
	}

	if sc.EndpointBuilder == nil {
		return ManagerConfig{}, fmt.Errorf("endpoint builder is required")
	}

	if len(sc.Tickers) == 0 {
		return ManagerConfig{}, fmt.Errorf("tickers are required")
	}

	// 2. set default values if not set
	if sc.MaxStreamsPerWebsocket == 0 {
		sc.MaxStreamsPerWebsocket = 1000
	}

	if sc.MaxWebsockets == 0 {
		sc.MaxWebsockets = 5
	}

	if sc.MaxWebsocketLifetime == 0 {
		sc.MaxWebsocketLifetime = 1 * time.Hour
	}

	if sc.FillStrategy == "" || !sc.FillStrategy.isValid() || sc.FillStrategy == FillStrategyAuto {
		// if there are more than 50 tickers, use evenly strategy
		switch {
		case len(sc.Tickers) > 50:
			sc.FillStrategy = FillStrategyEvenly
		default:
			sc.FillStrategy = FillStrategyFirst
		}
	}

	if sc.SubscriptionBuilder == nil {
		sc.SubscriptionBuilder = func(tickers []string) [][]byte {
			return nil
		}
	}

	if sc.Heartbeat == nil {
		sc.Heartbeat = func() Heartbeat {
			return Heartbeat{
				Interval: 0,
				Message:  nil,
			}
		}
	}

	// 3. calculate required values based on the config
	sc.websocketCapacity = sc.MaxWebsockets - 1
	sc.tickerCapacityPerWebsocket = sc.MaxStreamsPerWebsocket / sc.StreamsPerTicker

	if sc.websocketCapacity < 0 {
		return ManagerConfig{}, fmt.Errorf("MaxWebsockets must be greater than 1")
	}

	if sc.tickerCapacityPerWebsocket < 0 {
		return ManagerConfig{}, fmt.Errorf("MaxStreamsPerWebsocket must be greater than StreamsPerTicker")
	}

	buckets, err := sc.bucketTickers()
	if err != nil {
		return ManagerConfig{}, err
	}
	sc.buckets = buckets

	return sc, nil
}

// bucket tickers
func (sc ManagerConfig) bucketTickers() ([][]string, error) {
	if len(sc.Tickers) == 0 {
		return nil, fmt.Errorf("tickers are required")
	}

	totalStreams := int64(len(sc.Tickers)) * sc.StreamsPerTicker
	maxTotalStreams := sc.MaxWebsockets * sc.MaxStreamsPerWebsocket

	if totalStreams > maxTotalStreams {
		return nil, fmt.Errorf("total streams (%d) exceed maximum capacity (%d)", totalStreams, maxTotalStreams)
	}

	var buckets [][]string
	tickersPerBucket := int(sc.MaxStreamsPerWebsocket / sc.StreamsPerTicker)

	switch sc.FillStrategy {
	case FillStrategyFirst:
		numBuckets := (len(sc.Tickers) + tickersPerBucket - 1) / tickersPerBucket
		if numBuckets > int(sc.MaxWebsockets) {
			numBuckets = int(sc.MaxWebsockets)
		}
		buckets = make([][]string, 0, numBuckets)

		remainingTickers := sc.Tickers
		for len(remainingTickers) > 0 && len(buckets) < int(sc.MaxWebsockets) {
			end := tickersPerBucket
			if end > len(remainingTickers) {
				end = len(remainingTickers)
			}
			buckets = append(buckets, remainingTickers[:end])
			remainingTickers = remainingTickers[end:]
		}

	case FillStrategyEvenly:
		numBuckets := int(sc.MaxWebsockets)
		buckets = make([][]string, numBuckets)

		for i, ticker := range sc.Tickers {
			bucketIdx := i % numBuckets
			buckets[bucketIdx] = append(buckets[bucketIdx], ticker)
		}

		// remove empty buckets
		nonEmptyBuckets := buckets[:0]
		for _, bucket := range buckets {
			if len(bucket) > 0 {
				nonEmptyBuckets = append(nonEmptyBuckets, bucket)
			}
		}
		buckets = nonEmptyBuckets

	default:
		return nil, fmt.Errorf("invalid fill strategy: %s", sc.FillStrategy)
	}

	return buckets, nil
}

// manager manages a pool of websocket streamer actors
type Manager struct {
	config   ManagerConfig
	streams  []*stream
	repeater *actor.SendRepeater
}

type loop struct{}

type stream struct {
	uid      string
	bid      int64
	pid      *actor.PID
	endpoint string
	started  time.Time
	tickers  []string
}

func NewManager(config ManagerConfig) actor.Producer {
	return func() actor.Receiver {
		return &Manager{
			config: config,
		}
	}
}

func (m *Manager) Receive(c *actor.Context) {
	switch msg := c.Message().(type) {
	case actor.Initialized:
	case actor.Started:
		m.init(c)
	case actor.Stopped:
	case *loop:
		m.updateStreams(c)

	default:
		slog.Error("manager.Receive(): unknown message", "msg", msg)
	}
}

// setup the manager
// 1. validate and default the config
// 2. spawn the websocket streamers
// 3. start the repeater to manage the websocket streamers
func (m *Manager) init(c *actor.Context) {
	config, err := m.config.Validate()
	if err != nil {
		panic("manager.Receive(): invalid manager config: " + err.Error())
	}

	m.config = config

	streams := make([]*stream, 0, len(m.config.buckets))

	// spawn the websocket streamers
	for i, bucket := range m.config.buckets {
		endpoint := m.config.EndpointBuilder(bucket)

		s := &stream{
			uid:      uuid.New().String(),
			bid:      int64(i),
			started:  time.Now(),
			endpoint: endpoint,
			tickers:  bucket,
		}

		cconf := ConsumerConfig{
			Endpoint:             endpoint,
			SubscriptionMessages: m.config.SubscriptionBuilder(bucket),
			BucketID:             int64(i),
			ConsumerID:           s.uid,
			SendTo:               m.config.SendTo,
		}

		cpid := c.SpawnChild(
			NewConsumer(cconf),
			fmt.Sprintf("%s-ws", m.config.Exchange),
			actor.WithID(fmt.Sprintf("%s-%d-%s", m.config.Exchange, s.bid, s.uid)),
			actor.WithMaxRestarts(math.MaxInt),
			actor.WithRestartDelay(1*time.Second))

		s.pid = cpid
		streams = append(streams, s)
	}

	m.streams = streams

	repeater := c.Engine().SendRepeat(c.PID(), &loop{}, 1*time.Second)
	m.repeater = &repeater
}

func (m *Manager) createNewStream(c *actor.Context, oldStream *stream, index int) *stream {
	newStream := &stream{
		uid:      uuid.New().String(),
		bid:      int64(index),
		started:  time.Now(),
		endpoint: oldStream.endpoint,
		tickers:  oldStream.tickers,
	}

	cconf := ConsumerConfig{
		Endpoint:             newStream.endpoint,
		SubscriptionMessages: m.config.SubscriptionBuilder(newStream.tickers),
		BucketID:             newStream.bid,
		ConsumerID:           newStream.uid,
		SendTo:               m.config.SendTo,
	}

	newStream.pid = c.SpawnChild(
		NewConsumer(cconf),
		fmt.Sprintf("%s-ws", m.config.Exchange),
		actor.WithID(fmt.Sprintf("%s-%d-%s", m.config.Exchange, newStream.bid, newStream.uid)),
		actor.WithMaxRestarts(math.MaxInt),
		actor.WithRestartDelay(1*time.Second))

	return newStream
}

func (m *Manager) updateStreams(c *actor.Context) {
	for i, oldStream := range m.streams {
		if time.Since(oldStream.started) < m.config.MaxWebsocketLifetime {
			continue
		}

		slog.Info("stream expired", "pid", oldStream.pid, "tickers", len(oldStream.tickers))

		if m.config.RespawnOverlap > 0 {
			// if overlap is set, create a new stream and overlap the respawn
			newStream := m.createNewStream(c, oldStream, i)
			m.streams[i] = newStream
			time.Sleep(m.config.RespawnOverlap)
			c.Engine().Poison(oldStream.pid)
		} else {
			// if no overlap, stop and start new stream
			<-c.Engine().Poison(oldStream.pid).Done()
			newStream := m.createNewStream(c, oldStream, i)
			m.streams[i] = newStream
		}
	}
}
