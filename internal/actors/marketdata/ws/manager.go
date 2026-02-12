package ws

import (
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/anthdm/hollywood/actor"
	"github.com/google/uuid"
)

// FillStrategy defines how tickers are distributed across websocket consumers.
type FillStrategy string

const (
	// FillStrategyFirst fills each websocket up to capacity before using the next.
	FillStrategyFirst FillStrategy = "first"

	// FillStrategyEvenly distributes tickers as evenly as possible across websockets.
	FillStrategyEvenly FillStrategy = "evenly"

	// FillStrategyAuto resolves to first/evenly based on ticker count.
	FillStrategyAuto FillStrategy = "auto"
)

func (f FillStrategy) isValid() bool {
	return f == FillStrategyFirst || f == FillStrategyEvenly || f == FillStrategyAuto
}

type Heartbeat struct {
	Interval time.Duration
	Message  []byte
}

// ManagerConfig contains only input values for manager planning/execution.
type ManagerConfig struct {
	SendTo *actor.PID

	Exchange string
	Tickers  []string

	StreamsPerTicker       int64
	MaxStreamsPerWebsocket int64
	FillStrategy           FillStrategy
	MaxWebsockets          int64
	MaxWebsocketLifetime   time.Duration
	RespawnOverlap         time.Duration
	EndpointBuilder        func([]string) string
	SubscriptionBuilder    func([]string) [][]byte
	Heartbeat              func() Heartbeat
	Reconnect              ReconnectPolicy
}

// ManagerPlan contains computed values derived from ManagerConfig.
type ManagerPlan struct {
	Config                     ManagerConfig
	ResolvedFillStrategy       FillStrategy
	WebsocketCapacity          int64
	TickerCapacityPerWebsocket int64
	Buckets                    [][]string
}

// Plan validates, defaults and computes a manager execution plan.
func Plan(cfg ManagerConfig) (ManagerPlan, error) {
	if cfg.SendTo == nil {
		return ManagerPlan{}, fmt.Errorf("sendTo is required")
	}
	if cfg.Exchange == "" {
		return ManagerPlan{}, fmt.Errorf("exchange is required")
	}
	if len(cfg.Tickers) == 0 {
		return ManagerPlan{}, fmt.Errorf("tickers are required")
	}
	if cfg.StreamsPerTicker < 1 {
		return ManagerPlan{}, fmt.Errorf("streams per ticker must be >= 1")
	}
	if cfg.EndpointBuilder == nil {
		return ManagerPlan{}, fmt.Errorf("endpoint builder is required")
	}

	if cfg.MaxStreamsPerWebsocket == 0 {
		cfg.MaxStreamsPerWebsocket = 1000
	}
	if cfg.MaxWebsockets == 0 {
		cfg.MaxWebsockets = 5
	}
	if cfg.MaxWebsocketLifetime == 0 {
		cfg.MaxWebsocketLifetime = time.Hour
	}
	if cfg.SubscriptionBuilder == nil {
		cfg.SubscriptionBuilder = func([]string) [][]byte { return nil }
	}
	if cfg.Heartbeat == nil {
		cfg.Heartbeat = func() Heartbeat { return Heartbeat{} }
	}

	resolved := cfg.FillStrategy
	if resolved == "" || !resolved.isValid() || resolved == FillStrategyAuto {
		if len(cfg.Tickers) > 50 {
			resolved = FillStrategyEvenly
		} else {
			resolved = FillStrategyFirst
		}
	}

	websocketCapacity := cfg.MaxWebsockets
	if cfg.RespawnOverlap > 0 {
		websocketCapacity = cfg.MaxWebsockets - 1
	}
	if websocketCapacity < 1 {
		return ManagerPlan{}, fmt.Errorf("MaxWebsockets must be >= 1 without overlap and >= 2 with overlap")
	}

	tickerCapacityPerWebsocket := cfg.MaxStreamsPerWebsocket / cfg.StreamsPerTicker
	if tickerCapacityPerWebsocket < 1 {
		return ManagerPlan{}, fmt.Errorf("MaxStreamsPerWebsocket must support at least one ticker: maxStreams=%d streamsPerTicker=%d", cfg.MaxStreamsPerWebsocket, cfg.StreamsPerTicker)
	}

	totalTickers := int64(len(cfg.Tickers))
	maxTickerCapacity := websocketCapacity * tickerCapacityPerWebsocket
	if totalTickers > maxTickerCapacity {
		totalStreams := totalTickers * cfg.StreamsPerTicker
		maxTotalStreams := maxTickerCapacity * cfg.StreamsPerTicker
		return ManagerPlan{}, fmt.Errorf("total streams (%d) exceed maximum capacity (%d)", totalStreams, maxTotalStreams)
	}

	buckets, err := bucketTickers(cfg.Tickers, resolved, websocketCapacity, tickerCapacityPerWebsocket)
	if err != nil {
		return ManagerPlan{}, err
	}

	return ManagerPlan{
		Config:                     cfg,
		ResolvedFillStrategy:       resolved,
		WebsocketCapacity:          websocketCapacity,
		TickerCapacityPerWebsocket: tickerCapacityPerWebsocket,
		Buckets:                    buckets,
	}, nil
}

func bucketTickers(tickers []string, strategy FillStrategy, maxWebsockets int64, tickerCapacity int64) ([][]string, error) {
	if len(tickers) == 0 {
		return nil, fmt.Errorf("tickers are required")
	}
	if maxWebsockets < 1 {
		return nil, fmt.Errorf("max websockets must be >= 1")
	}
	if tickerCapacity < 1 {
		return nil, fmt.Errorf("ticker capacity per websocket must be >= 1")
	}

	tickersPerBucket := int(tickerCapacity)

	switch strategy {
	case FillStrategyFirst:
		numBuckets := (len(tickers) + tickersPerBucket - 1) / tickersPerBucket
		if numBuckets > int(maxWebsockets) {
			numBuckets = int(maxWebsockets)
		}

		buckets := make([][]string, 0, numBuckets)
		remainingTickers := tickers
		for len(remainingTickers) > 0 && len(buckets) < int(maxWebsockets) {
			end := tickersPerBucket
			if end > len(remainingTickers) {
				end = len(remainingTickers)
			}
			buckets = append(buckets, remainingTickers[:end])
			remainingTickers = remainingTickers[end:]
		}

		if len(remainingTickers) > 0 {
			return nil, fmt.Errorf("not enough websocket capacity for first fill strategy")
		}
		return buckets, nil

	case FillStrategyEvenly:
		numBuckets := int(maxWebsockets)
		buckets := make([][]string, numBuckets)

		for i, ticker := range tickers {
			bucketIdx := i % numBuckets
			buckets[bucketIdx] = append(buckets[bucketIdx], ticker)
		}

		nonEmptyBuckets := buckets[:0]
		for _, bucket := range buckets {
			if len(bucket) == 0 {
				continue
			}
			if len(bucket) > tickersPerBucket {
				return nil, fmt.Errorf("bucket exceeds ticker capacity: %d > %d", len(bucket), tickersPerBucket)
			}
			nonEmptyBuckets = append(nonEmptyBuckets, bucket)
		}
		return nonEmptyBuckets, nil
	default:
		return nil, fmt.Errorf("invalid fill strategy: %s", strategy)
	}
}

// Manager manages a pool of websocket consumer actors.
type Manager struct {
	config ManagerConfig
	plan   ManagerPlan

	streams []*stream

	repeaterStopOnce sync.Once
	repeaterStopFn   func()

	scheduledPoison map[string]cancelSchedule
	stopped         bool

	nowFn               func() time.Time
	scheduleFn          func(delay time.Duration, fn func()) cancelSchedule
	sendToManagerFn     func(pid *actor.PID, msg any)
	poisonFn            func(c *actor.Context, pid *actor.PID)
	createReplacementFn func(c *actor.Context, oldStream *stream, index int) (*stream, error)
	managerPID          *actor.PID
}

type loop struct{}

type poisonExpiredStream struct {
	StreamID   string
	PID        *actor.PID
	BucketID   int64
	ConsumerID string
	Endpoint   string
}

type cancelSchedule func()

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
		return &Manager{config: config}
	}
}

func (m *Manager) Receive(c *actor.Context) {
	switch msg := c.Message().(type) {
	case actor.Initialized:
	case actor.Started:
		m.init(c)
	case actor.Stopped:
		m.handleStopped(c)
	case *loop:
		m.updateStreams(c)
	case poisonExpiredStream:
		m.handlePoisonExpired(c, msg)
	default:
		slog.Error("manager.Receive(): unknown message", "msg", msg)
	}
}

func (m *Manager) init(c *actor.Context) {
	plan, err := Plan(m.config)
	if err != nil {
		slog.Error("manager init failed", "err", err)
		m.emitError(c, "unknown", "", "", 0, err)
		c.Engine().Poison(c.PID())
		return
	}
	m.plan = plan
	m.config = plan.Config
	m.scheduledPoison = make(map[string]cancelSchedule)
	if c != nil && c.PID() != nil {
		cloned := *c.PID().CloneVT()
		m.managerPID = &cloned
	}

	if m.nowFn == nil {
		m.nowFn = time.Now
	}
	if m.poisonFn == nil {
		m.poisonFn = func(ac *actor.Context, pid *actor.PID) {
			if ac == nil || pid == nil {
				return
			}
			ac.Engine().Poison(pid)
		}
	}
	if m.scheduleFn == nil {
		m.scheduleFn = func(delay time.Duration, fn func()) cancelSchedule {
			timer := time.AfterFunc(delay, fn)
			return func() {
				timer.Stop()
			}
		}
	}
	if m.sendToManagerFn == nil {
		engine := c.Engine()
		m.sendToManagerFn = func(pid *actor.PID, msg any) {
			if engine == nil || pid == nil {
				return
			}
			engine.Send(pid, msg)
		}
	}
	if m.createReplacementFn == nil {
		m.createReplacementFn = m.createNewStream
	}

	streams := make([]*stream, 0, len(m.plan.Buckets))
	for i, bucket := range m.plan.Buckets {
		s, spawnErr := m.spawnInitialStream(c, i, bucket)
		if spawnErr != nil {
			slog.Error("failed to spawn ws consumer", "bucket", i, "err", spawnErr)
			m.emitError(c, "unknown", "", m.config.EndpointBuilder(bucket), int64(i), spawnErr)
			continue
		}
		streams = append(streams, s)
	}
	m.streams = streams

	repeater := c.Engine().SendRepeat(c.PID(), &loop{}, time.Second)
	m.repeaterStopFn = func() { repeater.Stop() }
}

func (m *Manager) spawnInitialStream(c *actor.Context, index int, bucket []string) (*stream, error) {
	endpoint := m.config.EndpointBuilder(bucket)
	s := &stream{
		uid:      uuid.New().String(),
		bid:      int64(index),
		started:  m.now(),
		endpoint: endpoint,
		tickers:  bucket,
	}

	cconf := ConsumerConfig{
		Exchange:             m.config.Exchange,
		Endpoint:             endpoint,
		SubscriptionMessages: m.config.SubscriptionBuilder(bucket),
		Heartbeat:            m.config.Heartbeat(),
		Reconnect:            m.config.Reconnect,
		BucketID:             int64(index),
		ConsumerID:           s.uid,
		SendTo:               m.config.SendTo,
	}

	if c == nil {
		return nil, fmt.Errorf("actor context is nil")
	}

	s.pid = c.SpawnChild(
		NewConsumer(cconf),
		fmt.Sprintf("%s-ws", m.config.Exchange),
		actor.WithID(fmt.Sprintf("%s-%d-%s", m.config.Exchange, s.bid, s.uid)),
		actor.WithMaxRestarts(math.MaxInt),
		actor.WithRestartDelay(time.Second),
	)

	return s, nil
}

func (m *Manager) createNewStream(c *actor.Context, oldStream *stream, index int) (*stream, error) {
	newStream := &stream{
		uid:      uuid.New().String(),
		bid:      int64(index),
		started:  m.now(),
		endpoint: oldStream.endpoint,
		tickers:  oldStream.tickers,
	}

	cconf := ConsumerConfig{
		Exchange:             m.config.Exchange,
		Endpoint:             newStream.endpoint,
		SubscriptionMessages: m.config.SubscriptionBuilder(newStream.tickers),
		Heartbeat:            m.config.Heartbeat(),
		Reconnect:            m.config.Reconnect,
		BucketID:             newStream.bid,
		ConsumerID:           newStream.uid,
		SendTo:               m.config.SendTo,
	}

	if c == nil {
		return nil, fmt.Errorf("actor context is nil")
	}

	newStream.pid = c.SpawnChild(
		NewConsumer(cconf),
		fmt.Sprintf("%s-ws", m.config.Exchange),
		actor.WithID(fmt.Sprintf("%s-%d-%s", m.config.Exchange, newStream.bid, newStream.uid)),
		actor.WithMaxRestarts(math.MaxInt),
		actor.WithRestartDelay(time.Second),
	)

	return newStream, nil
}

func (m *Manager) updateStreams(c *actor.Context) {
	now := m.now()
	for i, oldStream := range m.streams {
		if now.Sub(oldStream.started) < m.config.MaxWebsocketLifetime {
			continue
		}
		if _, overlapScheduled := m.scheduledPoison[oldStream.uid]; overlapScheduled {
			continue
		}

		slog.Info("stream expired", "pid", oldStream.pid, "tickers", len(oldStream.tickers))
		newStream, err := m.createReplacementFn(c, oldStream, i)
		if err != nil {
			slog.Error("failed to replace stream", "consumerID", oldStream.uid, "err", err)
			m.emitError(c, "unknown", oldStream.uid, oldStream.endpoint, oldStream.bid, err)
			continue
		}
		m.streams[i] = newStream
		m.emitState(c, newStream, "restarted", nil)

		if m.config.RespawnOverlap > 0 {
			old := oldStream
			targetPID := m.managerPID
			sendFn := m.sendToManagerFn
			cancel := m.scheduleFn(m.config.RespawnOverlap, func() {
				sendFn(targetPID, poisonExpiredStream{
					StreamID:   old.uid,
					PID:        old.pid,
					BucketID:   old.bid,
					ConsumerID: old.uid,
					Endpoint:   old.endpoint,
				})
			})
			m.scheduledPoison[old.uid] = cancel
			m.emitState(c, oldStream, "overlap-draining", nil)
			continue
		}

		m.poisonFn(c, oldStream.pid)
		m.emitState(c, oldStream, "stopped", nil)
	}
}

func (m *Manager) handlePoisonExpired(c *actor.Context, msg poisonExpiredStream) {
	cancel, ok := m.scheduledPoison[msg.StreamID]
	if !ok {
		return
	}
	delete(m.scheduledPoison, msg.StreamID)
	cancel()

	m.poisonFn(c, msg.PID)
	m.emitState(c, &stream{
		uid:      msg.ConsumerID,
		bid:      msg.BucketID,
		pid:      msg.PID,
		endpoint: msg.Endpoint,
	}, "stopped", nil)
}

func (m *Manager) handleStopped(c *actor.Context) {
	if m.stopped {
		return
	}
	m.stopped = true

	m.stopRepeater()
	for id, cancel := range m.scheduledPoison {
		cancel()
		delete(m.scheduledPoison, id)
	}

	for _, s := range m.streams {
		m.poisonFn(c, s.pid)
		m.emitState(c, s, "stopped", nil)
	}
	m.streams = nil
}

func (m *Manager) stopRepeater() {
	m.repeaterStopOnce.Do(func() {
		if m.repeaterStopFn != nil {
			m.repeaterStopFn()
		}
	})
}

func (m *Manager) now() time.Time {
	if m.nowFn != nil {
		return m.nowFn()
	}
	return time.Now()
}

func (m *Manager) emitState(c *actor.Context, s *stream, status string, err error) {
	if c == nil || m.config.SendTo == nil {
		return
	}
	state := &WsState{
		Exchange: m.config.Exchange,
		Status:   status,
		Err:      err,
		At:       m.now(),
	}
	if s != nil {
		state.BucketID = s.bid
		state.ConsumerID = s.uid
		state.Endpoint = s.endpoint
	}
	c.Send(m.config.SendTo, state)
}

func (m *Manager) emitError(c *actor.Context, kind, consumerID, endpoint string, bucketID int64, err error) {
	if c == nil || m.config.SendTo == nil {
		return
	}
	msg := &WsError{
		Exchange:   m.config.Exchange,
		ConsumerID: consumerID,
		Endpoint:   endpoint,
		Kind:       kind,
		Err:        err,
		BucketID:   bucketID,
		At:         m.now(),
	}
	c.Send(m.config.SendTo, msg)
}
